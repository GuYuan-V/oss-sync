// 访问日志
package server

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
)

func accessLogger() gin.HandlerFunc {
	return gin.LoggerWithFormatter(formatAccessLog)
}

func formatAccessLog(params gin.LogFormatterParams) string {
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

