# CODEBUDDY.md This file provides guidance to CodeBuddy when working with code in this repository.

## 项目定位

`go-api-template` 是一个 **Go Web API 学习型模板**，不是企业级脚手架。它用一个 User 资源覆盖配置、MySQL、迁移、proto 契约、校验、JWT、测试等常见能力，并刻意保持"一条主线读到底"。

因此本仓库有两条与普通项目不同的约束：

- **文档是交付物**。`docs/level1/` 是 16 章从零重建教程，`docs/level2/` 是进阶实现记录，`docs/architecture.md` 解释取舍。改行为就要同步文档。
- **按需引入**。没有 Wire、gRPC、Redis、Kafka、OTel、golangci-lint；不要以"最佳实践"为由提前加入，依据是 `docs/next-step.md`。

## 常用命令

依赖：Go 1.26+、Docker（或本机 MySQL）；改 proto 需要 `buf` 与 `protoc-gen-go`，改 schema 需要 `migrate` CLI。Windows 没装 make 时，把 Makefile 对应行里的 `$(MIGRATE_DSN)` 替换成变量值后直接执行。

| 命令 | 用途 |
| --- | --- |
| `docker compose up -d` | 启动 MySQL 8.0（容器 `go-api-template-mysql`，库 `todo_x`，root/123456，端口 3306）。首次启动不会自动建库之外的任何表。 |
| `make migrate-up` | 执行全部未应用的迁移。服务启动时**不会**迁移，先跑这一步。 |
| `make migrate-down` | 只回滚最近一个版本（`down 1`）。 |
| `migrate create -ext sql -dir migrations -seq <name>` | 新建一对 `{version}_{name}.up.sql` / `.down.sql`，版本号自增。 |
| `make run` | `go run ./cmd/server`，默认读 `configs/config.yaml`；可用 `go run ./cmd/server -config <path>` 指定配置。 |
| `make build` | 构建到 `bin/server.exe`（`bin/` 已被 gitignore）。 |
| `make lint` | `go vet ./...`。仓库没有 golangci-lint。 |
| `gofmt -l .` | 列出未格式化的 Go 文件；`gofmt -w <file>` 修复。 |
| `make test` | `go test ./...`。未设 `MYSQL_TEST_DSN` 时集成测试是 **SKIP**，不是 PASS。 |
| `go test ./internal/user/ -run TestServiceLoginSuccess -v` | 运行单个测试。`-run` 接受正则，例如 `-run 'TestHandler(List|Get)'`。 |
| `$env:MYSQL_TEST_DSN="root:123456@tcp(localhost:3306)/todo_x?charset=utf8mb4&parseTime=True&loc=UTC"; go test ./internal/user/ -run TestUserRepositoryIntegration -v` | 跑真实 MySQL 集成测试（测试会写入并清理数据，可重复执行）。 |
| `make buf-lint` / `make buf-generate` | 校验 proto / 生成 Go 代码到 `gen/go`。生成需要联网拉取 buf.build 上的 protovalidate。 |
| `curl http://localhost:8080/healthz` | 健康检查，返回 `{"status":"ok"}`。 |
| `docker exec -it go-api-template-mysql mysql -uroot -p123456 -e "SHOW TABLES FROM todo_x; DESCRIBE todo_x.users;"` | 检查迁移结果与表结构。 |

项目级斜杠命令：`/verify`（格式 + vet + 测试 + buf lint）、`/gen`（lint → generate → build）、`/migrate-new`（生成成对迁移）、`/api-check`（proto 与路由漂移比对）。定义在 `.codebuddy/commands/`。

## 架构总览

### 启动装配（`cmd/server/main.go`）

没有 DI 框架，依赖链在一个文件里手工串起来：

```text
flag -config → automaxprocs → config.Load → database.New
  → jwt.New → user.NewRepository / NewRefreshTokenRepository / NewPasswordResetRepository
  → user.NewService → user.NewHandler → user.JWTAuth → app.NewRouter → app.NewServer
  → signal.NotifyContext → 10s 优雅停机
```

