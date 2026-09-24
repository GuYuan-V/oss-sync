# Server Plugin Guide

## 1. Plugins are the only installation entry point

Administrators upload one ZIP from **Admin settings → Plugins**. The plugin page is the single place to install, enable, disable, delete, and edit plugin text resources.

Blog templates and console themes are no longer uploaded, scaffolded, deleted, or maintained as separate packages. They are optional presentation resources carried by a plugin:

- Built-in blog templates `default` and `papertrail` always exist and cannot be deleted.
- The built-in console theme `default` always exists and cannot be deleted.
- Resources declared by enabled plugins are appended to the matching selector.
- When a plugin is disabled, its options disappear from selectors. A saved selection falls back to a built-in resource while the signed-in user keeps seeing a notice until they switch manually.

## 2. ZIP layout

Minimal WASM plugin:

```text
manifest.json
plugin.wasm
```

Executable plugin:

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

Executable plugins must include a prebuilt entrypoint for the server platform. The server runs the binary directly; it does not compile Go or execute a custom build command on the VM. Executable plugins have the file, network, database, environment, and process permissions of the server account. Upload only trusted code.

## 3. Manifest resources

`blog_themes` and `console_themes` are optional arrays. A plugin may provide neither, one kind, or both. Each resource needs a unique ID, display name, and package directory:

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

Rules:

- Resource IDs use lowercase letters, digits, `-`, and `_`, and are unique within each array.
- `path` is a directory inside the ZIP. Absolute paths, `.`, `..`, and backslashes are rejected.
- A blog resource must contain `template.html`.
- A console resource must contain `theme.css`.
- The display name appears in the user selector. The internal value is `plugin-id--resource-id`.
- Templates and themes own structure, style, and static assets. Functionality, settings, routes, data, and tasks belong to the plugin.

## 4. Blog template fields

Blog templates use Go `html/template`. Common fields:

| Field | Type | Purpose |
| --- | --- | --- |
| `.Title` | string | Browser title |
| `.ThemeName` | string | Current internal template name |
| `.ThemeBaseURL` | string | Static asset base URL |
| `.ThemeConfigJS` | JS | Safely serialized plugin settings |
| `.PluginData` | map | Data returned by enabled plugins through `blog.data` |
| `.IsHome` | bool | Whether this is the blog home |
| `.ContentHTML` | HTML | Rendered Markdown body |
| `.ArticlePost` | struct | Article title, date, categories, tags, cover, and reading metadata |

Plugin IDs may contain hyphens. Use `pluginField` for safe access:

```gotemplate
{{with pluginField .PluginData "reading-tools" "recommendations"}}
  {{range .Items}}<a href="{{.URL}}">{{.Title}}</a>{{end}}
{{end}}
```

`safeHTML` is for trusted plugin HTML. Plugins own the safety of their returned content. Inline scripts remain subject to the page CSP; use a same-origin plugin asset or route when script behavior is required.

## 5. Plugin data and request context

A plugin registering `blog.data` receives the current page context:

- `vault_id`, `share_id`, `path`
- `is_home`, `is_folder`
- `method`, `request_url`, `query`
- `headers`, `cookies`, `client_ip`

The returned JSON is exposed under `.PluginData[plugin-id]`. A failed or empty response becomes an empty object and does not make the whole page fall back. Comments, VIP, and subscription features can use this context for their own identity and authorization model and register plugin routes for submissions.

## 6. Plugin settings

Declare `text`, `textarea`, `url`, `choice`, or non-nested `group` fields in `settings`. Once enabled, the plugin appears in the top-level **Plugin settings** menu. Values are stored per Vault and sent to the plugin through `settings`.

Settings do not belong in a blog template or console theme and should not use a separate `settings.json`.

## 7. Online editing

The plugin management page can edit recognized text resources, including:

- `manifest.json`
- Template HTML, CSS, JS, JSON, and Markdown
- Console theme CSS, JSON, SVG, and other text metadata

Binary entrypoints, `plugin.wasm`, and other binary files are read-only. Saving a text resource revalidates the manifest, declared resource directories, and plugin startup; validation failure keeps the previous file and running version.

Online editing does not compile Go source into a new binary. Change plugin logic locally, build the platform binaries, and upload a new plugin package.

## 8. Lifecycle and trust

- ZIP paths, sizes, file counts, entrypoints, and manifest are validated before installation.
- Enabling a plugin materializes its declared templates and themes.
- Disabling or deleting a plugin removes its materialized resources.
- A plugin must be disabled before deletion.
- Built-in templates and themes are unaffected by plugin deletion.
- Executable plugins are trusted server code, not a sandbox.
- Uploaders are responsible for custom template and plugin-returned content.
