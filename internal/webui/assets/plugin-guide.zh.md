# OSS Sync 服务端插件开发指南


## 0. 先做选择：WASM 还是 executable

第一次开发通常选择 `executable`。

| 运行时 | 适合场景 | 能力边界 |
| --- | --- | --- |
| `executable` | 需要公开 Go SDK、数据库、网络、文件、定时任务、后台进程或完整业务功能 | 管理员信任的服务端程序，拥有服务端账号的正常操作系统权限 |
| `wasm` | 只需要窄边界的请求/响应扩展 | WASM ABI v1；没有 WASI、文件系统、网络和数据库导入 |

**安全结论:**`executable` 不是沙箱。它可以读取/写入服务端文件、访问数据库、连接网络、读取环境变量和运行命令，权限等同于运行 OSS Sync 的系统账号。只安装你审查过的代码。

## 1. 第一个插件应该怎么做

一个可运行的插件至少有四部分:

1. `manifest.json`:插件身份、版本、运行时和平台入口。
2. 当前服务器平台的预编译二进制。
3. 使用 `pkg/ossplugin` 的 Go 程序。
4. 可选的路由、Hook、管理页面、设置、数据库迁移、任务、静态资源和生命周期回调。

第一次不要直接实现完整评论系统、VIP 或同步业务。推荐按这个顺序验证:

1. 一个 `GET` 公共路由;
2. 一个 `POST` 用户路由;
3. 一个 `blog.data` Hook 或一个 AdminPage;
4. 再加入数据库表、设置、定时任务和第三方 API。

第一阶段的验收标准只有一条:

```text
安装 → 启用 → 插件进程 ready → 宿主调用 callback → 返回正确响应
```

## 2. 最小插件包

### 2.1 ZIP 目录

可执行插件示例:

```text
my-plugin.zip
├── manifest.json
├── plugin-linux-amd64
├── plugin-windows-amd64.exe
└── assets/
    └── app.js
```

manifest 中的入口路径必须和 ZIP 内路径完全一致。服务器会直接运行二进制,不会在 VM 上编译 Go。

最小 WASM 包:

```text
my-plugin.zip
├── manifest.json
└── plugin.wasm
```

安装前会校验包边界:

- ZIP 总大小最多 32 MiB;
- 最多 512 个文件;
- 解压后最多 64 MiB;
- 单个非 manifest 文件最多 32 MiB;
- `plugin.wasm` 最多 8 MiB;
- 拒绝绝对路径、`..`、反斜杠、重复条目、目录条目和符号链接。

### 2.2 最小 executable manifest

```json
{
  "id": "hello-tools",
  "name": "Hello tools",
  "version": "1.0.0",
  "description": "一个最小的可执行服务端插件",
  "api_version": 1,
  "runtime": "executable",
  "entrypoints": {
    "linux-amd64": "plugin-linux-amd64",
    "windows-amd64": "plugin-windows-amd64.exe"
  },
  "routes": [],
  "registration": {
    "routes": [
      {
        "method": "GET",
        "path": "/hello-tools",
        "callback": "hello.page",
        "auth": "public"
      }
    ]
  }
}
```

字段说明:

| 字段 | 必需 | 作用 |
| --- | --- | --- |
| `id` | 是 | 稳定插件 ID。升级时不要改变 |
| `name` | 是 | 管理页面显示名称 |
| `version` | 是 | 插件版本,用于升级识别 |
| `description` | 否 | 插件说明 |
| `api_version` | 是 | 当前为 `1` |
| `runtime` | executable 时建议写 | `executable` 或 `wasm` |
| `entrypoints` | executable 必需 | `<os>-<arch>` 到 ZIP 内入口路径的映射 |
| `routes` | 兼容声明 | 顶层 manifest 路由,用于命名空间路由 |
| `registration` | executable 建议使用 | 运行时 Hook、路由、管理页、资源、任务等完整注册 |
| `settings` | 可选 | 兼容的插件设置声明 |
| `hooks` | 可选 | 简化 Hook 声明 |
| `blog_themes` | 可选 | 博客主题资源 |
| `console_themes` | 可选 | 控制台主题资源 |
| `args` | 可选 | 启动插件时原样传入的参数 |

插件 ID 为 2～64 位，以小写字母开头，其余字符只能是小写字母、数字或 `-`。安装后的目录、数据库记录和路由都依赖这个 ID。

### 2.3 最小 Go 程序

