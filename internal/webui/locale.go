package webui

import "fmt"

// defaultWebLanguage 是未选择语言时使用的默认界面语言
const defaultWebLanguage = "zh"

// localeEntries 保存全部本地化条目，键为文案键，值为 [中文, 英文] 译文
// 由各 locale_*.go 的 init 在启动时填充，键重复时直接 panic
var localeEntries = map[string][2]string{}

// registerEntries 把一组本地化条目注册进全局表，遇到重复键时 panic
func registerEntries(entries map[string][2]string) {
	for key, entry := range entries {
		if _, exists := localeEntries[key]; exists {
			panic("duplicate locale key: " + key)
		}
		localeEntries[key] = entry
	}
}

// translate 按语言返回文案键对应的译文
// 键不存在时原样返回键名；语言非 en 时使用中文；传入参数时按 printf 语义格式化
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

// Languages 返回网页控制台支持的语言标识
func Languages() []string {
	return []string{"zh", "en"}
}
