package billing

import (
	"errors"
	"time"
	"uuid"

	"github.com/JerryDtj/XNCAgent-go/internal/user"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// batchPlan 是内存滚动的结果,flushBatch 按它各写一次库。
// usageIDs 只在 markUsage 时使用;预扣 id 为空则不改预扣状态。
type batchPlan struct {
	txs            []Transaction
	preholdIDs     []uuid.UUID
	preholdUpdates map[string]interface{}
	casMismatch    string
	markUsage      bool
	usageIDs       []int64
	total          int64
	available      int64
	frozen         int64
}

// applyUserBatch 一个用户一个事务。SettleUser 和 Release 共用:
// 锁账户 → 捞出本批 → 内存滚动计算 → 流水/状态/账户各批量写一次。
// 本批为空直接结束。滚动或写入失败则整批回滚,对外返回 0。
func applyUserBatch[T any](
	s *billingService,
	userID int64,
	limit int,
	load func(tx *gorm.DB, userID int64, limit int) ([]T, error),
	roll func(acct user.Account, maxSeq int64, rows []T) (batchPlan, error),
) (int, error) {
	if limit <= 0 {
		limit = 10
	}
	processed := 0
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var acct user.Account
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ?", userID).First(&acct).Error; e != nil {
			return e
		}
		rows, e := load(tx, userID, limit)
		if e != nil {
			return e
		}
		if len(rows) == 0 {
			return nil
		}
		var maxSeq int64
		if e := tx.Model(&Transaction{}).Where("user_id = ?", userID).
			Select("COALESCE(MAX(seq_no), 0)").Scan(&maxSeq).Error; e != nil {
			return e
		}
		plan, e := roll(acct, maxSeq, rows)
		if e != nil {
			return e
		}
		if e := flushBatch(tx, userID, plan); e != nil {
			return e
		}
		processed = len(rows)
		return nil
	})
	if err != nil {
		return 0, err
	}
	return processed, nil
}

// flushBatch 把滚动结果落库:流水一次、预扣状态一次、用量置标一次、账户一次。
func flushBatch(tx *gorm.DB, userID int64, plan batchPlan) error {
	if len(plan.txs) > 0 {
		if e := tx.CreateInBatches(&plan.txs, 1000).Error; e != nil {
			return e
		}
	}
	if len(plan.preholdIDs) > 0 {
		res := tx.Model(&PreholdModel{}).
			Where("id IN ? AND status = ?", plan.preholdIDs, "pending").
			Updates(plan.preholdUpdates)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != int64(len(plan.preholdIDs)) {
			return errors.New(plan.casMismatch)
		}
	}
	if plan.markUsage {
		res := tx.Table("usage").Where("id IN ?", plan.usageIDs).Update("settled", true)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != int64(len(plan.usageIDs)) {
			return errors.New("usage 置标行数不符")
		}
	}
	if len(plan.txs) > 0 {
		if e := tx.Model(&user.Account{}).Where("user_id = ?", userID).
			Updates(map[string]interface{}{
				"total":     plan.total,
				"available": plan.available,
				"frozen":    plan.frozen,
			}).Error; e != nil {
			return e
		}
	}
	return nil
}

type settleRow struct {
	ID               int64     `gorm:"column:id"`
	PreholdID        uuid.UUID `gorm:"column:prehold_id"`
	Model            string    `gorm:"column:model"`
	PromptTokens     int       `gorm:"column:prompt_tokens"`
	CompletionTokens int       `gorm:"column:completion_tokens"`
	Amount           int64     `gorm:"column:amount"`
	Status           string    `gorm:"column:status"`
}

func loadSettleRows(tx *gorm.DB, userID int64, limit int) ([]settleRow, error) {
	var rows []settleRow
	err := tx.Table("usage AS u").
		Select("u.id, u.prehold_id, u.model, u.prompt_tokens, u.completion_tokens, p.amount, p.status").
		Joins("JOIN preholds AS p ON p.id = u.prehold_id").
		Where("u.user_id = ? AND u.settled = false", userID).
		Order("u.id ASC").Limit(limit).
		Scan(&rows).Error
	return rows, err
}