```go
package main

import (
    "context"

    "github.com/helantianshen/oss-sync/pkg/ossplugin"
)

func main() {
    registration := ossplugin.Registration{
        Routes: []ossplugin.Route{
            {
                Method:   "GET",
                Path:     "/hello-tools",
                Callback: "hello.page",
                Auth:     "public",
            },
        },
    }

    ossplugin.RunMain(registration, func(client *ossplugin.Client) error {
        return client.On("hello.page", func(ctx context.Context, request ossplugin.Request) (ossplugin.Response, error) {
            return client.WriteTextResponse(200, "hello from OSS Sync"), nil
        })
    })
}
```

`RunMain` 负责启动 JSON Lines 协议、发送 ready 帧、注册 callback 和处理宿主请求。

插件日志必须写 stderr。**stdout 只能写插件协议帧**,不能写普通日志、调试文本或 panic 输出。

从仓库根目录构建示例:

```text
go build ./examples/server-plugin-echo/
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o plugin-linux-amd64 ./path/to/main.go
```

外部插件应使用自己的 Go module,通过 released version 或开发期明确的 `replace` 引用 `github.com/helantianshen/oss-sync/pkg/ossplugin`。不要复制一份 SDK 到插件目录。

## 3. manifest 与 RunMain registration 的关系

这套系统有两层声明,分别负责不同工作:

| 声明位置 | 作用 |
| --- | --- |
| `manifest.json` | 包校验、入口选择、安装、资源解压和恢复元数据 |
| `RunMain` 发送的 runtime registration | executable 进程当前真正启用的 Hook、路由、middleware、AdminPage、asset、setting、task、migration、dependency 和 lifecycle |

对于 executable 插件,ready 帧中的 registration 会成为当前运行实例的注册信息。因此以下内容必须保持一致:

- manifest 的 `registration.routes` 与 Go 的 `Registration.Routes`;
- manifest 的 `registration.admin_pages` 与 Go 的 `Registration.AdminPages`;
- manifest 的 `registration.assets` 与 Go 的 `Registration.Assets`;
- manifest 的 settings、tasks、migrations、dependencies 与 Go registration。

如果只改 manifest、不改 Go 二进制,常见结果是:

- 路由能安装但运行时不存在;
- CSS/JS 文件在磁盘上存在但返回 404;
- AdminPage 菜单存在但 callback 找不到;
- 升级后仍使用旧 registration。

顶层 manifest `routes` 是另一套兼容的命名空间路由声明。新 executable 插件需要 callback、`user`/`admin` 鉴权、浏览器 cookie 或动态注册时,优先使用 runtime `Registration.Routes`。

## 4. 路由与鉴权

### 4.1 runtime route

```go
ossplugin.Route{
    Method:   "POST",
    Path:     "/hello-tools/items",
    Callback: "items.create",
    Auth:     "user", // public、user 或 admin
    Priority: 10,
}
```

推荐使用插件专属前缀,例如 `/hello-tools/...`。不要抢占 `/dashboard`、`/api/sync`、`/login` 等核心路径。

runtime route 的请求对象 `ossplugin.Request` 包含:

- `Method`、`Path`;
- `Query`、`Params`;
- `Headers`、`Cookies`;
- 鉴权成功时的 `User`;
- `BodyBase64`;
- route 声明的 `Callback`;
- Hook 请求对应的 `Hook` 和 `Payload`;
- 当前 Vault settings(只在宿主允许时注入)。

鉴权值:

| `Auth` | 行为 |
| --- | --- |
| `public` | 不鉴权,匿名用户也能访问 |
| `user` | 需要普通用户身份 |
| `admin` | 需要管理员身份 |

用户路由可以使用 Bearer 身份,也可以使用控制台的 `oss_web_session` cookie。使用 cookie 的写请求(`POST`、`PUT`、`PATCH`、`DELETE`)必须提供 `X-CSRF-Token`,且它必须等于 `oss_csrf` cookie。Bearer 请求不需要 CSRF。

### 4.2 旧的命名空间路由

顶层 manifest route 的访问路径:

```text
/plugins/<plugin-id>/<declared-path>       公共命名空间
/api/plugins/<plugin-id>/<declared-path>   Bearer 鉴权命名空间
```

认证命名空间需要 `Authorization: Bearer ...`。请求 query 中包含 `vault_id` 时,宿主会检查调用者是否能访问该 Vault,并把该 Vault 的插件设置放到请求中。公共路由不会收到 Vault settings。

不要假设所有 route 都自动加插件 ID 前缀。runtime 动态路由是宿主根路径下的固定路径,命名空间路由才有 `/plugins/<plugin-id>` 前缀。

