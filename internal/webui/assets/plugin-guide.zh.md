服务端插件负责所有功能：设置、路由、Hook、后台页面、任务、数据库表和外部服务。只有管理员可以上传插件。

## 1. 创建 Go 插件

复制 `examples/server-plugin-echo`，修改插件 ID，然后导入 SDK：

```go
import "github.com/helantianshen/oss-sync/pkg/ossplugin"
```

注册一个路由和处理函数：

```go
registration := ossplugin.Registration{
  Routes: []ossplugin.Route{{Method: "GET", Path: "/hello", Callback: "hello", Auth: "public"}},
}

ossplugin.RunMain(registration, func(client *ossplugin.Client) error {
  return client.On("hello", func(_ context.Context, _ ossplugin.Request) (ossplugin.Response, error) {
    return client.WriteTextResponse(200, "hello"), nil
  })
})
```

SDK 会处理进程协议，不要自己编写 JSON Lines 帧。

## 2. 打包插件

只需要构建服务器运行平台对应的文件。Docker/Linux amd64 插件包：

```text
manifest.json
plugin
```

`manifest.json`：

```json
{
  "id": "hello-plugin",
  "name": "Hello plugin",
  "version": "1.0.0",
  "api_version": 1,
  "runtime": "executable",
  "entrypoints": {"linux-amd64": "plugin"}
}
```

在插件源码目录构建并打包：

```bash
GOOS=linux GOARCH=amd64 go build -o plugin .
zip hello-plugin.zip manifest.json plugin
```

Windows 服务器则使用：

```powershell
go build -o plugin.exe .
Compress-Archive manifest.json,plugin.exe hello-plugin.zip
```

一个 ZIP 可以同时放多个平台二进制。在 `entrypoints` 中声明 `windows-amd64`、`linux-amd64` 和 `linux-arm64`，服务器会自动选择。只部署一种服务器平台时，只放该平台文件即可。

## 3. 添加设置和功能

功能设置写在 `registration.Settings` 中，不要写到模板或主题里。需要什么就添加什么：

```text
Hooks、Routes、Middleware、AdminPages、Assets、
Tasks、Migrations、Dependencies、Lifecycle
```

SDK 提供 `Users`、`Vaults`、`Files`、`Shares`、`Devices`、`Collaborations` 和 `Blog` 客户端，可以调用宿主数据库、核心模型、设置和其他插件 Hook。

## 4. 关联模板或主题

1. 先构建插件 ZIP。
2. 创建博客模板或控制台主题。
3. 把插件 ZIP 放到模板或主题根目录，并命名为 `plugin.zip`。
4. 上传模板或主题。

示例：

```text
my-template.zip
├── template.html
├── style.css
├── theme.js
├── theme.json
└── plugin.zip
```

OSS Sync 会自动安装、启用插件并建立关联。插件设置随后出现在一级“插件设置”中。

## 5. 固定边界

模板和控制台主题只负责外观。所有功能和功能性设置都由插件负责。可执行插件拥有服务端账号的文件、网络、数据库、环境和进程权限，只上传你信任的代码。
