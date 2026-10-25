package serverplugin

import (
	"embed"
	"encoding/json"

	"github.com/helantianshen/oss-sync/internal/blog"
)

//go:embed assets/papertrail-settings.json assets/sakura-settings.json assets/glass-settings.json assets/starry-night-settings.json
var builtinAssets embed.FS

// builtinPluginDefs 内置设置插件：ID、显示名与绑定的博客模板
var builtinPluginDefs = []struct {
	ID       string
	Name     string
	Asset    string
	Template string
}{
	{"papertrail-settings", "纸迹", "assets/papertrail-settings.json", "papertrail"},
	{"sakura-settings", "樱坂", "assets/sakura-settings.json", "sakura"},
	{"glass-settings", "琉璃", "assets/glass-settings.json", "glass"},
	{"starry-night-settings", "星夜", "assets/starry-night-settings.json", "starry-night"},
}

// BuiltinManifests 返回服务端内置的宿主插件
func BuiltinManifests() []Manifest {
	manifests := make([]Manifest, 0, len(builtinPluginDefs))
	for _, def := range builtinPluginDefs {
		settings := []blog.ThemeSettingField{}
		if raw, err := builtinAssets.ReadFile(def.Asset); err == nil {
			var document struct {
				Settings []blog.ThemeSettingField `json:"settings"`
			}
			if json.Unmarshal(raw, &document) == nil {
				settings = document.Settings
			}
		}
		manifests = append(manifests, Manifest{
			ID: def.ID, Name: def.Name, Version: "1.0.0", APIVersion: CurrentAPIVersion,
			Routes: []RouteSpec{{Method: "GET", Path: "/settings", Public: false}}, Settings: settings,
		})
	}
	return manifests
}
