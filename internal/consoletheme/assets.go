package consoletheme

import (
	"embed"
	"path/filepath"
	"strings"
)

// builtinAssets 内置主题的静态资源，运行时随二进制发布
//
//go:embed assets/default/* assets/sakura/* assets/starry/* assets/glass/* assets/ink/*
var builtinAssets embed.FS

// ReadBuiltinAsset 读取内置控制台主题的公开资源；
// 名称未注册、扩展名不可服务或路径非法时返回 false
func ReadBuiltinAsset(name, rel string) ([]byte, bool) {
	if !IsBuiltin(name) || !safeEntry(rel) {
		return nil, false
	}
	if !servedExtensions[strings.ToLower(filepath.Ext(rel))] {
		return nil, false
	}
	content, err := builtinAssets.ReadFile("assets/" + name + "/" + rel)
	if err != nil {
		return nil, false
	}
	return content, true
}
