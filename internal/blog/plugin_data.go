package blog

import (
	"html/template"
	"strings"

	"github.com/gin-gonic/gin"
)

// sanitizeHeaderValue 去除换行等 CTL 字符并限长，用于把回退原因安全写入响应头
func sanitizeHeaderValue(value string) string {
	value = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, value)
	if len(value) > 200 {
		value = value[:200]
	}
	return value
}

// collectPluginData 调用 blog.data 钩子收集插件为当前页面注入的展示数据。
// 返回值以插件 ID 为键；未接入插件运行时或无插件时返回空 map，模板侧 .PluginData 始终非 nil
func (h *Handler) collectPluginData(c *gin.Context, p renderParams) map[string]any {
	if h.pluginData == nil {
		return map[string]any{}
	}
	cookies := map[string]string{}
	for _, cookie := range c.Request.Cookies() {
		cookies[cookie.Name] = cookie.Value
	}
	data, err := h.pluginData.ApplyHookData(c.Request.Context(), "blog.data", PluginDataPayload{
		VaultID:    p.VaultID,
		Theme:      p.ThemeName,
		ShareID:    p.ShareID,
		Path:       p.FilePath,
		IsHome:     p.IsHome,
		IsFolder:   p.IsFolder,
		Method:     c.Request.Method,
		RequestURL: c.Request.URL.String(),
		Query:      c.Request.URL.Query(),
		Headers:    cloneStringHeader(c.Request.Header),
		Cookies:    cookies,
		ClientIP:   c.ClientIP(),
	})
	if err != nil || data == nil {
		return map[string]any{}
	}
	return data
}

func cloneStringHeader(source map[string][]string) map[string][]string {
	out := make(map[string][]string, len(source))
	for key, values := range source {
		out[key] = append([]string(nil), values...)
	}
	return out
}

// customThemeFuncs 是自定义主题模板可用的辅助函数。
// 自定义主题由管理员上传、属受信代码，允许通过 safeHTML 注入插件返回的富文本
func customThemeFuncs() template.FuncMap {
	return template.FuncMap{
		// hasPlugin 判断某插件是否已装并注入了数据，用于跨插件可选引用的守卫
		"hasPlugin": func(data map[string]any, id string) bool {
			if data == nil {
				return false
			}
			_, ok := data[id]
			return ok
		},
		// pluginField 按插件 ID 与字段名安全读取插件数据，任一层缺失返回 nil，避免链式访问报错
		"pluginField": func(data map[string]any, id, field string) any {
			if data == nil {
				return nil
			}
			entry, ok := data[id].(map[string]any)
			if !ok {
				return nil
			}
			return entry[field]
		},
		// safeHTML 将插件返回的字符串标记为可信 HTML，供在文章内运行插件提供的组件/脚本片段
		"safeHTML": func(value any) template.HTML {
			switch v := value.(type) {
			case string:
				return template.HTML(v)
			case template.HTML:
				return v
			default:
				return ""
			}
		},
	}
}
