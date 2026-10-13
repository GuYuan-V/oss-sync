package blog

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/helantianshen/oss-sync/internal/models"
)

type themeMetadata struct {
	SupportsPublicBlog bool     `json:"supports_public_blog"`
	PublicSettings     []string `json:"public_settings"`
}

// SupportsPublicBlog 判断主题是否显式支持公开博客渲染
func SupportsPublicBlog(dataDir, themeName string) bool {
	if IsBuiltinTheme(themeName) {
		return themeName == "papertrail"
	}
	dir, err := themeDirectory(dataDir, themeName)
	if err != nil {
		return false
	}
	raw, err := os.ReadFile(filepath.Join(dir, "theme.json"))
	if err != nil {
		return CustomThemeExists(dataDir, themeName)
	}
	var metadata themeMetadata
	return json.Unmarshal(raw, &metadata) == nil && metadata.SupportsPublicBlog
}

func themePluginID(dataDir, themeName string) string {
	if IsBuiltinTheme(themeName) {
		return ""
	}
	dir, err := themeDirectory(dataDir, themeName)
	if err != nil {
		return ""
	}
	marker, err := readPluginThemeMarker(dir)
	if err != nil {
		return ""
	}
	return marker.PluginID
}

func publicThemeSettings(dataDir, themeName string) []string {
	if IsBuiltinTheme(themeName) {
		return nil
	}
	dir, err := themeDirectory(dataDir, themeName)
	if err != nil {
		return nil
	}
	raw, err := os.ReadFile(filepath.Join(dir, "theme.json"))
	if err != nil {
		return nil
	}
	var metadata themeMetadata
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return nil
	}
	if !metadata.SupportsPublicBlog {
		return nil
	}
	seen := make(map[string]struct{}, len(metadata.PublicSettings))
	keys := make([]string, 0, len(metadata.PublicSettings))
	for _, key := range metadata.PublicSettings {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	return keys
}

// filterBuiltinThemeConfig 按内置主题声明的公开键过滤 base 配置
// papertrail 的设置历史上直接写在 VaultSetting.ThemeConfig 上，
// 该字段与主题无关，切换到其他内置主题时必须剔除不属于它的键
func (h *Handler) filterBuiltinThemeConfig(themeName string, base models.JSONMap) models.JSONMap {
	if len(base) == 0 || h.DB == nil {
		return base
	}
	var plugin models.ServerPlugin
	if err := h.DB.Where("id = ? AND enabled = ?", "papertrail-settings", true).First(&plugin).Error; err != nil {
		return base
	}
	var manifest struct {
		Settings []struct {
			Key string `json:"key"`
		} `json:"settings"`
		Registration struct {
			Settings []struct {
				Key string `json:"key"`
			} `json:"settings"`
		} `json:"registration"`
	}
	if err := json.Unmarshal([]byte(plugin.ManifestJSON), &manifest); err != nil {
		return base
	}
	declared := make(map[string]struct{}, len(manifest.Settings)+len(manifest.Registration.Settings))
	for _, field := range manifest.Settings {
		declared[field.Key] = struct{}{}
	}
	for _, field := range manifest.Registration.Settings {
		declared[field.Key] = struct{}{}
	}
	if len(declared) == 0 {
		return base
	}
	filtered := make(models.JSONMap, len(base))
	for key, value := range base {
		if _, ok := declared[key]; ok {
			filtered[key] = value
		}
	}
	return filtered
}

