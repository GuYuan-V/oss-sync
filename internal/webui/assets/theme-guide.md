模板决定公开首页、公开仓库和分享页面的结构、样式与交互。`default` 和 `papertrail` 是只读内置模板；管理员可从任意现有模板创建副本，再编辑副本。

## 文件和目录

自定义模板保存在服务器数据目录中：

```text
data/themes/my-theme/
├── template.html
├── style.css
├── theme.js
├── settings.json
└── README.md
```

- `template.html`：页面结构，使用 Go `html/template` 语法，必须存在。
- `style.css`：模板样式，通过 `/themes/my-theme/style.css` 访问。
- `theme.js`：模板交互，通过 `/themes/my-theme/theme.js` 访问。
- `settings.json`：可选，声明仓库管理员能配置的模板专属设置。
- `README.md`：模板维护说明，不参与公开页面渲染。

模板名必须是 1 至 64 个字符，以字母或数字开头，只能包含字母、数字、连字符和下划线。名称也是目录名和公开资源 URL 的一部分，例如 `paper-notes` 对应 `/themes/paper-notes/`。

## 模板字段

`template.html` 可使用以下字段：

| 字段 | 说明 |
| --- | --- |
| `.Title` | 当前页面标题 |
| `.Description` | 仓库或文章简介 |
| `.Date` | 文章日期文本 |
| `.Summary` | 文章摘要 |
| `.URL` | 当前文章公开地址 |
| `.ThemeName` | 当前模板名称 |
| `.ThemeBaseURL` | 模板静态资源基础 URL |
| `.ThemeConfigJS` | 当前仓库模板设置的安全 JSON |
| `.ContentHTML` | 已渲染的 Markdown 或目录内容 |
| `.IsFolder` | 当前页面是否为文件夹分享 |
| `.FolderTitle` | 文件夹分享标题 |
| `.IsHome` | 当前页面是否为仓库博客首页 |
| `.HomePosts` | 博客首页文章列表；每项有标题、摘要、日期和 URL |
| `.BlogName` / `.LogoURL` | 博客名称与 Logo 地址 |
| `.BlogHomeURL` | 当前仓库博客首页地址 |
| `.Buttons` | 设置中声明的导航按钮列表 |
| `.AllowCopy` | 是否允许公开页面复制文章内容 |
| `.CustomHeader` / `.CustomFooter` | Vault 自定义页头和页脚 |
| `.FooterNotice` | 服务端页脚提示 |

最小模板示例：

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
  <script>window.__THEME_CONFIG__ = {{.ThemeConfigJS}};</script>
  <script src="{{.ThemeBaseURL}}/theme.js" defer></script>
</body>
</html>
```

普通字符串会自动转义；不要将用户输入直接拼接成 HTML 或脚本。

## 页面场景

- 文章分享页：使用 `.Title`、`.ContentHTML`、`.Date`、`.AllowCopy`。
- 文件夹分享页：使用 `.IsFolder`、`.FolderTitle`、`.ContentHTML`。
- 博客首页：使用 `.IsHome`、`.HomePosts`、`.BlogName`、`.Description`、`.LogoURL` 和 `.Buttons`。
- 所有页面都应通过 `.ThemeBaseURL` 引用自己的 CSS、JavaScript、图片和字体；不要写死其他模板名称或服务器域名。

## 模板专属设置

模板可用 `settings.json` 声明设置。设置页会按声明动态生成表单，保存值写入当前仓库的 `ThemeConfig`，模板可通过 `.ThemeConfigJS` 或 `window.__THEME_CONFIG__` 使用。

```json
{
  "settings": [
    {"key": "blog_name", "label": "博客名称", "type": "text", "max_length": 120},
    {"key": "description", "label": "博客介绍", "type": "textarea", "max_length": 500},
    {"key": "logo_url", "label": "Logo URL", "type": "url", "max_length": 512},
    {
      "key": "buttons",
      "label": "导航按钮",
      "type": "group",
      "max_items": 5,
      "fields": [
        {"key": "label", "label": "按钮名称", "type": "text", "max_length": 40, "required": true},
        {"key": "url", "label": "目标 URL", "type": "url", "max_length": 512, "required": true},
        {"key": "icon_url", "label": "图标 URL", "type": "url", "max_length": 512}
      ]
    }
  ]
}
```

支持 `text`、`textarea`、`url` 和可重复的 `group`。字段 key 使用小写字母、数字和下划线；URL 只接受 `http://`、`https://` 或以 `/` 开头的站内路径。

## 公开入口

- `/`：系统公开首页，只列出开启公开博客的仓库。
- `/b/<vault-id>`：仓库公开博客入口，需要在仓库设置中启用公开入口。
- `/p/<share-id>`：单篇文章或文件夹分享入口。

模板选择和模板设置都按仓库保存。修改模板文件后刷新公开页面即可生效，无需重启服务。调试资源时可给自己管理的静态资源 URL 加版本查询参数，例如 `style.css?v=2`。

## 交互与可访问性

- 页面应提供跳到正文的链接，正文使用 `<main>`，导航使用 `<nav aria-label="...">`。
- 所有按钮、主题切换和复制控件必须有可读文字或 `aria-label`，且可通过键盘操作。
- 复制按钮只在 `.AllowCopy` 为真时显示；复制结果应写入 `aria-live="polite"` 状态区域。
- 不得隐藏焦点轮廓，不得只用颜色表达状态；动画必须尊重 `prefers-reduced-motion`。
- 可复用 `/ui/assets/theme.js` 实现 `auto`、`light`、`dark`，不要自行保存与内置模板冲突的主题 key。

## 背景和三态主题

公开页面支持 `auto`、`light`、`dark`。模板脚本可读取 `data-theme`，并使用 `OSSTheme` 保存偏好。背景、文字和边框应由 CSS custom properties 管理：

```css
:root {
  --page-background: #f4f1e8;
  --page-ink: #172033;
}

:root[data-theme="dark"] {
  --page-background: #111827;
  --page-ink: #e5e7eb;
}

body {
  color: var(--page-ink);
  background: var(--page-background);
}
```

背景图片等资源放在模板目录内，并通过 `{{.ThemeBaseURL}}/文件名` 引用。

## 内置模板示例

- `default`：简洁阅读页面，可作为最小结构和三态主题的参考。创建副本时会补全可编辑的 `template.html`。
- `papertrail`：博客式首页，示范博客名称、介绍、Logo 和导航按钮等动态设置。
- `development-template`：服务器内置的开发起点，展示模板字段、静态资源和 `window.__THEME_CONFIG__`。

## 创建、编辑和发布

1. 在“从现有模板创建副本”中选择最接近目标的模板并命名。
2. 在模板列表中编辑 `template.html`、`style.css`、`theme.js` 或 `settings.json`。
3. 在仓库设置中选择新模板；若模板声明了设置，再打开“主题设置”。
4. 启用公开入口或创建分享，访问 `/b/<vault-id>` 或 `/p/<share-id>` 验证。
5. 自定义模板可下载为 ZIP；正在被仓库使用的模板不能删除。

## Papertrail 设置预览

Papertrail 的设置页会同时预览服务器公开博客目录卡片和博客首页标题区。预览只反映当前表单输入，保存仍由服务端校验。`logo_size` 支持 10–192 像素，`logo_shape` 可选 `square` 或 `circle`；圆形会同步用于公开目录、首页和顶部品牌标识。
