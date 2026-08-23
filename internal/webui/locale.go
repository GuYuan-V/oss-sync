// 国际化
package webui

import "fmt"

const defaultWebLanguage = "zh"

var localeEntries = map[string][2]string{}

func registerEntries(entries map[string][2]string) {
	for key, entry := range entries {
		if _, exists := localeEntries[key]; exists {
			panic("duplicate locale key: " + key)
		}
		localeEntries[key] = entry
	}
}

func translate(lang, key string, args ...any) string {
	entry, exists := localeEntries[key]
	if !exists {
		return key
	}

	value := entry[0]
	if lang == "en" {
		value = entry[1]
	}
	if len(args) > 0 {
		return fmt.Sprintf(value, args...)
	}
	return value
}

// Languages 返回网页控制台支持的语言标识。
func Languages() []string {
	return []string{"zh", "en"}
}

