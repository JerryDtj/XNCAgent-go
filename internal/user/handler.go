package user

import (
	"errors"
	"net/http"

	"github.com/JerryDtj/XNCAgent-go/internal/config"
	"github.com/JerryDtj/XNCAgent-go/internal/mail"
	"github.com/JerryDtj/XNCAgent-go/internal/middleware"
	"github.com/JerryDtj/XNCAgent-go/pkg/response"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type Handler struct {
	svc          *Service
	cookieSecure bool
}

const (
	refreshCookieName   = "refresh_token"
	refreshCookiePath   = "/api/v1/users" //覆盖/refresh和/logout
	refreshCookieMaxAge = 7 * 24 * 3600   // 与 refreshTTL 一致
)

func RegisterRoutes(r *gin.Engine, db *gorm.DB, rdb *redis.Client, jwtSecret string, smtpCfg config.SMTPConfig, cookieSecure bool) {
	h := &Handler{svc: NewService(db, rdb, mail.NewSender(smtpCfg), jwtSecret), cookieSecure: cookieSecure}
	g := r.Group("/api/v1/users")
	g.POST("/send-code", h.SendCode)
	g.POST("/login", h.Login)
	g.GET("/me", h.Me)
	g.POST("/refresh", h.Refresh)
	g.POST("/logout", h.Logout)
}

type sendCodeReq struct {
	Email string `json:"email" binding:"required,email"`
}

type loginReq struct {
	Email string `json:"email" binding:"required,email"`
	Code  string `json:"code" binding:"required,len=4,numeric"`
}

func (h *Handler) SendCode(c *gin.Context) {
	var req sendCodeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 1, "请填写正确的邮箱")
		return
	}
	if err := h.svc.SendLoginCode(req.Email); err != nil {
		response.Fail(c, http.StatusInternalServerError, 1, "验证码发送失败")
		return
	}
	response.OK(c, gin.H{"expires_in": int64(codeTTL.Seconds())})
}

func (h *Handler) Login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 1, "请填写正确的邮箱和 4 位数字验证码")
		return
	}
	tokens, err := h.svc.LoginWithCode(req.Email, req.Code)
	if err != nil {
		if errors.Is(err, ErrInvalidCode) || errors.Is(err, ErrCodeExpired) {
			response.Fail(c, http.StatusBadRequest, 1, err.Error())
			return
		}
		if errors.Is(err, ErrUserBanned) {
			response.Fail(c, http.StatusUnauthorized, 40101, err.Error())
			return
		}
		response.Fail(c, http.StatusInternalServerError, 1, "登录失败")
		return
	}
	h.setRefreshCookie(c, tokens.RefreshToken)
	response.OK(c, gin.H{
		"access_token": tokens.AccessToken,
		"expires_in":   tokens.ExpiresIn,
	})
}

func (h *Handler) Me(c *gin.Context) {
	uid, ok := c.Get(middleware.ContextUserID)
	if !ok {
		response.Fail(c, http.StatusUnauthorized, 40101, "未登录或登录已过期")
		return
	}
	userID, _ := uid.(int64)
	u, err := h.svc.GetByID(userID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			response.Fail(c, http.StatusUnauthorized, 40101, "未登录或登录已过期")
			return
		}
		response.Fail(c, http.StatusInternalServerError, 1, "查询失败")
		return
	}
	response.OK(c, gin.H{"user_id": u.ID, "email": u.Email})
}

func (h *Handler) Refresh(c *gin.Context) {
	raw, err := c.Cookie(refreshCookieName)
	if err != nil || raw == "" {
		response.Fail(c, http.StatusUnauthorized, 40101, "未登录或登录已过期")
		return
	}
	tokens, err := h.svc.RefreshToken(raw)
	if err != nil {
		h.clearRefreshCookie(c)
		response.Fail(c, http.StatusUnauthorized, 40101, "未登录或登录已过期")
		return
	}
	h.setRefreshCookie(c, tokens.RefreshToken)
	response.OK(c, gin.H{
		"access_token": tokens.AccessToken,
		"expires_in":   tokens.ExpiresIn,
	})
}

func (h *Handler) Logout(c *gin.Context) {
	raw, _ := c.Cookie(refreshCookieName)
	if raw != "" {
		_ = h.svc.Logout(raw) // 失败不影响响应，保证幂等
	}
	h.clearRefreshCookie(c)
	response.OK(c, nil)
}

func (h *Handler) setRefreshCookie(c *gin.Context, raw string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(
		refreshCookieName,
		raw,
		refreshCookieMaxAge,
		refreshCookiePath, 
		"", //domain 留空 = 当前域
		h.cookieSecure, // 生产 HTTPS 置 true
		true,           // httpOnly
	)
}

func (h *Handler) clearRefreshCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(refreshCookieName, "", -1, refreshCookiePath, "", h.cookieSecure, true)
}
