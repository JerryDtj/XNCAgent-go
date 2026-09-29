package proxy

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"

	"github.com/JerryDtj/XNCAgent-go/internal/middleware"
	"github.com/JerryDtj/XNCAgent-go/pkg/response"
	"github.com/gin-gonic/gin"
)

// HeaderUserID 是下游 Python agent 识别用户的请求头。
const HeaderUserID = "X-User-Id"

// Agent 把 /agent/* 原样转到 Python agent。
// 不改路径、不读 body；Authorization、Content-Type 等头随请求带过去。
// 客户端自带的 X-User-Id 一律丢弃，只写 JWT 中间件解出来的用户 ID。
// FlushInterval 为负数，流式响应每写一块就刷给客户端。
func Agent(rawTarget string) (gin.HandlerFunc, error) {
	upstream, err := url.Parse(rawTarget)
	if err != nil {
		return nil, err
	}
	if upstream.Scheme == "" || upstream.Host == "" {
		return nil, fmt.Errorf("agent proxy target must be an absolute URL")
	}
	reverse := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(upstream)
			pr.SetXForwarded()
		},
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("agent proxy %s %s: %v", r.Method, r.URL.RequestURI(), err)
			http.Error(w, "agent upstream unavailable", http.StatusBadGateway)
		},
	}
	return func(c *gin.Context) {
		// fail-fast：取不到 user_id 说明 JWT 中间件没跑或上下文键不匹配，鉴权可能整个被跳过，绝不反代。
		uid, ok := c.Get(middleware.ContextUserID)
		userID, isNum := uid.(int64)
		if !ok || !isNum || userID <= 0 {
			log.Printf("agent proxy missing user_id: %s %s", c.Request.Method, c.Request.URL.RequestURI())
			response.Fail(c, http.StatusInternalServerError, 500, "网关内部错误")
			c.Abort()
			return
		}
		c.Request.Header.Del(HeaderUserID)
		c.Request.Header.Set(HeaderUserID, strconv.FormatInt(userID, 10))
		reverse.ServeHTTP(c.Writer, c.Request)
	}, nil
}
