# HANDOFF — OSS Sync 当前工作状态

## 当前目标

发布 `v0.1.7-rc.1` Pre-Release，验证 Tag 触发的构建与资产发布链路；同时交付 Docker 后端镜像与 Compose 服务环境。

## 当前状态

- Docker/Compose 与预发布支持已推送到 `main`，提交为 `83cb2f3`、`741fb54`。
- `v0.1.7-rc.1` Tag 已推送，GitHub Pre-Release 已成功创建。
- CI 与 Release workflow 均已完成并通过。

## 已完成工作

- `Dockerfile` 使用 Go/Alpine 多阶段构建，仅打包后端、CA 证书和生产配置，并以 UID/GID 10001 运行。
- `docker-compose.yml` 使用项目默认 SQLite、`oss-data` 命名卷、必填管理员密码和 `/readyz` 健康检查。
- CI 新增 `Container` Job，在 GitHub Runner 构建 Compose、等待健康状态并访问 `/readyz`。
- Release 工作流支持 `vX.Y.Z` 和 `vX.Y.Z-rc.N`；RC Tag 自动创建 Pre-Release。
- Release 产物中的插件 `manifest.json` 会写入 Tag 版本，避免源码版本与发布版本不一致。
- 稳定版发布仍构建 Linux/macOS amd64/arm64、Windows amd64 服务端资产和插件三件套。

## 重要决策

- 单服务环境复用 SQLite，不额外引入 PostgreSQL；外部数据库仍可通过现有 `OSS_DB_*` 覆盖。
- 容器通过替换镜像升级，不执行容器内二进制自更新。
- Pre-Release 用于验证发布链路；服务端更新器继续拒绝预发布版本，GitHub latest 接口也不会把 Pre-Release 当作正式更新。
- Release 仍只接受 `main` 历史中的 Tag，并拒绝其他 Tag 格式。

## 修改的重要文件

- `Dockerfile`、`docker-compose.yml`、`.dockerignore`
- `.github/workflows/ci.yml`、`.github/workflows/release.yml`
- `README.md`、`README_zh.md`、`AGENTS.md`
- `.agent/HANDOFF.md`

## 验证情况

- `actionlint v1.7.12 .github/workflows/ci.yml .github/workflows/release.yml` ✅
- Prettier 3.6.2 检查 CI、Release、Compose YAML ✅
- `git diff --check` ✅（仅报告仓库既有 CRLF 转换提示）
- Release 插件 manifest 版本注入逻辑以 `0.1.7-rc.1` 校验 ✅
- Dockerfile 等价的本地静态 Go 构建与 `/readyz` 启动检查 ✅
- GitHub CI `33074048691` ✅：Plugin 22s、Container 55s、Backend 8m28s。
- GitHub Release `33074798074` ✅：`v0.1.7-rc.1` Pre-Release 和八个带 SHA-256 digest 的资产。
- 下载 Linux amd64 发布包并执行 `--version`，输出 `0.1.7-rc.1` ✅
- 下载插件 `manifest.json`，版本为 `0.1.7-rc.1` ✅

## 已知问题 / 风险

- Pre-Release 不会被正式版在线更新入口发现，这是预期行为。
- 当前 WSL 无 Docker CLI；容器链路已由 GitHub Runner 验证。

## 剩余工作

- 正式版上线时推送稳定 Tag（例如 `v0.1.7`），再验证服务端与插件在线更新入口。

## 推荐下一步

在隔离测试环境手动安装 `v0.1.7-rc.1` 插件与服务端资产；确认无问题后发布 `v0.1.7`。