`maxprocs.Set` 让容器里的 GOMAXPROCS 跟随 cgroup 配额。`database.New` **只连接、不迁移**；`model.go` 注释写明"永远不会调用 AutoMigrate"，`main.go` 与 `mysql.go` 也明确说明启动不执行迁移，三处注释共同构成护栏。

### 请求旅程

```text
Gin Router → JWT Middleware（仅受保护组）→ Handler → Service → Repository → GORM → MySQL
```

- **Handler**（`internal/user/handler.go`）：`protojson` 绑定 → `protovalidate` 校验 → 调 Service → `mapError`。它不写 SQL、不写业务规则。
- **Service**（`internal/user/service.go`）：注册/登录/改密/重置、bcrypt、token 轮换、分页游标、角色授权。依赖 `Repository`、`RefreshTokenRepository`、`PasswordResetRepository`、`TokenIssuer`、`MailSender` 五个接口。
- **Repository**（`internal/user/repository.go`）：GORM 读写，把 MySQL 1062 翻译成 `ErrEmailAlreadyExists`，把 `gorm.ErrRecordNotFound` 翻译成 `ErrUserNotFound`，上层看不到 GORM。

**接口由消费者定义**：`Repository` 接口写在 `service.go`，`Service` 接口写在 `handler.go`；实现结构体（`repository`、`service`）不导出。这样依赖方向严格单向，测试可以用手写 fake 替换。

### 契约与运行时是两套东西

- `proto/user/v1/user.proto` 是 **API Contract**：rpc 列表、字段、protovalidate 规则、AIP 风格资源名 `users/{id}`。
- `internal/app/router.go` 是 **HTTP Runtime**：Gin 路由逐条手写注册。

两者**没有自动生成关系**，必须人工同步。新增/修改接口时改完 proto 还要改路由，反之亦然。Gin 路由树不支持段中冒号，所以 AIP 的 `users/{id}:changePassword` 在运行时是 `POST /api/v1/users/:id/password`。

### 错误与状态码

领域错误是 `internal/user/errors.go` 里的哨兵错误，Handler 的 `mapError` 统一映射：

| 错误 | HTTP | code |
| --- | --- | --- |
| `ErrUserNotFound` | 404 | `NOT_FOUND` |
| `ErrEmailAlreadyExists` | 409 | `CONFLICT` |
| `ErrInvalidPassword` / `ErrInvalidRefreshToken` | 401 | `UNAUTHENTICATED` |
| `ErrPermissionDenied` | 403 | `PERMISSION_DENIED` |
| `ErrInvalidPageToken` / `ErrInvalidFilter` / `ErrInvalidOrderBy` / `ErrInvalidResetToken` | 400 | `INVALID_ARGUMENT` |
| 其他 | 500 | `INTERNAL`（只记日志，不泄露内部细节） |

响应体固定为 `{"error":{"code":"...","message":"..."}}`。成功状态码：只有面向集合的 `POST /api/v1/users` 返回 201，其余（含 `/auth/*`）返回 200。

### 认证与令牌

- Access Token：HS256，`Subject` 存 userID，默认 1 小时，`jwt.Manager` 只认 HS256 且校验 issuer。
- Refresh Token：不透明随机串，数据库只存 SHA-256 哈希；刷新时在事务内轮换，旧 token 立即失效；改密/重置密码会吊销该用户全部 refresh token。
- 密码重置令牌：30 分钟、一次性、只存哈希；`platform/mail.LogSender` 把 token 写进日志（仅开发可用）。
- 中间件只做认证，不做授权；属主/管理员判断在 Service 层，越权返回 `ErrPermissionDenied`。认证失败必须 `c.Abort()`，否则 Gin 会继续执行后面的 Handler。

### 数据与迁移

- `migrations/` 是 schema 的唯一来源：`000001` 建 `users`，`000002` refresh_tokens，`000003` password_reset_tokens，`000004` 加 role。
- 每个 up 必须有能抵消它的 down；已应用的迁移文件不修改，只追加新版本。
- `internal/user/model.go` 的字段、长度、索引名必须与 SQL 对齐（如 `uniqueIndex:idx_users_email` ↔ `idx_users_email`）；`TableName()` 显式返回 `users`。
- 列表查询是 base64 编码的 keyset cursor，token 与 `order_by` 绑定；过滤/排序字段走白名单映射到固定列，值一律用占位符，禁止拼接用户输入。

