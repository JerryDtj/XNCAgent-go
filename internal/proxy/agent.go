package proxy

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/gin-gonic/gin"
)

// Agent 把 /agent/* 原样转到 Python agent。
// 不改路径、不读 body；Authorization、Content-Type 等头随请求带过去。
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
		reverse.ServeHTTP(c.Writer, c.Request)
	}, nil
}
