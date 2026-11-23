---
alwaysApply: true
---

# go-api-template 项目规则

## 修改前

- 先读相关代码、测试和文档，不凭猜测修改。
- 涉及 API 时先看 `proto/user/v1/user.proto` 与 `docs/architecture.md`。
- 涉及数据库时先看 `migrations/` 与 `internal/user/model.go`。
- 涉及认证/授权时先看 `internal/user/middleware.go`、`handler.go`、`service.go` 与 `docs/level2/09-service-authorization.md`。

## 分层

- 保持 `Handler → Service → Repository` 的单向依赖。
- Handler 只做：绑定、protovalidate 校验、取路径参数、调用 Service、映射错误、写响应。
- Service 承载业务规则，返回领域哨兵错误，不接触 `gin.Context` 或 `userv1.*`。
- Repository 只做数据库读写，把 GORM/MySQL 错误翻译成领域错误。
- 接口由消费者定义，不要为了“看起来完整”新增无用方法。

## 契约与数据库

- API 变化先改 proto，再 `buf generate`。
- `buf generate` 同时生成 `gen/go` 与 `gen/openapi/openapi.yaml`，两者都要提交。
- 每个 RPC 必须带 `google.api.http` 注解，且路径/方法与 `internal/app/router.go` 逐条一致；改路由必须同步改注解并重新生成。
- 校验规则写在 proto 的请求 message 上，不要只写在 Handler。
- 表结构变化必须新增编号迁移，up/down 成对，禁止 AutoMigrate。
- 迁移文件命名保持 `00000X_描述.up.sql` / `.down.sql`。
- 字段类型、默认值、索引必须与 GORM Model 和迁移 SQL 一致。

## 安全

- 密码只存 bcrypt 哈希。
- Refresh Token / Password Reset Token 只存 SHA-256 哈希。
- 重置令牌必须一次性消费并带过期时间。
- 邮箱不存在时密码重置接口仍返回成功，避免邮箱枚举。
- 认证失败 401，权限不足 403，服务端装配错误 500。
- 不在日志中输出请求体、密码、token、API Key。
- 列表排序字段必须白名单；过滤值必须参数化，禁止 SQL 拼接。

## 测试与验证

- 新增行为必须补测试：Service 业务规则、Handler 协议行为、必要时 Repository 集成测试。
- 修改后运行：

```powershell
gofmt -l .
go build ./...
go vet ./...
go test ./...
buf lint
```

- 涉及迁移时执行 `make migrate-up` 并确认版本；涉及生成代码时执行 `buf generate`，确认 `gen/openapi/openapi.yaml` 同步更新。

## 文档

- 行为或架构变化同步更新 `docs/architecture.md`、`docs/next-step.md` 或对应的 `docs/level2/` 文档。
- 不新建未要求的文档或基础设施。
