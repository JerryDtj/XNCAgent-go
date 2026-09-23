package user

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/JerryDtj/XNCAgent-go/internal/mail"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	accessTTL  = 2 * time.Hour
	refreshTTL = 7 * 24 * time.Hour
	codeTTL    = time.Hour
)

var (
	ErrEmailTaken   = errors.New("该邮箱已注册")
	ErrInvalidCreds = errors.New("邮箱或密码错误")
	ErrUserBanned   = errors.New("账号已被封禁")
	ErrUserNotFound = errors.New("用户不存在")
	ErrInvalidCode  = errors.New("验证码错误")
	ErrCodeExpired  = errors.New("验证码已过期，请重新获取")
)

type Service struct {
	db        *gorm.DB
	rdb       *redis.Client
	mailer    *mail.Sender
	jwtSecret []byte
}

func NewService(db *gorm.DB, rdb *redis.Client, mailer *mail.Sender, jwtSecret string) *Service {
	return &Service{db: db, rdb: rdb, mailer: mailer, jwtSecret: []byte(jwtSecret)}
}

type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

type Claims struct {
	UserID int64 `json:"user_id"`
	jwt.RegisteredClaims
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func (s *Service) emailTaken(email string) (bool, error) {
	var n int64
	if err := s.db.Model(&User{}).Where("email = ?", email).Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *Service) SendRegisterCode(email string) error {
	email = normalizeEmail(email)
	taken, err := s.emailTaken(email)
	if err != nil {
		return err
	}
	if taken {
		return ErrEmailTaken
	}

	n, err := rand.Int(rand.Reader, big.NewInt(10000))
	if err != nil {
		return err
	}
	code := fmt.Sprintf("%04d", n.Int64())

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.rdb.Set(ctx, email, code, codeTTL).Err(); err != nil {
		return err
	}

	subject := "XNCAgent 注册验证码"
	body := fmt.Sprintf("您的注册验证码是 %s，1 小时内有效。如果不是您本人操作，请忽略本邮件。", code)
	if err := s.mailer.SendPlain(email, subject, body); err != nil {
		_ = s.rdb.Del(context.Background(), email).Err()
		return err
	}
	return nil
}

func (s *Service) consumeCode(email, code string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	stored, err := s.rdb.Get(ctx, email).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return ErrCodeExpired
		}
		return err
	}
	if subtle.ConstantTimeCompare([]byte(stored), []byte(code)) != 1 {
		return ErrInvalidCode
	}
	return nil
}

func (s *Service) Register(email, password, code string) (*User, *TokenPair, error) {
	email = normalizeEmail(email)
	if err := s.consumeCode(email, strings.TrimSpace(code)); err != nil {
		return nil, nil, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, nil, err
	}
	u := &User{
		Email:        email,
		PasswordHash: string(hash),
		Status:       "active",
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(u).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return ErrEmailTaken
			}
			return err
		}
		if err := tx.Create(&Account{UserID: u.ID}).Error; err != nil {
			return err
		}
		if err := tx.Create(&Intimacy{UserID: u.ID, CurrentLevel: 1}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	_ = s.rdb.Del(context.Background(), email).Err()
	tokens, err := s.issueTokens(u.ID)
	if err != nil {
		return nil, nil, err
	}
	return u, tokens, nil
}

func (s *Service) Login(email, password string) (*TokenPair, error) {
	email = normalizeEmail(email)
	var u User
	if err := s.db.Where("email = ?", email).First(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvalidCreds
		}
		return nil, err
	}
	if u.Status == "banned" {
		return nil, ErrUserBanned
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCreds
	}
	return s.issueTokens(u.ID)
}

func (s *Service) GetByID(id int64) (*User, error) {
	var u User
	if err := s.db.First(&u, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &u, nil
}

func (s *Service) issueTokens(userID int64) (*TokenPair, error) {
	now := time.Now()
	accessExpires := now.Add(accessTTL)
	claims := Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(accessExpires),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	access, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return nil, err
	}

	refresh := uuid.NewString()
	row := RefreshToken{
		UserID:    userID,
		Token:     refresh,
		ExpiresAt: now.Add(refreshTTL),
	}
	if err := s.db.Create(&row).Error; err != nil {
		return nil, err
	}
	return &TokenPair{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresIn:    int64(accessTTL.Seconds()),
	}, nil
}
