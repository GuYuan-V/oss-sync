# HANDOFF — OSS Sync 当前工作状态

## 当前目标

完善 Linux 一键部署与发布链路：官方脚本通过可选文件加速下载 Release 容器归档，校验后导入 Docker，同时保留端口、部署路径和整个项目的数据容量设置。

## 当前状态

- 功能实现及本机 Docker 真实验证已完成，Release 工作流语法已校验但尚未在 GitHub Actions 实际发布运行。
- 本轮项目总容量、后台页内更新和 Release 一键部署改动已提交并同步至 `origin/main`；未创建新 Release。
- 已发布版本仍为 `0.1.12`；这些修改需要后续提交并发布新版本后才能被公开安装脚本使用。

## 已完成工作

- `install.sh`：
  - 官方 `curl | bash` 地址保持不变，安装时可选内置 `gh-proxy.com` 文件加速、GitHub 官方下载或自定义文件加速前缀；
  - 按主机架构下载 Release 中的 `oss-sync-image_linux_amd64.tar.gz` / `arm64.tar.gz`，从 GitHub 官方地址获取 `checksums.txt`，SHA-256 校验通过后执行 `docker load`；
  - `OSS_RELEASE_PROXY=official` 支持非交互官方直连，自定义前缀可通过同名变量传入；`OSS_IMAGE` 仅作为高级 Registry 镜像覆盖保留；
  - 支持自定义端口、绝对部署路径和项目总容量上限（GiB，`0` 不限）；
  - 新安装使用 `<部署路径>/data` 绑定挂载，容器内继续使用 `/app/data`；
  - 重复执行会下载最新 Release，并从现有容器复用端口、部署路径和容量标签；
  - 检测到旧版命名卷时继续使用原卷，避免升级后出现空数据库；
  - 仅在目录属主不符合容器 UID/GID 时请求提权；
  - 保留健康检查、失败恢复旧容器和首次管理员安全提示。
- 应用层项目总容量：
  - `storage.max_total_size_mb` / `OSS_STORAGE_MAX_TOTAL_SIZE_MB` 配置整个数据目录上限；
  - 新增 `internal/storagequota`，统计数据目录内全部常规文件并串行化扩容写入；
  - 普通同步、V2 同步、协作上传和历史恢复在提交前检查总容量；覆盖写会为历史快照预留旧文件空间；
  - 超限返回 HTTP 507 和稳定代码 `project_storage_quota_exceeded`；插件提供中英文提示；
  - 管理后台数据页显示项目数据目录实际用量与部署上限，实时指标同步刷新。
- 在线更新页：
  - 检查和触发按钮改为纯页内按钮，不再依赖表单提交；
  - Release URL 改为只读文本，不再打开外部页面；
  - 保留原有 CSRF、确认、状态轮询和错误展示。
- Release 发布：
  - 继续发布 GHCR amd64/arm64 多架构镜像；
  - 额外导出两个可由 `docker load` 导入的架构镜像归档，归档内统一标记为 `oss-sync-server:release`；
  - 对服务端二进制、插件文件和容器归档等全部 Release 资产生成 `checksums.txt`；
  - CI 一键安装冒烟测试改为走本地 Release 归档下载、校验和导入路径。
- README、Compose 和 CI 已同步新增配置及安装器验证。

## 重要决策

- GitHub Release 文件代理不用于 `docker pull`；发布流程先把 OCI 镜像导出为压缩归档，安装器再通过文件代理下载并执行 `docker load`。
- 校验文件固定从 GitHub 官方 Release 地址获取，第三方代理只承载大体积镜像归档，避免代理同时替换归档和校验值。
- 当前 SQLite 一键部署没有 PostgreSQL 等外部 Docker Hub 依赖，因此不修改全局 `/etc/docker/daemon.json`；未来引入依赖容器时可直接使用 1Panel 的 `docker.1panel.live/library/<image>:<tag>` 完整地址。
- 项目容量采用跨发行版的应用层限制，不修改 Docker daemon、不创建 loop 文件系统，也不依赖 XFS project quota。
- 配额统计以 `storage.data_dir` 为边界。同步临时文件会先写入该目录，再在提交前计入检查；SQLite/WAL 等内部增长仍可能造成少量瞬时超出，因此它不是文件系统硬配额。
- 全局配额写锁是单进程上限；当前产品为单实例部署。若未来支持多实例写同一数据目录，需要升级为跨进程锁或共享配额服务。

## 修改的重要文件

- `install.sh`
- `internal/config/config.go`、`configs/config.*.yaml`
- `internal/storagequota/quota.go`
- `internal/syncapi/upload.go`、`v2.go`、`collab_upload.go`、`history.go`
- `internal/webui/system_metrics.go`、模板、指标脚本和语言文件
- `plugin/src/i18n.ts`、`plugin/src/localized-error.ts`
- `.github/workflows/ci.yml`、`.github/workflows/release.yml`、`docker-compose.yml`
- `README.md`、`README_zh.md`

## 验证情况

- `bash -n install.sh`、CI/Release Workflow YAML 解析、`docker compose config --quiet` ✅
- `go test ./... -count=1`、`go vet ./...` ✅
- Node.js 26 临时环境：`npm ci`、`npm exec tsc -- --noEmit`、`npm test`（250 项）、`npm run build` ✅
- Docker Desktop 真实验证 ✅：本地构建镜像，以自定义绑定目录、端口和 1 GiB 上限首次启动；重复执行升级后健康检查正常、挂载路径和容量环境变量正确、测试数据保持。
- 新 Release 安装链路真实验证 ✅：生成 Docker 压缩归档和 `checksums.txt`，经 `file://` 下载、SHA-256 校验、`docker load` 后健康启动；截断归档后安装器在导入前以“SHA-256 校验失败”退出。
- 当前 `gh-proxy.com` 对已发布的 0.1.12 Linux amd64 Release 资产 HEAD 请求返回完整文件长度 ✅。
- Docker 测试容器、镜像及临时数据已清理。

## 已知问题 / 风险

- 应用层容量不是宿主文件系统硬配额；进程外写入、数据库/WAL 增长以及很小的元数据开销无法做到字节级绝对封顶。
- 内置文件代理属于第三方服务，可用性可能变化；用户可随时选择 GitHub 官方或传入自定义加速前缀。
- 已发布的 0.1.12 不含 `oss-sync-image_linux_*.tar.gz` 和 `checksums.txt`；必须发布包含新资产的下一版本后，新安装链路才能公开使用。
- GitHub Actions 尚未实际跑过新增的跨架构镜像导出步骤，本机已验证同格式 amd64 归档，arm64 需要以首次发布结果为准。
- 旧版 `oss-data` 命名卷不会自动迁移到新部署目录，升级时优先保证数据安全。
- 默认公开监听仍存在首个注册者成为管理员的抢注窗口，这是用户已明确接受的部署取舍。

## 剩余工作

- 发布下一补丁版本，并检查两个镜像归档与 `checksums.txt` 均存在。
- 在干净 Linux VPS 上使用公开 `curl | bash` 地址验证交互安装。

## 推荐下一步

- 审查本轮安装与发布改动后提交并发布补丁版本；先观察 Release 工作流成功生成资产，再在干净 amd64/arm64 Linux 环境分别验收官方脚本与国内文件加速路径。
