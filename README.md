# OSS Sync

> Self-hosted Obsidian sync & share — Markdown, attachments and collaboration in one binary.

[![Go](https://img.shields.io/badge/Go-1.25-%2300ADD8?logo=go)](https://go.dev)
[![Node](https://img.shields.io/badge/Node-20-%23339933?logo=node.js)](https://nodejs.org)
[![Obsidian](https://img.shields.io/badge/Obsidian-1.4+-7C3AED)](https://obsidian.md)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)

[English](#) | [中文](./README_zh.md)

## Overview

OSS Sync is a self-hosted alternative to Obsidian Sync. It consists of a Go (Gin) backend and a TypeScript Obsidian plugin. Data stays on your own server: files, versions, shares and collaboration are all managed by you.

- **Vault-based**: one account can own multiple Vaults.
- **Device-aware**: each Obsidian client has a stable `client_id` with pending / approved / revoked states.
- **Offline-first**: local edits are queued, merged with three-way merge, and synced with revision-based CAS.
- **Durable queue**: pending ordinary Vault uploads are written to `.oss-sync-state.json` before transfer and resume after Obsidian restarts.

## Features

- Markdown, attachments and optional `.obsidian` config sync
- Create / modify / delete / rename with full and incremental manifest checks
- Revision-based conflict detection with “keep local / keep remote / keep both / ordered merge”
- Recycle bin with restore / permanent delete / retention
- File history: gzip snapshot, line diff, restore to any version
- Sharing: single file or folder, public URL, allow-copy toggle, GFM + wikilinks
- Blog: two built-in themes (`default`, `papertrail`), public index `/`, per-vault `/b/:vaultId`
- Collaboration on Markdown: invite / accept / revoke, real-time via SSE (fallback to long polling)
- Vault-scoped sync strategy: `user_choice` / `short_poll` / `long_poll`
- Console themes and blog themes as ZIP uploads
- Device onboarding starts with a device name; approval and per-Vault authorization are separate, and the authorized Vault list refreshes automatically in plugin settings
- WordPress-style server extensions: WASM compatibility plus administrator-trusted executable plugins with dynamic hooks, routes, middleware, admin pages, tasks, migrations, dependencies, and host RPC
- Public Go SDK: `github.com/helantianshen/oss-sync/pkg/ossplugin` for building trusted extensions without hand-written JSON Lines
- SQLite by default, PostgreSQL optional; periodic storage reconciliation

## Architecture

```
cmd/server        # HTTP entry
configs/          # dev / prod YAML
internal/
  auth            # register, login, JWT, device auth
  syncapi         # Vault revision, upload/download, rename/delete
  vaults          # Vault CRUD, members, settings
  devices         # device state, vault authorization, cursor
  collaboration   # invite, accept, content write, events
  history/recycle # snapshots, restore, retention
  blog            # themes, public pages
  serverplugin    # WASM and trusted executable plugins with namespaced routes
  webui           # console pages, admin
plugin/src        # Obsidian plugin
```

Sync uses only HTTP. Short polling `wait=0` or long polling `wait=30` per Vault. Collaboration uses an account-level channel: SSE over HTTPS (or `app://obsidian.md` with CORS), long polling over plain LAN HTTP.

## Quick Start

### Prerequisites

- Go 1.25+
- Node 20+, npm
- Obsidian 1.4+

### Run backend

```bash
go run ./cmd/server
```

First registered user automatically becomes admin. Afterwards use that admin account to create others.


Listens on `http://localhost:8080` by default, data in `data/`. Health:

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
```

Config via `OSS_ENV=dev|prod` → `configs/config.dev.yaml` / `configs/config.prod.yaml`, overridable by env: `OSS_SERVER_HOST`, `OSS_SERVER_PORT`, `OSS_DB_DRIVER`, `OSS_DB_DSN`, `OSS_STORAGE_DIR`, etc.

Postgres example:

```bash
export OSS_DB_DRIVER=postgres
export OSS_DB_DSN='postgres://user:pass@127.0.0.1:5432/oss?sslmode=disable'
go run ./cmd/server
```

### Linux binary deployment (systemd)

The unified Linux installation/management script downloads an official Linux amd64/arm64 binary, verifies its SHA-256 checksum and version, extracts its bundled production configuration, and registers `oss-sync.service`:

```bash
curl -fsSL https://raw.githubusercontent.com/helantianshen/oss-sync/main/oss.sh | sudo bash
```

Requirements: Linux with a running systemd, root access, Python 3, curl, coreutils, util-linux (`flock`), and system account tools (`useradd`, `getent`). The installer does not install Docker, Go, or other dependencies. Unsupported architectures fail with guidance to build manually; download or checksum failures never fall back to compilation.

Without arguments, the script installs when no deployment exists and opens the management menu otherwise. Use `sudo bash oss.sh install` to explicitly install or reinstall. It asks for the deployment directory, port (default `8080`), storage limit in GiB (`0` means unlimited), and Release source (default `https://gh-proxy.com/`, direct GitHub, or a custom HTTPS prefix). Set `OSS_INSTALL_DIR` to change the default `/opt/oss-sync` directory. It resolves the latest Release once and downloads all assets from that exact tag. The initial bootstrap URL above is direct; the selected proxy applies after the script has started.

The service runs as the dedicated `oss-sync` system account. The default directory contains `bin/oss-server`, `configs/config.prod.yaml`, `data/oss.db`, `service.env`, `VERSION`, and the standalone `oss.sh` management script. Existing YAML and data are preserved during updates; the new default template is saved as `configs/config.prod.yaml.dist`. The generated `service.env` overrides the port and storage limit; use the management command to change these values. Other application settings remain in the YAML. See [deployment details and Docker migration](docs/deployment.md).

```bash
sudo oss             # management menu
sudo oss 1           # update using an explicitly selected source
sudo oss 3           # systemd status
sudo oss 6           # restart
sudo oss 7 10        # set storage limit to 10 GiB
sudo oss 8 9090      # change port
sudo oss 9           # follow journal logs
```

Updates verify all artifacts before stopping the service, replace the binary, and validate `/readyz` and the expected version. On failure they attempt to restore the previous binary and configuration; database migrations are not reversed. Back up data before upgrading. For this systemd deployment, the web UI checks versions and directs administrators to the host management command; it does not launch the in-process update helper.

Non-interactive installation can set `OSS_PORT`, `OSS_STORAGE_LIMIT_GB`, `OSS_INSTALL_DIR`, and `OSS_RELEASE_PROXY=official` (or an HTTPS prefix). `OSS_VERSION` selects an exact Release tag, including its `v` prefix if present. Existing Docker deployments are not automatically migrated or deleted.

The installer requires a Release containing `oss.sh` and `oss-sync_<version>_<os>_<arch>.tar.gz` (Windows: `.zip`), listed in `checksums.txt`. Each runtime package contains `bin/oss-server` (Windows: `bin/oss-server.exe`), `configs/config.prod.yaml`, and `VERSION`; scripts and data are not included. A Release built with the updated workflow must be published before the new one-command installer can install successfully from the public latest Release.

### Docker (optional / existing deployments)

Docker remains available for source development and existing installations. It is no longer used by the one-command installer:

```bash
docker compose up -d --build
docker compose logs -f backend
```

Compose exposes port `8080` by default and stores data in the `oss-data` named volume. Update this deployment by rebuilding/replacing the image. Do not run the new binary installer over an existing Docker data directory; follow the migration guide. `docker compose down -v` deletes the data volume.

### Build plugin

Regular users can install **OSS Sync and Share** directly from Obsidian Community Plugins. The following steps are only for source development:

```bash
cd plugin
npm ci
npm run build
# outputs plugin/manifest.json, main.js, styles.css
# copy to vault: <vault>/.obsidian/plugins/oss-sync/
```

Reload Obsidian → Enable *Obsidian Sync & Share* → Set the device name → Enter a server URL including `http://` or `https://` → Sign in. Approve the device separately in the web console, then grant Vault access. The open settings page refreshes the authorized Vault list every three seconds. The plugin keeps a local `.oss-sync-state.json` (v3) at Vault root; it is never uploaded and stores the durable pending-operation queue.

## Plugins, blog templates, and console themes

The extension model has one simple rule:

- **Plugins own functionality**: settings, routes, hooks, data, admin pages, tasks, and integrations.
- **Blog templates own public-page structure and style**: `template.html`, `style.css`, optional `theme.js`, and `theme.json` capabilities.
- **Console themes own console appearance**: `theme.css`, images, and fonts.

Templates and themes must not contain functional settings. Do not add `settings.json` to a blog template. Declare settings in a plugin; OSS Sync renders them in the top-level **Plugin settings** menu and stores values per Vault.

### Fastest plugin workflow

1. Copy [`examples/server-plugin-echo`](examples/server-plugin-echo).
2. Change the plugin ID and handlers.
3. Build the executable and ZIP it with `manifest.json`.
4. Upload it from **Admin settings → Plugins**.

```powershell
cd examples/server-plugin-echo
go build -o plugin.exe .
Compress-Archive manifest.json,plugin.exe my-plugin.zip
```

Use the public Go SDK at `github.com/helantianshen/oss-sync/pkg/ossplugin`; it handles the JSON-lines process protocol. The short in-console guide covers settings, hooks, routes, pages, tasks, migrations, and host services. The complete reference is [`docs/server-plugins.md`](docs/server-plugins.md).

Executable plugins run on the server, so their binary must match the server platform. This does not require separate plugin records: one ZIP may contain `plugin.exe`, `plugin`, and `plugin-arm64`, with `windows-amd64`, `linux-amd64`, and `linux-arm64` entries in `manifest.json`. OSS Sync automatically selects the matching entry. For the simplest setup, build only the platform used by your server.

### Fastest template or theme workflow

Blog template:

```text
my-template.zip
├── template.html
├── style.css
├── theme.js
├── theme.json
└── plugin.zip   # optional functionality
```

Console theme:

```text
my-console-theme.zip
├── theme.css
├── images/
├── fonts/
└── plugin.zip   # optional functionality
```

To associate functionality, place the already-built plugin ZIP at the package root as `plugin.zip`. Uploading the template or theme automatically installs, enables, and associates the plugin. No additional association form is required. The web console includes concise **Template guide**, **Console theme guide**, and **Plugin guide** pages with copyable minimal examples.

## Configuration

| Env | Description |
|---|---|
| `OSS_ENV` | `dev` or `prod` |
| `OSS_SERVER_HOST` / `PORT` | listen address |
| `OSS_DB_DRIVER` / `DSN` | sqlite or postgres |
| `OSS_STORAGE_DIR` | file storage root |
| `OSS_ALLOW_ANONYMOUS_REGISTRATION` | initial register switch |
| `OSS_WEB_SESSION_TTL_HOURS` | web console session lifetime in hours; default `24` |
| `OSS_DEVICE_JWT_TTL_HOURS` | plugin device token lifetime in hours; default `720` (30 days) |
| `OSS_DEVICE_STALE_DAYS` | stale device threshold |
| `OSS_RECONCILE_INTERVAL_HOURS` | storage check interval |
| `OSS_UPDATE_DOWNLOAD_SOURCE` | server update source: `official`, `proxy`, or `custom` |
| `OSS_UPDATE_DOWNLOAD_PROXY` | HTTPS URL prefix used when the source is `custom` |


Vault settings (per Vault, admin can force):

- `sync_mode`: `user_choice` | `short_poll` | `long_poll`
- recycle bin days, storage quota, upload size

Server updates use `download_source` and `download_proxy` from the update section in `configs/config.dev.yaml` or `configs/config.prod.yaml`. The Admin → System → Server update panel can override them for the current check and update. The selected source is used for both release metadata and the binary download, which allows updates when the server cannot reach GitHub directly.

## Development

```bash
# backend
go test ./...
go test -race ./...
go vet ./...

# plugin
cd plugin
npm exec tsc -- --noEmit
npm test
npm run build
```

Project conventions: Go with `gofumpt` + `golangci-lint`, TypeScript strict, `uv`/`pnpm` not required, no emoji in UI, CSS via `console.css` tokens, no inline styles.

## Deployment

- Put a reverse proxy with HTTPS in front of the Go binary.
- Back up `data/` (SQLite file or Postgres dump) and the JWT secret stored in DB.
- After initial users are created, turn off open registration in *Admin → System*.
- Monitor `/readyz`; alert on non-200 or repeated reconcile failures.

## Security

- Passwords are bcrypt-hashed, never logged.
- JWT is HS256 with per-deployment random secret.
- Sessions: web uses 24-hour HttpOnly Secure SameSite cookies + CSRF; plugin uses a 30-day device-bound Bearer JWT. Expired plugin tokens are removed locally and require a new login.
- All mutating web requests require CSRF; all sync/collab requests require approved device + vault authorization.
- Server plugins accept WASM packages or administrator-trusted executable packages. WASM modules receive no WASI, filesystem, network, database, or environment access and use the legacy ABI. Executable packages declare platform entrypoints and communicate over a persistent bidirectional JSON-lines protocol; they can register arbitrary hooks, routes, middleware, admin pages, tasks, migrations, and dependencies, call host data/services through RPC, and inherit the server account's filesystem, network, database, environment, and command-execution permissions. The executable host model is intentionally comparable to WordPress plugin freedom.
- Enabled plugins may declare host-rendered settings; OSS Sync adds them to the top-level **Plugin settings** menu and stores values per Vault without allowing plugin HTML or JavaScript injection. Theme-linked settings such as Papertrail remain available outside the current Vault page and automatically select an accessible matching Vault.
- ABI v1 exposes blog/HTML content filters, theme render filters, administrator pages, and Obsidian editor commands. Comment filtering is reserved until the server has a comment entity and renderer; arbitrary JavaScript injection remains outside the host API.

## License

MIT — see [LICENSE](LICENSE).
