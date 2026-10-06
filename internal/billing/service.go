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
	// SettleUser 按用户批量结算:驱动表是 usage(有真实 token 才结算);
	// 每批 limit 条一个事务,锁一次账户,批内逐笔搬钱、逐笔写新流水,
	// 批末一次写回账户并自检不变式。返回本批条数(0 = 无待结算)。
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

// SettleUser 结算一个用户本批未结算用量(见接口注释)。
// 锁顺序:usage 无锁读 → accounts 行锁 → preholds CAS,全局一致。
func (s *billingService) SettleUser(ctx context.Context, userID int64, limit int) (int, error) {
	if limit <= 0 {
		limit = 10
	}
	processed := 0
	err := s.db.Transaction(func(tx *gorm.DB) error {
		// ① 锁账户,快照取到 running 变量;此后 acct 不再作为数值来源
		var acct user.Account
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ?", userID).First(&acct).Error; e != nil {
			return e
		}
		runTotal, runAvailable, runFrozen := acct.Total, acct.Available, acct.Frozen

		// ② 捞本批未结算用量。没有 usage 行的 pending prehold 是崩溃残留,
		//    归 Sweeper 管,这里不碰
		var rows []struct {
			ID               int64
			PreholdID        uuid.UUID
			Model            string
			PromptTokens     int `gorm:"column:prompt_tokens"`
			CompletionTokens int `gorm:"column:completion_tokens"`
		}
		if e := tx.Table("usage").
			Select("id, prehold_id, model, prompt_tokens, completion_tokens").
			Where("user_id = ? AND settled = false", userID).
			Order("id ASC").Limit(limit).
			Scan(&rows).Error; e != nil {
			return e
		}
		if len(rows) == 0 {
			return nil
		}

		// ③ 流水起点序号:账户锁已把同用户操作串行化,MAX 稳定
		var maxSeq int64
		if e := tx.Model(&Transaction{}).Where("user_id = ?", userID).
			Select("COALESCE(MAX(seq_no), 0)").Scan(&maxSeq).Error; e != nil {
			return e
		}

		for i, row := range rows {
			var ph PreholdModel
			if e := tx.Where("id = ?", row.PreholdID).First(&ph).Error; e != nil {
				return e
			}
			if ph.Status == "settled" {
				// 资金已结过(历史残留),只补置标,不动钱
				if e := tx.Table("usage").Where("id = ?", row.ID).
					Update("settled", true).Error; e != nil {
					return e
				}
				processed++
				continue
			}
			cost := costOf(row.Model, row.PromptTokens, row.CompletionTokens)
			txType := "SETTLE"
			held := min(runFrozen, ph.Amount) // 从冻结里只拿本笔的钱
			desc := "按实耗结算"
			if ph.Status == "released" {
				// Sweeper 已退冻结:成本全额从 available 直扣
				held = 0
				txType = "RECONCILE_FIX"
				desc = "预扣已释放,成本补扣"
			}
			// 快照:上一笔 after 就是这一笔 before
			totalBefore, frozenBefore := runTotal, runFrozen
			runTotal -= cost
			runFrozen -= held
			runAvailable -= (cost - held) // 多退少补,可为负(透支)

			ps := ph.ID.String()
			if e := tx.Create(&Transaction{
				EventID:        "st:" + ps, // 由预扣 id 派生,天然唯一
				SeqNo:          maxSeq + int64(i) + 1,
				UserID:         userID,
				PreholdID:      &ps,
				IdempotencyKey: "sys:" + ps,
				FlowVersion:    2,
				Type:           txType,
				Status:         "COMPLETED",
				Amount:         -cost, // 流水约定正增负减
				BalanceBefore:  totalBefore,
				BalanceAfter:   runTotal,
				FrozenBefore:   frozenBefore,
				FrozenAfter:    runFrozen,
				Description:    &desc,
			}).Error; e != nil {
				return e
			}
			// CAS 才是权威:只有仍 pending 才允许落成 settled;
			// 0 行 = 读完状态后被改过(理论上账户锁已排除,防御),回滚整批下轮重试。
			// released 分支不在这里:Sweeper 释放是既成事实,保持 released 不变
			if txType == "SETTLE" {
				res := tx.Model(&PreholdModel{}).
					Where("id = ? AND status = 'pending'", row.PreholdID).
					Updates(map[string]interface{}{"status": "settled", "settled_at": time.Now()})
				if res.Error != nil {
					return res.Error
				}
				if res.RowsAffected == 0 {
					return errors.New("预扣状态在结算中途被修改")
				}
			}
			if e := tx.Table("usage").Where("id = ?", row.ID).
				Update("settled", true).Error; e != nil {
				return e
			}
			processed++
		}

		// ④ 不变式自检:不过就回滚,钱不允许错着落库
		if runTotal != runAvailable+runFrozen || runTotal < 0 {
			return errors.New("结算后账户不变式破坏")
		}
		// ⑤ 批末一次写回账户
		if e := tx.Model(&user.Account{}).Where("user_id = ?", userID).
			Updates(map[string]interface{}{
				"total":     runTotal,
				"available": runAvailable,
				"frozen":    runFrozen,
			}).Error; e != nil {
			return e
		}
		return nil
	})
	return processed, err
}

// costOf 实耗计价(分)。v1 硬编码费率卡(§2 样本: rate_p=100, rate_c=200,
// 单位分/百万 token),TODO: 随 Estimator 迁入 config.yaml 的 billing.rates
func costOf(model string, promptTokens, completionTokens int) int64 {
	const rateP, rateC = 100, 200 // 分/百万 token
	t := int64(promptTokens)*rateP + int64(completionTokens)*rateC
	return (t + 999_999) / 1_000_000 // ceil,全程整数
}
