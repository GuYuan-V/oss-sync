> 本指南已针对 AI 识别优化（AI-Friendly / LLM-Readable）：字段契约、渲染规则与失败回退都以显式清单和表格给出，供人工与 AI 直接照抄使用。构建模板前请先读第 0 节的硬规则。

博客模板只负责公开页面的结构和样式。功能、数据库、动态行为和功能设置必须写在插件里。

## 0. 硬规则（构建前必读）

- 自定义模板用 Go `html/template` 且以 `missingkey=error` 渲染。
- 模板引用任何服务端未提供的字段或子字段（例如结构体上不存在的 `.Foo` 或 `.ArticlePost.Bar`），会让整页渲染失败。
- 渲染失败时服务端不会报错给访客，而是**静默回退到内置 `default` 主题**——页面看起来像内置默认样式，而不是你的模板。这通常被误判为“模板没生效”或“和其他主题一样”。
- 因此：**只使用第 2 节列出的字段**。需要展示新数据时，先确认字段在契约表中存在；不存在就不能用。
- 引用是否安全只取决于字段在契约表中是否列出，与当前页面类型无关：非文章页时 `.ArticlePost` 各字段为零值，可安全引用（用 `.IsHome` / `.IsFolder` 分支控制显示）。

## 1. 最小模板

创建一个目录：

```text
my-template/
├── template.html
├── style.css
├── theme.js
└── theme.json
```

`template.html`：

```html
<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}}</title>
  <link rel="stylesheet" href="{{.ThemeBaseURL}}/style.css">
</head>
<body>
  <main>{{.ContentHTML}}</main>
  <script src="{{.ThemeBaseURL}}/theme.js" defer></script>
</body>
</html>
```

`style.css` 写颜色、间距和布局；`theme.js` 只写页面交互。`.ThemeConfigJS` 来自关联插件，不是模板自己的配置。

`theme.json` 可选，用于声明能力与公开展示字段：

```json
{
  "supports_public_blog": true,
  "public_settings": ["blog_name", "description", "logo_url", "logo_size", "logo_shape", "banner_url", "mobile_banner_url", "buttons"]
}
```

- `supports_public_blog`：为 `true` 时才可作为 `/b/<vault-id>` 公开博客首页，并在仓库设置中开启公开博客。
- `public_settings`：显式列出可复制到公开页面的博客设置键；未列出的插件配置不会进入模板可读的展示配置。

不要创建 `settings.json`。模板不拥有功能设置。

## 2. 模板字段契约（完整清单）

以下是模板可引用的全部字段。表外字段一律视为不存在，引用会触发第 0 节的回退。

**2.1 页面与站点**

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `.Title` | string | 浏览器标题 |
| `.ThemeName` | string | 当前主题名称 |
| `.ThemeBaseURL` | string | 主题静态资源基址，如 `/themes/my-template`；引用包内资源一律用它 |
| `.ThemeConfigJS` | JS | 可安全插入 `<script>` 的博客配置 JSON |
| `.IsHome` | bool | 是否为博客首页（`/b/<vault-id>`） |
| `.IsFolder` | bool | 是否为文件夹目录视图 |
| `.FolderTitle` | string | 文件夹标题 |
| `.ArticleTitle` | string | 文章标题（文章页） |
| `.ContentHTML` | HTML | 已渲染的文章正文或目录索引 |
| `.AllowCopy` | bool | 当前分享是否允许一键复制正文 |
| `.ShareID` | string | 当前分享 ID |
| `.BlogHomeURL` | string | 开启公开博客时为 `/b/<vault-id>`，否则为空 |
| `.CustomHeader` / `.CustomFooter` | HTML | 仓库自定义页眉/页脚片段（可能为空） |
| `.FooterNotice` | HTML | 服务端页脚提示 |

**2.2 博客身份与横幅**

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `.BlogName` | string | 博客名称 |
| `.Description` | string | 博客介绍 |
| `.LogoURL` | string | Logo 图片地址 |
| `.LogoSize` | int | Logo 像素尺寸（`0` 表示未设置） |
| `.LogoShape` | string | `square` 或 `circle` |
| `.BannerURL` | string | 页面横幅图地址；空时自行回退包内图 |
| `.MobileBannerURL` | string | 移动端横幅图地址 |
| `.Buttons` | list | 自定义链接，元素含 `.Label` / `.URL` / `.IconURL` |

