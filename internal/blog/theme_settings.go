// Shared plugin-setting validation.
package blog

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	maxThemeSettingFields = 64
	maxThemeGroupFields   = 16
	maxThemeGroupItems    = 20
	maxThemeSettingLength = 2000
)

var themeSettingKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// ThemeSettingField declares one administrator-editable plugin setting field.
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

func validateThemeSettingFields(fields []ThemeSettingField, nested bool) error {
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if !themeSettingKeyPattern.MatchString(field.Key) {
			return fmt.Errorf("设置字段 key 不合法: %s", field.Key)
		}
		if _, exists := seen[field.Key]; exists {
			return fmt.Errorf("设置字段 key 重复: %s", field.Key)
		}
		seen[field.Key] = struct{}{}
		if strings.TrimSpace(field.Label) == "" {
			return fmt.Errorf("设置字段缺少 label: %s", field.Key)
		}
		switch field.Type {
		case "text", "textarea", "url":
			if field.MaxLength < 1 || field.MaxLength > maxThemeSettingLength {
				return fmt.Errorf("设置字段 max_length 不合法: %s", field.Key)
			}
			if len(field.Fields) != 0 || field.MaxItems != 0 {
				return fmt.Errorf("普通设置字段不能声明子字段: %s", field.Key)
			}
		case "choice":
			if len(field.Choices) < 2 || len(field.Choices) > 16 || field.MaxLength != 0 || len(field.Fields) != 0 || field.MaxItems != 0 {
				return fmt.Errorf("设置选择字段不合法: %s", field.Key)
			}
			choices := make(map[string]struct{}, len(field.Choices))
			for _, choice := range field.Choices {
				if !themeSettingKeyPattern.MatchString(choice) {
					return fmt.Errorf("设置选择值不合法: %s", choice)
				}
				if _, exists := choices[choice]; exists {
					return fmt.Errorf("设置选择值重复: %s", choice)
				}
				choices[choice] = struct{}{}
			}
		case "group":
			if nested {
				return fmt.Errorf("设置不支持嵌套 group: %s", field.Key)
			}
			if field.MaxItems < 1 || field.MaxItems > maxThemeGroupItems {
				return fmt.Errorf("设置 group max_items 不合法: %s", field.Key)
			}
			if len(field.Fields) == 0 || len(field.Fields) > maxThemeGroupFields {
				return fmt.Errorf("设置 group 子字段数量不合法: %s", field.Key)
			}
			if err := validateThemeSettingFields(field.Fields, true); err != nil {
				return err
			}
		default:
			return fmt.Errorf("设置字段类型不支持: %s", field.Type)
		}
	}
	return nil
}

// ValidateThemeConfig validates plugin-owned settings using the shared field contract.
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

// ValidateSettingFields validates a plugin host-rendered settings declaration.
func ValidateSettingFields(fields []ThemeSettingField) error {
	if len(fields) > maxThemeSettingFields {
		return fmt.Errorf("设置字段不能超过 %d 个", maxThemeSettingFields)
	}
	return validateThemeSettingFields(fields, false)
}

// ValidateSettingConfig sanitizes plugin values against a host-rendered declaration.
func ValidateSettingConfig(fields []ThemeSettingField, raw map[string]any) (map[string]any, error) {
	return ValidateThemeConfig(fields, raw)
}

func validateThemeScalar(field ThemeSettingField, raw any) (string, error) {
	value := ""
	if raw != nil {
		text, ok := raw.(string)
		if !ok {
			return "", fmt.Errorf("设置 %s 必须是文本", field.Label)
		}
		value = strings.TrimSpace(text)
	}
	if field.Required && value == "" {
		return "", fmt.Errorf("设置 %s 不能为空", field.Label)
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
		return "", fmt.Errorf("设置 %s 包含不支持的选项", field.Label)
	}
	if field.Type != "choice" && utf8.RuneCountInString(value) > field.MaxLength {
		return "", fmt.Errorf("设置 %s 超过长度限制", field.Label)
	}
	if field.Type == "url" && value != "" && !ValidPublicURL(value) {
		return "", fmt.Errorf("设置 %s 必须是 http(s) 或站内相对 URL", field.Label)
	}
	return value, nil
}

func validateThemeGroup(field ThemeSettingField, raw any) ([]any, error) {
	if raw == nil {
		return []any{}, nil
	}
	rows, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("设置 %s 必须是列表", field.Label)
	}
	if len(rows) > field.MaxItems {
		rows = rows[:field.MaxItems]
	}
	clean := make([]any, 0, len(rows))
	for _, rawRow := range rows {
		row, ok := rawRow.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("设置 %s 包含无效条目", field.Label)
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
