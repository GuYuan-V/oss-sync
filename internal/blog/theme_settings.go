package blog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	maxThemeSettingsBytes = 64 << 10
	maxThemeSettingFields = 64
	maxThemeGroupFields   = 16
	maxThemeGroupItems    = 20
	maxThemeSettingLength = 2000
)

var themeSettingKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// ThemeSettingField declares one administrator-editable setting owned by a theme.
type ThemeSettingField struct {
	Key         string              `json:"key"`
	Label       string              `json:"label"`
	Type        string              `json:"type"`
	Placeholder string              `json:"placeholder,omitempty"`
	MaxLength   int                 `json:"max_length,omitempty"`
	Required    bool                `json:"required,omitempty"`
	MaxItems    int                 `json:"max_items,omitempty"`
	Choices     []string            `json:"choices,omitempty"`
	Fields      []ThemeSettingField `json:"fields,omitempty"`
}

type themeSettingsDocument struct {
	Settings []ThemeSettingField `json:"settings"`
}

// ThemeSettings loads and validates the optional settings.json owned by a theme.
func ThemeSettings(dataDir, themeName string) ([]ThemeSettingField, error) {
	if err := ValidateThemeName(themeName); err != nil {
		return nil, err
	}
	var raw []byte
	var err error
	if IsBuiltinTheme(themeName) {
		raw, err = themeAssetsFS.ReadFile("assets/" + themeName + "/settings.json")
	} else {
		dir, dirErr := themeDirectory(dataDir, themeName)
		if dirErr != nil {
			return nil, dirErr
		}
		path := filepath.Join(dir, "settings.json")
		info, statErr := os.Stat(path)
		if errors.Is(statErr, fs.ErrNotExist) {
			return []ThemeSettingField{}, nil
		}
		if statErr != nil {
			return nil, fmt.Errorf("读取模板设置声明: %w", statErr)
		}
		if !info.Mode().IsRegular() || info.Size() > maxThemeSettingsBytes {
			return nil, errors.New("模板设置声明不是常规文件或超过 64 KiB")
		}
		raw, err = os.ReadFile(path)
	}
	if errors.Is(err, fs.ErrNotExist) {
		return []ThemeSettingField{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取模板设置声明: %w", err)
	}
	if len(raw) > maxThemeSettingsBytes {
		return nil, errors.New("模板设置声明超过 64 KiB")
	}
	return parseThemeSettings(raw)
}

func parseThemeSettings(raw []byte) ([]ThemeSettingField, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var document themeSettingsDocument
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("解析模板设置声明: %w", err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return nil, err
	}
	if len(document.Settings) > maxThemeSettingFields {
		return nil, fmt.Errorf("模板设置字段不能超过 %d 个", maxThemeSettingFields)
	}
	if err := validateThemeSettingFields(document.Settings, false); err != nil {
		return nil, err
	}
	return document.Settings, nil
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("模板设置声明只能包含一个 JSON 对象")
		}
		return fmt.Errorf("解析模板设置声明: %w", err)
	}
	return nil
}

