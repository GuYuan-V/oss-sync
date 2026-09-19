博客模板只负责公开页面的结构和样式。功能、数据库、动态行为和功能设置必须写在插件里。

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

`style.css` 写颜色、间距和布局；`theme.js` 只写页面交互。模板可读取 `.Title`、`.ContentHTML`、`.ThemeBaseURL`、`.HomePosts`、`.BlogName` 和 `.ThemeConfigJS`。`.ThemeConfigJS` 来自关联插件，不是模板自己的配置。

`theme.json` 可选，只用于声明能力：

```json
{"supports_public_blog": true}
```

不要创建 `settings.json`。模板不拥有功能设置。

## 2. 关联功能插件

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

## 3. 上传

在模板目录内执行 `zip -r ../my-template.zip .`，或在 PowerShell 中执行 `Compress-Archive * ../my-template.zip`。然后在 **管理员设置 -> 模板管理** 上传 ZIP，也可以直接从已有模板创建副本。模板名称只能包含字母、数字、连字符和下划线。

模板负责样式；插件负责功能。这是本项目的固定边界。
