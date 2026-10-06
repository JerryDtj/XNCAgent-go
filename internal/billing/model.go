package billing

import (
	"encoding/json"
	"time"
	"uuid"
)

type PreholdModel struct {
	ID        uuid.UUID `gorm:"primaryKey"`
	RequestID string    `gorm:"index" json:"request_id"`
	UserID    int64     `gorm:"index" json:"user_id"`
	Amount    int64     `json:"amount"`
	Status    string    `gorm:"index" json:"status"`
	ExpiresAt time.Time `gorm:"index" json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
	SettledAt time.Time `json:"settled_at"`
}

func (PreholdModel) TableName() string { return "preholds" }

type Transaction struct {
	ID             uint64 `gorm:"primaryKey"`
	EventID        string `gorm:"size:64;uniqueIndex"`
	SeqNo          int64
	UserID         int64   `gorm:"uniqueIndex:uq_idem"`
	PreholdID      *string `gorm:"size:64"`
	PromotionID    *int64
	IdempotencyKey string `gorm:"size:64;uniqueIndex:uq_idem"`
	FlowVersion    int    `gorm:"default:1"`
	Type           string `gorm:"size:20;uniqueIndex:uq_idem"`
	Status         string `gorm:"size:20;default:COMPLETED"`
	Amount         int64
	BalanceBefore  int64
	BalanceAfter   int64
	FrozenBefore   int64
	FrozenAfter    int64
	Description    *string   `gorm:"size:255"`
	CreatedAt      time.Time `gorm:"autoCreateTime"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime"`
}

func (Transaction) TableName() string {
	return "transactions"
}

type Usage struct {
	ID               uint64    `gorm:"primaryKey"`
	PreholdID        uuid.UUID `gorm:"type:uuid;uniqueIndex:uq_usage_prehold"`
	UserID           int64
	MessageID        int64
	Model            string `gorm:"type:text"`
	PromptTokens     int32
	CompletionTokens int32
	CacheMeta        json.RawMessage `gorm:"type:jsonb;default:'{}'"`
	Estimated        bool
	Settled          bool
	CreatedAt        time.Time `gorm:"default:now()"`
}

func (Usage) TableName() string {
	return "usage"
}
