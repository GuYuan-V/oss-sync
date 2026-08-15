# OSS Sync

<<<<<<< HEAD
项目地址：https://github.com/helantianshen/oss-sync

=======
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
OSS Sync 是一个自托管的 Obsidian 同步与分享项目，由 Gin 后端和 Obsidian 插件组成。后端保存账户、Vault、设备游标和文件元数据，插件负责监听本地文件变化、维护同步基线并处理冲突。

## 功能

- Markdown、附件和可选的 `.obsidian` 配置同步
- 文件新增、修改、删除和重命名
- 全量清单校验与基于 revision 的增量同步
- 多 Vault 隔离，一个账户可管理多个笔记仓库
<<<<<<< HEAD
- 设备状态机：待批准 / 已批准 / 已吊销，仓库级设备授权
- 冲突检测及远端覆盖、本地覆盖、保留双方三种处理方式
- 文件与文件夹公开分享、允许复制开关
- 文件历史：gzip 快照、逐行 diff、版本恢复
- 回收站：删除正文保留、恢复、永久删除、保留期自动清理
- Markdown 文件协作：邀请、接受、协作者正文写入、事件通知
- 同步策略：`user_choice` / 强制短轮询 / 强制长轮询
- 内置 `default` 与 `papertrail` 博客模板，正文居中、文章目录、三态主题切换、权限控制复制与返回顶部
- 公开博客目录、自定义模板上传 / 脚手架 / 编辑 / 下载 / 删除
- 统一网页控制台：侧边栏导航、仓库、设备、分享、历史、回收站、个人中心与管理后台
- SQLite 默认存储，可切换 PostgreSQL
- 启动及定时存储对账

同步只使用 HTTP API。插件按服务端策略使用短轮询（`changes?wait=0`）或长轮询（`changes?wait=30`），服务端最长等待 30 秒，不依赖 WebSocket。协作事件使用账户级实时通道：HTTPS 或本机地址优先使用 SSE，服务器仅为 Obsidian 桌面的 `app://obsidian.md` Origin 返回跨域许可；局域网明文 HTTP 自动使用账户级长轮询，事件到达即唤醒，不等待 30 秒超时。
=======
- 多设备游标、设备重命名和吊销
- 冲突检测及远端覆盖、本地覆盖、保留双方三种处理方式
- 文件和文件夹公开分享
- Markdown、Obsidian 双链和本地图片渲染
- 网页注册页和管理员控制台
- SQLite 默认存储，可切换 PostgreSQL
- 启动及定时存储对账

同步只使用 HTTP API。插件默认定时轮询远端 revision，服务端变更接口也支持最长 30 秒的等待参数，不依赖 WebSocket。
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b

## 项目结构

```text
cmd/server/          后端入口
configs/             开发和生产配置
<<<<<<< HEAD
internal/            认证、同步、Vault、设备、历史、回收站、协作、模板和存储逻辑
=======
internal/            认证、同步、Vault、设备、分享和存储逻辑
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
plugin/src/          Obsidian 插件源码
plugin/tests/        插件同步逻辑测试
```

## 环境要求

- Go 1.25+
- Node.js 20+
- npm
- Obsidian 1.4+

## 启动后端

项目默认使用开发配置和 SQLite：

```bash
go run ./cmd/server
```

第一次启动时，如果数据库里还没有管理员，服务会在终端中隐藏输入并确认管理员密码。管理员用户名默认为 `admin`。创建完成后，以后的启动不会再次询问密码。

非交互式部署需要在首次启动时提供初始密码：

```bash
OSS_ADMIN_PASSWORD='replace-with-a-strong-password' go run ./cmd/server
```

项目不内置通用管理员密码。数据库中已有管理员后，不再需要保留 `OSS_ADMIN_PASSWORD`。

默认监听 `http://localhost:8080`，数据写入项目下的 `data/` 目录。

