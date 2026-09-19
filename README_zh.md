# OSS Sync

> 自托管的 Obsidian 同步与分享 — 单二进制搞定 Markdown、附件与协作。

[![Go](https://img.shields.io/badge/Go-1.25-%2300ADD8?logo=go)](https://go.dev)
[![Node](https://img.shields.io/badge/Node-20-%23339933?logo=node.js)](https://nodejs.org)
[![Obsidian](https://img.shields.io/badge/Obsidian-1.4+-7C3AED)](https://obsidian.md)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)

[English](./README.md) | 中文

## 简介

OSS Sync 是 Obsidian 官方同步的自托管替代，由 Go（Gin）后端与 TypeScript 插件组成。数据完全留在你自己的服务器：文件、版本、分享与协作均自主可控。

- **多 Vault**：单账号可拥有多个笔记仓库。
- **设备感知**：每个客户端以稳定 `client_id` 标识，状态 `待批准 / 已批准 / 已吊销`。
- **离线优先**：本地编辑先入队，经三方合并后按 revision 的 CAS 同步。
- **队列持久化**：普通 Vault 的待上传操作先写入 `.oss-sync-state.json`，关闭并重新打开 Obsidian 后会自动续传。

## 功能

- Markdown、附件、可选 `.obsidian` 配置同步
- 新增/修改/删除/重命名，支持全量与增量清单校验
- 基于 revision 的冲突检测，支持“保留本地 / 保留远端 / 保留双方 / 有序合并”
- 回收站：恢复/永久删除/保留期自动清理
- 文件历史：gzip 快照、逐行 diff、回退到任意版本
- 分享：单篇或文件夹、公开链接、允许复制开关、GFM 与双链
- 博客：内置 `default` 与 `papertrail` 主题，公开首页 `/` 与按 Vault 的 `/b/:vaultId`
- Markdown 协作：邀请/接受/撤销，SSE 实时（失败降级长轮询）
- 仓库级同步策略：`user_choice` / `short_poll` / `long_poll`
- 控制台与博客主题 ZIP 上传
- 首次使用先设置设备名称；设备批准与仓库授权分离，插件设置页会自动刷新已授权仓库
- 类 WordPress 的服务端扩展：兼容 WASM，并支持管理员信任的可执行插件、动态 Hook、路由、中间件、后台页面、任务、迁移、依赖和宿主 RPC
- 公开 Go SDK：`github.com/helantianshen/oss-sync/pkg/ossplugin`，无需手写 JSON Lines 即可构建受信任扩展
- 默认 SQLite，PostgreSQL 可选；定时存储对账

## 架构

```
cmd/server        # HTTP 入口
configs/          # dev / prod 配置
internal/
  auth            # 注册、登录、JWT、设备鉴权
  syncapi         # Vault revision、上传下载、重命名删除
  vaults          # Vault 增删改查、成员、设置
  devices         # 设备状态、仓库授权、游标
  collaboration   # 邀请、接受、正文写入、事件
  history/recycle # 快照、恢复、保留
  blog            # 模板、公开页
  serverplugin    # WASM 与受信任可执行插件、命名空间路由
  webui           # 控制台页面、管理后台
plugin/src        # Obsidian 插件
```

同步仅走 HTTP。短轮询 `wait=0` 或长轮询 `wait=30` 按 Vault 独立。协作走账号级通道：HTTPS 下优先 SSE（`app://obsidian.md` 放行 CORS），局域网明文 HTTP 用长轮询。

## 快速开始

### 环境要求

- Go 1.25+
- Node 20+, npm
- Obsidian 1.4+

### 启动后端

```bash
go run ./cmd/server
```

首个注册用户自动成为管理员，之后用该管理员创建其他账户。


默认监听 `http://localhost:8080`，数据在 `data/`。健康检查：

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
```

通过 `OSS_ENV=dev|prod` 选择 `configs/config.dev.yaml` / `prod.yaml`，可用环境变量覆盖：`OSS_SERVER_HOST`、`OSS_SERVER_PORT`、`OSS_DB_DRIVER`、`OSS_DB_DSN`、`OSS_STORAGE_DIR` 等。

Postgres 示例：

```bash
export OSS_DB_DRIVER=postgres
export OSS_DB_DSN='postgres://user:pass@127.0.0.1:5432/oss?sslmode=disable'
go run ./cmd/server
```

### Docker

Linux 服务器一键安装或升级：

```bash
curl -fsSL https://raw.githubusercontent.com/helantianshen/oss-sync/main/install.sh | sudo bash
```

官方引导脚本会依次询问映射端口、GitHub Release 更新源、部署路径和整个项目的数据容量上限；更新源可选加速地址、GitHub 官方地址或自定义 HTTPS 地址前缀。最新 amd64/arm64 容器归档及其 `checksums.txt` 都通过所选下载源获取，完成 SHA-256 校验后通过 `docker load` 导入。未安装 Docker 时，经确认后使用 Docker 官方脚本安装。端口留空时会避开常用服务端口，从 `10000-25565` 随机选择，并在所有网络接口开放。新部署默认将持久数据保存到 `/opt/oss-sync/data`，容量为应用层总限制，`0` 表示不限。达到上限后服务端会拒绝继续写入同步数据，并在管理后台显示项目空间用量。

安装完成后可运行全局命令 `oss` 或 `oss-sync` 打开管理菜单，用于更新、卸载、查看运行状态与空间用量、启停或重启服务，以及修改项目总容量和映射端口。每次更新都会先选择更新源，因此无需重新安装即可更换加速地址。卸载只移除容器和管理命令，项目数据默认保留。

再次执行同一安装命令会下载最新 Release 并重建容器，同时复用现有端口、部署路径和容量设置。旧版本创建的 `oss-data` 命名卷会继续保留，不自动迁移。非交互环境可使用 `OSS_PORT`、`OSS_RELEASE_PROXY=official`（或自定义 HTTPS 地址前缀）、`OSS_INSTALL_DIR`、`OSS_STORAGE_LIMIT_GB` 和 `OSS_INSTALL_DOCKER=1`。全局命令更新可使用 `OSS_RELEASE_SOURCE=official`、`OSS_RELEASE_SOURCE=proxy`，或使用 `OSS_RELEASE_PROXY=https://example.com/` 指定自定义源。高级场景仍可用 `OSS_IMAGE` 指定完整 Registry 镜像，例如 `ghcr.io/helantianshen/oss-sync-server:<版本号>`。

默认 SQLite 部署不会拉取 PostgreSQL。手动增加 Docker Hub 依赖时，可以直接使用 `docker.1panel.live/library/postgres:17` 这类 1Panel 完整镜像地址，无需改变 Release 下载源或修改 Docker daemon 配置。

源码开发环境仍可使用 Docker Compose 构建：

使用 Docker Compose 一键构建并启动 SQLite 服务环境：

```bash
docker compose up -d --build
docker compose logs -f backend
```

服务默认暴露在 `http://localhost:8080`，数据保存在 `oss-data` 命名卷。可用 `OSS_PORT=9090` 修改宿主机端口，使用 `OSS_STORAGE_MAX_TOTAL_SIZE_MB` 设置项目数据目录的应用层总容量上限。

只构建和运行后端镜像：

```bash
docker build -t oss-sync-backend .
docker run --rm -p 8080:8080 \
  -v oss-data:/app/data \
  oss-sync-backend
```


容器部署也支持在“管理后台 → 系统设置 → 服务端更新”中直接更新。镜像将服务端二进制放在可写运行目录中；校验通过并替换后，进程退出，由 Docker 的重启策略启动新二进制。需要应用镜像级变更时仍应重建或替换镜像。删除容器不会删除命名卷；`docker compose down -v` 会删除数据，请谨慎执行。

### 构建插件

普通用户可在 Obsidian 的社区插件市场搜索 **OSS Sync and Share** 直接安装。以下步骤仅用于源码开发：

```bash
cd plugin
npm ci
npm run build
# 产物 plugin/manifest.json, main.js, styles.css
# 复制到 <vault>/.obsidian/plugins/oss-sync/
```

在 Obsidian 中重载插件并启用 *Obsidian Sync & Share*：先设置设备名称，再填写包含 `http://` 或 `https://` 的服务端地址并登录。在网页控制台中先批准设备，再单独授权仓库。插件设置页保持打开时每 3 秒刷新一次仓库授权列表。插件在 Vault 根目录维护本地 `.oss-sync-state.json`（v3），该文件不会上传，并保存可在重启后续传的待处理队列。

## 插件、博客模板与控制台主题

扩展系统只有一条核心规则：

- **插件负责功能**：设置、路由、Hook、数据、后台页面、任务和外部集成。
- **博客模板只负责公开页面的结构与样式**：`template.html`、`style.css`、可选 `theme.js` 和 `theme.json` 能力声明。
- **控制台主题只负责控制台外观**：`theme.css`、图片和字体。

模板和主题不能保存功能设置。博客模板不要创建 `settings.json`；需要设置时在插件中声明，OSS Sync 会在一级 **插件设置** 菜单中渲染，并按 Vault 保存。

### 最简单的插件创建方式

1. 复制 [`examples/server-plugin-echo`](examples/server-plugin-echo)。
2. 修改插件 ID 和处理函数。
3. 编译可执行文件，与 `manifest.json` 一起打包。
4. 在 **管理员设置 → 插件管理** 上传 ZIP。

```powershell
cd examples/server-plugin-echo
go build -o plugin.exe .
Compress-Archive manifest.json,plugin.exe my-plugin.zip
```

插件使用公开 Go SDK `github.com/helantianshen/oss-sync/pkg/ossplugin`，不需要手写 JSON Lines 协议。控制台内的“插件指南”提供设置、Hook、路由、后台页面、任务、迁移和宿主服务的简明示例；完整参考见 [`docs/server-plugins.md`](docs/server-plugins.md)。

可执行插件运行在服务器上，因此二进制需要匹配服务器系统，但不需要分别管理多个插件。一个 ZIP 可以同时包含 `plugin.exe`、`plugin` 和 `plugin-arm64`，并在 `manifest.json` 中分别声明 `windows-amd64`、`linux-amd64`、`linux-arm64`，服务器会自动选择。只追求最简单使用时，只构建当前服务器平台即可。

### 最简单的模板或主题创建方式

博客模板：

```text
my-template.zip
├── template.html
├── style.css
├── theme.js
├── theme.json
└── plugin.zip   # 可选功能插件
```

控制台主题：

```text
my-console-theme.zip
├── theme.css
├── images/
├── fonts/
└── plugin.zip   # 可选功能插件
```

需要关联功能时，把已经构建好的插件 ZIP 放到模板或主题 ZIP 根目录，并命名为 `plugin.zip`。上传模板或主题时，系统会自动安装、启用并建立关联，不需要再填写关联表单。网页控制台已经内置简短的 **模板指南**、**服务器主题指南** 和 **插件指南**，都提供可直接修改的最小示例。

## 配置

| 环境变量 | 说明 |
|---|---|
| `OSS_ENV` | `dev` 或 `prod` |
| `OSS_SERVER_HOST` / `PORT` | 监听地址 |
| `OSS_DB_DRIVER` / `DSN` | sqlite 或 postgres |
| `OSS_STORAGE_DIR` | 文件存储根 |
| `OSS_ALLOW_ANONYMOUS_REGISTRATION` | 初始注册开关 |
| `OSS_WEB_SESSION_TTL_HOURS` | 网页控制台会话有效小时数，默认 `24` |
| `OSS_DEVICE_JWT_TTL_HOURS` | 插件设备令牌有效小时数，默认 `720`（30 天） |
| `OSS_DEVICE_STALE_DAYS` | 设备过期阈值 |
| `OSS_RECONCILE_INTERVAL_HOURS` | 对账周期 |
| `OSS_UPDATE_DOWNLOAD_SOURCE` | 服务端更新源：`official`、`proxy` 或 `custom` |
| `OSS_UPDATE_DOWNLOAD_PROXY` | `custom` 源使用的 HTTPS 地址前缀 |


Vault 级设置（管理员可强制）：`sync_mode`、`recycle_days`、`storage_quota`、`upload_size`。

服务端更新使用 `configs/config.dev.yaml` 或 `configs/config.prod.yaml` 的 `download_source` 与 `download_proxy`。管理员也可以在“管理后台 → 系统设置 → 服务端更新”中为本次检查和更新选择来源。所选地址同时用于获取版本信息和下载文件，因此服务器无法直连 GitHub 时仍可检查并更新。

## 开发

```bash
# 后端
go test ./...
go test -race ./...
go vet ./...

# 插件
cd plugin
npm exec tsc -- --noEmit
npm test
npm run build
```

约定：Go `gofumpt` + `golangci-lint`，TS 严格模式，无 emoji，样式走 `console.css` 变量，无内联样式。

## 部署

- 前置支持 HTTPS 的反向代理。
- 备份 `data/`（SQLite 文件或 Postgres dump）与存于 DB 的 JWT 密钥。
- 初始用户创建后在 *管理后台 → 系统设置* 关闭开放注册。
- 监控 `/readyz`，非 200 或对账持续失败时告警。

## 安全

- 密码 bcrypt 存储，永不落日志。
- JWT 为 HS256，密钥按部署随机生成并落库。
- 网页会话使用 24 小时有效的 HttpOnly Secure SameSite Cookie + CSRF；插件使用 30 天有效的设备绑定 Bearer JWT。插件令牌过期后会从本地移除并提示重新登录。
- 所有变更接口校验已批准设备 + 仓库授权。
- 服务端插件支持 WASM 包和管理员信任的可执行包。WASM 模块不提供 WASI、文件、网络、数据库或环境变量访问，通过旧 ABI 返回响应；可执行包声明平台入口并通过常驻双向 JSON Lines 协议通信，可以注册任意 Hook、路由、中间件、后台页面、任务、数据库迁移和依赖，并通过宿主 RPC 调用核心数据/服务，继承服务端账号的文件、网络、数据库、环境和命令执行权限。可执行插件的宿主模型刻意对齐 WordPress 插件的自由度。
- 启用插件可以声明宿主设置字段；系统会在一级 **插件设置** 菜单中显示，并按 Vault 保存，插件不能注入 HTML 或 JavaScript。Papertrail 等主题关联设置不再要求先进入当前仓库页面，全局入口会自动选择当前用户可访问的对应仓库。
- 当前服务端插件已经支持博客/HTML 内容过滤、主题渲染过滤、管理员插件页面和 Obsidian 编辑器命令；评论过滤预留到项目有评论实体和渲染入口后接入，任意 JavaScript 注入仍不开放。

## 许可证

MIT — 见 [LICENSE](LICENSE)。
