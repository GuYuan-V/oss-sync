package update

import (
	"fmt"
	"net/url"
	"strings"
)

const defaultReleaseProxy = "https://gh-proxy.com/"

// resolveDownloadURL applies the administrator-selected Release file source.
// The original GitHub digest and size remain authoritative after download.
func resolveDownloadURL(assetURL, source, customProxy string) (string, error) {
	switch strings.TrimSpace(source) {
	case "", "official":
		return assetURL, nil
	case "proxy":
		return defaultReleaseProxy + assetURL, nil
	case "custom":
		prefix := strings.TrimSpace(customProxy)
		if len(prefix) > 1024 {
			return "", newUpdateError(CodeInvalidURL, "custom download proxy is too long", ErrInvalidURL)
		}
		u, err := url.Parse(prefix)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return "", newUpdateError(CodeInvalidURL, fmt.Sprintf("custom download proxy must be an HTTPS prefix, got %q", prefix), ErrInvalidURL)
		}
		return strings.TrimRight(prefix, "/") + "/" + assetURL, nil
	default:
		return "", newUpdateError(CodeInvalidURL, fmt.Sprintf("unknown download source %q", source), ErrInvalidURL)
	}
}
