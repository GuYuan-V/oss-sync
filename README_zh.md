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
# 或带环境变量
OSS_ENV=prod OSS_ADMIN_PASSWORD='strong-pass' go run ./cmd/server
```

空库首次启动会在终端隐藏输入管理员密码（默认用户 `admin`），或使用 `OSS_ADMIN_PASSWORD` 非交互创建。之后无需重复输入。

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

使用 Docker Compose 一键构建并启动 SQLite 服务环境：

```bash
OSS_ADMIN_PASSWORD='strong-pass' docker compose up -d --build
docker compose logs -f backend
```

服务默认暴露在 `http://localhost:8080`，数据保存在 `oss-data` 命名卷。可用 `OSS_PORT=9090` 修改宿主机端口。

只构建和运行后端镜像：

```bash
docker build -t oss-sync-backend .
docker run --rm -p 8080:8080 \
  -e OSS_ADMIN_PASSWORD='strong-pass' \
  -v oss-data:/app/data \
  oss-sync-backend
```

容器部署应通过重建或替换镜像升级，不使用进程内二进制自更新。删除容器不会删除命名卷；`docker compose down -v` 会删除数据，请谨慎执行。

### 构建插件

```bash
cd plugin
npm ci
npm run build
# 产物 plugin/manifest.json, main.js, styles.css
# 复制到 <vault>/.obsidian/plugins/oss-sync/
```

在 Obsidian 中重载插件并启用 *Obsidian Sync & Share*，填入服务端地址、用户名/密码，创建或绑定 Vault。插件在库根维护本地 ` .oss-sync-state.json`（v3），不上传。

## 配置

| 环境变量 | 说明 |
|---|---|
| `OSS_ENV` | `dev` 或 `prod` |
| `OSS_SERVER_HOST` / `PORT` | 监听地址 |
| `OSS_DB_DRIVER` / `DSN` | sqlite 或 postgres |
| `OSS_STORAGE_DIR` | 文件存储根 |
| `OSS_ADMIN_USERNAME` / `PASSWORD` | 冷启动管理员 |
| `OSS_ALLOW_ANONYMOUS_REGISTRATION` | 初始注册开关 |
| `OSS_DEVICE_STALE_DAYS` | 设备过期阈值 |
| `OSS_RECONCILE_INTERVAL_HOURS` | 对账周期 |

Vault 级设置（管理员可强制）：`sync_mode`、`recycle_days`、`storage_quota`、`upload_size`。

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
- 网页会话为 HttpOnly Secure SameSite Cookie + CSRF，插件为 Bearer JWT，互不混用。
- 所有变更接口校验已批准设备 + 仓库授权。

## 许可证

MIT — 见 [LICENSE](LICENSE)。
