# OSS Sync 项目协作说明

## 范围与入口

- 后端是 Go 1.25 + Gin + GORM，入口为 `cmd/server/main.go`，路由集中在 `internal/server/server.go`。
- Obsidian 插件源码位于 `plugin/src/`，测试位于 `plugin/tests/`。
- `README.md` 描述产品与部署，`DESIGN.md` 只描述现有视觉系统，`.agent/HANDOFF.md` 记录当前工作状态。

## 修改约束

- 保持 Vault 隔离、设备授权、单调 revision、`operation_id` 幂等和 `synclock` 并发约束；同步写路径必须同时考虑数据库、文件正文、历史快照和失败恢复。
- 模型变更需同步检查 `internal/database/database.go` 的迁移/回填，并兼容 SQLite 与 PostgreSQL。
- 插件只修改 `plugin/src/`；`plugin/main.js` 是忽略的构建产物，不提交。
- `Dockerfile` 只构建后端镜像，`docker-compose.yml` 使用 SQLite 命名卷启动服务环境；容器升级通过替换镜像完成。
- 不提交 `data/`、数据库、日志、构建产物或密钥；配置新增字段需同时检查 dev/prod YAML、环境变量覆盖和校验。
- 优先复用已有 Handler、策略、路径校验和锁；避免顺手重构或新增依赖。

## 最小验证

后端改动：

```bash
gofmt -w <修改的 Go 文件>
go test ./...
go vet ./...
```

涉及并发同步时再运行 `go test -race ./...`。插件改动使用 Node.js 20+：

```bash
cd plugin
npm ci
npm exec tsc -- --noEmit
npm test
npm run build
```

文档改动至少检查链接、命令和配置名是否与源码一致。任务实际修改项目后，按全局 Policy 更新 `.agent/HANDOFF.md`。
