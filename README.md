# go-api-template

一个 **Go Web API 学习型模板**。它不是企业级脚手架，而是刻意保持简单、
可以直接复制改造、并且配有一套从零手动重建教程（[docs/init/](docs/init/00-overview.md)）的小项目。

> 推荐的学习方式：先通读本 README → 按 docs/init/ 顺序，在空目录里手动把整个项目重新搭一遍 →
> 再回来对照差异、思考取舍 → 最后用这个模板写你自己的小项目。

## 技术栈

| 组件 | 用途 |
| --- | --- |
| Go 1.26.x | 语言与工具链 |
| Gin | HTTP 框架（仅作运行时，路由手动注册） |
| GORM | MySQL ORM |
| golang-migrate | 数据库 schema 迁移（唯一迁移方式，不用 AutoMigrate） |
| Protobuf + Buf | API 契约管理与代码生成 |
| Protovalidate | 请求校验（规则写在 proto 里） |
| JWT (golang-jwt/v5) | 登录态，HS256 Access Token |
| automaxprocs | 容器环境下自动校正 GOMAXPROCS |

**刻意没有引入**：Wire、gRPC、grpc-gateway、Redis、Kafka、OpenTelemetry、自动 Mock 工具。
它们不是现在需要的，等需求出现再加（见 [docs/next-step.md](docs/next-step.md)）。

## 如何运行

前置条件：Go 1.26+、Docker（或本机 MySQL）、
[migrate CLI](docs/init/05-migrate.md)、[protoc-gen-go](docs/init/06-protobuf-buf.md)（改 proto 时才需要）。

```bash
# 1. 启动 MySQL（本地开发库 todo_x）
docker compose up -d

# 2. 执行数据库迁移（不会自动建库，首次请先创建 todo_x，见 docs/init/04-mysql.md）
make migrate-up

# 3. 启动服务
make run

# 4. 试一下
curl http://localhost:8080/healthz
```

注册 / 登录 / 访问受保护接口的完整 curl 流程见 [docs/init/14-run-and-check.md](docs/init/14-run-and-check.md)。

## 目录结构

```text
go-api-template/
├── cmd/server/            # main.go：所有依赖在这里手动组装（无 Wire）
├── configs/               # config.yaml：本地配置
├── docs/
│   ├── init/              # ★ 从零重建本项目的分步教程（核心产物）
│   ├── architecture.md    # 架构说明与设计取舍
│   └── next-step.md       # 后续升级路线（按需引入，不要提前引入）
├── gen/go/                # buf 生成的 Go 代码（故意提交进仓库）
├── internal/
│   ├── app/               # 路由注册 + HTTP Server
│   ├── platform/          # 通用能力：config / database / jwt
│   └── user/              # User Feature（Package by Feature）
├── migrations/            # SQL 迁移文件（golang-migrate）
├── proto/                 # API 契约（proto-first）
├── buf.yaml / buf.gen.yaml
├── docker-compose.yml     # 只有 MySQL
└── Makefile
```

## 常用命令

| 命令 | 作用 |
| --- | --- |
| `make run` | 本地启动 `go run ./cmd/server` |
| `make build` | 构建到 `bin/server.exe` |
| `make test` | `go test ./...`（集成测试需设置 `MYSQL_TEST_DSN`，否则自动跳过） |
| `make lint` | `go vet ./...` |
| `make buf-lint` / `make buf-generate` | proto 校验 / 生成 |
| `make migrate-up` / `make migrate-down` | 执行 / 回滚一次迁移 |

没有 make 时，直接复制 Makefile 里对应的原始命令执行即可。

## 后续可以怎么扩展

按需升级，不要提前升级：[docs/next-step.md](docs/next-step.md)
（Refresh Token、Password Reset、Redis、可观测性等的引入时机都在里面）。
