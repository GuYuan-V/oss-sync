package syncapi

import (
	"net"
	"net/http"
	"strings"
)

// collabQueryTokenAllowed 判断查询参数凭据是否放行
// 默认仅允许 TLS 或 https 转发链路；配置打开后，
// 反代在内网以明文 http 转发 SSE 的场景也可用，此时令牌会经过未加密链路
func (h *Handler) collabQueryTokenAllowed(req *http.Request, forwardedProto string) bool {
	if req.TLS != nil || strings.EqualFold(strings.TrimSpace(forwardedProto), "https") {
		return true
	}
	if h.Cfg != nil && h.Cfg.Sync.AllowInsecureQueryToken {
		return true
	}
	return isLoopbackAddress(req.Host) && isLoopbackAddress(req.RemoteAddr)
}

func isLoopbackAddress(address string) bool {
	host := strings.TrimSpace(address)
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
