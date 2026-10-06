package billing

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/JerryDtj/XNCAgent-go/internal/user"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type BillingService interface {
	Prehold(ctx context.Context, userID int64, amount int64, requestId string) (uuid.UUID, bool, error)
	// SettleUser 按用户批量结算:驱动表是 usage(有真实 token 才结算)。
	// 每批 limit 条一个事务:一次捞出、内存逐笔算完、流水/预扣/用量/账户各写一次。
	// 返回本批条数(0 = 无待结算)。
	SettleUser(ctx context.Context, userID int64, limit int) (int, error)
}

type billingService struct {
	db *gorm.DB
}

func NewService(db *gorm.DB) BillingService {
	return &billingService{db: db}
}

var ErrInsufficientBalance = errors.New("系统预测到您的账户余额可能不支持下次提问,请充值")
var ErrBalanceExhausted = errors.New("账户余额不足")

func (s *billingService) Prehold(ctx context.Context, userID int64, amount int64, requestId string) (uuid.UUID, bool, error) {
	preholdID := uuid.New()
	reused := false
	// 查询是否存在预扣,如果不存在那么直接返回nil
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var existingPrehold PreholdModel
		e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ? AND request_id = ? AND status = 'pending' AND expires_at > NOW()", userID, requestId).
			First(&existingPrehold).Error
		if e == nil {
			//查到数据,说明已经存在预扣,直接返回
			reused, preholdID = true, existingPrehold.ID
			return nil
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		//先锁account账户
		var acct user.Account
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ?", userID).First(&acct).Error; e != nil {
			return e
		}
		if acct.Available <= 0 {
			return ErrBalanceExhausted
		}
		if acct.Available <= amount {
			return ErrInsufficientBalance
		}
		//插入预扣
		if e := tx.Create(&PreholdModel{
			ID:        preholdID,
			UserID:    userID,
			RequestID: requestId,
			Amount:    amount,
			Status:    "pending",
			ExpiresAt: time.Now().Add(time.Minute * 30),
		}).Error; e != nil {
			if errors.Is(e, gorm.ErrDuplicatedKey) {
				//并发双击:另一个请求已插入同 request_id 的 pending 预扣,
				//索引保证了只有一条,这里转为复用而不是报错
				var pending PreholdModel
				if e2 := tx.Where("user_id = ? AND request_id = ? AND status = 'pending' AND expires_at > NOW()", userID, requestId).
					First(&pending).Error; e2 != nil {
					return e2
				}
				reused, preholdID = true, pending.ID
				return nil
			}
			return e
		}
		//冻结+流水。快照必须在 Updates 之前取到局部变量:
		//GORM 的 map 式 Updates 会把新值回写进 acct 结构体,
		//之后再读 acct.Frozen 拿到的是"之后"的值,快照就记错了
		totalBefore := acct.Total
		frozenBefore := acct.Frozen
		if e := tx.Model(&acct).Updates(map[string]interface{}{
			"available": acct.Available - amount,
			"frozen":    acct.Frozen + amount,
		}).Error; e != nil {
			return e
		}
		//流水序号:用户内单调递增。这里能安全地"查 MAX +1",是因为
		//accounts 行锁已经把同一用户的预扣串行化了,读到的 MAX 是稳定的
		var maxSeq int64
		if e := tx.Model(&Transaction{}).Where("user_id = ?", userID).
			Select("COALESCE(MAX(seq_no), 0)").Scan(&maxSeq).Error; e != nil {
			return e
		}
		s := preholdID.String()
		description := "请求预扣"
		if e := tx.Create(&Transaction{
			EventID:        "ph:" + s, // 全局锚,PREHOLD 用预扣 id 派生,天然唯一
			SeqNo:          maxSeq + 1,
			UserID:         userID,
			PreholdID:      &s,
			Amount:         amount,
			BalanceBefore:  totalBefore, // 列注释定义:balance_* 是总余额快照;预扣不动 total
			BalanceAfter:   totalBefore,
			FrozenBefore:   frozenBefore,
			FrozenAfter:    frozenBefore + amount,
			IdempotencyKey: requestId,
			FlowVersion:    2,
			Type:           "PREHOLD",
			Status:         "COMPLETED",
			Description:    &description,
		}).Error; e != nil {
			return e
		}
		return nil
	})

	return preholdID, reused, err
}

