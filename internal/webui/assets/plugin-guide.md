A server plugin owns functionality. It can provide settings, routes, hooks, admin pages, tasks, database tables, and integrations.

## 1. Create a Go plugin

Copy `examples/server-plugin-echo` and change the plugin ID. Import the SDK:

```go
import "github.com/helantianshen/oss-sync/pkg/ossplugin"
```

Register a route and handler:

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

The SDK handles the process protocol. Do not write JSON Lines frames yourself.

## 2. Build the ZIP

Build only for the platform that runs the server. A Docker/Linux amd64 package is:

```text
manifest.json
plugin
```

Use this manifest:

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

Build and package from the plugin source directory:

```bash
GOOS=linux GOARCH=amd64 go build -o plugin .
zip hello-plugin.zip manifest.json plugin
```

For a Windows server:

```powershell
go build -o plugin.exe .
Compress-Archive manifest.json,plugin.exe hello-plugin.zip
```

One ZIP may include multiple binaries. Add `windows-amd64`, `linux-amd64`, and `linux-arm64` entries to `entrypoints`; the server selects the matching file automatically. If you only deploy to one platform, include only that platform.

## 3. Add settings or more features

Put functional settings in `registration.Settings`, not in a template or theme. Add any of these when needed:

```text
Hooks, Routes, Middleware, AdminPages, Assets,
Tasks, Migrations, Dependencies, Lifecycle
```

The SDK service clients include `Users`, `Vaults`, `Files`, `Shares`, `Devices`, `Collaborations`, and `Blog`. They can call the host database, core models, settings, and other plugin hooks.

## 4. Link a plugin to a template or theme

1. Build the plugin ZIP.
2. Create a blog template or console theme.
3. Put the plugin ZIP at its root and name it `plugin.zip`.
4. Upload the template or theme.

Example:

```text
my-template.zip
├── template.html
├── style.css
├── theme.js
├── theme.json
└── plugin.zip
```

OSS Sync installs, enables, and associates the plugin automatically. The plugin settings then appear in the top-level Plugin settings menu.

## 5. Important boundary

Templates and console themes provide presentation only. Plugins provide all functionality and functional settings. Executable plugins run with the server account's file, network, database, environment, and process permissions. Upload only code you trust.
