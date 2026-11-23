# AGENTS.md

本文件为 AI 智能体提供 `go-api-template` 的项目级上下文。只记录稳定事实和硬约束，不记录临时任务。

## 项目

`go-api-template` 是一个 Go Web API 学习型模板，不是企业级脚手架。它刻意保持小而完整，便于阅读、复制和按教程重建。

技术栈：

- Go 1.26.x
- Gin（只做 HTTP runtime，路由在 `internal/app/router.go` 手动注册）
- GORM + MySQL
- golang-migrate（唯一 schema 变更方式，不使用 AutoMigrate）
- Protobuf + Buf（`proto/` 是 API Contract，`gen/go/` 是生成代码）
- Protovalidate（校验规则写在 proto）
- JWT HS256 Access Token + 不透明 Refresh Token（轮换）
- bcrypt 密码哈希
- automaxprocs

## 目录

```text
cmd/server/           程序入口，所有依赖手动组装
configs/              本地配置
docs/level1/          从零重建教程
docs/level2/          Level 2 增量教程（Refresh/Reset/分页/过滤/角色）
internal/app/         路由与 HTTP Server
internal/platform/    config / database / jwt / mail
internal/user/        User Feature（model/repository/service/handler/测试）
migrations/           SQL 迁移（up/down 成对）
proto/user/v1/        API 契约
gen/go/user/v1/       buf 生成代码（提交进仓库）
gen/openapi/          buf 生成的 OpenAPI 3 文档（提交进仓库）
```

## 常用命令

```powershell
go build ./...
go vet ./...
go test ./...

buf lint
buf generate

make migrate-up
make migrate-down
```

没有 make 时直接执行 Makefile 中的原始命令。若 `go` / `buf` / `migrate` 不在 PATH，可使用完整路径或先把对应 `bin` 目录加入 PATH。

`buf generate` 同时产出 `gen/go`（Go 代码）与 `gen/openapi/openapi.yaml`（OpenAPI 3 文档），依赖两个本地插件：

```powershell
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install github.com/google/gnostic/cmd/protoc-gen-openapi@latest
```

集成测试需要 `MYSQL_TEST_DSN`，未设置时会自动跳过：

```powershell
$env:MYSQL_TEST_DSN = "root:123456@tcp(localhost:3306)/todo_x?charset=utf8mb4&parseTime=True&loc=UTC"
go test ./internal/user/ -run "TestUserRepository|TestTokenRepositories" -v
```

## 架构边界

- 依赖方向：`Handler → Service → Repository → GORM → MySQL`。
- 上层只依赖下层接口；接口由消费者定义：
  - Handler 定义 `Service` 接口；
  - Service 定义 `Repository` / `RefreshTokenRepository` / `PasswordResetRepository` / `TokenIssuer` / `MailSender`。
- `internal/user` 按 Feature 组织，不拆成全局的 models/services/repositories 目录。
- 不引入 Wire、gRPC、grpc-gateway、Redis、Kafka、OpenTelemetry 等，除非确有需求。
- 不在启动时自动迁移数据库；schema 变更必须走 `migrations/`。

## 硬约束

1. 影响 API 的改动先改 `proto/user/v1/user.proto`，再执行 `buf generate`，最后改 Handler。新增/修改 rpc 必须补 `google.api.http` 注解并与 `internal/app/router.go` 的路由逐条对齐；`gen/go` 与 `gen/openapi/openapi.yaml` 一起提交。
2. 任何表结构变化都必须新增 `migrations/00000X_*.up.sql` 和对应的 `.down.sql`。
3. 密码只存 bcrypt 哈希；数据库中禁止出现明文密码。
4. Refresh Token / Password Reset Token 只存 SHA-256 哈希，原始串只返回给客户端。
5. JSON 请求绑定使用 `protojson`，契约校验使用 `protovalidate`，不要换成 `encoding/json` 或手写校验。
6. Service 只返回 `internal/user/errors.go` 中的领域哨兵错误；HTTP 状态码统一在 `handler.mapError` 映射。
7. 认证失败返回 401，权限不足返回 403；Context 缺少 userID 属于服务端装配错误，返回 500。
8. 禁止把原始请求体、密码、refresh token、reset token 写进日志。
9. 列表过滤/排序字段必须走白名单；过滤值必须使用 SQL 参数占位符，禁止拼接用户输入。
10. 改代码后至少运行 `gofmt -l .`、`go build ./...`、`go vet ./...`、`go test ./...`。

## 文档地图

- 架构与取舍：`docs/architecture.md`
- 升级路线：`docs/next-step.md`
- 分页原理：`docs/pagination-tokens.md`
- 从零重建：`docs/level1/00-overview.md`
- Level 2 实现：`docs/level2/00-overview.md`

## 完成定义

- 编译、静态检查、测试全绿；
- proto 已重新生成（`gen/go` 与 `gen/openapi`），OpenAPI 路径与路由一致；
- 迁移 up/down 成对且已在本地执行验证；
- 新增行为有对应测试；
- 错误映射、日志与文档同步更新；
- 没有引入未要求的依赖或基础设施。
