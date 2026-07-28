# OSS Blog 自定义主题开发模板

这个目录由管理面板创建，服务端会将它作为一个 Vault 的博客页面模板使用。

## 文件

- `template.html`：页面结构，使用 Go `html/template` 语法。
- `style.css`：通过 `/themes/<主题名称>/style.css` 提供。
- `theme.js`：通过 `/themes/<主题名称>/theme.js` 提供。

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

模板字段使用 `{{.Title}}`。普通字段会自动 HTML 转义；`ContentHTML` 已由服务端 Markdown 渲染器生成。不要把不受信任的文本标记为 HTML。

主题名只能使用字母、数字、`-` 与 `_`，长度最多 64。请仅让受信任的服务器管理员编辑此目录。