健康检查：

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
```

`/readyz` 除了检查数据库连接，还会在存在未解决的文件缺失或哈希不一致时返回 `503`。

### 配置环境

通过 `OSS_ENV` 选择配置文件：

```bash
OSS_ENV=dev go run ./cmd/server
OSS_ENV=prod go run ./cmd/server
```

对应文件：

- `configs/config.dev.yaml`
- `configs/config.prod.yaml`

以下环境变量会覆盖 YAML 配置：

| 环境变量 | 说明 |
| --- | --- |
| `OSS_ADMIN_USERNAME` | 首次创建的管理员用户名，默认 `admin` |
| `OSS_ADMIN_PASSWORD` | 非交互式首次启动所需的管理员密码 |
| `OSS_ALLOW_ANONYMOUS_REGISTRATION` | 新数据库注册开关的初始值；之后由管理面板控制 |
| `OSS_DB_DRIVER` | `sqlite` 或 `postgres` |
| `OSS_DB_DSN` | 数据库连接字符串 |
| `OSS_SERVER_HOST` | HTTP 监听地址 |
| `OSS_SERVER_PORT` | HTTP 监听端口 |
| `OSS_STORAGE_DIR` | 文件存储目录 |
| `OSS_DEVICE_STALE_DAYS` | 设备失效天数 |
| `OSS_RECONCILE_INTERVAL_HOURS` | 存储对账周期 |

生产环境首次启动会自动生成 48 字节随机 JWT 签名密钥并保存到数据库；之后每次启动均复用该值。请妥善备份数据库，遗失数据库会使所有现有会话失效。

```bash
export OSS_ENV=prod
export OSS_ADMIN_PASSWORD='replace-with-a-strong-password'
go run ./cmd/server
```

<<<<<<< HEAD
### 切换 SQLite / PostgreSQL

数据库驱动只能在启动时通过配置文件或环境变量选择，不能在网页中热切换。当前使用的驱动会显示在管理员网页的“系统设置 → 数据库配置”中，但修改配置后必须重启服务。

- SQLite（默认）：`database.driver: sqlite`，`database.dsn: data/oss.db`。
- PostgreSQL：将驱动改为 `postgres`，并设置 PostgreSQL DSN。推荐在部署环境中使用环境变量，避免把密码提交到配置文件：

```bash
export OSS_DB_DRIVER=postgres
export OSS_DB_DSN='postgres://oss_user:replace-with-password@127.0.0.1:5432/oss_sync?sslmode=disable'
go run ./cmd/server
```

也可以在 `configs/config.prod.yaml` 中设置：

```yaml
database:
  driver: postgres
  dsn: "postgres://oss_user:replace-with-password@127.0.0.1:5432/oss_sync?sslmode=disable"
```

切换到 PostgreSQL 会连接一个独立数据库并自动创建表结构，但不会迁移现有 SQLite 数据；请在切换前备份 SQLite 数据库并自行完成数据迁移。

=======
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
### 用户注册

服务启动流程如下：

1. 数据库没有管理员时，从 `OSS_ADMIN_PASSWORD` 创建管理员，或等待终端隐藏输入密码。
2. 数据库已有管理员时直接启动，不修改现有管理员。
3. 新数据库默认开放普通用户注册。
<<<<<<< HEAD
4. 管理员访问 `http://localhost:8080/login` 登录统一网页控制台，在"管理后台 → 系统设置"打开或关闭注册。
5. 普通用户访问 `http://localhost:8080/register` 创建账户，再使用相同用户名和密码登录 Obsidian 插件或网页控制台。
6. 登录后在插件的 Vault 区域手动创建服务端仓库，或明确选择已有仓库；只有这两种操作才会绑定并执行全量同步。

