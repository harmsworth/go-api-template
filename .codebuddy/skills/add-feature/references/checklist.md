# 跨层改动检查清单

按顺序勾选；未完成的项要明确说明原因，不要默认通过。

## 契约

- [ ] `proto/user/v1/user.proto` 已更新，字段编号未复用、未跳号
- [ ] 校验规则写在 proto（`buf.validate.field`），未在 Handler 重复实现
- [ ] 资源名仍为 `users/{id}`，路径参数校验为 `^users/[0-9]+$`
- [ ] `buf lint` 通过；3 条 AIP 相关 except 未被删除
- [ ] `buf generate` 已执行，`gen/go` 与 `gen/openapi/openapi.yaml` 变更已纳入提交
- [ ] 新增 rpc 带 `google.api.http` 注解，且与 `internal/app/router.go` 的路由一致
- [ ] `internal/app/router.go` 已注册对应路由，公开/受保护分组正确
- [ ] 成功状态码符合约定：集合创建 201，其余 200

## Schema 与 model

- [ ] 新迁移通过 `migrate create -seq` 生成，版本号连续
- [ ] up 与 down 成对且可互相抵消
- [ ] 未修改任何已应用的迁移文件
- [ ] `internal/user/model.go` 的字段名、长度、`not null`、默认值、索引名与 SQL 一致
- [ ] `TableName()` 显式返回表名
- [ ] 没有引入 `AutoMigrate` 或启动时迁移

## 代码分层

- [ ] 新方法已加入 `service.go` 的消费者接口，实现结构体保持未导出
- [ ] Repository 只做持久化，业务规则在 Service
- [ ] Repository 把 MySQL 1062 / `ErrRecordNotFound` 翻译成领域错误
- [ ] 新增领域错误已在 `errors.go` 定义，并在 `mapError` 有分支
- [ ] 所有错误响应走统一的 `{"error":{"code","message"}}`
- [ ] 依赖仍在 `cmd/server/main.go` 手工注入，业务包未自建连接或读配置

## 安全

- [ ] 密码使用 bcrypt，未存明文
- [ ] refresh / reset token 只存 SHA-256，原始值只出现在响应或开发日志
- [ ] 令牌轮换/消费在事务内，重复使用会失败
- [ ] 改密或重置密码会吊销该用户全部 refresh token
- [ ] 授权判断在 Service 层，越权返回 `ErrPermissionDenied`
- [ ] `filter` / `order_by` 走白名单，值使用 `?` 占位符
- [ ] 游标与 `order_by` 绑定，排序带 id 作为 tie-breaker

## 测试

- [ ] `service_test.go` 覆盖新增业务规则（含失败路径）
- [ ] `handler_test.go` 覆盖绑定、校验、状态码与错误体
- [ ] 需要真实数据库的查询已补 `repository_integration_test.go`
- [ ] 集成测试在未设 `MYSQL_TEST_DSN` 时仍然 `t.Skip`
- [ ] `gofmt -l .` 无输出，`go vet ./...` 通过
- [ ] `go test ./...` 通过（SKIP 项已如实记录）

## 文档

- [ ] `README.md` 的命令表/接口示例与实际一致
- [ ] `docs/architecture.md` 的请求旅程、错误映射或分层图已更新
- [ ] 对应 `docs/level1/NN-*.md` 保持七段结构并更新代码块
- [ ] 对应 `docs/level2/*.md` 已补实现记录
- [ ] `docs/next-step.md` 的状态表已更新（如涉及新能力）
- [ ] `docs/level1/15-review.md` 的常见坑已补充（如遇到新坑）
