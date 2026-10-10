package serverplugin

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
)

// executablePlatformPattern 匹配可执行插件的平台标识，形如 linux-amd64
var executablePlatformPattern = regexp.MustCompile(`^[a-z0-9]+-[a-z0-9]+$`)

// validateRuntime 按运行时校验 manifest 的入口、参数与平台约束
// WASM 不允许声明入口与参数；可执行运行时入口数 1..16、参数不超过 32 个且单个不超过 1024 字节
func validateRuntime(manifest Manifest) error {
	switch manifestRuntime(manifest) {
	case RuntimeWASM:
		if len(manifest.Entrypoints) != 0 || len(manifest.Args) != 0 {
			return errors.New("WASM plugins cannot declare entrypoints or args")
		}
	case RuntimeExecutable:
		if len(manifest.Entrypoints) == 0 || len(manifest.Entrypoints) > 16 {
			return errors.New("executable plugins must declare entrypoints")
		}
		for platform, entrypoint := range manifest.Entrypoints {
			if platform != "any" && !executablePlatformPattern.MatchString(platform) {
				return fmt.Errorf("invalid executable platform %q", platform)
			}
			if !validPluginFilePath(entrypoint) || entrypoint == "manifest.json" {
				return fmt.Errorf("invalid executable entrypoint %q", entrypoint)
			}
		}
		if len(manifest.Args) > 32 {
			return errors.New("executable plugin args exceed 32")
		}
		for _, arg := range manifest.Args {
			if strings.ContainsRune(arg, '\x00') || len(arg) > 1024 {
				return errors.New("executable plugin arg is invalid")
			}
		}
	default:
		return fmt.Errorf("unsupported plugin runtime %q", manifest.Runtime)
	}
	return nil
}

// validPluginFilePath 判断是否为规范化的包内相对路径
// 拒绝绝对路径、盘符、反斜杠、空字节与含 .. 的逃逸路径
func validPluginFilePath(name string) bool {
	return name != "" &&
		!strings.Contains(name, "\\") &&
		!strings.Contains(name, ":") &&
		!strings.ContainsRune(name, '\x00') &&
		!strings.HasPrefix(name, "/") &&
		path.Clean(name) == name &&
		name != "." &&
		!strings.HasPrefix(name, "../") &&
		!strings.Contains(name, "/../")
}
