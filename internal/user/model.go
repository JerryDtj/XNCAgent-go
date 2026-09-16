package user

import "time"

type User struct {
	ID           int64     `gorm:"primaryKey"`
	Email        string    `gorm:"size:100;uniqueIndex"`
	PasswordHash string    `gorm:"column:password_hash"`
	Phone        *string   `gorm:"size:20"`
	Status       string    `gorm:"size:20"`
	CreatedAt    time.Time `gorm:"autoCreateTime"`
	UpdatedAt    time.Time `gorm:"autoUpdateTime"`
}

func (User) TableName() string { return "users" }

type Account struct {
	ID        int64     `gorm:"primaryKey"`
	UserID    int64     `gorm:"column:user_id;uniqueIndex"`
	Total     int64     `gorm:"default:0"`
	Available int64     `gorm:"default:0"`
	Frozen    int64     `gorm:"default:0"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

func (Account) TableName() string { return "accounts" }

type Intimacy struct {
	ID           int64      `gorm:"primaryKey"`
	UserID       int64      `gorm:"column:user_id;uniqueIndex"`
	CurrentLevel int        `gorm:"column:current_level;default:1"`
	CurrentScore int        `gorm:"column:current_score;default:0"`
	ChatCount    int        `gorm:"column:chat_count;default:0"`
	LastChatAt   *time.Time `gorm:"column:last_chat_at"`
	Version      int        `gorm:"default:0"`
	CreatedAt    time.Time  `gorm:"autoCreateTime"`
	UpdatedAt    time.Time  `gorm:"autoUpdateTime"`
}

func (Intimacy) TableName() string { return "user_intimacy" }

type RefreshToken struct {
	ID        int64     `gorm:"primaryKey"`
	UserID    int64     `gorm:"column:user_id"`
	Token     string    `gorm:"size:255;uniqueIndex"`
	ExpiresAt time.Time `gorm:"column:expires_at"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
}

func (RefreshToken) TableName() string { return "refresh_tokens" }
