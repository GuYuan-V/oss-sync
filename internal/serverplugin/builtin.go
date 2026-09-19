package serverplugin

import (
	"embed"
	"encoding/json"

	"github.com/helantianshen/oss-sync/internal/blog"
)

//go:embed assets/papertrail-settings.json
var builtinAssets embed.FS

// BuiltinManifests returns host-owned plugins shipped with the server.
func BuiltinManifests() []Manifest {
	settings := []blog.ThemeSettingField{}
	if raw, err := builtinAssets.ReadFile("assets/papertrail-settings.json"); err == nil {
		var document struct {
			Settings []blog.ThemeSettingField `json:"settings"`
		}
		if json.Unmarshal(raw, &document) == nil {
			settings = document.Settings
		}
	}
	return []Manifest{{
		ID: "papertrail-settings", Name: "Papertrail", Version: "1.0.0", APIVersion: CurrentAPIVersion,
		Routes: []RouteSpec{{Method: "GET", Path: "/settings", Public: false}}, Settings: settings,
	}}
}
