# HANDOFF — OSS Sync 当前工作状态

## 当前目标

维护已公开发布的 `0.1.17`，准备发布容器内网页自更新改动为 `0.1.18`。

## 当前状态

- 2026-09-23：完成自定义博客主题的服务端文章元数据适配。`renderParams` 新增 `ArticlePost`（摘要/日期/分类/标签/封面/字数/阅读分钟）与 `BannerURL`/`MobileBannerURL`，`HomePost` 增加分类/标签/封面/字数；新增 `internal/blog/frontmatter.go` 解析 Markdown frontmatter（有效块从正文隐藏，损坏块保留原文），封面附件经 `frontmatter image` 纳入分享鉴权；内置 `papertrail-settings` 插件的生效范围从仅 papertrail 泛化为所有 `supports_public_blog` 的主题，并新增横幅 URL 字段。HikariTish-Shirone 主题已在 192.168.1.221 冒烟通过（文章页/首页/封面附件/设置页）。改动尚未提交。
- `0.1.17` 已公开发布，Release 工作流 `33617267604` 成功。
- Release 包含服务端多平台二进制、Obsidian 插件、`install.sh`、`manage.sh`、amd64/arm64 离线 Docker 镜像归档和覆盖全部资产的 `checksums.txt`。
- GHCR 已发布 amd64/arm64 多架构镜像及 `latest` 标签。
- 容器内网页自更新改动已完成，尚未提交或发布；`0.1.17` 仍是当前公开版本。
- `.agent/参考脚本/` 是用户提供的 1Panel 参考资料，不属于项目产物；除非用户明确要求，不修改、不提交。
- 当前工作区包含尚未提交的公开分享、插件同步和服务端插件改动；本轮继续完成普通 Vault 同步队列恢复、设备身份登录流程、无仓库设备批准和服务地址校验，并保留结构化控制台诊断日志。

## 当前能力

### 一键部署与管理

- 官方入口保持为仓库中的 `install.sh`，安装器按架构下载 Release 镜像归档并执行 SHA-256 校验、`docker load`、容器启动和健康检查。
- 安装时支持：
  - `gh-proxy.com` 文件加速（默认）；
  - GitHub 官方下载；
  - 自定义 HTTPS 文件加速前缀；
  - 自定义端口，留空时在 `10000-25565` 中随机选择可用端口；
  - 自定义绝对部署路径；
  - 项目总容量上限（GiB，`0` 表示不限）。
- 新安装使用 `<部署路径>/data` 绑定挂载；升级会复用已有端口、数据路径、容量设置及旧版命名卷，失败时恢复旧容器。
- 安装后提供全局命令 `oss` / `oss-sync`，管理菜单支持更新、保留数据卸载、状态查看、启动、停止、重启、修改容量和修改端口。
- Docker 部署的服务端二进制位于镜像内可写运行目录，网页更新通过校验后原子替换并退出进程，由 Docker 重启策略启动新版本；`oss` / `oss-sync` 仍可用于宿主机重建镜像更新。

### 项目总容量限制

- `storage.max_total_size_mb` / `OSS_STORAGE_MAX_TOTAL_SIZE_MB` 限制整个数据目录的应用层总容量。
- 普通同步、V2 同步、协作上传和历史恢复均在提交前检查容量；覆盖写会预留历史快照空间。
- 超限返回 HTTP 507 和稳定错误码 `project_storage_quota_exceeded`，插件提供中英文提示。
- 管理后台显示项目数据目录实际用量、容量上限、监听端口和运行状态。

### 服务端在线更新

- 检查更新在页面内完成；仅检查到更高版本后显示“确认更新”。
- 原生二进制部署可选择 `gh-proxy.com`、GitHub 官方或自定义 HTTPS 前缀下载 Release 文件。
- 下载源只影响 Release 文件；版本检查仍访问 GitHub API。下载后继续校验候选资产大小与 SHA-256。
- Docker 部署同样显示更新源和网页确认更新按钮；更新完成后由 Docker 重启策略拉起新二进制。镜像级变更仍通过 `oss` / `oss-sync` 或重新执行安装脚本应用。
- 更新页逻辑位于外部静态资源，符合当前 `script-src 'self'` / `style-src 'self'` CSP。

### 服务端插件扩展

