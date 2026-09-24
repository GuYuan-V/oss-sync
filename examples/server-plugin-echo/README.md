# Executable server plugin example

This is the fastest starting point for a trusted server plugin: copy this directory, change the ID, remove features you do not need, then build and ZIP it. The SDK handles the process protocol.

Build the Windows package from this example directory:

```powershell
cd examples/server-plugin-echo
go build -o plugin.exe .
Compress-Archive manifest.json,plugin.exe 中文可执行插件.zip
```

Upload the ZIP from the administrator plugin page. The plugin imports `pkg/ossplugin` and registers a dynamic route, arbitrary Hook, global middleware, admin page, Cron task, migration, and lifecycle callbacks. It also demonstrates the typed host SDK with `client.Services().Models()`, handles `GET /hello` and `GET /dynamic-hello`, and prefixes `blog.content` hook content with `executable:`.

Plugins may optionally declare blog templates and console themes in `manifest.json`; those resources are installed and selected through the plugin page. See the embedded plugin guide for the resource directory contract and online text editor. Templates and themes are presentation resources owned by the plugin.
