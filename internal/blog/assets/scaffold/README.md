# OSS Sync 博客模板脚手架

本目录包含可编辑的模板脚手架文件。创建自定义模板后，可编辑以下文件：

- `template.html`：页面布局，使用 Go `html/template` 语法。
- `style.css`：主题样式，使用 CSS custom properties 定义颜色。
- `theme.js`：页面脚本，提供主题切换与交互。
- `settings.json`：可选的模板专属设置声明，由仓库“主题设置”页面动态生成表单。

可用模板字段：

| 字段 | 说明 |
| --- | --- |
| `.Title` | 页面标题 |
| `.ThemeName` | 主题名称 |
| `.ThemeBaseURL` | 主题静态资源基础 URL |
| `.ThemeConfigJS` | 主题配置 JSON（安全序列化） |
| `.ContentHTML` | 渲染后的文章 HTML |
| `.IsFolder` | 是否为文件夹目录视图 |
| `.FolderTitle` | 文件夹标题 |
| `.CustomHeader` | 自定义页头 HTML |
| `.CustomFooter` | 自定义页脚 HTML |
| `.FooterNotice` | 页脚提示 |
| `.LogoURL` / `.BlogName` / `.Description` / `.Buttons` | papertrail 博客设置 |

主题名仅允许字母、数字、连字符和下划线。修改文件后刷新公开页面即可生效。

`settings.json` 支持 `text`、`textarea`、`url` 和可重复的 `group` 字段。保存值会写入仓库的 `.ThemeConfigJS`；完整字段格式和示例见管理后台“模板管理 → 模板指南”。
