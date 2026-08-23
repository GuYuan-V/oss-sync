// 协作传输
package syncapi

import (
	"net"
	"net/http"
	"strings"
)

// collabQueryTokenAllowed prevents query credentials from crossing plaintext networks.
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

