package serverplugin

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/helantianshen/oss-sync/internal/blog"
)

const maxRegistrations = 1024

var extensionNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._:/-]{0,127}$`)

// ExtensionRegistration 是可执行插件在运行时向宿主提供的能力声明。
type ExtensionRegistration struct {
	Hooks        []RegisteredHook         `json:"hooks,omitempty"`
	Routes       []RegisteredRoute        `json:"routes,omitempty"`
	Middleware   []RegisteredMiddleware   `json:"middleware,omitempty"`
	AdminPages   []RegisteredAdminPage    `json:"admin_pages,omitempty"`
	Assets       []RegisteredAsset        `json:"assets,omitempty"`
	Settings     []blog.ThemeSettingField `json:"settings,omitempty"`
	Tasks        []RegisteredTask         `json:"tasks,omitempty"`
	Migrations   []RegisteredMigration    `json:"migrations,omitempty"`
	Dependencies []RegisteredDependency   `json:"dependencies,omitempty"`
	Lifecycle    RegisteredLifecycle      `json:"lifecycle,omitempty"`
}

type RegisteredLifecycle struct {
	Activate   string `json:"activate,omitempty"`
	Deactivate string `json:"deactivate,omitempty"`
	Upgrade    string `json:"upgrade,omitempty"`
	Uninstall  string `json:"uninstall,omitempty"`
}

type RegisteredHook struct {
	Name     string `json:"name"`
	Callback string `json:"callback,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Priority int    `json:"priority,omitempty"`
	ID       string `json:"id,omitempty"`
	Label    string `json:"label,omitempty"`
}

type RegisteredRoute struct {
	Method   string `json:"method"`
	Path     string `json:"path"`
	Callback string `json:"callback,omitempty"`
	Auth     string `json:"auth,omitempty"`
	Priority int    `json:"priority,omitempty"`
}

type RegisteredMiddleware struct {
	Name       string `json:"name"`
	Callback   string `json:"callback,omitempty"`
	Stage      string `json:"stage"`
	PathPrefix string `json:"path_prefix,omitempty"`
	Priority   int    `json:"priority,omitempty"`
}

type RegisteredAdminPage struct {
	Slug     string `json:"slug"`
	Label    string `json:"label"`
	Callback string `json:"callback,omitempty"`
	Parent   string `json:"parent,omitempty"`
	Position int    `json:"position,omitempty"`
}

type RegisteredAsset struct {
	Path string `json:"path"`
	URL  string `json:"url,omitempty"`
}

type RegisteredTask struct {
	Name     string `json:"name"`
	Schedule string `json:"schedule"`
	Callback string `json:"callback,omitempty"`
}

type RegisteredMigration struct {
	ID         string   `json:"id"`
	Statements []string `json:"statements"`
}

type RegisteredDependency struct {
	PluginID   string `json:"plugin_id"`
	Constraint string `json:"constraint,omitempty"`
}

type pluginRegistration struct {
	PluginID string
	Value    ExtensionRegistration
}

func registrationFromManifest(manifest Manifest) ExtensionRegistration {
	if manifest.Registration != nil {
		registration := *manifest.Registration
		registration.Settings = append([]blog.ThemeSettingField{}, registration.Settings...)
		return registration
	}
	hooks := make([]RegisteredHook, 0, len(manifest.Hooks))
	for _, hook := range manifest.Hooks {
		hooks = append(hooks, RegisteredHook{
			Name: hook.Name, Callback: hook.Name, Kind: "filter", ID: hook.ID, Label: hook.Label,
		})
	}
	return ExtensionRegistration{Hooks: hooks, Settings: manifest.Settings}
}

