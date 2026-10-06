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
	"gorm.io/gorm"
)

const (
	accessTTL  = 2 * time.Hour
	refreshTTL = 7 * 24 * time.Hour
	codeTTL    = time.Hour
	codeKeyFmt = "login_code:%s"
)

var (
	ErrUserBanned           = errors.New("账号已被封禁")
	ErrUserNotFound         = errors.New("用户不存在")
	ErrInvalidCode          = errors.New("验证码错误")
	ErrCodeExpired          = errors.New("验证码已过期，请重新获取")
	ErrRefreshTokenNotFound = errors.New("刷新令牌不存在或已被吊销")
	ErrRefreshTokenExpired  = errors.New("刷新令牌已过期")
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

func codeKey(email string) string {
	return fmt.Sprintf(codeKeyFmt, email)
}

func (s *Service) SendLoginCode(email string) error {
	email = normalizeEmail(email)

	n, err := rand.Int(rand.Reader, big.NewInt(10000))
	if err != nil {
		return err
	}
	code := fmt.Sprintf("%04d", n.Int64())

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.rdb.Set(ctx, codeKey(email), code, codeTTL).Err(); err != nil {
		return err
	}

	subject := "XNCAgent 登录验证码"
	body := fmt.Sprintf("您的登录验证码是 %s，1 小时内有效。未注册的邮箱验证成功后会自动开通账号。如果不是您本人操作，请忽略本邮件。", code)
	if err := s.mailer.SendPlain(email, subject, body); err != nil {
		_ = s.rdb.Del(context.Background(), codeKey(email)).Err()
		return err
	}
	return nil
}

func (s *Service) consumeCode(email, code string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	key := codeKey(email)
	stored, err := s.rdb.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return ErrCodeExpired
		}
		return err
	}
	if subtle.ConstantTimeCompare([]byte(stored), []byte(strings.TrimSpace(code))) != 1 {
		return ErrInvalidCode
	}
	_ = s.rdb.Del(ctx, key).Err()
	return nil
}

func (s *Service) LoginWithCode(email, code string) (*TokenPair, error) {
	email = normalizeEmail(email)
	if err := s.consumeCode(email, code); err != nil {
		return nil, err
	}

	var u User
	err := s.db.Where("email = ?", email).First(&u).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		created, createErr := s.createUser(email)
		if createErr != nil {
			return nil, createErr
		}
		u = *created
	}
	if u.Status == "banned" {
		return nil, ErrUserBanned
	}
	return s.issueTokens(s.db, u.ID)
}

func (s *Service) createUser(email string) (*User, error) {
	u := &User{
		Email:  email,
		Status: "active",
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(u).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return gorm.ErrDuplicatedKey
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
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			var existing User
			if findErr := s.db.Where("email = ?", email).First(&existing).Error; findErr != nil {
				return nil, findErr
			}
			return &existing, nil
		}
		return nil, err
	}
	return u, nil
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

// 生成一个新的token(刷新+授权)
func (s *Service) issueTokens(db *gorm.DB, userID int64) (*TokenPair, error) {
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
	if err := db.Create(&row).Error; err != nil {
		return nil, err
	}
	return &TokenPair{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresIn:    int64(accessTTL.Seconds()),
	}, nil
}

// 刷新token(授权)
func (s *Service) RefreshToken(rawToken string) (*TokenPair, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return nil, ErrRefreshTokenNotFound
	}

	var pair *TokenPair
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var row RefreshToken
		if err := tx.Where("token = ?", rawToken).First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrRefreshTokenNotFound
			}
			return err
		}
		if time.Now().After(row.ExpiresAt) {
			//过期的顺手清理掉,避免堆积
			_ = tx.Delete(&RefreshToken{}, row.ID).Error
			return ErrRefreshTokenExpired
		}

		res := tx.Where("token = ?", rawToken).Delete(&RefreshToken{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			//同一个token的另一个refresh已经先删了
			return ErrRefreshTokenNotFound
		}
		newPair, err := s.issueTokens(tx, row.UserID)
		if err != nil {
			return err
		}
		pair = newPair
		return nil
	})
	if err != nil {
		return nil, err
	}
	return pair, nil
}

func (s *Service) Logout(rawToken string) error {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return nil
	}
	return s.db.Where("token = ?", rawToken).Delete(&RefreshToken{}).Error
}
