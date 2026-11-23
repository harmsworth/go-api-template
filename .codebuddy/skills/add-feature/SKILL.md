---
name: add-feature
description: 在 go-api-template 中新增或修改一个用户可见的 API 能力时使用。适用于需要同时改动 proto、迁移、GORM model、repository、service、handler、路由、测试与文档的跨层任务；单纯改文案或单文件重构不要使用。
allowed-tools: Read, Grep, Glob, Write, Edit, Bash
---

# 新增一个 API 能力

本项目是 proto-first + 显式迁移 + 手工装配的模板。任何跨层能力都必须按下面的顺序推进，跳过任一步都会造成契约漂移。

## 步骤

1. **确认边界**：读 `CODEBUDDY.md`、`docs/architecture.md`、`.codebuddy/rules/` 三条规则，确认改动属于哪个 Feature（当前只有 `internal/user/`）。
2. **改契约**：编辑 `proto/user/v1/user.proto`。字段命名遵循现有风格（`create_time`/`update_time`、资源名 `users/{id}`）；校验规则写在 `buf.validate.field`，不要留到 Handler。
3. **生成代码**：执行 `buf lint` → `buf generate`，确认 `gen/go` 与 `gen/openapi/openapi.yaml` 都已变更。生成物需要提交，不能加进 `.gitignore`。
4. **改 schema**：若需要新列/新表，用 `migrate create -ext sql -dir migrations -seq <name>` 生成成对文件，先写 up 再写可完全抵消的 down。已应用的迁移文件不得修改。
5. **改 model**：在 `internal/user/model.go` 同步字段、长度、默认值、索引名；`TableName()` 显式声明；禁止 `AutoMigrate`。
6. **扩接口**：在 `internal/user/service.go` 的消费者接口里加方法（只加 Service 真正需要的方法），在 `internal/user/repository.go` 实现；把基础设施错误翻译成领域错误。
7. **写业务规则**：在 `service.go` 实现校验、权限、事务边界；密码用 bcrypt；令牌只存 SHA-256；越权返回 `ErrPermissionDenied`。
8. **接 HTTP**：在 `internal/user/handler.go` 用 `protojson` 绑定 + `protovalidate` 校验 + `mapError`；在 `internal/app/router.go` 注册路由（公开组或 `protected` 组），并核对状态码（仅集合创建 201）；同时给 proto 的 rpc 补 `google.api.http` 注解并重新生成。
9. **补测试**：业务规则 → `service_test.go`（手写 fake）；绑定/校验/中间件 → `handler_test.go`（httptest）；GORM 查询 → `repository_integration_test.go`（未设 `MYSQL_TEST_DSN` 时 skip）。
10. **验证**：`gofmt -l .`、`go vet ./...`、`go test ./...`、`buf lint`；有 MySQL 时再跑集成测试。
11. **同步文档**：按 `.codebuddy/rules/docs-sync/RULE.mdc` 的映射更新 README / architecture / level1 / level2 / next-step。

## 关键约束

- 不引入 Wire、gRPC、grpc-gateway、Redis、Kafka、OTel、testify、mockgen，除非需求已经出现并写入 `docs/next-step.md`。
- 不改写历史迁移、不启用 `AutoMigrate`、不在启动时迁移。
- 路由与 proto 没有代码生成关系，必须人工保持一致。
- 集成测试被 SKIP 不等于通过，汇报时要写明。

## 参考

- 逐项勾选清单：`references/checklist.md`
- 分层与请求旅程：`docs/architecture.md`
- 从零重建步骤：`docs/level1/`（08 model/repository、09 service、10 jwt、11 handler/router、13 testing）
