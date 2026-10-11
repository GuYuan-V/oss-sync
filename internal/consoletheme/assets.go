package consoletheme

import "embed"

// builtinAssets 内嵌默认主题的静态资源，运行时随二进制发布
//
//go:embed assets/default/*
var builtinAssets embed.FS