func validateRegistration(registration ExtensionRegistration) error {
	counts := []int{
		len(registration.Hooks), len(registration.Routes), len(registration.Middleware),
		len(registration.AdminPages), len(registration.Settings), len(registration.Tasks),
		len(registration.Migrations), len(registration.Dependencies),
	}
	for _, count := range counts {
		if count > maxRegistrations {
			return errors.New("extension registration count exceeds limit")
		}
	}
	for _, hook := range registration.Hooks {
		if !validExtensionName(hook.Name) || !validOptionalCallback(hook.Callback) {
			return fmt.Errorf("invalid registered hook %q", hook.Name)
		}
		if hook.Kind != "" && hook.Kind != "action" && hook.Kind != "filter" {
			return fmt.Errorf("invalid registered hook kind %q", hook.Kind)
		}
	}
	for _, route := range registration.Routes {
		if !validMethod(route.Method) || !validDynamicRoutePath(route.Path) || !validOptionalCallback(route.Callback) {
			return fmt.Errorf("invalid registered route %s %q", route.Method, route.Path)
		}
		if route.Auth != "" && route.Auth != "public" && route.Auth != "user" && route.Auth != "admin" {
			return fmt.Errorf("invalid registered route auth %q", route.Auth)
		}
	}
	for _, middleware := range registration.Middleware {
		if !validExtensionName(middleware.Name) || !validOptionalCallback(middleware.Callback) {
			return fmt.Errorf("invalid registered middleware %q", middleware.Name)
		}
		if middleware.Stage != "before" && middleware.Stage != "after" {
			return fmt.Errorf("invalid middleware stage %q", middleware.Stage)
		}
		if middleware.PathPrefix != "" && !validDynamicRoutePath(middleware.PathPrefix) {
			return fmt.Errorf("invalid middleware path prefix %q", middleware.PathPrefix)
		}
	}
	for _, page := range registration.AdminPages {
		if !pluginIDPattern.MatchString(page.Slug) || !boundedText(page.Label, 1, 128) || !validOptionalCallback(page.Callback) {
			return fmt.Errorf("invalid registered admin page %q", page.Slug)
		}
	}
	for _, asset := range registration.Assets {
		if !validPluginFilePath(asset.Path) || asset.Path == "manifest.json" || strings.HasPrefix(asset.Path, "../") {
			return fmt.Errorf("invalid registered asset %q", asset.Path)
		}
	}
	if err := blog.ValidateSettingFields(registration.Settings); err != nil {
		return fmt.Errorf("registered settings: %w", err)
	}
	for _, task := range registration.Tasks {
		if !validExtensionName(task.Name) || strings.TrimSpace(task.Schedule) == "" || !validOptionalCallback(task.Callback) {
			return fmt.Errorf("invalid registered task %q", task.Name)
		}
	}
	for _, migration := range registration.Migrations {
		if !validExtensionName(migration.ID) || len(migration.Statements) == 0 || len(migration.Statements) > 128 {
			return fmt.Errorf("invalid registered migration %q", migration.ID)
		}
		for _, statement := range migration.Statements {
			if strings.TrimSpace(statement) == "" || len(statement) > MaxRequestBytes {
				return fmt.Errorf("invalid statement in migration %q", migration.ID)
			}
		}
	}
	for _, dependency := range registration.Dependencies {
		if !pluginIDPattern.MatchString(dependency.PluginID) || dependency.PluginID == "" {
			return fmt.Errorf("invalid plugin dependency %q", dependency.PluginID)
		}
	}
	for _, callback := range []string{
		registration.Lifecycle.Activate,
		registration.Lifecycle.Deactivate,
		registration.Lifecycle.Upgrade,
		registration.Lifecycle.Uninstall,
	} {
		if !validOptionalCallback(callback) {
			return fmt.Errorf("invalid lifecycle callback %q", callback)
		}
	}
	return nil
}

func validExtensionName(name string) bool {
	return extensionNamePattern.MatchString(name)
}

func validOptionalCallback(callback string) bool {
	return callback == "" || validExtensionName(callback)
}

func validDynamicRoutePath(routePath string) bool {
	return strings.HasPrefix(routePath, "/") &&
		len(routePath) <= 1024 &&
		!strings.ContainsRune(routePath, '\x00') &&
		!strings.Contains(routePath, "\\") &&
		!strings.Contains(routePath, "..") &&
		!strings.Contains(routePath, "//")
}
