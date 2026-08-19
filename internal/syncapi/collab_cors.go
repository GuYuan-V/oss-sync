package syncapi

import "github.com/gin-gonic/gin"

const obsidianDesktopOrigin = "app://obsidian.md"

func allowObsidianDesktopOrigin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetHeader("Origin") == obsidianDesktopOrigin {
			c.Header("Access-Control-Allow-Origin", obsidianDesktopOrigin)
			c.Header("Vary", "Origin")
		}
		c.Next()
	}
}
