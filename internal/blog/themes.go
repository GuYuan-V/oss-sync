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
)

type themeMetadata struct {
	SupportsPublicBlog bool `json:"supports_public_blog"`
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

// CreateDevelopmentTheme 把内置起始模板复制到 data/themes/<name>；已存在目录一律拒绝覆盖，管理员正在编辑的模板不会被控制台改写
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
