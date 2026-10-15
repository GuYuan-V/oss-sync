# 本地语法高亮

`syntax.js` 由 `syntax.source.js`、Prism **1.30.0** 与 esbuild **0.28.2** 构建，包含 markup、CSS、clike、JavaScript、TypeScript、Go、JSON、Bash、Python、SQL、YAML 和 Markdown。Prism 采用 MIT 许可，完整许可见 `licenses/Prism-MIT.txt`。

脚本不扫描页面，也不自动高亮。调用方只需按需加载本地 `syntax.js`，再调用 `window.ShironeHighlight(codeElement)`。函数只接受一个 `CODE` 元素，从 `textContent` 读取源码；已支持语种返回 `true`，未声明或未知语种返回 `false` 并保留原内容。

使用已有依赖时，设置 `NODE_PATH` 指向包含固定版本 `prismjs` 的 `node_modules` 目录，然后执行：

```sh
esbuild syntax.source.js --bundle --minify --format=iife --platform=browser --target=es2020 --legal-comments=inline --banner:js='/*! Shirone syntax: Prism 1.30.0 (MIT); see licenses/Prism-MIT.txt. */' --outfile=syntax.js
```

语法标记使用 Prism 的 `.token` 类名，颜色由博客主题样式提供。构建不启用远程语法加载、插件自动加载或 Worker。