### 配置与 platform

`internal/platform/` 只提供能力，不懂业务：`config` 读 YAML 并在 `setDefaults` 补默认值；`database` 建连接池（10/5/1h）；`jwt`；`mail`。业务包不得自己读环境变量或建数据库连接，所有 platform 实例都由 `main.go` 构造后注入。

### 生成代码

`gen/go/user/v1/user.pb.go` **故意提交入库**，clone 后不跑 `buf generate` 也能编译。`buf.gen.yaml` 使用两个本地插件：`protoc-gen-go` 配 `paths=source_relative` 输出 `gen/go`，`protoc-gen-openapi`（gnostic）输出 `gen/openapi/openapi.yaml`（OpenAPI 3，路径来自 proto 的 `google.api.http` 注解，同样提交入库）。`buf.yaml` 的 STANDARD lint 显式排除 3 条与 AIP 返回风格冲突的规则，不要随手删除这些 except。

### 测试分三层

| 文件 | 覆盖 | 依赖 |
| --- | --- | --- |
| `service_test.go` | 业务规则，手写 fake | 无 |
| `handler_test.go` | bind/validate/mapError、中间件、授权 | `httptest`，无数据库 |
| `repository_integration_test.go` | GORM 查询、唯一键、令牌事务 | 真实 MySQL + `MYSQL_TEST_DSN` |

没有 testify、mockgen。测试与被测代码同包（`package user`），可直接用未导出的 fake 与构造函数。

## 不可违背的约定

1. **禁止 `AutoMigrate`，禁止启动时迁移**。schema 变更只能通过 `migrations/` + `migrate up` 显式执行，且 up/down 成对。
2. **改 proto 必须 `buf generate` 并提交 `gen/go` 与 `gen/openapi/openapi.yaml`**；每个 rpc 必须带 `google.api.http` 注解并与 `router.go` 对齐；校验规则写在 proto，不写在 Handler。
3. **改行为必须同步文档**：`README.md`、`docs/architecture.md`、对应的 `docs/level1` / `docs/level2` 章节。
4. **不提前引入依赖**。新组件要先能回答"解决什么问题、为什么是现在"，并更新 `docs/next-step.md`。
5. **错误只走 `mapError`**，不要在 Handler 里临时拼状态码或错误 JSON。
6. **业务包不碰配置与连接**；新增 platform 能力由 `main.go` 注入。

## 修改指南

- 新增一个 API 能力：按 `.codebuddy/skills/add-feature/SKILL.md` 的跨层顺序执行（proto → generate → 迁移 → model → repository → service → handler → router → 测试 → 文档）。
- 只改查询/分页：`service.go` 的白名单与游标编码、`repository.go` 的 keyset 条件必须一起改，并补 `TestServiceListUsers*` 与集成测试。
- 排查"启动正常但一查就报错"：先确认迁移是否执行、`todo_x` 是否已建库。
- PowerShell 里发 JSON 请求容易被引号吞掉，先 `Set-Content` 写文件再 `--data "@file"`。

## CodeBuddy 配置

- `.codebuddy/rules/`：`architecture-boundaries`、`schema-and-contract`、`docs-sync`，三条均在会话开始全文加载。
- `.codebuddy/commands/`：`verify`、`gen`、`migrate-new`、`api-check`。
- `.codebuddy/agents/code-reviewer.md`：项目专用只读审查代理。
- `.codebuddy/skills/add-feature/`：跨层新增能力的工作流与检查清单。
- `.codebuddy/settings.json`：`PostToolUse` 对编辑后的 `.go` 文件执行 `gofmt -w`；脚本异常静默退出 0，格式化失败只输出非阻塞告警（exit 1），不会中止编辑。该 hook 使用 Windows PowerShell；非 Windows 环境请替换其中的 command。
