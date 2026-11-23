---
name: go-api-template
description: 在 go-api-template 项目中新增或修改 API、数据库迁移、认证授权、分页查询时使用；按项目既有的 proto-first、分层、golang-migrate 与测试流程执行。
---

# go-api-template 开发技能

## 使用场景

当任务涉及以下内容时使用本技能：

- 新增/修改 HTTP API 或 proto 契约；
- 新增/修改数据库表或字段；
- 修改登录、JWT、Refresh Token、密码重置、权限；
- 修改分页、过滤、排序；
- 新增 User Feature 之外的新 Feature；
- 需要按项目规范补测试和文档。

## 项目不变量

- `proto/user/v1/user.proto` 是 API Contract；`internal/app/router.go` 是 HTTP Runtime，两者需要人工保持一致。
- 每个 RPC 带 `google.api.http` 注解，用于生成 `gen/openapi/openapi.yaml`；注解不改变运行时，但必须与路由对齐。
- 依赖方向：`Handler → Service → Repository`，接口由消费者定义。
- `internal/user` 按 Feature 组织，不拆成全局分层目录。
- 数据库变更只走 `migrations/`，禁止 AutoMigrate。
- 密码只存 bcrypt 哈希；refresh/reset token 只存 SHA-256 哈希。
- JSON 使用 protojson，校验使用 protovalidate。
- HTTP 状态码只在 `handler.mapError` 映射。
- 不引入未要求的框架或基础设施。

## 标准流程

### 1. 先读再改

按任务类型读取：

```text
API/契约      proto/user/v1/user.proto、internal/app/router.go、handler.go
业务规则      internal/user/service.go、service_test.go
数据库        migrations/、model.go、repository.go
认证授权      middleware.go、handler.go、service.go、docs/level2/09
Refresh/Reset docs/level2/06、07
分页/过滤     docs/pagination-tokens.md、docs/level2/08
角色权限      docs/level2/09
```

### 2. 契约先行

1. 修改 `proto/user/v1/user.proto`；
2. 执行：

```powershell
buf lint
buf generate
```

3. 确认 `gen/go/user/v1/user.pb.go` 与 `gen/openapi/openapi.yaml` 都已更新；
4. 不要在 Handler 里手写 proto 已能表达的校验。

### 3. 数据库变更

1. 新增 `migrations/00000X_描述.up.sql` 与 `.down.sql`；
2. 字段与 `internal/user/model.go` 保持一致；
3. 执行 `make migrate-up`；
4. 需要时补 Repository 集成测试。

### 4. 代码顺序

按依赖顺序修改，便于每步都能编译：

```text
errors.go（领域错误）
  ↓
model.go / repository.go（存储）
  ↓
service.go（业务规则）
  ↓
handler.go（协议翻译）
  ↓
router.go（路由）
  ↓
main.go（依赖组装）
```

### 5. 测试

- Service：手写 fake，覆盖业务分支和边界；
- Handler：`httptest`，覆盖状态码、错误码、无副作用；
- Repository：`MYSQL_TEST_DSN` 存在时跑真实 MySQL；
- 涉及 token 的改动必须测过期、轮换、吊销和一次性消费。

### 6. 验证

```powershell
gofmt -l .
go build ./...
go vet ./...
go test ./...
buf lint
```

有数据库时：

```powershell
$env:MYSQL_TEST_DSN = "root:123456@tcp(localhost:3306)/todo_x?charset=utf8mb4&parseTime=True&loc=UTC"
go test ./internal/user/ -run "TestUserRepository|TestTokenRepositories" -v
```

## 新增 Feature 的骨架

```text
internal/<feature>/
├── model.go
├── repository.go
├── service.go
├── handler.go
├── errors.go
├── service_test.go
└── handler_test.go
```

再在 `cmd/server/main.go` 手动构造：`db → repository → service → handler`，并在 `internal/app/router.go` 注册路由。

## 完成定义

- proto 已生成，迁移 up/down 成对并验证；
- 编译、vet、测试全绿；
- 错误映射完整；
- 没有敏感信息进入日志；
- 行为变化已同步到 `docs/architecture.md`、`docs/next-step.md` 或 `docs/level2/`。
