package middleware

import "github.com/gin-gonic/gin"

const obsidianDesktopOrigin = "app://obsidian.md"

// AllowObsidianDesktopOrigin 放行 Obsidian 桌面端来源的协作 SSE 与轮询请求，
// 不提供通配 CORS 策略。
func AllowObsidianDesktopOrigin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetHeader("Origin") == obsidianDesktopOrigin {
			c.Header("Access-Control-Allow-Origin", obsidianDesktopOrigin)
			c.Header("Vary", "Origin")
		}
		c.Next()
	}
}
