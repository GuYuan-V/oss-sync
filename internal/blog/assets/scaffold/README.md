# OSS Sync 博客模板脚手架

本目录说明插件内博客模板的字段契约；独立模板管理、在线脚手架和独立 ZIP 上传入口不可用。模板通过插件包安装，可编辑以下文件：

- `template.html`：页面布局，使用 Go `html/template` 语法。
- `style.css`：主题样式，使用 CSS custom properties 定义颜色。
- `theme.js`：页面脚本，提供主题切换与交互。
- 不要添加 `settings.json`：功能设置必须在插件 manifest 的 `settings` 中声明。

可用模板字段：自定义模板使用 `missingkey=zero`，缺失 map 键与不存在的结构体字段行为不同；模板失败时回退 `default`，响应头 `X-Theme-Fallback` 给出原因。完整契约见“插件管理 → 插件指南 → 博客模板契约”。

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

## 页面与内容契约

- `/b/<vault-id>` 是动态博客首页，使用 `.IsHome` 和 `.HomePosts`，此时 `.ContentHTML` 为空。首页包含全部有效的单篇 Markdown 分享，按分享创建时间倒序排列；不自动列出仅通过文件夹分享的文章。
- `/p/<share-id>` 为单篇文章；文件夹分享会跳转到带尾斜线的目录页。目录页 `.IsFolder=true`；目录下文章的 `.IsFolder=false`。正文和目录 HTML 通过 `.ContentHTML` 输出。
- `default` 只支持分享阅读；`papertrail` 支持博客首页。自定义模板必须实现 `.IsHome` 分支，并建议提供 `theme.json` 的 `supports_public_blog: true`。未提供元数据文件的旧模板按支持首页处理；提供文件后，该字段省略或为 false 均不支持首页。
- `.VaultID`、`.PluginData` 也可使用。插件数据通过 `pluginField`、`hasPlugin` 或带 `with` 守卫的 `index` 读取；没有 `.ThemeConfig` 字段，配置使用 `.ThemeConfigJS`。
- `public_settings` 生效需同时声明 `supports_public_blog: true`，且键存在于所属已启用插件的 settings schema 中。不要将密钥列入公开白名单。
- Markdown YAML 摘要支持 `|`、`>` 多行写法，标签支持 YAML 列表。`summary`、`date`、`cover` 分别为 `description`、`published`、`image` 的备用字段。
- 图片 `./`、`../` 相对文章目录解析，不能越出仓库；其他路径先匹配仓库根路径，再匹配文章相对路径。裸文件名最后按同仓库附件名查找。`http(s)` 图片与封面直接使用外部地址；以 `/` 开头的封面是站点 URL。
- 文件夹分享的资源 URL 可带 `source` 指定文章；来源必须仍在该分享内且确实引用该资源。请使用服务端生成的资源 URL。
