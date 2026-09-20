// 控制台默认主题的嵌入资源。
package consoletheme

import "embed"

//go:embed assets/default/*
var builtinAssets embed.FS