- `internal/serverplugin` 支持 WASM 和管理员信任的可执行插件，管理员可从网页上传、安装、启用、停用和删除插件。
- WASM ZIP 仍严格限制为 `manifest.json` 和 `plugin.wasm`；可执行 ZIP 支持 `manifest.json`、平台 `entrypoints` 和资源文件。两种格式都校验路径穿越、重复条目、大小、清单/API/路由格式。
- WASM 由 Wazero 在无 WASI、无文件系统/网络/数据库导入的沙箱中运行；可执行插件作为服务端账号权限下的常驻子进程运行，通过双向带请求 ID 的 JSON Lines dispatcher 并发处理请求和宿主调用。可执行插件启动时可动态注册任意 Hook、路由（含参数和优先级）、前后置中间件（含响应替换）、管理员页面/菜单/资产、Cron 任务、数据库迁移、依赖和生命周期回调，并通过公开 Go SDK `pkg/ossplugin` 与宿主 RPC 使用用户、Vault、文件、分享、设备、协作、博客、插件设置、核心模型 CRUD、SQL 和插件间 Hook。两种运行时都限制请求/响应大小和单次调用时间；旧命名空间路由仍兼容。
- 插件文件使用原子安装和统一载荷 SHA-256 完整性校验；服务启动时恢复已启用插件，篡改或损坏的包不会被加载。可执行进程崩溃或协议违规会唤醒挂起请求并可通过重新启用恢复；插件迁移按插件/迁移 ID 幂等执行。
- 管理页面位于 `/dashboard/admin/plugins`，沿用现有管理员会话、CSRF、中英文文案和响应式控制台布局。
- 插件管理页现在有嵌入式 Markdown 指南；插件 `manifest.json` 可声明与模板设置相同的 `text`、`textarea`、`url`、`choice` 和非嵌套 `group` 字段。
- 启用且声明设置的插件会自动出现在当前 Vault 侧边栏，设置按 Vault 和插件独立保存；认证插件请求可通过 `vault_id` 获得该 Vault 设置，公开路由不会获得设置。
- 主题 ZIP 可在根目录携带 `plugin.zip`；管理员上传主题时会自动校验并安装该插件，插件安装失败会回滚主题；主题在线编辑器不显示二进制插件包。
- Vault 设置页的公开博客开关根据当前主题的 `theme.json` 中 `supports_public_blog` 能力动态启用/禁用，不支持时显示提示并由服务端强制关闭。
- ABI v1 现在提供博客/HTML 内容过滤、主题渲染过滤、管理员插件页面和 Obsidian 编辑器命令；评论 hook 仅预留到项目出现评论实体和渲染入口后接入，任意 JavaScript 注入仍未开放。
- 插件设置入口已从当前 Vault 子菜单移到独立的一级“插件设置”菜单；插件列表显示由哪些博客模板/控制台主题携带，删除确认会提示并同步删除关联项。
- 博客模板已收口为纯样式/结构层：移除 Vault 模板设置页面和路由，模板不再读取、上传、复制、编辑或打包 `settings.json`；模板功能配置只能由关联插件声明，并通过插件设置页面保存后提供给模板。
- 模板、控制台主题和服务端插件中英文指南已重写为简明教程：先创建插件，再将 `plugin.zip` 放到模板/主题 ZIP 根目录即可自动安装、启用并建立关联。
- Linux/Docker 适配已在 CentOS Stream 9 `x86_64` 虚拟机验证。远端使用 Podman 5.8.2 兼容运行 Docker 镜像；多平台插件 ZIP 同时包含 Windows amd64、Linux amd64 和 Linux arm64 入口，容器实际启动 Linux amd64 入口并在重启后恢复。
- 可执行插件示例补齐 manifest 已声明的认证 `POST /echo` 回调，并把 `linux-arm64` 入口修正为独立的 `plugin-arm64`；根目录完整/最小插件 ZIP 以及博客模板、控制台主题内嵌插件包均已重新生成。
- Vault 删除备份改为写入持久化数据目录 `<data>/backups/vaults`，非 root UID `10001` 容器可创建、下载和删除备份，容器替换后备份仍在数据卷内。
- 回收站恢复现在统一执行保留期判定：到期记录在列表中显示不可恢复，REST 恢复返回 `410 recycle_retention_expired`，网页恢复同样拒绝；生产 `CompactTombstones` 清理后恢复返回 404。
- 启动存储对账会按稳定 `file_id` 关闭已删除文件的 missing/hash_mismatch 问题，避免 storage key 改为 recycle 路径后 readiness 永久 503。
- 管理员更新用户接口会返回数据库中实际持久化的角色和容量；插件关联删除对已经不存在的控制台主题可幂等重试。
- 主题公开博客能力通过主题的 `theme.json` 声明；Vault 设置页会禁用不支持主题的开关并显示提示，服务端同时强制校验。

