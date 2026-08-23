// 平台适配
package update

import (
	"fmt"
	"runtime"

	"github.com/oss/oss-server/internal/version"
)

// Platform 表示受支持的 OS/Arch 组合。
type Platform struct {
	GOOS   string
	GOARCH string
}

// SupportedPlatforms 返回所有受支持的平台集合。
// 仅支持：linux/amd64, linux/arm64, windows/amd64, darwin/amd64, darwin/arm64。
func SupportedPlatforms() []Platform {
	return []Platform{
		{GOOS: "linux", GOARCH: "amd64"},
		{GOOS: "linux", GOARCH: "arm64"},
		{GOOS: "windows", GOARCH: "amd64"},
		{GOOS: "darwin", GOARCH: "amd64"},
		{GOOS: "darwin", GOARCH: "arm64"},
	}
}

var supportedMap = func() map[string]struct{} {
	m := make(map[string]struct{}, 5)
	for _, p := range SupportedPlatforms() {
		m[p.GOOS+"/"+p.GOARCH] = struct{}{}
	}
	return m
}()

// IsSupportedPlatform 判断给定 GOOS/GOARCH 是否受支持。
func IsSupportedPlatform(goos, goarch string) bool {
	_, ok := supportedMap[goos+"/"+goarch]
	return ok
}

// IsCurrentPlatformSupported 判断当前运行平台是否受支持。
func IsCurrentPlatformSupported() bool {
	return IsSupportedPlatform(runtime.GOOS, runtime.GOARCH)
}

// AssetName 返回严格的发布资产文件名，格式：
//
//	oss-server_<version>_<goos>_<goarch>.tar.gz      // linux, darwin
//	oss-server_<version>_<goos>_<goarch>.zip         // windows
//
// version 允许带或不带 v 前缀，内部会规范化并校验为严格 SemVer。
// 不支持的平台返回错误。
func AssetName(v, goos, goarch string) (string, error) {
	if !IsSupportedPlatform(goos, goarch) {
		return "", newUpdateError(
			CodeUnsupportedPlatform,
			fmt.Sprintf("unsupported platform %s/%s", goos, goarch),
			ErrUnsupportedPlatform,
		)
	}
	norm := version.Normalize(v)
	if norm == "" {
		return "", newUpdateError(CodeInvalidVersion, "version is empty", ErrInvalidVersion)
	}
	if _, err := version.Parse(norm); err != nil {
		return "", newUpdateError(CodeInvalidVersion, fmt.Sprintf("invalid version %q", v), err)
	}
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("oss-server_%s_%s_%s%s", norm, goos, goarch, ext), nil
}

// CurrentAssetName 返回当前平台的资产文件名。
func CurrentAssetName(v string) (string, error) {
	return AssetName(v, runtime.GOOS, runtime.GOARCH)
}

// ExpectedAssetNames 返回某一版本在所有支持平台上的资产名集合。
func ExpectedAssetNames(v string) (map[string]string, error) {
	out := make(map[string]string, 5)
	for _, p := range SupportedPlatforms() {
		n, err := AssetName(v, p.GOOS, p.GOARCH)
		if err != nil {
			return nil, err
		}
		out[p.GOOS+"/"+p.GOARCH] = n
	}
	return out, nil
}