网页注册只能创建 `user` 角色，不能获取管理员权限，也不会自动创建 Vault。关闭注册只阻止新账户创建，已有账户仍可登录和同步。网页会话使用独立 HttpOnly cookie，与插件的 JWT Bearer token 互不混用；修改状态的网页请求必须携带 CSRF token。
=======
4. 管理员访问 `http://localhost:8080/admin` 登录，在控制台打开或关闭注册。
5. 普通用户访问 `http://localhost:8080/register` 创建账户，再使用相同用户名和密码登录 Obsidian 插件。
6. 登录后在插件的 Vault 区域手动创建服务端仓库，或明确选择已有仓库；只有这两种操作才会绑定并执行全量同步。

网页注册只能创建 `user` 角色，不能获取管理员权限，也不会自动创建 Vault。关闭注册只阻止新账户创建，已有账户仍可登录和同步。
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b

配置中的 `allow_anonymous_registration`（或对应环境变量）只决定新数据库第一次创建注册设置时的初始值。管理员在网页保存后，选择会写入数据库，重启不会被配置覆盖：

```yaml
auth:
  bootstrap_admin_username: "admin"
  allow_anonymous_registration: true
```

## 构建插件

```bash
cd plugin
npm ci
npm run build
```

构建后需要将以下文件放入 Obsidian Vault 的 `.obsidian/plugins/oss-sync/`：

```text
plugin/manifest.json
plugin/main.js
plugin/styles.css
```

在 Obsidian 中重新加载第三方插件并启用 **Obsidian Sync & Share**。随后在插件设置中填写：

1. 后端地址
2. 用户名和密码
3. 没有账户时打开网页注册，有账户时直接登录
4. 手动创建并同步服务端 Vault，或选择已有 Vault 进行绑定

插件会在本地 Vault 根目录维护 `.oss-sync-state.json`。该文件保存服务端 revision、待处理操作和冲突状态，不会上传到服务端。

## 同步机制

每个服务端 Vault 维护独立、单调递增的 revision。客户端提交修改时必须携带本地基线中的 `base_revision`：

- revision 一致时写入新版本
- revision 不一致时返回 `409`，由插件记录冲突
- 删除会立即移除服务端正文并保留墓碑
- 设备确认游标后，定时任务才能压缩对应墓碑
- 客户端游标落后于已压缩 revision 时，服务端返回 `410`，插件重新获取完整清单

每个修改请求还带有稳定的 operation ID，用于避免重试造成重复写入。服务端按 Vault 和路径加锁，保证并发修改时 revision 和磁盘内容一致。

插件支持两种同步入口：

- 全量同步：读取完整服务端清单，同时扫描本地文件
- 增量同步：读取上次游标之后的服务端变化，并处理本地待提交操作

## 文件存储

默认数据目录结构：

```text
data/
├── oss.db
└── vaults/
    └── <vault-id>/
        ├── files/
        ├── tmp/
        └── quarantine/
```

服务启动时会执行一次存储对账，之后按配置周期重复执行。对账会：

- 校验数据库哈希与磁盘内容
- 恢复可验证的上传或重命名备份
- 清理过期临时文件
- 隔离无数据库记录的文件
- 记录无法自动修复的存储问题

## 分享

<<<<<<< HEAD
登录用户可以从 Obsidian 文件菜单或网页控制台创建文章或文件夹分享。公开地址格式为：
=======
登录用户可以从 Obsidian 文件菜单创建文章或文件夹分享。公开地址格式为：
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b

```text
http://<server>/p/<share-id>
```

<<<<<<< HEAD
分享页面支持 GFM、Obsidian 双链、本地图片以及可按 Vault 指定的主题。只有被分享内容实际引用的附件才能通过公开资源接口访问。Vault 所有者或管理员在仓库设置中开启“公开首页博客”后，服务器根路径会列出该 Vault，并通过 `/b/<vault-id>` 提供博客首页。博客首页只列出该仓库已单篇分享且仍存在的 Markdown 文章；未开启的 Vault 和未分享文章不会出现在公开页面。

## 博客模板