func validateThemeSettingFields(fields []ThemeSettingField, nested bool) error {
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if !themeSettingKeyPattern.MatchString(field.Key) {
			return fmt.Errorf("模板设置字段 key 不合法: %s", field.Key)
		}
		if _, exists := seen[field.Key]; exists {
			return fmt.Errorf("模板设置字段 key 重复: %s", field.Key)
		}
		seen[field.Key] = struct{}{}
		if strings.TrimSpace(field.Label) == "" {
			return fmt.Errorf("模板设置字段缺少 label: %s", field.Key)
		}
		switch field.Type {
		case "text", "textarea", "url":
			if field.MaxLength < 1 || field.MaxLength > maxThemeSettingLength {
				return fmt.Errorf("模板设置字段 max_length 不合法: %s", field.Key)
			}
			if len(field.Fields) != 0 || field.MaxItems != 0 {
				return fmt.Errorf("普通模板设置字段不能声明子字段: %s", field.Key)
			}
		case "choice":
			if len(field.Choices) < 2 || len(field.Choices) > 16 || field.MaxLength != 0 || len(field.Fields) != 0 || field.MaxItems != 0 {
				return fmt.Errorf("模板设置选择字段不合法: %s", field.Key)
			}
			choices := make(map[string]struct{}, len(field.Choices))
			for _, choice := range field.Choices {
				if !themeSettingKeyPattern.MatchString(choice) {
					return fmt.Errorf("模板设置选择值不合法: %s", choice)
				}
				if _, exists := choices[choice]; exists {
					return fmt.Errorf("模板设置选择值重复: %s", choice)
				}
				choices[choice] = struct{}{}
			}
		case "group":
			if nested {
				return fmt.Errorf("模板设置不支持嵌套 group: %s", field.Key)
			}
			if field.MaxItems < 1 || field.MaxItems > maxThemeGroupItems {
				return fmt.Errorf("模板设置 group max_items 不合法: %s", field.Key)
			}
			if len(field.Fields) == 0 || len(field.Fields) > maxThemeGroupFields {
				return fmt.Errorf("模板设置 group 子字段数量不合法: %s", field.Key)
			}
			if err := validateThemeSettingFields(field.Fields, true); err != nil {
				return err
			}
		default:
			return fmt.Errorf("模板设置字段类型不支持: %s", field.Type)
		}
	}
	return nil
}

// ValidateThemeConfig parses untrusted values into the shape declared by settings.json.
func ValidateThemeConfig(fields []ThemeSettingField, raw map[string]any) (map[string]any, error) {
	clean := make(map[string]any, len(fields))
	for _, field := range fields {
		if field.Type == "group" {
			rows, err := validateThemeGroup(field, raw[field.Key])
			if err != nil {
				return nil, err
			}
			clean[field.Key] = rows
			continue
		}
		value, err := validateThemeScalar(field, raw[field.Key])
		if err != nil {
			return nil, err
		}
		clean[field.Key] = value
	}
	return clean, nil
}

func validateThemeScalar(field ThemeSettingField, raw any) (string, error) {
	value := ""
	if raw != nil {
		text, ok := raw.(string)
		if !ok {
			return "", fmt.Errorf("模板设置 %s 必须是文本", field.Label)
		}
		value = strings.TrimSpace(text)
	}
	if field.Required && value == "" {
		return "", fmt.Errorf("模板设置 %s 不能为空", field.Label)
	}
	if field.Type == "choice" {
		if value == "" {
			return field.Choices[0], nil
		}
		for _, choice := range field.Choices {
			if value == choice {
				return value, nil
			}
		}
		return "", fmt.Errorf("模板设置 %s 包含不支持的选项", field.Label)
	}
	if field.Type != "choice" && utf8.RuneCountInString(value) > field.MaxLength {
		return "", fmt.Errorf("模板设置 %s 超过长度限制", field.Label)
	}
	if field.Type == "url" && value != "" && !ValidPublicURL(value) {
		return "", fmt.Errorf("模板设置 %s 必须是 http(s) 或站内相对 URL", field.Label)
	}
	return value, nil
}

func validateThemeGroup(field ThemeSettingField, raw any) ([]any, error) {
	if raw == nil {
		return []any{}, nil
	}
	rows, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("模板设置 %s 必须是列表", field.Label)
	}
	if len(rows) > field.MaxItems {
		rows = rows[:field.MaxItems]
	}
	clean := make([]any, 0, len(rows))
	for _, rawRow := range rows {
		row, ok := rawRow.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("模板设置 %s 包含无效条目", field.Label)
		}
		parsed := make(map[string]any, len(field.Fields)+1)
		complete := true
		for _, child := range field.Fields {
			rawValue, exists := row[child.Key]
			if child.Required {
				text, isText := rawValue.(string)
				if !exists || !isText || strings.TrimSpace(text) == "" {
					complete = false
					break
				}
			}
			value, err := validateThemeScalar(child, rawValue)
			if err != nil {
				return nil, err
			}
			parsed[child.Key] = value
		}
		if !complete {
			continue
		}
		parsed["position"] = len(clean) + 1
		clean = append(clean, parsed)
	}
	return clean, nil
}

// ValidPublicURL accepts absolute HTTP(S) URLs and root-relative local URLs.
func ValidPublicURL(raw string) bool {
	if strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//") {
		return true
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}
