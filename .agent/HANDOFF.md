# HANDOFF — OSS Sync 当前工作状态

## 当前目标

新增由稳定版本 Tag 触发的 GitHub Release 构建与发布流程。

## 当前状态

- PR #3 已合并到 `main`，合并提交为 `1dfba43`。
- GitHub Actions CI 已推送到 `main`，代码提交为 `4036c1b`。
- 首次 CI 运行 `32628115047` 全部通过。
- Release workflow 已推送到 `main`，代码提交为 `5096fb7`。
- 推送后 CI 运行 `32630458708` 全部通过；普通 push 未触发 Release workflow，符合 Tag-only 设计。

## 已完成工作

- 后端 Job 使用 `go.mod` 的 Go 版本，执行 `go test -race ./...` 和 `go vet ./...`。
- 插件 Job 使用 Node.js 20，执行 `npm ci`、TypeScript 检查、测试和生产构建。
- 两个 Job 在 `main` 推送和面向 `main` 的 Pull Request 上并行触发，仅授予 `contents: read`。
- Release 工作流仅响应 `v<major>.<minor>.<patch>` Tag，并要求 Tag 提交属于 `main` 历史。
- Release 交叉构建 Linux/macOS amd64/arm64 和 Windows amd64 服务端资产，同时构建插件三件套。
- Release 使用 Runner 自带的 `gh release create` 创建正式 GitHub Release。

## 重要决策

- 复用项目已有验证命令，不引入新脚本或依赖。
- `go test -race ./...` 同时覆盖普通测试，CI 不重复执行 `go test ./...`。
- 普通 PR/main 推送不发布；只有稳定 SemVer Tag 会创建 Release。
- 服务端版本由 Tag 通过 `ldflags` 注入，插件版本仍来自 `plugin/manifest.json`。
- 不引入第三方 Release Action，也不新增项目内发布脚本。

## 修改的重要文件

- `.github/workflows/ci.yml`
- `.github/workflows/release.yml`
- `.agent/HANDOFF.md`

## 验证情况

- `actionlint v1.7.12 .github/workflows/ci.yml` ✅
- GitHub Actions `Plugin` ✅（23s）：`npm ci`、TypeScript 检查、249 项测试、生产构建。
- GitHub Actions `Backend` ✅（7m54s）：`go test -race ./...`、`go vet ./...`。
- `actionlint v1.7.12 .github/workflows/ci.yml .github/workflows/release.yml` ✅
- 使用模拟版本 `0.1.3` 本地交叉构建五个服务端资产 ✅；Linux amd64 `--version` 注入校验 ✅
- Node.js 26：`npm run build` 及 `main.js` / `manifest.json` / `styles.css` 收集校验 ✅
- GitHub Actions `32630458708`：Plugin ✅（21s），Backend Race/Vet ✅（6m44s）
- 真实 Tag 触发、Release 创建与 GitHub asset digest 待首次发布验证。

## 已知问题 / 风险

- 仓库尚无 Release，真实发布与自更新闭环尚未验证。
- 服务端 Release Tag 与插件 `manifest.json` 使用独立版本，发布时需确认两者的变更意图。

## 剩余工作

- 首次真实发布需由用户明确版本号并授权推送 Tag。

## 推荐下一步

提交发布工作流后，先用首个稳定 Tag 验证资产名称、digest 与服务端/插件自更新闭环。
