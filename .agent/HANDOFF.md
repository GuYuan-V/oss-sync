# HANDOFF — OSS Sync 当前工作状态

## 当前目标

修复 Linux 服务端系统指标（CPU 名称/使用率、服务器磁盘、内存）显示为 0/空的问题。

## 当前状态

- `main` 已同步至 `origin/main` @ `e55209b`，本地无落后。
- 系统指标 Linux 实现已修复并通过全量回归验证。

## 已完成工作

- `internal/webui/system_metrics_other.go`（`!windows`）从占位实现替换为真实实现：
  - `cpuModelName()`：解析 `/proc/cpuinfo` 首个 `model name`，无则回退 `Hardware/Processor`，否则 `CPU`。
  - `readCPUSample()`：优先读取 `/proc/stat` 的 `cpu` 行（total = 各字段和，idle = idle+iowait），失败回退 `runtime/metrics`。
  - `memoryUsage()`：优先解析 `/proc/meminfo` 的 `MemTotal`/`MemAvailable`（缺则 `MemFree+Buffers+Cached`），失败回退 `runtime.MemStats`。
  - `diskUsage()`：`unix.Statfs(dataDir || "/")`，`total = Blocks*Bsize`，`used = total - Bfree*Bsize`。
- 保留 Windows 实现不变；非 Linux（Darwin）无 `/proc` 时自动回退到原逻辑。

## 重要决策

- 不引入新依赖（`golang.org/x/sys/unix` 已在 `go.mod`），拒绝 `gopsutil` 等重库；解析 `/proc` 为最小可移植方案。
- 磁盘以 `Bfree` 计算已用（与 `df Used` 一致），而非 `Bavail`，避免高估。
- 内存改为系统物理内存（`MemTotal-MemAvailable`），与 Windows `GlobalMemoryStatusEx` 语义对齐，替代进程堆内存。

## 修改的重要文件

- `internal/webui/system_metrics_other.go`

## 验证情况

- `gofmt -w` ✅
- `go vet ./...` ✅
- `go test ./...` ✅（22 包通过，含 `internal/webui`）
- 手动验证（容器内）：`disk 79.8GB/1081GB`、`cpu model "AMD Ryzen 7 7735H"`、`mem 4.6/12.5GB 36.8%`、`cpu busy 10.5%` 均非零 ✅

## 已知问题 / 风险

- Darwin/无 `/proc` 环境仍走回退路径，CPU 可能为 `runtime/metrics` 的 0（已验证 Linux 容器正常）。
- 旧内核无 `MemAvailable` 时估算 `free+buffers+cached`，误差可接受。

## 剩余工作

- 无；如需 Darwin 系统内存更精准，可后续补充 `sysctl` 实现。

## 推荐下一步

- 在真实部署（Docker `data` 卷路径）刷新 `/dashboard` 与 `/dashboard/metrics`，确认前端 `metrics.js` 渲染非零；随后发布补丁版本。
