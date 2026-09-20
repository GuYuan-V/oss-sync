// 协作跨域。
package syncapi

import (
	"github.com/gin-gonic/gin"

	"github.com/helantianshen/oss-sync/internal/transport/http/middleware"
)

// allowObsidianDesktopOrigin 将路由声明保留在本地，实际跨域策略复用 HTTP 中间件的统一实现。
func allowObsidianDesktopOrigin() gin.HandlerFunc {
	return middleware.AllowObsidianDesktopOrigin()
}