### 4.3 返回响应

```go
return client.WriteTextResponse(200, "ok"), nil

return client.WriteJSONResponse(200, map[string]any{
    "ok": true,
})
```

自定义响应:

```go
return ossplugin.Response{
    Status:  201,
    Headers: map[string]string{
        "Content-Type": "application/json; charset=utf-8",
    },
    BodyBase64: base64.StdEncoding.EncodeToString(body),
}, nil
```

单次请求/响应最多 1 MiB,一次 callback 约 2 秒。失败时返回真实 HTTP 错误码和 JSON 错误,不要把失败伪装成 200。

## 5. AdminPage 与自动控制台主题

注册管理页面:

```go
AdminPages: []ossplugin.AdminPage{
    {
        Slug:     "orders",
        Label:    "Orders",
        Callback: "orders.admin",
    },
},
```

启用后,管理员菜单出现:

```text
/dashboard/admin/plugins/<plugin-id>/page/orders
```

### 5.1 默认写法:返回 HTML 片段

插件只返回片段,不要返回完整 `<html>`:

```go
func ordersAdmin(client *ossplugin.Client) ossplugin.Handler {
    return func(context.Context, ossplugin.Request) (ossplugin.Response, error) {
        html := `<section class="ledger-panel">
  <header><h2>Orders</h2></header>
  <form method="post">
    <label><span>Search</span><input name="q"></label>
    <label><span>Status</span><select name="status"><option>open</option></select></label>
    <button class="button button--primary" type="submit">Search</button>
  </form>
</section>`
        return client.WriteTextResponse(200, html), nil
    }
}
```

宿主会自动把片段放入控制台外壳并加载:

- `console.css`;
- `theme.js` 与当前亮色/暗色状态;
- 当前控制台主题 CSS;
- 控制台侧边栏和顶部栏;
- 当前用户与 CSRF 上下文。

推荐复用这些类名:

- `.button`、`.button--primary`、`.button--danger`;
- `.text-button`;
- `.gate-panel`、`.gate-form`、`.stack-form`;
- `.ledger-panel`、`.control-grid`、`.action-cell`。

裸 `<input>` 和 `<select>` 会得到控制台基础样式。裸 `<button>` 和 `<textarea>` 不会自动得到完整按钮/编辑器样式:

```html
<button class="button button--primary" type="submit">保存</button>
<textarea class="code-textarea" name="content"></textarea>
```

### 5.2 明确接管:返回完整 HTML

如果 callback 返回内容以 `<!doctype html>` 或 `<html>` 开头,宿主原样输出。只有在插件明确负责以下内容时才这样做:

- `<html>`、`<head>`、`<body>`;
- CSS/JS 加载;
- 响应式布局;
- 亮色/暗色主题;
- CSRF、导航和登录态 UI。

完整文档不会自动继承控制台主题。

## 6. 静态资源

声明 runtime asset:

```go
Assets: []ossplugin.Asset{
    {Path: "app.js"},
    {Path: "app.css"},
},
```

ZIP:

```text
manifest.json
plugin
app.js
app.css
```

访问:

```text
/plugins/<plugin-id>/assets/app.js
/plugins/<plugin-id>/assets/app.css
```

`manifest.registration.assets` 和 Go 的 `Registration.Assets` 必须一致,否则文件可能在磁盘上存在但返回 404。

独立页面可以手动引用基础控制台 CSS:

```html
<link rel="stylesheet" href="/ui/assets/console.css">
<script src="/ui/assets/theme.js"></script>
```

这只加载基础样式和主题状态,不会自动加载用户选择的 console theme.css,也不会生成完整控制台外壳。默认推荐 AdminPage 返回片段,让宿主统一包装。

## 7. 设置

```go
Settings: []ossplugin.SettingField{
    {Key: "endpoint", Label: "Endpoint", Type: "url", MaxLength: 500},
    {Key: "label", Label: "Label", Type: "text", MaxLength: 100},
},
```

宿主设置格式支持 `text`、`textarea`、`url`、`choice` 和非嵌套 `group`，但当前 Go SDK 的 `SettingField` 只包含简单字段；`choice` 需要 `choices`，`group` 需要嵌套字段，不能直接用上面的 SDK 类型声明。设置按 Vault 保存；博客主题资源可在 `theme.json` 的 `public_settings` 中声明允许公开的键，宿主只将这些键映射到通用博客字段和 `.ThemeConfigJS`，其他公共路由不会收到 Vault settings。

SDK 读取设置:

