# HANDOFF — OSS Sync 当前工作状态

## 当前目标

维护已发布的 `0.1.13` 一键部署：所选文件加速源同时下载 Release 校验文件和容器归档，避免 `checksums.txt` 固定直连 GitHub 超时。

## 当前状态

- 加速源修复已由提交 `9f9af43` 同步至 `origin/main`，版本标签 `0.1.13` 已发布。
- Release 工作流和主分支 CI 均通过，公开 Release 包含 amd64/arm64 镜像归档与 `checksums.txt`。
- 已使用公开官方脚本和 `gh-proxy.com` 完成真实安装，运行中的服务端版本确认是 `0.1.13`。

## 已完成工作

- `install.sh`：
  - 官方 `curl | bash` 地址保持不变，安装时可选内置 `gh-proxy.com` 文件加速、GitHub 官方下载或自定义文件加速前缀；
  - 按主机架构下载 Release 中的 `oss-sync-image_linux_amd64.tar.gz` / `arm64.tar.gz`，`checksums.txt` 和镜像归档均使用用户所选下载源，SHA-256 校验通过后执行 `docker load`；
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
  - CI 一键安装冒烟测试通过仅存在于代理路径的本地 Release 资产执行下载、校验和导入，防止校验文件重新绕过代理。
  - `0.1.13` 已公开发布，两个架构镜像归档、服务端/插件资产和 `checksums.txt` 均已生成。
- README、Compose 和 CI 已同步新增配置及安装器验证。

## 重要决策

- GitHub Release 文件代理不用于 `docker pull`；发布流程先把 OCI 镜像导出为压缩归档，安装器再通过文件代理下载并执行 `docker load`。
- 国内网络无法保证访问 GitHub Release，即使小体积 `checksums.txt` 也会超时，因此选择加速源后校验文件与归档走同一代理；SHA-256 可检测传输损坏，但不能防御代理同时替换两者，若该信任边界不可接受应升级为签名校验。
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
- 本轮 `bash -n install.sh` 与 CI/Release Workflow YAML 解析 ✅。
- 本地代理隔离测试 ✅：官方路径不存在、仅代理路径提供 `checksums.txt` 与归档时，安装、校验、`docker load` 和健康检查通过。
- Release `0.1.13` 工作流 ✅：多架构 GHCR、两个镜像归档、全部资产校验文件和 GitHub Release 发布成功。
- 主分支 CI ✅：Backend `go test -race ./...` / `go vet ./...`、Plugin 全套检查、Container 及一键安装冒烟测试全部通过。
- 公网安装 ✅：从官方 `raw.githubusercontent.com/.../install.sh` 启动，通过 `gh-proxy.com` 下载 947 B 校验文件和 12.3 MiB amd64 归档，SHA-256、导入、健康检查及 `--version=0.1.13` 均通过。
- Docker 测试容器、镜像及临时数据已清理。

## 已知问题 / 风险

- 应用层容量不是宿主文件系统硬配额；进程外写入、数据库/WAL 增长以及很小的元数据开销无法做到字节级绝对封顶。
- 内置文件代理属于第三方服务，可用性可能变化；用户可随时选择 GitHub 官方或传入自定义加速前缀。
- 校验文件与归档使用同一第三方代理时，SHA-256 只保证两者一致；它不能替代发布签名。
- 旧版 `oss-data` 命名卷不会自动迁移到新部署目录，升级时优先保证数据安全。
- 默认公开监听仍存在首个注册者成为管理员的抢注窗口，这是用户已明确接受的部署取舍。

## 剩余工作

- 在干净 Linux VPS 上使用公开 `curl | bash` 地址验证交互安装。

## 推荐下一步

- 让原测试机重新执行官方脚本并选择选项 1；若使用其他文件代理则选择选项 3，选择选项 2 仍要求服务器可直连 GitHub。