func (h *Handler) publicThemeConfig(vaultID, themeName string, base models.JSONMap) models.JSONMap {
	// 内置主题同样要按自身声明键过滤 base：
	// VaultSetting.ThemeConfig 是 vault 级字段，可能残留其他主题写入的配置
	pluginID := themePluginID(h.Cfg.Storage.DataDir, themeName)
	if pluginID == "" {
		return h.filterBuiltinThemeConfig(themeName, base)
	}
	if h.DB == nil {
		return base
	}
	publicSettings := publicThemeSettings(h.Cfg.Storage.DataDir, themeName)
	if len(publicSettings) == 0 {
		return base
	}
	var plugin models.ServerPlugin
	if err := h.DB.Where("id = ? AND enabled = ?", pluginID, true).First(&plugin).Error; err != nil {
		return base
	}
	var manifest struct {
		Settings []struct {
			Key string `json:"key"`
		} `json:"settings"`
		Registration struct {
			Settings []struct {
				Key string `json:"key"`
			} `json:"settings"`
		} `json:"registration"`
		BlogThemes []struct {
			ID string `json:"id"`
		} `json:"blog_themes"`
	}
	if err := json.Unmarshal([]byte(plugin.ManifestJSON), &manifest); err != nil {
		return base
	}
	prefix := pluginID + "--"
	if !strings.HasPrefix(themeName, prefix) {
		return base
	}
	resourceID := strings.TrimPrefix(themeName, prefix)
	declaredResource := false
	for _, resource := range manifest.BlogThemes {
		if resource.ID == resourceID {
			declaredResource = true
			break
		}
	}
	if !declaredResource {
		return base
	}
	var setting models.VaultPluginSetting
	if err := h.DB.Where("vault_id = ? AND plugin_id = ?", vaultID, pluginID).First(&setting).Error; err != nil {
		return base
	}
	declaredSettings := make(map[string]struct{}, len(manifest.Settings)+len(manifest.Registration.Settings))
	for _, field := range manifest.Settings {
		declaredSettings[field.Key] = struct{}{}
	}
	for _, field := range manifest.Registration.Settings {
		declaredSettings[field.Key] = struct{}{}
	}
	var config models.JSONMap
	for _, key := range publicSettings {
		if _, ok := declaredSettings[key]; !ok {
			continue
		}
		if value, ok := setting.Config[key]; ok {
			if config == nil {
				config = make(models.JSONMap, len(base)+len(publicSettings))
				for baseKey, baseValue := range base {
					config[baseKey] = baseValue
				}
			}
			config[key] = value
		}
	}
	if config != nil {
		return config
	}
	return base
}

const (
	customTemplateFile = "template.html"
	maxTemplateSize    = 1 << 20 // 页面布局模板上限为 1 MiB
)

// ValidateThemeName 将主题名称限制为单个可移植目录组件，同时适用于磁盘访问与公开资源 URL
func ValidateThemeName(name string) error {
	if !validThemeName(name) {
		return errors.New("主题名称只能使用字母、数字、连字符和下划线，且长度为 1–64")
	}
	return nil
}

func validThemeName(name string) bool {
	if name == "" || utf8.RuneCountInString(name) > 64 || strings.TrimSpace(name) != name {
		return false
	}
	for index, r := range name {
		if index == 0 {
			if !unicode.IsLetter(r) && !unicode.IsNumber(r) {
				return false
			}
			continue
		}
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '-' && r != '_' {
			return false
		}
	}
	return true
}

func themeDirectory(dataDir, themeName string) (string, error) {
	if err := ValidateThemeName(themeName); err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "themes", themeName), nil
}

// CustomThemeExists 判断主题是否具备可渲染的布局模板
func CustomThemeExists(dataDir, themeName string) bool {
	if themeName == "default" {
		return true
	}
	dir, err := themeDirectory(dataDir, themeName)
	if err != nil {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, customTemplateFile))
	return err == nil && info.Mode().IsRegular()
}

// CreateDevelopmentTheme 复制内置模板；目标目录已存在时拒绝覆盖
func CreateDevelopmentTheme(dataDir, themeName string) (string, error) {
	if themeName == "default" {
		return "", errors.New("default 是内置主题，不能覆盖")
	}
	dir, err := themeDirectory(dataDir, themeName)
	if err != nil {
		return "", err
	}
	root := filepath.Dir(dir)
	if err := os.MkdirAll(root, 0o750); err != nil {
		return "", fmt.Errorf("创建主题根目录: %w", err)
	}
	if err := os.Mkdir(dir, 0o750); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return "", errors.New("该主题目录已经存在；为避免覆盖，请换一个名称或直接编辑现有目录")
		}
		return "", fmt.Errorf("创建主题目录: %w", err)
	}

	entries, err := themeAssetsFS.ReadDir("assets/development-template")
	if err != nil {
		return "", fmt.Errorf("读取内置开发模板: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		content, err := themeAssetsFS.ReadFile("assets/development-template/" + entry.Name())
		if err != nil {
			return "", fmt.Errorf("读取内置模板文件: %w", err)
		}
		if err := os.WriteFile(filepath.Join(dir, entry.Name()), content, 0o640); err != nil {
			return "", fmt.Errorf("写入开发模板: %w", err)
		}
	}
	return dir, nil
}

func (h *Handler) customThemeTemplate(themeName string) (*template.Template, error) {
	if themeName == "default" {
		return nil, errors.New("default uses the built-in layout")
	}
	dir, err := themeDirectory(h.Cfg.Storage.DataDir, themeName)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, customTemplateFile)
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxTemplateSize {
		return nil, errors.New("主题模板不是常规文件或文件过大")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// missingkey=zero 让缺失的 map 键（ThemeConfig、PluginData 子键）退化为零值而非整页失败；
	// 结构体字段缺失仍会报错，保留字段契约校验能力
	return template.New("custom-theme").Funcs(customThemeFuncs()).Option("missingkey=zero").Parse(string(raw))
}