// SettleUser 结算一个用户本批未结算用量。
// 语句次数不随条数涨:锁账户、捞本批、取序号,然后流水/预扣/用量/账户各写一次。
func (s *billingService) SettleUser(ctx context.Context, userID int64, limit int) (int, error) {
	if limit <= 0 {
		limit = 10
	}
	processed := 0
	err := s.db.Transaction(func(tx *gorm.DB) error {
		// ① 锁账户。同一用户的预扣和结算都抢这把锁,快照之后不再读 acct
		var acct user.Account
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ?", userID).First(&acct).Error; e != nil {
			return e
		}
		runTotal, runAvailable, runFrozen := acct.Total, acct.Available, acct.Frozen

		// ② 一次捞出本批。驱动表是 usage:没有 usage 的 pending 预扣是崩溃残留,归 Sweeper
		type settleRow struct {
			ID               int64     `gorm:"column:id"`
			PreholdID        uuid.UUID `gorm:"column:prehold_id"`
			Model            string    `gorm:"column:model"`
			PromptTokens     int       `gorm:"column:prompt_tokens"`
			CompletionTokens int       `gorm:"column:completion_tokens"`
			Amount           int64     `gorm:"column:amount"`
			Status           string    `gorm:"column:status"`
		}
		var rows []settleRow
		if e := tx.Table("usage AS u").
			Select("u.id, u.prehold_id, u.model, u.prompt_tokens, u.completion_tokens, p.amount, p.status").
			Joins("JOIN preholds AS p ON p.id = u.prehold_id").
			Where("u.user_id = ? AND u.settled = false", userID).
			Order("u.id ASC").Limit(limit).
			Scan(&rows).Error; e != nil {
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

		// ③ 内存里逐笔滚动。每笔自己的 cost,上一笔 after 是下一笔 before
		settleDesc := "按实耗结算"
		fixDesc := "预扣已释放,成本补扣"
		txRows := make([]Transaction, 0, len(rows))
		pendingIDs := make([]uuid.UUID, 0, len(rows))
		usageIDs := make([]int64, len(rows))
		for i, row := range rows {
			usageIDs[i] = row.ID
			if row.Status == "settled" {
				continue // 钱已结过,只补 usage 置标
			}
			cost := costOf(row.Model, row.PromptTokens, row.CompletionTokens)
			txType := "SETTLE"
			held := min(runFrozen, row.Amount) // 只解冻这一笔预扣,不动别人的冻结
			desc := &settleDesc
			if row.Status == "released" {
				held = 0 // Sweeper 已把冻结退回 available,成本全额从 available 扣
				txType = "RECONCILE_FIX"
				desc = &fixDesc
			}
			totalBefore, frozenBefore := runTotal, runFrozen
			runTotal -= cost
			runFrozen -= held
			runAvailable -= cost - held // 多退少补,available 可为负

			maxSeq++
			ps := row.PreholdID.String()
			txRows = append(txRows, Transaction{
				EventID:        "st:" + ps,
				SeqNo:          maxSeq,
				UserID:         userID,
				PreholdID:      &ps,
				IdempotencyKey: "sys:" + ps,
				FlowVersion:    2,
				Type:           txType,
				Status:         "COMPLETED",
				Amount:         -cost, // 正增负减
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

		// ④ 不过就回滚,钱不允许错着落库。total = available + frozen,且 total 不为负
		if runTotal != runAvailable+runFrozen || runTotal < 0 {
			return errors.New("结算后账户不变式破坏")
		}

		// ⑤ 算完再写。1000 行也是一条多值 INSERT,不是 1000 次往返
		if len(txRows) > 0 {
			if e := tx.CreateInBatches(&txRows, 1000).Error; e != nil {
				return e
			}
		}
		if len(pendingIDs) > 0 {
			// CAS:影响行数不够 = 有人改过状态,整批回滚,下轮重试。released 不在这批里
			res := tx.Model(&PreholdModel{}).
				Where("id IN ? AND status = ?", pendingIDs, "pending").
				Updates(map[string]interface{}{"status": "settled", "settled_at": time.Now()})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected != int64(len(pendingIDs)) {
				return errors.New("预扣状态在结算中途被修改")
			}
		}
		res := tx.Table("usage").Where("id IN ?", usageIDs).Update("settled", true)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != int64(len(usageIDs)) {
			return errors.New("usage 置标行数不符")
		}
		if len(txRows) > 0 {
			if e := tx.Model(&user.Account{}).Where("user_id = ?", userID).
				Updates(map[string]interface{}{
					"total":     runTotal,
					"available": runAvailable,
					"frozen":    runFrozen,
				}).Error; e != nil {
				return e
			}
		}
		processed = len(rows)
		return nil
	})
	if err != nil {
		return 0, err
	}
	return processed, nil
}

// costOf 实耗计价(分)。v1 硬编码费率卡(§2 样本: rate_p=100, rate_c=200,
// 单位分/百万 token),TODO: 随 Estimator 迁入 config.yaml 的 billing.rates
func costOf(model string, promptTokens, completionTokens int) int64 {
	const rateP, rateC = 100, 200 // 分/百万 token
	t := int64(promptTokens)*rateP + int64(completionTokens)*rateC
	return (t + 999_999) / 1_000_000 // ceil,全程整数
}
