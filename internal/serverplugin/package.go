package serverplugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"runtime"
	"strings"

	"github.com/helantianshen/oss-sync/internal/blog"
)

const (
	CurrentAPIVersion    = 1
	MaxManifestBytes     = 64 << 10
	MaxArchiveBytes      = 32 << 20
	MaxWasmBytes         = 8 << 20
	MaxPluginMemoryPages = 256
	MaxRequestBytes      = 1 << 20
	MaxResponseBytes     = 1 << 20
	MaxRoutes            = 32
	MaxPluginFiles       = 512
	MaxPluginFileBytes   = 32 << 20
	MaxExtractedBytes    = 64 << 20
	RuntimeWASM          = "wasm"
	RuntimeExecutable    = "executable"
)

var (
	ErrInvalidManifest = errors.New("invalid server plugin manifest")
	ErrInvalidPackage  = errors.New("invalid server plugin package")
	ErrPluginExists    = errors.New("server plugin already exists")
	ErrPluginNotFound  = errors.New("server plugin not found")
	ErrPluginEnabled   = errors.New("server plugin is enabled")
	ErrPluginDisabled  = errors.New("server plugin is disabled")
)

var pluginIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,63}$`)
var routePathPattern = regexp.MustCompile(`^/(?:[A-Za-z0-9._~-]+(?:/[A-Za-z0-9._~-]+)*)?$`)

// Manifest 是单个服务端插件包受信的解析后元数据
type Manifest struct {
	ID           string                   `json:"id"`
	Name         string                   `json:"name"`
	Version      string                   `json:"version"`
	Description  string                   `json:"description,omitempty"`
	APIVersion   int                      `json:"api_version"`
	Routes       []RouteSpec              `json:"routes"`
	Settings     []blog.ThemeSettingField `json:"settings,omitempty"`
	Hooks        []HookSpec               `json:"hooks,omitempty"`
	Registration *ExtensionRegistration   `json:"registration,omitempty"`
	Runtime      string                   `json:"runtime,omitempty"`
	Entrypoints  map[string]string        `json:"entrypoints,omitempty"`
	Args         []string                 `json:"args,omitempty"`
}

// HookSpec 声明受信插件处理的宿主集成点
type HookSpec struct {
	Name  string `json:"name"`
	ID    string `json:"id,omitempty"`
	Label string `json:"label,omitempty"`
}

// RouteSpec 声明插件固定命名空间内的一条路由
type RouteSpec struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Public bool   `json:"public"`
}

// PluginRequest 是暴露给服务端插件的请求数据
type PluginRequest struct {
	Method     string              `json:"method"`
	Path       string              `json:"path"`
	Callback   string              `json:"callback,omitempty"`
	Query      map[string][]string `json:"query,omitempty"`
	Params     map[string]string   `json:"params,omitempty"`
	Headers    map[string][]string `json:"headers,omitempty"`
	Cookies    map[string]string   `json:"cookies,omitempty"`
	User       *PluginUser         `json:"user,omitempty"`
	Settings   map[string]any      `json:"settings,omitempty"`
	Hook       string              `json:"hook,omitempty"`
	Payload    map[string]any      `json:"payload,omitempty"`
	BodyBase64 string              `json:"body_base64,omitempty"`
}

type PluginUser struct {
	ID       uint   `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

// PluginResponse 是服务端插件可返回的响应形态
type PluginResponse struct {
	Status     int               `json:"status"`
	Headers    map[string]string `json:"headers,omitempty"`
	BodyBase64 string            `json:"body_base64,omitempty"`
}