内置两个只读模板：`default`（简洁阅读）和 `papertrail`（轻量博客）。内置模板不可下载、删除或在线编辑。

自定义模板保存在 `data/themes/<主题名>/`，来源包括上传 ZIP、内置模板脚手架和旧版主题。管理员在"管理后台 → 模板管理"可以：

1. 上传模板 ZIP（校验大小、文件数、路径穿越、符号链接）；
2. 以任意内置或自定义模板为基础创建完整的可编辑副本；
3. 浏览和编辑自定义模板中的文本文件；
4. 下载自定义模板 ZIP；
5. 删除未被仓库使用的自定义模板。

创建副本会生成以下目录，已有同名目录绝不会被覆盖：
=======
分享页面支持 GFM、Obsidian 双链、本地图片以及可按 Vault 指定的主题。只有被分享内容实际引用的附件才能通过公开资源接口访问。

### 自定义博客主题

Vault 的成员权限和博客主题均由平台管理员在 `/admin` 管理。打开某个 Vault 的“管理成员”页面，在“博客模板”区可以：

1. 输入 `default` 以使用内置主题；
2. 创建一份开发模板并自动启用；或
3. 启用已经放入数据目录的主题。

创建开发模板会生成以下目录，已有同名目录绝不会被覆盖：
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b

```text
data/
└── themes/
<<<<<<< HEAD
    └── my-blog/
        ├── template.html
        ├── style.css
        ├── theme.js
        ├── settings.json
        └── README.md
```

修改这些文件后刷新公开分享页即可生效，无需重启服务。`README.md` 列出模板可用字段；页面布局使用 Go `html/template` 语法，例如 `{{.Title}}`、`{{.ContentHTML}}` 与 `{{.ThemeBaseURL}}`。主题的 CSS、JS 和其他静态文件通过 `/themes/<主题名>/...` 提供。主题名只能使用字母、数字、`-` 和 `_`。

模板可通过可选的 `settings.json` 声明 `text`、`textarea`、`url` 和可重复 `group` 字段。仓库选择模板后，控制台会以“<模板名> 设置”显示专属入口，重复项由用户按需添加或删除，校验后的值写入仓库 `ThemeConfig` 并通过 `.ThemeConfigJS` 提供给模板。内置 `papertrail` 使用同一机制配置博客名称、介绍、logo 与导航按钮。两个内置模板均使用居中阅读栏、根据标题生成的文章目录、auto/light/dark 三态切换、由分享权限控制的一键复制和返回顶部；偏好保存在浏览器 localStorage。

管理员“模板指南”说明模板字段、博客首页数据、无障碍要求和发布检查；“服务器主题指南”说明网页主题变量、响应式约束，以及服务器网页与 Obsidian 插件样式的边界。

Papertrail 设置页提供公开目录卡片和博客首页标题区的实时预览。Logo 可设为 10–192 像素，并选择方形或圆形；预览仅帮助编辑，模板选择和公开入口仍在仓库设置中管理。

## 服务器网页主题

服务器网页主题保存在 `data/console-themes/<主题名>/`，必须包含 `theme.css`。它在基础控制台样式之后加载，可覆盖颜色、字体、背景、边框、间距与组件外观，并可携带常见图片和字体资源。

管理员在“管理员设置 → 服务器主题”可以上传 ZIP、从任意现有主题创建副本、在线编辑文本文件、下载和删除未被使用的主题，并通过页面内 Markdown 指南查看完整格式。普通用户只能在“个人中心 → 服务器网页主题”切换管理员已发布的主题。内置 `default` 主题只读，正在被用户选择的自定义主题不能删除。服务器主题只改变网页，不改变 Obsidian 插件；插件继续使用 Obsidian 宿主样式和变量。

## 网页控制台

所有登录后页面使用统一侧边栏应用壳，无顶部横向导航。登录地址 `http://<server>/login`，注册地址 `/register`，登出 `/logout`。

用户端页面：

