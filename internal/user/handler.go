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
	svc *Service
}

func RegisterRoutes(r *gin.Engine, db *gorm.DB, rdb *redis.Client, jwtSecret string, smtpCfg config.SMTPConfig) {
	h := &Handler{svc: NewService(db, rdb, mail.NewSender(smtpCfg), jwtSecret)}
	g := r.Group("/api/v1/users")
	g.POST("/send-code", h.SendCode)
	g.POST("/register", h.Register)
	g.POST("/login", h.Login)
	g.GET("/me", h.Me)
}

type sendCodeReq struct {
	Email string `json:"email" binding:"required,email"`
}

type registerReq struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8"`
	Code     string `json:"code" binding:"required,len=4,numeric"`
}

type loginReq struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

func (h *Handler) SendCode(c *gin.Context) {
	var req sendCodeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 1, "请填写正确的邮箱")
		return
	}
	if err := h.svc.SendRegisterCode(req.Email); err != nil {
		if errors.Is(err, ErrEmailTaken) {
			response.Fail(c, http.StatusBadRequest, 1, err.Error())
			return
		}
		response.Fail(c, http.StatusInternalServerError, 1, "验证码发送失败")
		return
	}
	response.OK(c, gin.H{"expires_in": int64(codeTTL.Seconds())})
}

func (h *Handler) Register(c *gin.Context) {
	var req registerReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 1, "请填写正确的邮箱、至少 8 位密码和 4 位数字验证码")
	if err != nil {
		if errors.Is(err, ErrEmailTaken) || errors.Is(err, ErrInvalidCode) || errors.Is(err, ErrCodeExpired) {
			response.Fail(c, http.StatusBadRequest, 1, err.Error())
			return
		}
		response.Fail(c, http.StatusInternalServerError, 1, "注册失败")
		return
	}
	response.OK(c, gin.H{"user_id": u.ID, "email": u.Email})
}

func (h *Handler) Login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 1, "请填写邮箱和密码")
	if err != nil {
		if errors.Is(err, ErrInvalidCreds) || errors.Is(err, ErrUserBanned) {
			response.Fail(c, http.StatusUnauthorized, 40101, err.Error())
			return
		}
		response.Fail(c, http.StatusInternalServerError, 1, "登录失败")
		return
	}
	response.OK(c, tokens)
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