### 发布流程

- 推送符合 SemVer（如 `0.1.17` / `v0.1.17`）格式的标签会触发 `.github/workflows/release.yml`。
- 工作流构建 Linux amd64/arm64、macOS amd64/arm64、Windows amd64 服务端包和插件资产。
- 同时发布 GHCR 多架构镜像、两个可供 `docker load` 的镜像归档、安装与管理脚本及 `checksums.txt`。

## 重要决策

- GitHub Release 文件代理不能代理 `docker pull`；因此发布离线镜像归档，安装器下载后使用 `docker load`。
- 选择代理时，`checksums.txt` 与目标资产走同一来源；SHA-256 可检测传输损坏，但不能防止代理同时替换二者。如需更强供应链保证，应增加独立签名。
- 当前一键部署使用 SQLite，没有 PostgreSQL 等额外 Docker Hub 依赖，因此不修改宿主机全局 Docker 镜像配置。
- 容量限制采用跨发行版的应用层实现，不创建 loop 文件系统，也不依赖 XFS project quota。
- 容量锁为单进程锁；当前产品按单实例部署。未来若支持多实例共享数据目录，需改为跨进程协调。
- 默认公开监听存在首个注册者成为管理员的抢注窗口，这是用户已明确接受的部署取舍。

## 重要文件

- 部署与管理：`install.sh`、`manage.sh`、`docker-compose.yml`、`Dockerfile`
- 发布：`.github/workflows/ci.yml`、`.github/workflows/release.yml`
- 容量：`internal/storagequota/`、`internal/syncapi/`、`internal/config/config.go`
- 在线更新：`internal/update/`、`internal/webui/admin_update.go`、`internal/webui/templates/admin_system.html`、`internal/webui/assets/app.js`
- 插件错误提示：`plugin/src/i18n.ts`、`plugin/src/localized-error.ts`
- 普通同步与设备登录：`plugin/src/sync-engine.ts`、`plugin/src/baseline.ts`、`plugin/src/settings-tab.ts`、`plugin/src/login-state.ts`、`internal/webui/admin.go`、`internal/webui/templates/admin_devices.html`
- 产品说明：`README.md`、`README_zh.md`

## 已完成验证

