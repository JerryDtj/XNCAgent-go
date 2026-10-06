package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"runtime/debug"
	"strings"

	"github.com/gin-gonic/gin"
)

func newRouter() *gin.Engine {
	gin.DebugPrintRouteFunc = printRoute
	gin.DebugPrintFunc = printDebug
	gin.DefaultErrorWriter = chineseErrorWriter{}

	router := gin.New()
	router.Use(gin.LoggerWithFormatter(chineseAccessLog))
	router.Use(gin.CustomRecoveryWithWriter(io.Discard, recoverChinese))
	return router
}

type chineseErrorWriter struct{}

func (chineseErrorWriter) Write(p []byte) (int, error) {
	msg := strings.TrimSpace(string(p))
	msg = strings.TrimPrefix(msg, "[GIN-debug] [ERROR] ")
	if msg != "" {
		log.Printf("网关错误: %s", msg)
	}
	return len(p), nil
}

func printRoute(method, path, handler string, n int) {
	log.Printf("路由 %-6s %-25s -> %s（%d 个处理函数）", method, path, handler, n)
}

func printDebug(format string, values ...any) {
	switch {
	case strings.Contains(format, `Running in "debug" mode`):
		log.Print("当前为调试模式。生产环境请设置环境变量 GIN_MODE=release，或调用 gin.SetMode(gin.ReleaseMode)")
	case strings.Contains(format, "Creating an Engine"):
		log.Print("已创建带访问日志和异常恢复的引擎")
	case strings.Contains(format, "requires Go"):
		log.Print("警告：Gin 需要 Go 1.25 及以上版本")
	case strings.Contains(format, "trusted all proxies"):
		log.Print("警告：当前信任全部代理，这不安全。请配置可信代理")
	case strings.Contains(format, "SetHTMLTemplate"):
		log.Print("警告：SetHTMLTemplate 不是线程安全的，只能在初始化时调用")
	case strings.HasPrefix(format, "Listening and serving HTTP on unix"):
		log.Printf("正在监听 Unix 套接字 %s", values[0])
	case strings.HasPrefix(format, "Listening and serving HTTP on fd@"):
		log.Printf("正在监听文件描述符 %v", values[0])
	case strings.HasPrefix(format, "Listening and serving HTTP on listener"):
		log.Printf("正在监听 %v", values[0])
	case strings.HasPrefix(format, "Listening and serving HTTP on"):
		log.Printf("正在监听 %s", values[0])
	case strings.HasPrefix(format, "Listening and serving HTTPS on"):
		log.Printf("正在监听 HTTPS %s", values[0])
	case strings.HasPrefix(format, "Listening and serving QUIC on"):
		log.Printf("正在监听 QUIC %s", values[0])
	case strings.HasPrefix(format, "redirecting request"):
		log.Printf("重定向请求 %v：%v -> %v", values[0], values[1], values[2])
	case strings.Contains(format, "Headers were already written"):
		log.Printf("警告：响应头已写出，无法把状态码 %v 改为 %v", values[0], values[1])
	case strings.Contains(format, "PORT is undefined"):
		log.Print("未设置环境变量 PORT，默认使用 :8080")
	case strings.Contains(format, "Environment variable PORT="):
		log.Printf("环境变量 PORT=%v", values[0])
	case strings.Contains(format, "error on parse multipart"):
		log.Printf("解析 multipart 表单失败: %v", values[0])
	case strings.Contains(format, "cannot write message"):
		log.Printf("处理请求错误时写回失败: %v", values[0])
	default:
		msg := strings.TrimSpace(fmt.Sprintf(format, values...))
		if msg != "" {
			log.Print(msg)
		}
	}
}

func chineseAccessLog(p gin.LogFormatterParams) string {
	line := fmt.Sprintf("[网关] %s | %3d | %13v | %15s | %-7s %s",
		p.TimeStamp.Format("2006/01/02 - 15:04:05"),
		p.StatusCode,
		p.Latency,
		p.ClientIP,
		p.Method,
		p.Path,
	)
	if msg := strings.TrimSpace(p.ErrorMessage); msg != "" {
		line += " | " + msg
	}
	return line + "\n"
}

func recoverChinese(c *gin.Context, err any) {
	log.Printf("请求异常已恢复 %s %s: %v\n%s", c.Request.Method, c.Request.URL.RequestURI(), err, debug.Stack())
	c.AbortWithStatus(http.StatusInternalServerError)
}
