package webui

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/oss/oss-server/internal/blog"
)

var templateTranslationPattern = regexp.MustCompile(`\{\{-?\s*\.Layout\.T\s+"([a-zA-Z0-9_.]+)"`)
var goHandlerTranslationPattern = regexp.MustCompile(`h\.t\(c,\s*"([a-zA-Z0-9_.]+)"`)

type literalKeyReference struct {
	Path string
	Key  string
}

func extractLiteralKeys(sourceFS fs.FS, root, extension string, pattern *regexp.Regexp) ([]literalKeyReference, error) {
	var references []literalKeyReference
	err := fs.WalkDir(sourceFS, root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != extension {
			return nil
		}
		raw, err := fs.ReadFile(sourceFS, path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		for _, match := range pattern.FindAllSubmatch(raw, -1) {
			references = append(references, literalKeyReference{Path: path, Key: string(match[1])})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan %s: %w", root, err)
	}
	return references, nil
}

func TestTemplates_whenTranslationKeyLiteralUsed_hasCatalogEntry(t *testing.T) {
	references, err := extractLiteralKeys(webFS, "templates", ".html", templateTranslationPattern)
	if err != nil {
		t.Fatal(err)
	}

	for _, reference := range references {
		if _, exists := localeEntries[reference.Key]; !exists {
			t.Errorf("%s references unregistered locale key %q", reference.Path, reference.Key)
		}
	}
}

func TestTemplates_whenRenderedInSupportedLanguages_executeWithoutRawLocaleKeys(t *testing.T) {
	funcs := template.FuncMap{
		"formatBytes": formatBytes,
		"timeFmt": func(value time.Time) string {
			if value.IsZero() {
				return "-"
			}
			return value.Local().Format("2006-01-02 15:04")
		},
		"sub":      func(a, b int) int { return a - b },
		"urlquery": url.QueryEscape,
	}
	tpl, err := template.New("x").Funcs(funcs).Option("missingkey=default").ParseFS(
		webFS,
		"templates/*.html",
		"templates/partials/*.html",
	)
	if err != nil {
		t.Fatalf("parse web UI templates: %v", err)
	}

	names := make([]string, 0, len(tpl.Templates()))
	for _, parsed := range tpl.Templates() {
		if parsed.Tree == nil || parsed.Tree.Root == nil || len(parsed.Tree.Root.Nodes) == 0 {
			continue
		}
		names = append(names, parsed.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		for _, lang := range Languages() {
			t.Run(name+"/"+lang, func(t *testing.T) {
				pageData := struct {
					Layout layoutData
					Data   map[string]any
				}{
					Layout: layoutData{Language: lang},
					Data:   templateSmokeFixture(),
				}
				var rendered bytes.Buffer
				if err := tpl.ExecuteTemplate(&rendered, name, pageData); err != nil {
					t.Fatalf("execute template %q in %s: %v", name, lang, err)
				}
				assertNoRawLocaleKey(t, rendered.String())
			})
		}
	}
}

func TestVaultThemeSettingsTemplate_whenPapertrail_hasLivePreviewHooks(t *testing.T) {
	funcs := template.FuncMap{
		"formatBytes": formatBytes,
		"timeFmt": func(value time.Time) string {
			if value.IsZero() {
				return "-"
			}
			return value.Local().Format("2006-01-02 15:04")
		},
		"sub":      func(a, b int) int { return a - b },
		"urlquery": url.QueryEscape,
	}
	tpl, err := template.New("x").Funcs(funcs).ParseFS(
		webFS,
		"templates/*.html",
		"templates/partials/*.html",
	)
	if err != nil {
		t.Fatalf("parse papertrail template: %v", err)
	}

	data := themeSettingsData{
		VaultID:       "vault-1",
		VaultName:     "Test Vault",
		ThemeName:     "papertrail",
		SettingsLabel: "papertrail",
		Fields: []themeSettingFieldView{
			{
				Schema: blog.ThemeSettingField{Key: "logo_url", Label: "Logo", Type: "url", MaxLength: 2000},
				Value:  "https://example.com/logo.png",
			},
			{
				Schema: blog.ThemeSettingField{Key: "blog_name", Label: "Name", Type: "text", MaxLength: 64},
				Value:  "Original Name",
			},
			{
				Schema: blog.ThemeSettingField{Key: "description", Label: "Description", Type: "textarea", MaxLength: 120},
				Value:  "Original description",
			},
			{
				Schema: blog.ThemeSettingField{
					Key:      "buttons",
					Label:    "Buttons",
					Type:     "group",
					MaxItems: 3,
					Fields: []blog.ThemeSettingField{
						{Key: "label", Label: "Label", Type: "text", MaxLength: 20},
						{Key: "url", Label: "URL", Type: "url", MaxLength: 2000},
						{Key: "icon_url", Label: "Icon", Type: "url", MaxLength: 2000},
					},
				},
				Rows: []themeSettingRowView{{Values: map[string]string{
					"label":    "Docs",
					"url":      "https://example.com/docs",
					"icon_url": "https://example.com/icon.png",
				}}},
			},
		},
	}

	pageData := struct {
		Layout layoutData
		Data   themeSettingsData
	}{
		Layout: layoutData{Language: defaultWebLanguage},
		Data:   data,
	}

	var rendered bytes.Buffer
	if err := tpl.ExecuteTemplate(&rendered, "vault-theme-settings", pageData); err != nil {
		t.Fatalf("execute vault theme settings template: %v", err)
	}

	out := rendered.String()
	for _, expected := range []string{
		"data-papertrail-preview",
		"data-papertrail-home-links",
		"data-preview-name",
		"data-preview-description",
		"data-preview-logo",
		"data-preview-field=\"blog_name\"",
		"data-theme-setting-group-key=\"buttons\"",
		"data-group-key=\"buttons\"",
		"data-group-field-key=\"label\"",
		"data-group-field-key=\"url\"",
	} {
		if !strings.Contains(out, expected) {
			t.Fatalf("rendered template missing hook %q", expected)
		}
	}
}

func TestVaultSettingsTemplate_whenRendered_showsHeaderFooterGuidanceAndHints(t *testing.T) {
	tpl, err := template.New("x").ParseFS(
		webFS,
		"templates/vault_settings.html",
	)
	if err != nil {
		t.Fatalf("parse vault settings template: %v", err)
	}

	data := vaultSettingsData{
		VaultID:                "vault-1",
		VaultName:              "Vault One",
		ThemeName:              "default",
		RecycleBinDays:         30,
		DefaultRecycleDays:     30,
		IsPublicBlog:           true,
		CustomFragmentsEnabled: true,
		CanManage:              true,
		CustomHeader:           "# Header",
		CustomFooter:           "# Footer",
	}

	pageData := struct {
		Layout layoutData
		Data   vaultSettingsData
	}{
		Layout: layoutData{Language: defaultWebLanguage},
		Data:   data,
	}

	var rendered bytes.Buffer
	if err := tpl.ExecuteTemplate(&rendered, "vault-settings", pageData); err != nil {
		t.Fatalf("render vault settings template: %v", err)
	}

	page := rendered.String()
	wantFragments := []string{
		translate(defaultWebLanguage, "vault.custom_fragments_desc"),
		translate(defaultWebLanguage, "vault.custom_header_hint"),
		translate(defaultWebLanguage, "vault.custom_footer_hint"),
	}
	for _, want := range wantFragments {
		if !strings.Contains(page, want) {
			t.Fatalf("rendered vault settings template missing guidance %q", want)
		}
	}
}

func templateSmokeFixture() map[string]any {
	empty := []any{}
	return map[string]any{
		"Backups":               empty,
		"Breadcrumbs":           empty,
		"ConsoleThemes":         empty,
		"CreatedAt":             time.Time{},
		"Devices":               empty,
		"Diff":                  empty,
		"Fields":                empty,
		"Files":                 empty,
		"Filters":               map[string]any{"Action": "", "Username": "", "Device": "", "From": "", "To": ""},
		"Folders":               empty,
		"History":               empty,
		"MaxLongPollWaitSec":    1,
		"LongPollWaitSec":       1,
		"MaxSyncDebounceSec":    3,
		"SyncDebounceSec":       3,
		"MaxRecycleBinDays":     1,
		"DefaultRecycleBinDays": 1,
		"MaxVaultStorageMB":     0,
		"VaultStorageMB":        0,
		"MaxUploadSizeMB":       1,
		"MemoryBytes":           int64(0),
		"MemoryTotalBytes":      int64(0),
		"Metrics": map[string]any{
			"CPUUsagePercent":    0.0,
			"MemoryUsedBytes":    int64(0),
			"MemoryTotalBytes":   int64(0),
			"MemoryUsagePercent": 0.0,
			"DiskUsedBytes":      int64(0),
			"DiskTotalBytes":     int64(0),
			"ProjectStorageUsed": int64(0),
			"ProjectStorageMax":  int64(0),
		},
		"Members":               empty,
		"RecentDevices":         empty,
		"RecentHistory":         empty,
		"RegistrationEnabled":   true,
		"Shares":                empty,
		"StorageUsed":           int64(0),
		"StorageQuota":          int64(0),
		"UserCount":             int64(0),
		"VaultCount":            int64(0),
		"DeviceCount":           int64(0),
		"VaultStorageUsed":      int64(0),
		"SyncMode":              "user_choice",
		"Themes":                empty,
		"Users":                 empty,
		"Vaults":                empty,
		"WebLanguage":           defaultWebLanguage,
		"ConfigMaxUploadSizeMB": 1,
	}
}

func assertNoRawLocaleKey(t *testing.T, output string) {
	t.Helper()
	keyPattern := regexp.MustCompile(`[a-z]+(?:\.[a-z_]+)+`)
	for _, candidate := range keyPattern.FindAllString(output, -1) {
		if _, exists := localeEntries[candidate]; exists {
			t.Errorf("rendered output leaked raw locale key %q", candidate)
		}
	}
	for key := range localeEntries {
		if strings.Contains(output, key) {
			t.Errorf("rendered output leaked raw locale key %q", key)
		}
	}
}

func TestHandlers_whenTranslationKeyLiteralUsed_hasCatalogEntry(t *testing.T) {
	pkgDir := "."
	pattern := goHandlerTranslationPattern
	var refs []literalKeyReference
	err := filepath.WalkDir(pkgDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("read %s: %w", path, readErr)
		}
		for _, match := range pattern.FindAllSubmatch(raw, -1) {
			refs = append(refs, literalKeyReference{Path: path, Key: string(match[1])})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range refs {
		if _, exists := localeEntries[ref.Key]; !exists {
			t.Errorf("%s references unregistered locale key %q", ref.Path, ref.Key)
		}
	}
}
