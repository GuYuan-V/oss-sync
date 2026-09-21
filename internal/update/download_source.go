package update

import (
	"fmt"
	"net/url"
	"strings"
)

const defaultReleaseProxy = "https://gh-proxy.com/"

// resolveUpdateURL 把选定的下载源应用于 GitHub API 或 Release 地址
func resolveUpdateURL(rawURL, source, customProxy string) (string, error) {
	switch strings.TrimSpace(source) {
	case "", "official":
		return rawURL, nil
	case "proxy":
		return defaultReleaseProxy + rawURL, nil
	case "custom":
		prefix := strings.TrimSpace(customProxy)
		if len(prefix) > 1024 {
			return "", newUpdateError(CodeInvalidURL, "custom download proxy is too long", ErrInvalidURL)
		}
		u, err := url.Parse(prefix)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return "", newUpdateError(CodeInvalidURL, fmt.Sprintf("custom download proxy must be an HTTPS prefix, got %q", prefix), ErrInvalidURL)
		}
		return strings.TrimRight(prefix, "/") + "/" + rawURL, nil
	default:
		return "", newUpdateError(CodeInvalidURL, fmt.Sprintf("unknown download source %q", source), ErrInvalidURL)
	}
}

// resolveDownloadURL 把选定的下载源应用于 Release 资产地址
// 下载后仍以原始 digest 与 size 为准
func resolveDownloadURL(assetURL, source, customProxy string) (string, error) {
	return resolveUpdateURL(assetURL, source, customProxy)
}
