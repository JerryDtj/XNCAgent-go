package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type Body struct {
	Code      int         `json:"code"`
	Message   string      `json:"message"`
	Data      interface{} `json:"data,omitempty"`
	RequestID string      `json:"request_id,omitempty"`
}

func write(c *gin.Context, httpStatus, code int, msg string, data interface{}) {
	c.JSON(httpStatus, Body{
		Code:      code,
		Message:   msg,
		Data:      data,
		RequestID: c.GetHeader("X-Request-ID"),
	})
}

func OK(c *gin.Context, data interface{}) {
	write(c, http.StatusOK, 0, "成功", data)
}

func Fail(c *gin.Context, httpStatus, code int, msg string) {
	write(c, httpStatus, code, msg, nil)
}