以上取值来自博客设置（见第 4 节），未设置时为空/零值。

**2.3 单篇文章元数据 `.ArticlePost`**

文章页有效；首页与文件夹目录页为零值（用 `.IsHome` / `.IsFolder` 分支避免显示空信息）。

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `.ArticlePost.Summary` | string | 摘要，优先 frontmatter `description`，否则取正文首段 |
| `.ArticlePost.Date` | string | 日期 `YYYY-MM-DD`，优先 frontmatter `published`，否则取文件修改时间 |
| `.ArticlePost.Category` | string | 分类（frontmatter `category`） |
| `.ArticlePost.Tags` | list | 标签字符串列表（frontmatter `tags`） |
| `.ArticlePost.CoverURL` | string | 封面地址（frontmatter `image`，见第 3 节） |
| `.ArticlePost.WordCount` | int | 正文字数（中日韩按字，西文按词） |
| `.ArticlePost.ReadingMinutes` | int | 预计阅读分钟（按 400 字/分向上取整） |

**2.4 首页文章列表 `.HomePosts`**

`.IsHome` 为真时可用，元素字段：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `.Title` | string | 文章标题 |
| `.Summary` | string | 摘要 |
| `.URL` | string | 文章地址 `/p/<share-id>` |
| `.Date` | string | 日期 `YYYY-MM-DD` |
| `.Category` | string | 分类 |
| `.Tags` | list | 标签列表 |
| `.CoverURL` | string | 封面地址 |
| `.WordCount` | int | 字数 |

## 3. Frontmatter 元数据

文章 Markdown 顶部可放一段 `---` 包裹的 YAML，用于提供上表的元数据：

```markdown
---
title: 一篇笔记
description: 用于列表与搜索的摘要
published: 2026-09-22
category: 随笔
tags: [笔记, 生活]
image: 附件/封面.webp
---
正文从这里开始。
```

- 所有键都可省略；缺省时标题用文件名、摘要取正文首段、日期取文件修改时间。
- `tags` 支持行内 `[a, b]` 或缩进列表（每行 `- 标签`）两种写法。
- `image` 是仓库内附件路径或 `http(s)`/站内绝对地址；附件封面会被解析为公开地址并自动纳入分享鉴权，无需在正文再次引用。
- 完整闭合的 `---` 块会作为元数据并从正文中隐藏；未闭合或格式损坏的块保留原文，不当作元数据。

## 4. 博客设置来源

`.BlogName`、`.Description`、`.LogoURL`、`.LogoSize`、`.LogoShape`、`.BannerURL`、`.MobileBannerURL`、`.Buttons` 的取值有两个来源：

- **内置博客设置**：对任何 `supports_public_blog` 为真的主题生效（不限内置主题）。在一级 **插件设置** 中选择对应仓库即可填写博客名称、介绍、Logo、横幅和自定义链接。
- **关联功能插件**：模板自带的 `plugin.zip` 可声明额外设置；`theme.json` 的 `public_settings` 决定哪些键复制到模板可读的公开配置。

## 5. 关联功能插件

如果模板需要博客统计、表单、评论、外部 API 或其他功能：

1. 先创建一个服务端插件。
2. 在插件中声明 `settings`、Hook、路由或后台页面。
3. 将插件 ZIP 命名为 `plugin.zip`。
4. 把 `plugin.zip` 放到模板 ZIP 根目录。

最终 ZIP：

```text
my-template.zip
├── template.html
├── style.css
├── theme.js
├── theme.json
└── plugin.zip
```

上传模板后，系统会自动安装、启用插件并建立模板关联。插件设置会出现在一级“插件设置”中；模板只负责展示这些配置。

## 6. 上传

在模板目录内执行 `zip -r ../my-template.zip .`，或在 PowerShell 中执行 `Compress-Archive * ../my-template.zip`。然后在 **管理员设置 -> 模板管理** 上传 ZIP，也可以直接从已有模板创建副本。模板名称只能包含字母、数字、连字符和下划线。

模板负责样式；插件负责功能。这是本项目的固定边界。
