# OSS Sync 博客模板脚手架

本目录包含可编辑的模板脚手架文件。创建自定义模板后，可编辑以下文件：

- `template.html`：页面布局，使用 Go `html/template` 语法。
- `style.css`：主题样式，使用 CSS custom properties 定义颜色。
- `theme.js`：页面脚本，提供主题切换与交互。
- 不要添加 `settings.json`：功能设置必须在插件 manifest 的 `settings` 中声明。

可用模板字段（自定义模板以 `missingkey=error` 渲染，引用下表以外的字段会让整页失败并回退到内置 `default` 主题；完整说明见网页内置「插件指南」）：

| 字段 | 说明 |
| --- | --- |
| `.Title` | 页面标题 |
| `.ThemeName` | 主题名称 |
| `.ThemeBaseURL` | 主题静态资源基础 URL |
| `.ThemeConfigJS` | 主题配置 JSON（安全序列化） |
| `.ContentHTML` | 渲染后的文章 HTML |
| `.IsHome` | 是否为博客首页 |
| `.IsFolder` | 是否为文件夹目录视图 |
| `.FolderTitle` | 文件夹标题 |
| `.ArticleTitle` | 文章标题（文章页） |
| `.AllowCopy` | 是否允许一键复制 |
| `.ShareID` | 当前分享 ID |
| `.BlogHomeURL` | 公开博客地址 `/b/<vault-id>`，否则为空 |
| `.CustomHeader` / `.CustomFooter` | 自定义页头/页脚 HTML |
| `.FooterNotice` | 页脚提示 |
| `.BlogName` / `.Description` | 博客名称与介绍 |
| `.LogoURL` / `.LogoSize` / `.LogoShape` | Logo 地址、尺寸、形状 |
| `.BannerURL` / `.MobileBannerURL` | 桌面端与移动端横幅地址 |
| `.Buttons` | 自定义链接，元素含 `.Label` / `.URL` / `.IconURL` |
| `.HomePosts` | 首页文章列表；元素含 `.Title` / `.Summary` / `.URL` / `.Date` / `.Category` / `.Tags` / `.CoverURL` / `.WordCount` |
| `.ArticlePost` | 文章元数据（文章页）；含 `.Summary` / `.Date` / `.Category` / `.Tags` / `.CoverURL` / `.WordCount` / `.ReadingMinutes` |

文章 Markdown 顶部完整闭合的 `---` frontmatter（`title` / `description` / `published` / `category` / `tags` / `image`）作为上表元数据并从正文隐藏；未闭合或损坏的块保留原文。

主题资源目录由插件 manifest 的 `blog_themes[].path` 声明；管理员上传并启用插件后，资源才会出现在选择框。功能设置在插件的 `settings` 中声明，不把插件 ZIP 嵌套进模板目录。

## 主题元数据

博客主题资源可在资源目录根部提供 `theme.json`：

```json
{
  "supports_public_blog": true,
  "public_settings": ["blog_name", "description", "logo_url", "logo_size", "logo_shape", "banner_url", "mobile_banner_url", "buttons"]
}
```

`public_settings` 只能列出插件 `settings` 中已声明的键。插件配置按 Vault 保存在 `VaultPluginSetting`；服务端只把主题白名单中同时存在于插件 settings schema 的值合并到 Vault 的博客主题配置，并映射到 `.BlogName`、`.Description`、`.LogoURL`、`.LogoSize`、`.LogoShape`、`.BannerURL`、`.MobileBannerURL`、`.Buttons` 和 `.ThemeConfigJS`。未列入白名单或插件 schema 的值不会通过插件设置注入公开页面。
