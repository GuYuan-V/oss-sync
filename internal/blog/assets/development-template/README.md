# OSS Blog 自定义主题开发模板

这个目录由管理面板创建，服务端会将它作为一个 Vault 的博客页面模板使用。

## 文件

- `template.html`：页面结构，使用 Go `html/template` 语法。
- `style.css`：通过 `/themes/<主题名称>/style.css` 提供。
- `theme.js`：通过 `/themes/<主题名称>/theme.js` 提供。
<<<<<<< HEAD
- `settings.json`：可选，声明当前模板在仓库“主题设置”页面显示的字段。
=======
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b

修改这些文件后刷新公开分享页即可看到结果，不需要重启服务。

## 模板字段

| 字段 | 说明 |
| --- | --- |
| `.Title` | 页面标题 |
| `.ThemeName` | 当前主题名称 |
| `.ThemeBaseURL` | 当前主题静态资源地址，例如 `/themes/my-theme` |
| `.ThemeConfigJS` | 可安全插入 `<script>` 的 JSON 配置 |
| `.ContentHTML` | Markdown 或目录索引渲染出的 HTML |
| `.IsFolder` | 当前是否为文件夹分享 |
| `.FolderTitle` | 文件夹分享标题 |
| `.CustomHeader` / `.CustomFooter` | 旧版 Vault 自定义片段（若存在） |
| `.FooterNotice` | 服务端提示内容 |
<<<<<<< HEAD
| `.AllowCopy` | 当前分享是否允许显示一键复制 |
| `.BlogHomeURL` | 已开启公开博客时的 `/b/<vault-id>` 地址，否则为空 |
| `.IsHome` | 当前是否为 Vault 博客首页 |
| `.BlogName` / `.Description` / `.LogoURL` | 博客身份信息 |
| `.Buttons` | 博客自定义链接列表 |
| `.HomePosts` | 博客首页可访问的分享文章列表 |
=======
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b

模板字段使用 `{{.Title}}`。普通字段会自动 HTML 转义；`ContentHTML` 已由服务端 Markdown 渲染器生成。不要把不受信任的文本标记为 HTML。

主题名只能使用字母、数字、`-` 与 `_`，长度最多 64。请仅让受信任的服务器管理员编辑此目录。
<<<<<<< HEAD

## 模板专属设置

`settings.json` 顶层使用 `settings` 数组。字段支持 `text`、`textarea`、`url` 与可重复的 `group`，并通过 `max_length`、`max_items` 和 `required` 限制输入。服务端只保存声明过且通过校验的值，模板从 `.ThemeConfigJS` 或 `window.__THEME_CONFIG__` 读取。完整示例见管理后台“模板管理 → 模板指南”。

## 发布检查

- 使用 `.ThemeBaseURL` 引用包内 CSS、脚本、图片和字体，不要写死域名或其他模板名。
- 文章、文件夹和博客首页分别检查 `.ContentHTML`、`.IsFolder` 与 `.IsHome` 分支。
- 保留键盘可达的控件、可见焦点和 `prefers-reduced-motion`；复制控件只在 `.AllowCopy` 为真时显示。
- 不要在模板中拼接未经信任的 HTML 或脚本。完整字段和无障碍约束见“模板管理 → 模板指南”。
=======
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