```go
var settings struct {
    Endpoint string `json:"endpoint"`
    Token    string `json:"token"`
}
if err := client.Services().GetSetting(ctx, vaultID, &settings); err != nil {
    return ossplugin.Response{}, err
}
```

插件不能用 settings 注入任意 HTML/JavaScript。配置使用声明字段,操作界面使用 AdminPage。

## 8. Hook 与博客扩展

### 8.1 内容过滤

```go
Hooks: []ossplugin.Hook{
    {Name: "blog.content", Callback: "blog.content", Kind: "filter"},
},
```

```go
client.On("blog.content", func(_ context.Context, request ossplugin.Request) (ossplugin.Response, error) {
    content, _ := request.Payload["content"].(string)
    return client.WriteTextResponse(200, content+"\n<!-- plugin -->"), nil
})
```

`filter` 返回替换值,`action` 只执行副作用。多个 filter 按 priority 执行,前一个输出会成为下一个输入。

常用宿主 Hook 包括 `blog.content`、`theme.render` 和 `blog.data`。插件自定义 Hook 可以通过宿主 Hook 机制触发,但必须先注册。

### 8.2 blog.data

`blog.data` 是添加评论、VIP 状态、统计、推荐、文章卡片等数据的首选方式,不必重写正文。插件会收到:

- `vault_id`、`share_id`、`path`;
- `is_home`、`is_folder`;
- `method`、`request_url`、`query`;
- `headers`、`cookies`、`client_ip`。

返回 JSON 后,博客主题通过 `.PluginData[plugin-id]`读取:

```gotemplate
{{with pluginField .PluginData "comments-plugin" "items"}}
  {{range .}}<article>{{.body}}</article>{{end}}
{{end}}
```

`safeHTML` 只用于插件自己生成且已经确认安全的 HTML。用户评论、数据库正文不能未经清洗直接传给 `safeHTML`。

## 9. 数据库、迁移和宿主服务

### 9.1 插件自己的表

```go
Migrations: []ossplugin.Migration{
    {
        ID: "comments_v1",
        Statements: []string{
            `CREATE TABLE IF NOT EXISTS plugin_comments (
                id INTEGER PRIMARY KEY,
                vault_id TEXT NOT NULL,
                file_path TEXT NOT NULL,
                body TEXT NOT NULL,
                created_at DATETIME NOT NULL
            )`,
        },
    },
},
```

migration ID 按插件只应用一次。每批 migration 在一个数据库事务中执行。升级时保持迁移可增量执行,不要依赖不可逆的破坏性修改。

### 9.2 Query/Exec

```go
rows, err := client.Services().Query(ctx,
    "SELECT id, body FROM plugin_comments WHERE file_path = ? ORDER BY id DESC",
    path,
)

_, err = client.Services().Exec(ctx,
    "INSERT INTO plugin_comments (vault_id, file_path, body, created_at) VALUES (?, ?, ?, CURRENT_TIMESTAMP)",
    vaultID, path, body,
)
```

`db.exec` 对 executable 插件有意保持强能力。必须遵守:

- 优先使用插件自己的表;
- 使用参数,不要字符串拼接 SQL;
- 不要直接改核心 `files` 行;
- 不要直接改磁盘 blob;
- 不要把 token、密码写进日志;
- 不要假设核心数据库内部字段长期稳定。

### 9.3 核心模型

`host.models` 当前包含:

```text
users, vaults, files, shares, collaborations, devices
```

SDK 提供 `Models`、`ModelList`、`Create`、`Update`、`Delete`。统计、只读面板可以使用它们;评论、VIP、通知等业务数据通常应该放在插件自己的表里。

### 9.4 文件读写

```go
file, err := client.Services().GetFile(ctx, vaultID, path)
result, err := client.Services().PutFile(ctx, vaultID, path, content)
```

`PutFile` 会调用宿主真实写入管线,包含路径校验、配额、原子落盘、hash 去重、同步 revision、历史记录、长轮询通知和协作通知。**不要用 SQL 直接写 `files` 表或存储 blob。**

宿主 RPC 不会替插件完成全部业务鉴权。每次操作都应检查 `request.User`、目标 Vault 和资源归属,不要相信浏览器传入的 `vault_id`。

## 10. 定时任务

```go
Tasks: []ossplugin.Task{
    {Name: "send_digest", Schedule: "@hourly", Callback: "digest.run"},
},
```

任务使用宿主现有 Cron 调度器。任务必须幂等:进程重启或任务重试不能重复发通知或破坏数据。用插件表保存 checkpoint 或唯一事件 ID。

