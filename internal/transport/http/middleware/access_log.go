// Package middleware 收敛各功能共享的 HTTP 横切关注点
// 身份模型与凭据校验归 auth 包所有，仍保留在该包内
package middleware

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
)

// AccessLogger 返回服务端访问日志中间件，成功请求保持安静，
// 客户端与服务端错误不记录查询参数
func AccessLogger() gin.HandlerFunc {
	return gin.LoggerWithFormatter(FormatAccessLog)
}

// FormatAccessLog 格式化单条 Gin 访问日志，不对外暴露查询参数
func FormatAccessLog(params gin.LogFormatterParams) string {
	if params.StatusCode < 400 {
		return ""
	}
	path, _, _ := strings.Cut(params.Path, "?")
	return fmt.Sprintf(
		"[GIN] %s | %3d | %13v | %15s | %-7s %s\n",
		params.TimeStamp.Format("2006/01/02 - 15:04:05"),
		params.StatusCode,
		params.Latency,
		params.ClientIP,
		params.Method,
		path,
	)
}
