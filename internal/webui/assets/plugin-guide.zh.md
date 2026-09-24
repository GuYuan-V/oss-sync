# 服务端插件指南

## 1. 插件是唯一安装入口

管理员只从 **管理后台 → 插件管理** 上传一个 ZIP。插件安装后可启用、停用、删除和编辑文本资源。

博客模板和控制台主题不再单独上传、脚手架、删除或维护。它们是插件可选携带的展示资源：

- 内置博客模板 `default`、`papertrail` 始终存在，不能删除
- 内置控制台主题 `default` 始终存在，不能删除
- 已启用插件声明的模板和主题会自动追加到对应选择框
- 停用插件后，它提供的选项从选择框消失；仍保存的旧选择会实际回退到内置项，并持续提示登录用户手动切换

## 2. ZIP 基本结构

WASM 插件最小结构：

```text
manifest.json
plugin.wasm
```

可执行插件结构：

```text
manifest.json
bin/plugin.exe
bin/plugin
assets/...
blog/clean/template.html
blog/clean/style.css
blog/clean/theme.json
console/clean/theme.css
console/clean/assets/...
```

可执行插件必须携带当前服务器平台的预编译入口。服务器直接运行 ZIP 内的二进制，不在 VM 上编译 Go 或执行插件自定义构建命令。可执行插件拥有服务端账号的文件、网络、数据库、环境和进程权限，只上传信任的代码。

## 3. manifest 展示资源

`blog_themes` 和 `console_themes` 都是可选数组，可以只提供一种，也可以不提供。每个资源需要唯一 ID、展示名称和包内目录：

```json
{
  "id": "reading-tools",
  "name": "Reading tools",
  "version": "1.0.0",
  "api_version": 1,
  "runtime": "executable",
  "entrypoints": {
    "windows-amd64": "bin/plugin.exe",
    "linux-amd64": "bin/plugin"
  },
  "routes": [],
  "blog_themes": [
    { "id": "clean", "name": "Clean reading", "path": "blog/clean" }
  ],
  "console_themes": [
    { "id": "clean", "name": "Clean console", "path": "console/clean" }
  ]
}
```

规则：

- `id` 只能使用小写字母、数字、`-` 和 `_`，每个数组内不能重复
- `path` 是 ZIP 内目录，不能是绝对路径，不能包含 `.`、`..` 或反斜杠
- 博客资源目录必须包含 `template.html`
- 控制台主题目录必须包含 `theme.css`
- 资源展示名显示在用户选择框中，内部值使用 `插件ID--资源ID`
- 模板和主题只负责结构、样式和静态资源；功能、设置、路由、数据和任务仍由插件负责

## 4. 博客模板字段

博客模板通过 Go `html/template` 渲染。常用字段：

| 字段 | 类型 | 用途 |
| --- | --- | --- |
| `.Title` | string | 浏览器标题 |
| `.ThemeName` | string | 当前模板内部名称 |
| `.ThemeBaseURL` | string | 当前模板静态资源地址 |
| `.ThemeConfigJS` | JS | 安全序列化的插件设置 |
| `.PluginData` | map | 启用插件通过 `blog.data` 注入的数据 |
| `.IsHome` | bool | 是否是博客首页 |
| `.ContentHTML` | HTML | Markdown 渲染后的正文 |
| `.ArticlePost` | struct | 当前文章标题、日期、分类、标签、封面和阅读信息 |

插件 ID 可能含连字符，使用 `pluginField` 读取：

```gotemplate
{{with pluginField .PluginData "reading-tools" "recommendations"}}
  {{range .Items}}<a href="{{.URL}}">{{.Title}}</a>{{end}}
{{end}}
```

`safeHTML` 只适用于插件返回的可信 HTML。插件应自行保证内容安全；内联脚本仍受页面 CSP 限制，需要脚本时使用插件提供的同源资源或路由。

## 5. 插件数据与请求上下文

注册 `blog.data` 后，插件会收到当前页面上下文：

- `vault_id`、`share_id`、`path`
- `is_home`、`is_folder`
- `method`、`request_url`、`query`
- `headers`、`cookies`、`client_ip`

插件返回的 JSON 会按插件 ID 注入 `.PluginData`。失败或空响应只会得到空对象，不会让整页模板回退。评论、VIP、订阅等功能可以使用这些上下文自行维护身份与授权，并通过插件路由接收提交。

## 6. 插件设置

在 `settings` 中声明 `text`、`textarea`、`url`、`choice` 或非嵌套 `group` 字段。启用插件后，设置入口出现在一级 **插件设置**，值按 Vault 保存，并通过 `settings` 传给插件。

插件设置不放在博客模板或控制台主题目录中，也不再使用独立的 `settings.json`。

## 7. 在线编辑

插件管理页的“编辑”入口只显示包内可识别的文本资源，例如：

- `manifest.json`
- 模板 HTML、CSS、JS、JSON、Markdown
- 控制台主题 CSS、JSON、SVG、字体说明等文本文件

二进制入口、`plugin.wasm` 和其他不可识别的二进制文件只读。保存文本资源时会重新校验 manifest、资源目录和插件启动；验证失败会保留原文件和原运行版本。

在线编辑不会把 Go 源码编译成新的二进制。需要修改插件逻辑时，在本地构建新的平台二进制并重新上传升级。

## 8. 生命周期与安全

- 上传后先校验 ZIP 路径、大小、文件数、入口和 manifest
- 启用插件后宿主自动物化其模板和主题资源
- 停用或删除插件时自动移除其物化资源
- 删除插件前必须先停用
- 内置模板和主题不受插件删除影响
- 可执行插件是管理员信任代码，不是沙箱
- 自定义模板和插件返回内容由上传者负责安全性
