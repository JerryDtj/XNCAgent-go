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

	Release(ctx context.Context, userID int64, limit int) (int, error)
}

type billingService struct {
	db *gorm.DB
}

func NewService(db *gorm.DB) BillingService {
	return &billingService{db: db}
}

var ErrInsufficientBalance = errors.New("系统预测到您的账户余额可能不支持下次提问,请充值")
var ErrBalanceExhausted = errors.New("账户余额不足")

// Prehold 预扣积分
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

// SettleUser 结算一个用户本批未结算用量。一个用户一个事务,见 applyUserBatch。
func (s *billingService) SettleUser(ctx context.Context, userID int64, limit int) (int, error) {
	return applyUserBatch(s, userID, limit, loadSettleRows, rollSettle)
}

// costOf 实耗计价(分)。v1 硬编码费率卡(§2 样本: rate_p=100, rate_c=200,
// 单位分/百万 token),TODO: 随 Estimator 迁入 config.yaml 的 billing.rates
func costOf(model string, promptTokens, completionTokens int) int64 {
	const rateP, rateC = 100, 200 // 分/百万 token
	t := int64(promptTokens)*rateP + int64(completionTokens)*rateC
	return (t + 999_999) / 1_000_000 // ceil,全程整数
}

// Release 释放预扣。一个用户一个事务,见 applyUserBatch。
// 释放那些已经预扣、但在规定时间内没有 usage 上报的记录,避免冻结锁死。
func (s *billingService) Release(ctx context.Context, userID int64, limit int) (int, error) {
	return applyUserBatch(s, userID, limit, loadReleaseRows, rollRelease)
}
