# OSS Blog 自定义主题开发模板

这个目录由管理面板创建，服务端会将它作为一个 Vault 的博客页面模板使用。

## 文件

- `template.html`：页面结构，使用 Go `html/template` 语法。
- `style.css`：通过 `/themes/<主题名称>/style.css` 提供。
- `theme.js`：通过 `/themes/<主题名称>/theme.js` 提供。
- 不要添加 `settings.json`：模板只负责样式，功能设置由关联插件提供。

修改这些文件后刷新公开分享页即可看到结果，不需要重启服务。

## 模板字段

> 自定义模板以 `missingkey=error` 渲染：引用下表以外的字段会让整页渲染失败并静默回退到内置 `default` 主题。只使用下表字段。完整说明见“模板管理 → 模板指南”。

| 字段 | 说明 |
| --- | --- |
| `.Title` | 页面标题 |
| `.ThemeName` | 当前主题名称 |
| `.ThemeBaseURL` | 当前主题静态资源地址，例如 `/themes/my-theme` |
| `.ThemeConfigJS` | 可安全插入 `<script>` 的 JSON 配置 |
| `.ContentHTML` | Markdown 或目录索引渲染出的 HTML |
| `.IsHome` | 当前是否为 Vault 博客首页 |
| `.IsFolder` | 当前是否为文件夹分享 |
| `.FolderTitle` | 文件夹分享标题 |
| `.ArticleTitle` | 文章标题（文章页） |
| `.AllowCopy` | 当前分享是否允许显示一键复制 |
| `.ShareID` | 当前分享 ID |
| `.BlogHomeURL` | 已开启公开博客时的 `/b/<vault-id>` 地址，否则为空 |
| `.CustomHeader` / `.CustomFooter` | 仓库自定义片段（若存在） |
| `.FooterNotice` | 服务端提示内容 |
| `.BlogName` / `.Description` | 博客名称与介绍 |
| `.LogoURL` / `.LogoSize` / `.LogoShape` | Logo 地址、像素尺寸、`square`/`circle` |
| `.BannerURL` / `.MobileBannerURL` | 桌面端与移动端横幅地址 |
| `.Buttons` | 自定义链接列表，元素含 `.Label` / `.URL` / `.IconURL` |
| `.HomePosts` | 首页文章列表；元素含 `.Title` / `.Summary` / `.URL` / `.Date` / `.Category` / `.Tags` / `.CoverURL` / `.WordCount` |
| `.ArticlePost` | 文章元数据（文章页）；含 `.Summary` / `.Date` / `.Category` / `.Tags` / `.CoverURL` / `.WordCount` / `.ReadingMinutes` |

文章 Markdown 顶部完整闭合的 `---` frontmatter（`title` / `description` / `published` / `category` / `tags` / `image`）会作为上表元数据并从正文隐藏；未闭合或损坏的块保留原文。`image` 支持仓库附件路径与 `http(s)` 地址，附件封面自动纳入分享鉴权。

模板字段使用 `{{.Title}}`。普通字段会自动 HTML 转义；`ContentHTML` 已由服务端 Markdown 渲染器生成。不要把不受信任的文本标记为 HTML。

主题名只能使用字母、数字、`-` 与 `_`，长度最多 64。请仅让受信任的服务器管理员编辑此目录。

## 模板专属设置
如果需要设置项，请创建服务端插件，在插件的 `settings` 中声明字段。插件可以在 `manifest.json` 的 `blog_themes` 中声明包含本模板文件的资源目录；管理员从插件管理页上传插件，启用后模板才会出现在仓库设置选择框。

## 发布检查

- 使用 `.ThemeBaseURL` 引用包内 CSS、脚本、图片和字体，不要写死域名或其他模板名。
- 文章、文件夹和博客首页分别检查 `.ContentHTML`、`.IsFolder` 与 `.IsHome` 分支。
- 保留键盘可达的控件、可见焦点和 `prefers-reduced-motion`；复制控件只在 `.AllowCopy` 为真时显示。
- 不要在模板中拼接未经信任的 HTML 或脚本。完整字段和插件资源契约见网页内置「插件指南」。