- 后端：`go test ./... -count=1`、`go vet ./...`、关键并发路径 race 测试通过。
- 插件：TypeScript 检查、265 项测试和生产构建通过；新增 pending 上传重启恢复与服务地址协议校验测试。
- 本轮同步/设备回归：普通同步启动恢复 pending 队列，短轮询下限 3 秒；设备首次登录先设置名称，改名生成新 client ID；插件不再提供设备改名/吊销操作；服务端设备批准与仓库授权分离，管理员无仓库也可批准设备。
- 安装器：Shell 语法、Compose 配置、CI 本地代理隔离测试通过。
- Docker：真实安装、升级、数据保留、容量与端口修改、启停重启、状态查看及保留数据卸载通过；新容器镜像已实际启动，`/readyz` 通过，`/app/runtime/oss-server` 对容器用户可写，选择自定义更新源时页面显示输入框，进程退出后 Docker 自动重启通过。
- 更新页：Go/WebUI 定向测试、JavaScript 语法、CSP、三种下载源解析和隔离 Docker 页面验证通过。
- `0.1.17`：公开 Release，非草稿、非预发布，15 个资产均已上传。
- 本轮公开分享回归：`go test ./... -count=1`、`go vet ./...`、模板/CSS 定向测试通过；Playwright 在桌面和 `375x667` 移动端确认目录抽屉可打开、目录链接可点击、归因文字居中且仅项目名可点击。
- 服务端插件回归：`go test ./... -count=1`、`go test -race ./... -count=1 -timeout=30m`、`go vet ./...`、`go build ./cmd/server` 和格式检查通过；新增 WASM/可执行包、公开 Go SDK、双向 JSON Lines dispatcher、动态注册、跨进程宿主 RPC、任意 Hook/路由、路由参数、全局中间件和响应替换、管理员页面/菜单/资产、Cron、幂等迁移、依赖检查、activation/deactivation/upgrade/uninstall 生命周期、完整性校验和启停删/重启测试通过。
- 服务端插件真实 QA：隔离临时数据目录启动本地服务，`/healthz` 和 `/readyz` 返回 200；浏览器注册、登录、进入插件管理页、上传 `中文可执行插件.zip`、自动启用、访问 `/plugins/hello-world-executable/hello` 和认证 `blog.content` Hook 均成功；重启后可执行路由仍返回 200。管理页桌面布局无溢出，最终页面控制台仅有预期 favicon 404。
- 模板样式边界回归：旧 `/dashboard/vaults/:vault_id/theme-settings` 路由返回 404；Papertrail 功能设置仍通过 `papertrail-settings` 插件页面保存；模板 ZIP 中的 `settings.json` 会被拒绝，脚手架、复制和下载也不会带出该文件。
- CentOS 容器 QA：远端 `/healthz`、`/readyz` 和动态 `/dynamic-hello` 均返回 200；管理员浏览器上传多平台插件包成功，后台 `Orders` 页面成功，`podman top` 确认运行 `/app/data/plugins/hello-world-executable/plugin`，容器重启后服务和插件均恢复。
- 服务端插件失败回滚回归：篡改包启用失败会保存 `LastError`；数据库删除失败会恢复插件目录和数据库记录。
- 插件/主题联动回归：插件指南、宿主设置字段、Vault 设置保存、主题携带 `plugin.zip`、模板能力标记和公开博客禁用逻辑均已通过定向与全量测试。
- 插件关联回归：插件重复依赖复用、博客模板/控制台主题关联记录、独立一级插件设置导航和关联删除提示已通过定向与全量测试。
- CentOS 9 最终全功能验收：Podman 容器以 UID/GID `10001` 运行；管理员和普通用户分别完成注册/登录、设备 pending/批准/吊销、Vault 创建/隔离/成员 participant-manager 权限、文件上传/下载/CAS/幂等/manifest/changes/ack/rename/delete、历史 diff/恢复、回收站 30 天默认与 1 天覆盖/到期 410/真实清理、分享文章与目录、公开博客、协作邀请/接受/编辑/冲突/撤销/离开/SSE/长轮询、账户设置上限、管理员用户/设备/注册开关/更新状态、博客模板和控制台主题上传/脚手架/编辑/下载/删除、WASM 与 Linux 可执行插件上传/动态路由/认证路由/Hook/Host RPC/后台页/升级/禁用/启用/删除、Vault 备份创建/下载/删除，以及同卷容器替换后的数据、插件、JWT secret 和 readiness 恢复。最终 `/healthz`、`/readyz` 均为 200，插件和关联主题删除后对应公开路由均为 404。
- 最终质量门：`go test ./...`、`go vet ./...`、Go LSP diagnostics、`npm exec tsc -- --noEmit`、`npm test`（263/263）和 `npm run build` 通过；真实浏览器验证管理员系统/数据/模板/主题/插件页面和普通用户权限边界，唯一控制台噪声是预期的 `/favicon.ico` 404。

## 已知问题 / 风险

- 应用层容量不是文件系统硬配额；进程外写入、SQLite/WAL 增长及少量元数据开销可能造成瞬时超出。
- 内置第三方文件代理的可用性可能变化，用户可切换官方源或自定义 HTTPS 前缀。
- GitHub 未认证 REST API 按出口 IP 每小时仅 60 次；共享代理出口可能返回 `403 rate limit exceeded`。
- `0.1.17` 发布前的网页更新真实下载测试因 Docker 与宿主代理共用出口且匿名额度耗尽，在版本检查阶段被 403 阻断；新镜像的容器页面、可写运行目录和重启行为已验证，真实 Release 下载替换仍未验证。
- 旧版 `oss-data` 命名卷不会主动迁移到新绑定目录；升级时优先继续使用旧卷以避免数据丢失。
- Windows 已安装 LLVM-MinGW UCRT 工具链并启用 CGO；`go test -race ./... -count=1 -timeout=30m` 全量通过，无 race 报告。动态注册、宿主 RPC、幂等迁移和全局路由/中间件测试也已通过。

## 剩余工作

- 容器内网页自更新的真实 Release 下载、二进制替换和重启后版本确认验证。
- 当前工作区这批公开分享与插件同步改动尚未提交或发布。
- 服务端 WASM/可执行插件系统及其管理页面当前也尚未提交或发布。

## 推荐下一步

- 在 GitHub API 额度可用的环境中补一次 `0.1.18` 网页更新真实下载端到端验证，包含 Docker 容器内更新后自动重启和版本确认。
- 若匿名 API 限流在真实部署中频繁出现，再实现 latest-release 非 API 回退；当前不预先增加复杂度。
- 发布服务端插件能力前，建议补充经过签名或独立信任源验证的插件包分发流程；当前管理员上传包的完整性校验用于防止安装后篡改，不解决管理员上传恶意插件的问题。可执行运行时明确不提供沙箱，任何可执行包都应按服务端代码审核。
