package middleware

import (
	"net/http"
	"strings"

	"github.com/JerryDtj/XNCAgent-go/pkg/response"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const ContextUserID = "user_id"

type userClaims struct {
	UserID int64 `json:"user_id"`
	jwt.RegisteredClaims
}

func JWT(secret string) gin.HandlerFunc {
	skip := map[string]struct{}{
		"/health":                 {},
		"/api/v1/users/register":  {},
		"/api/v1/users/login":     {},
	}
	key := []byte(secret)
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}
		if _, ok := skip[c.Request.URL.Path]; ok {
			c.Next()
			return
		}
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			response.Fail(c, http.StatusUnauthorized, 40101, "unauthorized")
			c.Abort()
			return
		}
		raw := strings.TrimPrefix(header, "Bearer ")
		claims := &userClaims{}
		token, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (interface{}, error) {
			if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
				return nil, jwt.ErrSignatureInvalid
			}
			return key, nil
		})
		if err != nil || !token.Valid || claims.UserID == 0 {
			response.Fail(c, http.StatusUnauthorized, 40101, "unauthorized")
			c.Abort()
			return
		}
		c.Set(ContextUserID, claims.UserID)
		c.Next()
	}
}
