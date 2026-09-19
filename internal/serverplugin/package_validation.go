package serverplugin

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
)

var executablePlatformPattern = regexp.MustCompile(`^[a-z0-9]+-[a-z0-9]+$`)

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