- 概览：仓库数、待批准设备、近期修改记录、最近同步和实时系统指标。CPU 型号与使用率、进程内存使用率及已用/总量、当前账户仓库存储每 5 秒刷新一次，不刷新整页。
- 仓库管理：我的仓库、新建仓库、仓库文件（预览 / 下载 / 删除）。
- 当前仓库：文件、分享管理、回收站、修改记录、协作成员、仓库设置，以及由当前模板声明的主题设置。
- 设备管理：批准 pending 设备、修改名称、选择仓库授权、查看上次接入时间与同步游标、吊销。设备首次登录后是 pending；只有批准并选择仓库后才可同步。
- 个人中心：账户信息、同步与存储偏好、服务器网页主题切换与修改密码（修改后其他会话与插件 token 失效）。

管理员页面（"管理后台"菜单）：

- 用户管理：切换角色、重置密码、删除用户（保护最后一个管理员）。
- 全部仓库：跨用户查看与进入仓库管理。
- 全部设备：跨用户批准、授权、吊销。
- 数据信息：用户、仓库、设备和备份概览；CPU、内存、服务器磁盘与逻辑仓库存储实时刷新；近期修改与最近同步设备采用双栏布局。
- 模板管理：上传、脚手架、编辑、下载、删除自定义模板。
- 服务器主题：上传、创建副本、在线编辑、下载和删除服务器网页主题。
- 系统设置：注册开关、同步与存储上限、默认回收站保留天数、删除仓库备份。

## 同步策略与协作

同步模式是仓库级配置（`vault_settings.sync_mode`）：`user_choice` 允许设备自选，`short_poll` / `long_poll` 为管理员强制。插件通过 `GET /api/vaults/:vault_id/sync/strategy` 获取 `effective_mode`，按返回值工作。

Markdown 文件协作：owner 或 manager 邀请用户；插件通过账户级协作收件箱发现来自其他 Vault 的邀请，并使用每条协作自己的 Vault 标识完成接受、拒绝、下载、编辑与撤销。邀请、正文修改和撤销会发布到账户级 SSE/长轮询通道，同时唤醒旧插件绑定的 Vault 通道。被邀请者接受后可在本地 `协作oss/<owner>/<path>` 目录编辑，正文经协作接口写入原文件并推进 revision。协作目录不参与普通同步。

## 日志与排障

服务器和插件默认不输出启动、关闭、轮询成功、定时任务成功或诊断日志。控制台仅保留错误和失败日志；日志不得包含密码、JWT、cookie、授权头或 SSE token。部署监控应使用 `/healthz`、`/readyz` 和失败日志。
=======
    └── my-field-notes/
        ├── template.html
        ├── style.css
        ├── theme.js
        └── README.md
```

修改这些文件后刷新公开分享页即可生效，无需重启服务。`README.md` 列出模板可用字段；页面布局使用 Go `html/template` 语法，例如 `{{.Title}}`、`{{.ContentHTML}}` 与 `{{.ThemeBaseURL}}`。主题的 CSS、JS 和其他静态文件通过 `/themes/<主题名>/...` 提供。主题名只能使用字母、数字、`-` 和 `_`，且仅应由受信任的服务器管理员编辑。
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b

## 测试

后端：

```bash
go test ./...
go test -race ./...
go vet ./...
```

插件：

```bash
cd plugin
npm exec tsc -- --noEmit
npm test
npm run build
```

## 生产部署建议

- 使用反向代理提供 HTTPS
- 备份数据库中的 JWT 签名密钥（与数据库一并备份即可）
<<<<<<< HEAD
- 完成用户开户后在"系统设置"关闭新用户注册
=======
- 完成用户开户后在 `/admin` 关闭新用户注册
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
- 首次启动后从部署环境中移除 `OSS_ADMIN_PASSWORD`
- 定期备份数据库和存储目录
- 监控 `/readyz` 和服务日志
- 升级前先备份 SQLite 数据库
