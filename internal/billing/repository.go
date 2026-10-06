package billing

import (
	"context"

	"gorm.io/gorm"
)

type billingRepository struct {
	db *gorm.DB
}

func newBillingRepository(db *gorm.DB) *billingRepository {
	return &billingRepository{
		db: db,
	}
}

func (r *billingRepository) CreatePrehold(ctx context.Context, userID int64, amount int64, requestId string) (*PreholdModel, error) {
	prehold := &PreholdModel{
		UserID:    userID,
		Amount:    amount,
		RequestID: requestId,
	}
	return prehold, nil
}
