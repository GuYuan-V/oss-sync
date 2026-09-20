// 协作传输
package syncapi

import (
	"net"
	"net/http"
	"strings"
)

// collabQueryTokenAllowed 限制查询参数凭据只出现在加密或本机回环链路上。
func collabQueryTokenAllowed(req *http.Request, forwardedProto string) bool {
	if req.TLS != nil || strings.EqualFold(strings.TrimSpace(forwardedProto), "https") {
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