func ParseManifest(raw []byte) (Manifest, error) {
	if len(raw) == 0 || len(raw) > MaxManifestBytes {
		return Manifest{}, fmt.Errorf("%w: manifest size is invalid", ErrInvalidManifest)
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("%w: %v", ErrInvalidManifest, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Manifest{}, fmt.Errorf("%w: manifest must contain one JSON object", ErrInvalidManifest)
		}
		return Manifest{}, fmt.Errorf("%w: %v", ErrInvalidManifest, err)
	}
	if err := ValidateManifest(manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func ValidateManifest(manifest Manifest) error {
	if !pluginIDPattern.MatchString(manifest.ID) {
		return fmt.Errorf("%w: id must be 2-64 lowercase letters, digits, or hyphens", ErrInvalidManifest)
	}
	if !boundedText(manifest.Name, 1, 128) {
		return fmt.Errorf("%w: name length is invalid", ErrInvalidManifest)
	}
	if !boundedText(manifest.Version, 1, 64) {
		return fmt.Errorf("%w: version length is invalid", ErrInvalidManifest)
	}
	if len(manifest.Description) > 2000 {
		return fmt.Errorf("%w: description is too long", ErrInvalidManifest)
	}
	if manifest.APIVersion != CurrentAPIVersion {
		return fmt.Errorf("%w: unsupported api_version %d", ErrInvalidManifest, manifest.APIVersion)
	}
	if len(manifest.Routes) > MaxRoutes {
		return fmt.Errorf("%w: route count is invalid", ErrInvalidManifest)
	}
	if manifestRuntime(manifest) == RuntimeWASM && len(manifest.Routes) == 0 && len(manifest.Hooks) == 0 && len(manifest.Settings) == 0 {
		return fmt.Errorf("%w: WASM plugin must declare a route, hook, or setting", ErrInvalidManifest)
	}
	if err := validateRuntime(manifest); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidManifest, err)
	}
	if err := blog.ValidateSettingFields(manifest.Settings); err != nil {
		return fmt.Errorf("%w: plugin settings: %v", ErrInvalidManifest, err)
	}
	if err := validateHooks(manifest.Hooks, manifestRuntime(manifest) == RuntimeExecutable); err != nil {
		return fmt.Errorf("%w: plugin hooks: %v", ErrInvalidManifest, err)
	}
	seen := make(map[string]struct{}, len(manifest.Routes))
	for _, route := range manifest.Routes {
		if !validMethod(route.Method) {
			return fmt.Errorf("%w: unsupported route method %q", ErrInvalidManifest, route.Method)
		}
		if !validRoutePath(route.Path) {
			return fmt.Errorf("%w: unsafe route path %q", ErrInvalidManifest, route.Path)
		}
		key := route.Method + " " + route.Path
		if _, exists := seen[key]; exists {
			return fmt.Errorf("%w: duplicate route %q", ErrInvalidManifest, key)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func manifestRuntime(manifest Manifest) string {
	if manifest.Runtime == "" {
		return RuntimeWASM
	}
	return manifest.Runtime
}

func manifestEntrypoint(manifest Manifest) (string, error) {
	if manifestRuntime(manifest) != RuntimeExecutable {
		return "plugin.wasm", nil
	}
	platform := runtime.GOOS + "-" + runtime.GOARCH
	entrypoint := manifest.Entrypoints[platform]
	if entrypoint == "" {
		entrypoint = manifest.Entrypoints["any"]
	}
	if entrypoint == "" {
		return "", fmt.Errorf("no executable entrypoint for %s", platform)
	}
	return entrypoint, nil
}

func validateHooks(hooks []HookSpec, dynamic bool) error {
	if len(hooks) > 32 {
		return errors.New("hook count is invalid")
	}
	seen := make(map[string]struct{}, len(hooks))
	for _, hook := range hooks {
		if dynamic && !validExtensionName(hook.Name) || !dynamic && !validHookName(hook.Name) {
			return fmt.Errorf("unsupported hook %q", hook.Name)
		}
		if hook.Name == "editor.command" && (!pluginIDPattern.MatchString(hook.ID) || !boundedText(hook.Label, 1, 128)) {
			return errors.New("editor command id or label is invalid")
		}
		if _, exists := seen[hook.Name]; exists {
			return fmt.Errorf("duplicate hook %q", hook.Name)
		}
		seen[hook.Name] = struct{}{}
	}
	return nil
}

func validHookName(name string) bool {
	switch name {
	case "blog.content", "markdown.content", "theme.render", "admin.page", "editor.command", "comment.content":
		return true
	default:
		return false
	}
}

func validMethod(method string) bool {
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE":
		return true
	default:
		return false
	}
}

func validRoutePath(routePath string) bool {
	return routePathPattern.MatchString(routePath) &&
		routePath == cleanRoutePath(routePath) &&
		!strings.Contains(routePath, "//")
}

func cleanRoutePath(routePath string) string {
	parts := strings.Split(routePath, "/")
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part {
		case "", ".":
			if part == "" && len(cleaned) == 0 {
				cleaned = append(cleaned, "")
			}
		case "..":
			return ""
		default:
			cleaned = append(cleaned, part)
		}
	}
	return strings.Join(cleaned, "/")
}

func boundedText(value string, min, max int) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed == value && len(trimmed) >= min && len(trimmed) <= max
}
