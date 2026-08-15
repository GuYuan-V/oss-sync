package blog

import (
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
)

const (
	customTemplateFile = "template.html"
	maxTemplateSize    = 1 << 20 // 1 MiB is ample for a page layout.
)

var themeNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// ValidateThemeName limits theme names to one portable directory component.
// It is used for both disk access and the public asset URL.
func ValidateThemeName(name string) error {
	if !themeNamePattern.MatchString(name) {
		return errors.New("主题名称只能使用字母、数字、连字符和下划线，且长度为 1–64")
	}
	return nil
}

func themeDirectory(dataDir, themeName string) (string, error) {
	if err := ValidateThemeName(themeName); err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "themes", themeName), nil
}

// CustomThemeExists reports whether a theme has a renderable layout.
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

// CreateDevelopmentTheme copies the bundled starter into data/themes/<name>.
// It deliberately refuses to replace an existing directory, so a template
// being edited by an administrator can never be overwritten by the console.
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
	return template.New("custom-theme").Option("missingkey=error").Parse(string(raw))
}
