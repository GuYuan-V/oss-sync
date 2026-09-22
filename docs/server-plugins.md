# Server Plugins

OSS Sync supports two server-plugin runtimes:

- `wasm`: the existing WASM ABI. It remains available for packages that need the narrow host boundary.
- `executable`: a trusted, persistent process launched by the OSS Sync server account. It can use the normal operating-system permissions of that account.

Only administrators can upload plugins. The web-console upload flow validates the package, performs an executable handshake when needed, and enables the plugin after installation. Treat every executable package as server code: it can read files, use the network, access databases, and run commands.

## Package

Every ZIP must contain `manifest.json` and the files referenced by the manifest. The archive is limited to 32 MiB, has at most 512 files, and extracts to at most 64 MiB. Each non-manifest file is limited to 32 MiB; WASM is limited to 8 MiB. Absolute paths, `..`, backslashes, duplicate entries, directories, and symlinks are rejected.

### WASM package

```text
manifest.json
plugin.wasm
```

WASM packages must contain exactly those two files. Existing ABI v1 packages continue to work.

### Executable package

```text
manifest.json
bin/plugin.exe
assets/any-files-needed-by-the-plugin
```

The executable entrypoint is selected from the current server platform. `any` is the fallback when no exact `<os>-<arch>` key exists.

```json
{
  "id": "hello-world",
  "name": "Hello world",
  "version": "1.0.0",
  "description": "A trusted executable example.",
  "api_version": 1,
  "runtime": "executable",
  "entrypoints": {
    "windows-amd64": "bin/plugin.exe",
    "linux-amd64": "bin/plugin",
    "any": "bin/plugin"
  },
  "args": [],
  "routes": [
    { "method": "GET", "path": "/hello", "public": true },
    { "method": "POST", "path": "/echo", "public": false }
  ]
}
```

Entrypoint paths use forward slashes and must stay inside the package. Use a platform-specific executable when the binary format differs between operating systems. `args` are passed unchanged to the process.

Plugin IDs are lowercase names containing letters, digits, and hyphens. Route paths are fixed, absolute paths with no wildcards, query markers, or path traversal. A plugin can declare at most 32 routes.

## Executable protocol

The host starts one process for every enabled executable plugin. The working directory is the installed plugin directory. The host writes requests to stdin and reads responses from stdout as UTF-8 JSON Lines. Plugin diagnostics may be written to stderr; stdout must contain protocol frames only.

The first line sent by the plugin must be:

```json
{"type":"ready","api_version":1}
```

The host then sends a request frame. `request` is the same JSON object used by the WASM ABI:

```json
{"type":"request","id":"1","request":{"method":"GET","path":"/hello","query":{"name":["world"]}}}
```

The plugin returns a response frame with the same ID:

```json
{"type":"response","id":"1","response":{"status":200,"headers":{"Content-Type":"text/plain; charset=utf-8"},"body_base64":"aGVsbG8="}}
```

For a request-specific failure, the plugin may return:

```json
{"type":"error","id":"1","error":"request failed"}
```

When the plugin is disabled or the server shuts down, the host sends:

```json
{"type":"shutdown"}
```

The process should flush stdout after every frame and exit after `shutdown`. The host correlates responses by `id`, so requests may be processed concurrently. A missing handshake, malformed frame, process crash, or protocol violation makes all pending calls fail and the instance unavailable until it is enabled again. One invocation is limited to two seconds; requests and responses are limited to 1 MiB.

The process receives these environment variables:

```text
OSS_PLUGIN_ID          installed plugin ID
OSS_PLUGIN_DIR         absolute installed plugin directory
OSS_PLUGIN_PROTOCOL    protocol version, currently 1
```

## Host extensions

An executable plugin can register its own host extensions in the `ready` frame. Registration is not limited to OSS Sync's built-in hook names:

```json
{
  "type": "ready",
  "api_version": 1,
  "registration": {
    "hooks": [{ "name": "orders.before_save", "kind": "filter", "callback": "orders.before_save", "priority": 10 }],
    "routes": [{ "method": "POST", "path": "/orders/*", "auth": "admin", "callback": "orders.create" }],
    "middleware": [{ "name": "audit", "stage": "before", "path_prefix": "/api/", "callback": "audit.request" }],
    "admin_pages": [{ "slug": "orders", "label": "Orders", "callback": "orders.admin" }],
    "tasks": [{ "name": "sync_orders", "schedule": "@hourly", "callback": "orders.sync" }],
    "migrations": [{ "id": "orders_v1", "statements": ["CREATE TABLE orders (id INTEGER NOT NULL)"] }],
    "dependencies": [{ "plugin_id": "payments" }]
  }
}
```

Hooks are arbitrary names. `filter` callbacks return a replacement JSON value; `action` callbacks run for their side effects. Routes support exact paths and `/*` prefixes, and can require public, user, or admin authentication. Middleware can run before or after any host request. Admin pages appear in the administrator menu and are rendered by their callback. Tasks use the existing Cron scheduler. Each pending migration batch is applied in one database transaction; successful migrations are recorded once per plugin and migration ID. Dependencies must be enabled before the dependent plugin can start.

## Host SDK and services

The executable protocol is bidirectional. A plugin may send a `host_call` frame while processing a request:

```json
{"type":"host_call","id":"host-1","method":"db.query","params":{"query":"SELECT * FROM files WHERE vault_id = ?","args":["vault-id"]}}
```

The host replies with `host_response` or `host_error`. The current SDK methods include `db.query`, `db.exec`, `host.models`, `host.model.list`, `host.model.create`, `host.model.update`, `host.model.delete`, `host.vault.*`, `host.file.get`, `host.share.*`, `host.blog.*`, `host.plugin.list`, `host.settings.get`, `host.settings.set`, and `host.hook`. The Go SDK exposes these as typed `Users`, `Vaults`, `Files`, `Shares`, `Devices`, `Collaborations`, and `Blog` clients. `host.hook` lets one plugin trigger any registered action or filter by name. Core model access includes users, Vaults, files, shares, collaborations, and devices. Since executable plugins are fully trusted, `db.exec` intentionally permits plugin-owned SQL and tables. The plugin also retains normal server-account filesystem, network, environment, and process access.

This is the extension model boundary: plugins can define business features, persistence, routes, filters, admin surfaces, scheduled work, and dependencies, while the host supplies authentication, lifecycle, core data, and request dispatch. Uploading an existing plugin ID runs registered migrations and the `upgrade` lifecycle callback, then restores the previous enabled state. The old package is retained until activation succeeds. On failure the host restores its previous package, metadata, runtime registrations, and tasks; restoration errors are reported. A failed migration batch is rolled back, but committed migrations and external lifecycle effects are not reversed by package restoration. Upgrade migrations and lifecycle callbacks must remain compatible with the previous plugin version.

## Routes, settings, and hooks

Declare host hooks in `hooks`:

```json
{
  "hooks": [
    { "name": "blog.content" },
    { "name": "editor.command", "id": "format-note", "label": "Format note" }
  ]
}
```

Supported hooks are `blog.content`, `markdown.content`, `theme.render`, `admin.page`, `editor.command`, and `comment.content`. Content hooks receive host JSON and return replacement content. `editor.command` is discovered from active registrations by the Obsidian plugin and registered in its command palette. The HTTP hook endpoint accepts only this hook, requires a device-bound token plus device and user access to the Vault, and requires `metadata.plugin_id` and `metadata.command_id` to identify one registered command. The host supplies the authenticated user and device identity; internal hooks cannot be invoked through this endpoint. Editor results are applied only if the document and its content still match the request snapshot. `admin.page` is available at `/dashboard/admin/plugins/<id>/page/<slug>`.

The optional `settings` declaration uses the constrained host field schema (`text`, `textarea`, `url`, `choice`, and non-nested `group`). OSS Sync renders these fields in the top-level **Plugin settings** menu and stores values per Vault. Theme-linked settings remain available outside the current Vault page and automatically select an accessible matching Vault. Public routes never receive Vault settings. Plugins cannot inject settings HTML or JavaScript.

## WASM ABI v1

The WASM module must not import any function or memory. WASI is unavailable. It must export:

```text
memory                  exported linear memory
oss_abi_version         () -> i32, returns 1
oss_alloc               (i32 byte_length) -> i32
oss_handle              (i32 request_ptr, i32 request_length) -> i64
```

`oss_handle` returns a packed pointer and length: the high 32 bits are the response pointer and the low 32 bits are the response byte length. The request and response are the same JSON shapes used by the executable protocol.

Only `Content-Type`, `Cache-Control`, `ETag`, and `Location` response headers are accepted. Requests and responses are limited to 1 MiB, and one invocation is limited to two seconds.

## Routes

Public routes are served at `/plugins/<plugin-id>/<declared-path>`. Authenticated routes are served at `/api/plugins/<plugin-id>/<declared-path>` and require a normal OSS Sync Bearer JWT. A route declared as authenticated is never exposed through the public namespace.

When an authenticated route includes `vault_id` in its query string, OSS Sync verifies that the caller can access that Vault and includes its saved plugin settings in the request JSON. Public routes never receive Vault settings.

## Lifecycle

1. Upload the ZIP from **Admin settings -> Plugins**.
2. OSS Sync validates the manifest and payload. Executable packages must complete the `ready` handshake.
3. The web-console upload flow enables the plugin after installation. Explicit installs through the manager remain disabled until `Enable` is called.
4. Enabling starts one persistent WASM or executable instance.
5. Enabled plugins are loaded again on the next server start.
6. Disable a plugin before deleting it. Executable shutdown is graceful first and bounded by a timeout.
7. If a process crashes, its requests fail and the plugin can be enabled again to start a fresh process.

If an enabled plugin cannot be loaded after restart, it remains recorded with its last error and does not receive requests.

## Security model

WASM provides memory isolation. Executable plugins intentionally do not: they are administrator-trusted server programs. They can read and write server files, access the database, use the network, read environment variables, and run system commands, with the same permissions as the server account. Install only code that the administrator has reviewed. The server still validates ZIP boundaries and the declared protocol, but those checks are not a sandbox.

The host capabilities are namespaced HTTP routes, Vault-scoped settings, blog/HTML content filters, theme render filters, administrator pages, and Obsidian editor commands. `comment.content` is reserved until OSS Sync has a comment entity and renderer.

Host model list and typed Vault/Share RPC responses use the snake_case JSON fields declared by `pkg/ossplugin` DTOs. Database models are not the wire format.