## 11. 生命周期与升级

```go
Lifecycle: ossplugin.Lifecycle{
    Activate:   "plugin.activate",
    Deactivate: "plugin.deactivate",
    Upgrade:    "plugin.upgrade",
    Uninstall:  "plugin.uninstall",
},
```

流程:

1. 管理员上传 ZIP;
2. 宿主校验路径、大小、manifest 和入口;
3. executable 完成 ready handshake;
4. 网页上传流程安装后自动启用;
5. 每个启用的 executable 插件运行一个常驻进程;
6. 升级时执行迁移和 lifecycle,旧包在新包启动成功前保留;
7. 停用时发送 shutdown,主题和资源选择消失;
8. 删除前必须先停用。

进程崩溃或违反协议时,等待中的调用会失败。重新启用插件会启动新进程。

## 12. 测试清单

上传前至少运行:

```text
go test ./...
go build ./...
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o plugin-linux-amd64 ./main.go
```

手工验证:

- 空数据目录安装;
- 目标 OS/架构启动;
- 公共 route 状态和响应;
- 缺少/正确/错误凭据的用户 route;
- cookie 写请求缺少 CSRF 时返回 403;
- 两个 Vault 的 settings 不互串;
- migration 重启后只执行一次;
- 停用后 route 和 task 消失;
- 升级保留数据,启动失败能恢复;
- 插件日志写 stderr 而不是 stdout;
- 用户 HTML 已转义或清洗;
- `PutFile` 的真实修改推进 revision,重复内容不产生新版本。

## 13. 常见问题

| 现象 | 首先检查 |
| --- | --- |
| 插件无法安装 | ZIP 是否有 `manifest.json`,路径是否安全,入口是否存在 |
| 安装成功但无法启用 | 手工运行二进制,检查 ready 帧、stdout 协议、stderr 错误、`api_version` |
| route 404 | 这是 manifest 命名空间 route 还是 runtime route?路径和 callback 是否一致? |
| route 401 | `Auth`、Bearer header、web session cookie 是否正确? |
| cookie POST 403 | `X-CSRF-Token` 是否等于 `oss_csrf` cookie? |
| settings 为空 | 是否是认证 route?是否带 `vault_id`?公共 route 不会收到 settings |
| AdminPage 没样式 | callback 是否返回 HTML 片段,而不是完整 `<html>`?是否使用 `.button` 类? |
| AdminPage 没侧边栏 | callback 可能返回了 `<html>`/`<!doctype html>`,主动选择了完整文档模式 |
| asset 404 | `manifest.registration.assets`、Go `Registration.Assets` 和 ZIP 路径是否三者一致? |
| 写文件没有同步 revision | 是否调用 `Services().PutFile`?不要直接改核心表/磁盘 |
| 进程不可用 | stdout 是否混入日志?响应是否超过 1 MiB?callback 是否超过约 2 秒? |

## 14. 给 AI 的插件开发提示词

把下面的上下文放在任务开头,可以明显减少 AI 臆造 API:

```text
你正在实现 OSS Sync executable server plugin。
使用 github.com/helantianshen/oss-sync/pkg/ossplugin。
插件是管理员信任的服务端代码,不是 sandbox。
不要发明 SDK 方法或 manifest 字段。先检查 pkg/ossplugin/sdk.go、
internal/serverplugin/package.go、internal/serverplugin/registration.go 和 docs。
插件业务数据使用自己的 migration/表。
文件写入必须调用 Services().PutFile,不能直接修改核心 files 表或磁盘 blob。
浏览器 cookie 写请求必须发送 X-CSRF-Token,并匹配 oss_csrf cookie。
AdminPage 默认返回 HTML 片段,让宿主自动包装控制台主题；只有明确返回
<!doctype html> 或 <html> 时才接管完整文档。
stdout 只能用于插件协议,日志写 stderr。
编码前先列出 manifest、callbacks、routes、数据模型、权限规则和测试用例。
先实现最小端到端链路,再逐步增加功能。
```

要求 AI 输出:

1. 包目录树;
2. manifest 与 runtime registration;
3. callback 到 route 的映射;
4. 请求字段和响应 JSON schema;
5. migration SQL 与资源归属规则;
6. 输入校验、鉴权和威胁模型;
7. 各目标平台的构建/打包命令;
8. 覆盖鉴权、重复保存、重试、升级和回滚的确定性测试。

如果 AI 发明不存在的 SDK 方法、混淆 route 命名空间、把 executable 当成沙箱,或建议直接修改核心文件表,不要直接采用。
