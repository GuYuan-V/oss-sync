# 二进制与 systemd 部署

## 支持范围

一键安装支持 Linux amd64 和 arm64，要求 systemd 正在运行。依赖 Python 3、curl、coreutils、util-linux 的 flock，以及 useradd/getent；脚本只检查依赖，不安装工具链或 Docker。不支持的系统/架构明确退出，不自动构建源码。

Release 独立发布 `oss.sh` 和各平台运行包，并在 `checksums.txt` 中记录脚本和包的 SHA-256：

```text
oss-sync_<版本>_linux_amd64.tar.gz
oss-sync_<版本>_linux_arm64.tar.gz
oss-sync_<版本>_darwin_amd64.tar.gz
oss-sync_<版本>_darwin_arm64.tar.gz
oss-sync_<版本>_windows_amd64.zip
oss.sh
checksums.txt
```

每个包只包含 `bin/oss-server`（Windows 为 `bin/oss-server.exe`）、`configs/config.prod.yaml` 和 `VERSION`，不包含脚本或 data。`scripts/package-release.py` 统一生成该布局；Windows 和 macOS 暂仅提供运行包，没有自动部署脚本。手动运行时以解压目录为工作目录，并设置 `OSS_ENV=prod`。

首次使用新安装器前必须发布对应格式的 Release。旧格式资产名 `oss-server_<版本>_<系统>_<架构>` 不再发布，新的后端更新器严格识别 `oss-sync_...`；旧版后端自身无法识别新的资产名，需要先通过宿主机脚本或手动替换升级。GHCR 镜像发布保留，但不再发布 Docker 离线镜像归档附件。

## 安装与目录

```bash
curl -fsSL https://raw.githubusercontent.com/helantianshen/oss-sync/main/oss.sh -o oss.sh
sudo bash oss.sh install
```

不带参数时，未安装自动进入安装流程，已安装进入管理菜单；`install` 显式安装或重装。脚本会从自身所在目录或已有 unit 识别部署位置。

默认目录 `/opt/oss-sync`，可通过 `sudo env OSS_INSTALL_DIR=/srv/oss-sync bash oss.sh install` 指定。路径只允许字母、数字、下划线、点、短横线和斜杠，不能是符号链接。由于服务的 `ProtectHome=true`，不要部署到 `/home`、`/root` 或 `/run/user` 下。

```text
/opt/oss-sync/
  bin/oss-server             程序，root 所有
  configs/config.prod.yaml   应用配置，root 可写、服务组可读
  configs/config.prod.yaml.dist  当前版本的默认模板
  service.env                管理脚本生成的环境覆盖
  deployment.env             root 专用部署元数据
  .binary-deployment         二进制部署标识
  data/                      服务账户可写，默认包含 SQLite 与正文
  VERSION                    当前安装版本
  oss.sh                     独立发布的统一安装管理脚本
/etc/systemd/system/oss-sync.service
/usr/local/bin/oss           指向 oss.sh
/usr/local/bin/oss-sync      指向 oss.sh
```

服务账户和组为 `oss-sync`，不以 root 运行。unit 仅授予绑定低端口所需的 `CAP_NET_BIND_SERVICE`，不授予其他 Linux capability。程序和配置不允许服务账户写入。systemd 设置 `WorkingDirectory` 为部署目录，使用 `OSS_ENV=prod` 加载生产 YAML；默认数据和 SQLite 路径相对该目录解析。日志写入 journal，不创建单独的日志轮转服务。

默认监听 `0.0.0.0:8080`，不会自动配置防火墙、TLS 或反向代理。健康检查访问本机 `127.0.0.1:<端口>/readyz`，YAML 中的监听地址须允许此访问。受 `ProtectHome` 与文件权限约束，外部数据库/存储目录需要自行配置合适权限；推荐使用默认 data 目录。

端口和容量保存在生成的 `service.env`，会覆盖 YAML 同名设置，通过管理命令修改。其他配置直接编辑 YAML 后执行 `sudo oss 6`。更新保留现有 YAML，将新版本模板保存为 `configs/config.prod.yaml.dist`，管理员可比较并按发布说明合并新增配置项。不要手工编辑或 source 来历不明的 `deployment.env`。

## 下载与版本

首次安装可交互选择默认加速、官方直连、自定义 HTTPS 前缀；重复执行安装器默认复用已保存前缀。管理菜单中的更新会让用户重新选择更新源，也支持环境变量：

```bash
sudo env OSS_RELEASE_SOURCE=official oss 1
sudo env OSS_RELEASE_SOURCE=proxy oss 1
sudo env OSS_RELEASE_PROXY=https://mirror.example.com/ oss 1
sudo env OSS_RELEASE_PROXY=official OSS_VERSION=v1.2.3 oss 1
```

`OSS_VERSION` 必须与远端 tag 一致；资产名去除其 `v` 前缀。未指定时只查询一次 latest-release API，再从固定 tag 下载全部文件。代理同时覆盖 API、校验文件、运行包和统一脚本；不会失败后自动直连。代理和校验文件来自同一源，SHA-256 不是独立发布签名。

