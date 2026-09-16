package user

import (
	"errors"
	"net/http"

	"github.com/JerryDtj/XNCAgent-go/internal/middleware"
	"github.com/JerryDtj/XNCAgent-go/pkg/response"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Handler struct {
	svc *Service
}

func RegisterRoutes(r *gin.Engine, db *gorm.DB, jwtSecret string) {
	h := &Handler{svc: NewService(db, jwtSecret)}
	g := r.Group("/api/v1/users")
	g.POST("/register", h.Register)
	g.POST("/login", h.Login)
	g.GET("/me", h.Me)
}

type registerReq struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8"`
}

type loginReq struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

func (h *Handler) Register(c *gin.Context) {
	var req registerReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 1, "invalid request")
		return
	}
	u, err := h.svc.Register(req.Email, req.Password)
	if err != nil {
		if errors.Is(err, ErrEmailTaken) {
			response.Fail(c, http.StatusBadRequest, 1, err.Error())
			return
		}
		response.Fail(c, http.StatusInternalServerError, 1, "register failed")
		return
	}
	response.OK(c, gin.H{"user_id": u.ID, "email": u.Email})
}

func (h *Handler) Login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 1, "invalid request")
		return
	}
	tokens, err := h.svc.Login(req.Email, req.Password)
	if err != nil {
		if errors.Is(err, ErrInvalidCreds) || errors.Is(err, ErrUserBanned) {
			response.Fail(c, http.StatusUnauthorized, 40101, err.Error())
			return
		}
		response.Fail(c, http.StatusInternalServerError, 1, "login failed")
		return
	}
	response.OK(c, tokens)
}

func (h *Handler) Me(c *gin.Context) {
	uid, ok := c.Get(middleware.ContextUserID)
	if !ok {
		response.Fail(c, http.StatusUnauthorized, 40101, "unauthorized")
		return
	}
	userID, _ := uid.(int64)
	u, err := h.svc.GetByID(userID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			response.Fail(c, http.StatusUnauthorized, 40101, "unauthorized")
			return
		}
		response.Fail(c, http.StatusInternalServerError, 1, "query failed")
		return
	}
	response.OK(c, gin.H{"user_id": u.ID, "email": u.Email})
}
