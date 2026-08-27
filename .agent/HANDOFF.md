# HANDOFF — OSS Sync 当前工作状态

## 当前目标

修复二轮审查 4 项：并发测试常驻化、DB 故障与 409 区分、`ResolveRegistrationRole` 清理、HANDOFF 文档准确性。

## 当前状态

- `main` 已同步至 `origin/main @ bc48ac7`（原子注册、CPU/ARM、死配置清理）；本地 1 提交待推送，修复二轮审查并通过回归。
- 原子注册、`/proc` 指标、错误语义均已验证。

## 已完成工作

- **并发测试常驻化** 新增 `internal/auth/registration_concurrent_test.go:TestConcurrentFirstRegistration_OnlyOneAdmin`（`go test -run` 可复现，10 并发仅 1 admin，原临时文件已常驻），修正 HANDOFF 空跑描述。
- **DB 故障区分** `internal/auth/accounts.go` 新增 `IsUsernameTakenError`（`UNIQUE`/`duplicate` 检测）；`internal/auth/handler.go` 与 `internal/webui/auth_pages.go` 匿名/管理员分支均改为 `if IsUsernameTakenError(err) → 409 else → 500`，避免 DB 故障误判 409。
- **Resolve 清理** 删除 `internal/auth/accounts.go:ResolveRegistrationRole`（`grep` 0 调用），`auth` 包仅保留 `CreateAccount`/`CreateAccountForAnonymousRegistration`。
- **延续一轮修复**：`registrationMu`+事务原子首注（`accounts/handler/auth_pages`）、`/proc/stat` 跳过 `guest`（`i>=8 break`）、`cpuModelName` 仅 `model name`→`Hardware`、`bootstrap.go`/`BootstrapAdminUsername`/`yaml`/`main.go`/`bootstrap_test` 精简（`net -84`）。
- **前置** 系统指标 `system_metrics_other.go` 真实实现（`/proc/cpuinfo`、`/proc/stat`、`/proc/meminfo`、`unix.Statfs`），实测 `disk 79.8GB/1081GB`、`cpu AMD Ryzen 7 7735H`。

## 重要决策

- 单机以 `registrationMu` + `db.Transaction` 保证跨 `web`/`API` 原子性；SQLite 写串行天然互斥。Postgres 多实例下 `COUNT` 在 `READ COMMITTED` 无锁仍可能并发读 0，需 `SELECT … FOR UPDATE` / 咨询锁或唯一约束，当前单实例 SQLite 部署不引入分布式锁，已在 HANDOFF 风险中明示。
- `IsUsernameTakenError` 以错误串匹配兼容 `sqlite`/`postgres`，不引入驱动特定类型。
- `ResolveRegistrationRole` 无调用直接删除，缩减 API。

## 修改的重要文件

- `internal/auth/accounts.go`、`internal/auth/handler.go`、`internal/webui/auth_pages.go`
- `internal/auth/registration_concurrent_test.go`（新增常驻）
- `internal/auth/bootstrap.go`（已删）、`internal/config/config.go`、`configs/config.*.yaml`、`cmd/server/main.go`
- `internal/webui/system_metrics_other.go`

## 验证情况

- `gofmt -w`（Go 文件） ✅
- `go vet ./...` ✅
- `go test ./... -count=1` ✅（22 包，`webui` 单独及 `-race` 均通过）
- `go test ./internal/auth -run TestConcurrentFirstRegistration_OnlyOneAdmin -count=1 -v` ✅（常驻测试，1/10 admin）
- `grep -rn BootstrapAdmin/EnsureBootstrap/ResolveRegistrationRole` 0 命中 ✅
- `grep IsUsernameTakenError` 仅在 `accounts/handler/auth_pages` ✅

## 已知问题 / 风险

- 多实例 Postgres 仍依赖进程锁，需后续加 `FOR UPDATE` 或部分唯一索引强化；单实例 SQLite 无风险。
- Darwin 无 `/proc` 回退 `runtime`。

## 剩余工作

- 无

## 推荐下一步

- 空库并发注册与 `docker compose up` 首注验证后发布补丁。
