# HANDOFF — OSS Sync 当前工作状态

## 当前目标

提供类似 1Panel 的 Linux 一键部署：通过一个 `curl | bash` 命令安装 Docker（需确认）、拉取最新多架构镜像并启动后端。

## 当前状态

- 一键安装脚本、GHCR 多架构发布、CI 冒烟检查及中英文文档已完成。
- Docker Desktop 真实验证已通过，首次部署、重复升级和数据持久化均正常。
- `0.1.11` Release 与公开多架构镜像已发布；`0.1.12` 修复无 Docker、无 TTY 时的安装提示。

## 已完成工作

- 根目录新增 `install.sh`：
  - 仅支持 Linux，校验端口、绑定地址、容器名和卷名；
  - Docker 缺失时从 `/dev/tty` 询问，确认后使用 `https://get.docker.com` 官方脚本安装；
  - 拉取 `ghcr.io/helantianshen/oss-sync-server:latest`，创建 `oss-data` 命名卷并启动容器；
  - 默认绑定 `0.0.0.0:8080`，直接提供公网访问地址；
  - 重复执行可升级，健康检查失败时恢复原容器，数据卷不删除；
  - 支持 `OSS_IMAGE`、`OSS_PORT`、`OSS_BIND_ADDRESS`、`OSS_INSTALL_DOCKER` 等覆盖。
- `.github/workflows/release.yml` 在发布二进制和插件资产之外，通过 GHCR 发布 `linux/amd64`、`linux/arm64` 镜像；正式版同步 `latest`，RC 仅发布版本标签。
- `.github/workflows/ci.yml` 使用本地构建镜像真实运行 `install.sh`，并检查独立端口 `/readyz`。
- 无 Docker 且无法读取 `/dev/tty` 时明确提示设置 `OSS_INSTALL_DOCKER=1`，不再因未赋值变量中断；CI 常驻覆盖该路径。
- `README.md` / `README_zh.md` 增加一键安装、公开访问、非交互安装、固定版本及市场安装说明。

## 重要决策

- 不新增独立“部署二进制”：Docker 多架构镜像已经包含对应架构的最新后端二进制，Shell 只负责宿主机探测与容器生命周期，减少一套重复发布/校验逻辑。
- 按用户要求默认公开监听 `0.0.0.0:8080`，不增加一次性初始化令牌；首个注册用户仍会成为管理员。
- Docker 缺失安装采用 Docker 官方 convenience script，并且只在用户明确确认后执行。
- 容器继续使用命名卷；升级只替换容器，不删除用户数据。

## 修改的重要文件

- `install.sh`（新增）
- `.github/workflows/release.yml`
- `.github/workflows/ci.yml`
- `README.md`、`README_zh.md`

## 验证情况

- `bash -n install.sh` ✅
- 非法端口失败路径 ✅
- 无 Docker 且拒绝安装的失败路径、Docker 官方安装源可达性 ✅
- 无 Docker 且无控制终端时输出非交互安装提示，不出现 `answer: unbound variable` ✅
- 无特权脚本驱动测试：首次启动、重复升级、健康状态、`0.0.0.0:18081` 端口映射、数据卷及重启策略 ✅
- CI/Release Workflow YAML 解析 ✅
- `go test ./... -count=1` ✅
- `go vet ./...` ✅
- 本机 Docker Desktop 真实验证：本地构建镜像后经 `install.sh` 首次启动及重复升级均成功，容器 `healthy`、`/readyz` 正常、端口绑定 `0.0.0.0`、命名卷数据保持；测试资源已清理 ✅
- CI 安装器真实容器冒烟 ✅

## 已知问题 / 风险

- GHCR `0.1.11` 与 `latest` 已确认可匿名读取，并包含 `linux/amd64`、`linux/arm64`。
- 默认公开监听存在“公网首个注册者成为管理员”的抢注窗口，这是用户明确接受的部署取舍。

## 剩余工作

- 在干净 Linux VPS 上执行公开的一键安装命令做最终验收。

## 推荐下一步

- CI 通过后发布一个补丁版本，在干净 Linux VPS 上执行 README 的 `curl | sudo bash` 命令做最终验收。