`OSS_RELEASE_BASE_URL` 是高级离线/测试入口，必须同时设置不带 v 的 `OSS_VERSION`，例如固定版本的 HTTPS 目录或 `file:///绝对目录`。本地文件源同时设置 `OSS_RELEASE_PROXY=official`。部署目录和全局命令位置分别由 `OSS_INSTALL_DIR`、`OSS_GLOBAL_BIN_DIR` 指定；后者默认 `/usr/local/bin`。

## 服务管理与更新

`sudo oss` 或 `sudo oss-sync` 打开菜单；也可直接传递编号或名称：

| 操作 | 命令 |
| --- | --- |
| 更新 | `sudo oss 1` / `sudo oss update` |
| 卸载服务，保留配置和数据 | `sudo oss 2` / `sudo oss uninstall` |
| 查看状态 | `sudo oss 3` / `sudo oss status` |
| 启动、停止、重启 | `sudo oss 4`、`sudo oss 5`、`sudo oss 6` |
| 存储上限，GiB | `sudo oss 7 10` |
| 端口 | `sudo oss 8 9090` |
| 持续查看日志 | `sudo oss 9` |

更新和修改端口/容量需要短暂停机。下载、SHA-256、归档结构、程序版本和脚本语法检查均在停机前完成；下载失败不停止旧服务。包内路径、文件类型、VERSION 和二进制版本都通过校验后，备份旧程序/配置/脚本和 unit，停止服务、替换文件、启动并核对就绪版本。健康检查约等待 60 轮，每轮 HTTP 超时上限 2 秒，轮间等待 1 秒。失败时尝试恢复旧程序和配置以及原启停状态。

回退不恢复数据库、正文和数据库迁移。更新前应停止服务并做一致的数据备份；若旧程序不兼容新数据库，回退程序可能仍不能恢复业务。脚本不声称提供零停机更新，也不自动降级数据。断电或 SIGKILL 不属于 shell trap 可保证恢复的范围；保留的数据和配置可用于人工恢复。

systemd 使用 `Restart=on-failure`，异常退出由 systemd 重启。`OSS_UPDATE_MANAGER=systemd` 是部署标记，阻止现有网页/插件更新 API 启动 helper；网页仍可检查版本并提示使用宿主机命令。它不控制下载源。普通手动二进制部署没有该标记时继续使用原有自更新机制。

卸载移除 unit 和全局命令，保留运行目录、数据、配置及服务账户。执行 `sudo bash /opt/oss-sync/oss.sh install` 可以重新注册服务；自定义路径请相应替换。保留原目录才能识别旧设置。

升级时一并下载、校验并原子替换同一 Release 的 `oss.sh`。本次流程继续执行已加载的脚本，新脚本从下次调用生效；升级失败时脚本与 VERSION、配置模板一起回退。

## 旧 Docker 部署迁移

本次不提供自动迁移。新安装器遇到未标记的已有 data 目录会退出；不要覆盖旧部署目录，先使用新的空目录安装。

1. 记录旧版本、配置、端口、数据库类型和 Docker 数据挂载位置。命名卷和绑定挂载均应以 `docker inspect` 为准。
2. 停止旧容器，备份数据目录；SQLite 数据库及可能存在的 WAL/SHM 应作为整体在停机后复制。PostgreSQL 使用独立数据库备份流程。
3. 在独立目录安装新服务，可先使用另一个端口。停止新服务，再将备份数据复制到新 data 目录，合并原配置，保留新服务生成的 systemd 管理标记。
4. 将新 data 目录及复制的数据所有权设为新 `oss-sync` 账户和组；不要更改旧数据副本。确认数据库 DSN 与正文目录指向新位置。
5. 启动新服务，验证账号、Vault、同步和历史数据。若要沿用原端口，先确保旧容器停止，再通过管理命令改端口。
6. 验证后禁用旧容器自动启动；旧容器和备份的删除由管理员另行决定。不要让两个实例同时访问同一份 SQLite/正文目录。

全局 `oss` 命令如仍属于旧部署，新安装器会拒绝覆盖。可指定独立 `OSS_GLOBAL_BIN_DIR` 安装，迁移完成后再由管理员调整链接。新的 Release 不再提供旧 Docker 安装器所需的镜像归档和同名脚本，因此不要再用旧一键更新命令。继续使用 Docker 时应手动替换镜像，或先按上述步骤迁移。仓库代码变更本身不会修改正在运行的容器。

## 不支持架构的源码构建

管理员可以手动下载固定发布标签的源码，按该版本 `go.mod` 的工具链要求构建，并自行验证平台兼容性：

```bash
go build -trimpath -ldflags '-X github.com/helantianshen/oss-sync/internal/version.Version=1.2.3' -o oss-server ./cmd/server
```

其中版本必须与实际检出的发布源码一致。这不是一键安装器的自动回退路径，也不保证所有 Go 支持平台均兼容本项目。源码部署需要自行管理版本、服务和回滚；后续如增加受支持架构，应先加入发布及部署验证。
