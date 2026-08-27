# HANDOFF — OSS Sync 当前工作状态

## 当前目标

移除环境变量预制管理员路径（`OSS_ADMIN_PASSWORD`），仅保留“首个网页注册自动成为 admin”。

## 当前状态

- `main` 已推送 `dc0e98f`（修复 Linux 系统指标）；本地在 `dc0e98f` 之上待推送 1 提交，移除预置逻辑。
- 系统指标与管理员判定均已通过全量回归验证。

## 已完成工作

- **系统指标** `internal/webui/system_metrics_other.go`（`!windows`）真实实现：`cpuModelName`→`/proc/cpuinfo`、`readCPUSample`→`/proc/stat`、`memoryUsage`→`/proc/meminfo`、`diskUsage`→`unix.Statfs`；实测 `disk 79.8GB/1081GB`、`cpu AMD Ryzen 7 7735H 3.12%`、`mem 4.7/12.5GB`。
- **管理员判定** 移除预置路径：
  - `internal/auth/bootstrap.go` 改为 no-op（仅判存量 admin，不读 `OSS_ADMIN_PASSWORD`）
  - `internal/config/config.go` 移除 `OSS_ADMIN_USERNAME` 环境覆盖及注释
  - `docker-compose.yml` 移除 `OSS_ADMIN_USERNAME`/`OSS_ADMIN_PASSWORD` 必填环境
  - `.github/workflows/ci.yml` 移除 `OSS_ADMIN_PASSWORD`
  - `README.md`/`README_zh.md` 更新为“首个注册即 admin”，移除 env 示例与表格项
  - `internal/auth/bootstrap_test.go` 更新 `TestEnsureBootstrapAdminFromEnvironment` 为 no-op 预期

## 重要决策

- 管理员唯一判定 `User.Role=="admin"` 不变，首注锁 `ResolveRegistrationRole`（`adminCount==0→admin`）为唯一入口，删除重量依赖 `gopsutil`、保留最小 `/proc` 方案。
- 不引入新 env，保持部署简单；既有数据库不受影响，`EnsureBootstrapAdmin` 兼容签名。

## 修改的重要文件

- `internal/webui/system_metrics_other.go`
- `internal/auth/bootstrap.go`、`internal/auth/bootstrap_test.go`
- `internal/config/config.go`
- `docker-compose.yml`、`.github/workflows/ci.yml`
- `README.md`、`README_zh.md`

## 验证情况

- `gofmt -w` ✅
- `go vet ./...` ✅
- `go test ./...` ✅（全量通过，`webui` 单独及 `-race` 均通过）
- `go test ./internal/auth -run TestEnsureBootstrap` ✅（预置不再创建，首注仍为 admin）

## 已知问题 / 风险

- 既有通过 `OSS_ADMIN_PASSWORD` 预置的实例，升级后不影响已创建 admin；空库首次启动必须走网页注册。
- Darwin 无 `/proc` 仍回退（此前风险保留）。

## 剩余工作

- 无

## 推荐下一步

- 验证空库 `docker compose up --build` 后首个 `/register` 是否成为 admin；随后发布补丁版本并归档 `OSS_ADMIN_PASSWORD` 文档。