func rollSettle(acct user.Account, maxSeq int64, rows []settleRow) (batchPlan, error) {
	runTotal, runAvailable, runFrozen := acct.Total, acct.Available, acct.Frozen
	settleDesc := "按实耗结算"
	fixDesc := "预扣已释放,成本补扣"
	txs := make([]Transaction, 0, len(rows))
	pendingIDs := make([]uuid.UUID, 0, len(rows))
	usageIDs := make([]int64, len(rows))
	for i, row := range rows {
		usageIDs[i] = row.ID
		if row.Status == "settled" {
			continue
		}
		cost := costOf(row.Model, row.PromptTokens, row.CompletionTokens)
		txType := "SETTLE"
		held := min(runFrozen, row.Amount)
		desc := &settleDesc
		if row.Status == "released" {
			held = 0
			txType = "RECONCILE_FIX"
			desc = &fixDesc
		}
		totalBefore, frozenBefore := runTotal, runFrozen
		runTotal -= cost
		runFrozen -= held
		runAvailable -= cost - held

		maxSeq++
		ps := row.PreholdID.String()
		txs = append(txs, Transaction{
			EventID:        "st:" + ps,
			SeqNo:          maxSeq,
			UserID:         acct.UserID,
			PreholdID:      &ps,
			IdempotencyKey: "sys:" + ps,
			FlowVersion:    2,
			Type:           txType,
			Status:         "COMPLETED",
			Amount:         -cost,
			BalanceBefore:  totalBefore,
			BalanceAfter:   runTotal,
			FrozenBefore:   frozenBefore,
			FrozenAfter:    runFrozen,
			Description:    desc,
		})
		if txType == "SETTLE" {
			pendingIDs = append(pendingIDs, row.PreholdID)
		}
	}
	if runTotal != runAvailable+runFrozen || runTotal < 0 {
		return batchPlan{}, errors.New("结算后账户不变式破坏")
	}
	return batchPlan{
		txs:        txs,
		preholdIDs: pendingIDs,
		preholdUpdates: map[string]interface{}{
			"status":     "settled",
			"settled_at": time.Now(),
		},
		casMismatch: "预扣状态在结算中途被修改",
		markUsage:   true,
		usageIDs:    usageIDs,
		total:       runTotal,
		available:   runAvailable,
		frozen:      runFrozen,
	}, nil
}

type releaseRow struct {
	PreholdID uuid.UUID `gorm:"column:prehold_id"`
	Amount    int64     `gorm:"column:amount"`
}

func loadReleaseRows(tx *gorm.DB, userID int64, limit int) ([]releaseRow, error) {
	var rows []releaseRow
	subQuery := tx.Model(&Usage{}).
		Select("1").
		Where("usage.prehold_id = p.id")
	err := tx.Table("preholds AS p").
		Select("p.id as prehold_id, p.user_id as id, p.amount, p.status").
		Where("p.status = 'pending' AND p.expires_at < now() and p.user_id = ?", userID).
		Where("NOT EXISTS (?)", subQuery).
		Order("p.created_at ASC").Limit(limit).
		Scan(&rows).Error
	return rows, err
}

func rollRelease(acct user.Account, maxSeq int64, rows []releaseRow) (batchPlan, error) {
	runAvailable, runFrozen := acct.Available, acct.Frozen
	releaseDesc := "释放没有得到回应的预扣"
	txs := make([]Transaction, 0, len(rows))
	pendingIDs := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		frozenBefore := runFrozen
		runFrozen -= row.Amount
		runAvailable += row.Amount
		maxSeq++
		ps := row.PreholdID.String()
		txs = append(txs, Transaction{
			EventID:        "rl:" + ps,
			SeqNo:          maxSeq,
			UserID:         acct.UserID,
			PreholdID:      &ps,
			IdempotencyKey: "sys:" + ps,
			FlowVersion:    2,
			Type:           "CANCEL",
			Status:         "COMPLETED",
			Amount:         row.Amount,
			BalanceBefore:  acct.Total,
			BalanceAfter:   acct.Total,
			FrozenBefore:   frozenBefore,
			FrozenAfter:    runFrozen,
			Description:    &releaseDesc,
		})
		pendingIDs = append(pendingIDs, row.PreholdID)
	}
	if acct.Total != runAvailable+runFrozen || runFrozen < 0 {
		return batchPlan{}, errors.New("释放兜底失败,账户不变式破坏")
	}
	return batchPlan{
		txs:            txs,
		preholdIDs:     pendingIDs,
		preholdUpdates: map[string]interface{}{"status": "released"},
		casMismatch:    "预扣状态在释放中途被修改",
		total:          acct.Total,
		available:      runAvailable,
		frozen:         runFrozen,
	}, nil
}
