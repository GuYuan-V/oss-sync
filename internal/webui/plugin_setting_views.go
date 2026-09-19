package webui

import "github.com/helantianshen/oss-sync/internal/blog"

type themeSettingRowView struct {
	Values map[string]string
}

func buildThemeSettingRows(field blog.ThemeSettingField, raw any) []themeSettingRowView {
	rows := make([]themeSettingRowView, 0, field.MaxItems)
	stored, ok := raw.([]any)
	if !ok {
		return rows
	}
	for _, item := range stored {
		values := make(map[string]string, len(field.Fields))
		entry, ok := item.(map[string]any)
		if ok {
			for _, child := range field.Fields {
				if value, ok := entry[child.Key].(string); ok {
					values[child.Key] = value
				}
			}
		}
		rows = append(rows, themeSettingRowView{Values: values})
		if len(rows) == field.MaxItems {
			break
		}
	}
	return rows
}
