# Level 2 教程按层重写计划

## Summary

在 `docs/level2_new/` 下重新生成一套 Level 2 增量教程，覆盖 01~06 全部改动（含属主校验、Refresh Token、Password Reset、opaque 分页、过滤排序、角色与管理员权限）。

新教程按“模型 → 存储 → 配置/平台 → 契约 → 服务 → Handler → 路由/组装 → 测试 → 运行验证”的顺序组织，每一章参考 `docs/level1` 的固定章节结构，给出可直接手写的完整代码或精确替换位置。

本次只新增文档，不修改 `docs/level2`、业务代码或配置。

## Current State Analysis

### 1. 现有 `docs/level2` 的问题

- 现有 7 篇按“功能”组织：01 属主授权、02 Refresh、03 Reset、04 分页、05 过滤排序、06 角色。
- 01 从 Handler 的 `authorizeOwner` 开始，后续 02~06 又各自从 proto/migration/model/repository/service/handler 走一遍。
- 同一个文件被反复修改：`service.go` 出现在 02/03/04/05/06，`handler.go` 出现在 01/02/03/04/05/06。
- 新手读完 01 只看到 Handler，读完 02 又回到 proto 和 migration，无法先建立“最终的分层代码长什么样”的整体印象。
- 02~06 之间的依赖解释得不错，但“阅读顺序”和“代码修改顺序”混在一起。

### 2. `docs/level1` 可复用的风格

每篇固定 7 段：

1. 这一阶段做什么
2. 为什么要做（依赖谁 / 谁依赖它）
3. 手动执行什么命令
4. 创建/修改什么文件
5. 把什么代码写进去
6. 解释关键代码
7. 做一个小检查

新教程沿用这 7 段，必要时追加“常见错误速查”。代码块给出完整新文件或精确的“找到 X，替换为 Y”。

### 3. 当前 Level 2 代码改动清单（按层）

**模型与迁移**

- `internal/user/model.go`：`User.Role`、`RoleUser`、`RoleAdmin`
- `internal/user/refresh_token.go`：`RefreshToken` 模型 + `TableName`
- `internal/user/password_reset.go`：`PasswordResetToken` 模型 + `TableName`
- `migrations/000002_add_refresh_tokens.up/down.sql`
- `migrations/000003_add_password_reset_tokens.up/down.sql`
- `migrations/000004_add_user_role.up/down.sql`

**领域错误与存储**

- `internal/user/errors.go`：`ErrInvalidPageToken`、`ErrInvalidFilter`、`ErrInvalidOrderBy`、`ErrPermissionDenied`、`ErrInvalidRefreshToken`、`ErrRefreshTokenNotFound`、`ErrInvalidResetToken`、`ErrResetTokenNotFound`
- `internal/user/repository.go`：`List(ctx, ListQuery)`，指向 `ListQuery` 的 keyset/过滤/排序 SQL
- `internal/user/refresh_token.go`：`refreshTokenRepository` 的 `Create`、`FindByHash`、`RevokeAllForUser`、`Rotate`
- `internal/user/password_reset.go`：`passwordResetRepository` 的 `Create`、`Consume`
- `internal/user/service.go`：`Repository`、`RefreshTokenRepository`、`PasswordResetRepository` 接口，`Cursor`、`ListQuery` 类型

**配置与平台**

- `internal/platform/config/config.go`：`JWTConfig.RefreshTTLHours` 与默认值
- `configs/config.yaml`：Access 1h / Refresh 168h
- `internal/platform/mail/mail.go`：`LogSender`

**HTTP 契约**

- `proto/user/v1/user.proto`：
  - `User.role`
  - `TokenResponse.refresh_token`
  - `RefreshTokenRequest` + `Refresh` RPC
  - `RequestPasswordResetRequest`、`ResetPasswordRequest` + 两个 RPC
  - `ListUsersRequest.filter`、`order_by`
- `gen/go/user/v1/user.pb.go`：`buf generate` 产物

**服务层**

- `TokenPair`
- `CreateUser` 默认 `RoleUser`
- `Register`/`Login` 返回 `*TokenPair`
- `Refresh`、`issueTokens`、`newOpaqueToken`、`hashToken`
- `ChangePassword` 最终签名与 refresh token 吊销
- `RequestPasswordReset`/`ResetPassword`
- `ListUsers` 最终签名、`parseOrderBy`、`parseFilter`、`sortValueOf`、`formatDBTime`、`pageTokenPayload` 编解码
- `authorizeTarget`、`UpdateUser`/`DeleteUser` 最终签名

**Handler / 路由 / 组装**

- `internal/user/handler.go`：Service 接口、`tokenResponse`、`userToProto` Role、`mapError` 新错误、`actorAndTarget`、`ListUsers` query 参数、8 个端点改造
- `internal/user/middleware.go`：确认两个失败分支都调用 `c.Abort()`
- `internal/app/router.go`：3 条新公开路由
- `cmd/server/main.go`：新 Repository、MailSender、`NewService` 参数、Refresh TTL

**测试**

- `internal/user/service_test.go`：fake refresh/reset/mail、ListQuery fake、凭据/重置/分页/过滤/排序/角色测试
- `internal/user/handler_test.go`：新测试环境、token 端点、刷新、重置、管理员、Abort 链测试
- `internal/user/repository_integration_test.go`：查询/keyset/令牌仓库集成测试

**Makefile**

- Level 2 没有修改 Makefile；新文档只引用现有 `make migrate-up`、`make test` 等目标。

### 4. 关键依赖与编译边界

- `Service` 接口定义在 `handler.go`（消费者定义）。只要 Service 方法签名变化，Handler 的接口和调用点必须同步修改，否则 `go build ./...` 会失败。
- `Repository` 接口定义在 `service.go`。修改 `List(ctx, ListQuery)` 时，`repository.go` 与 `service.ListUsers` 必须同步。
- `NewService` 参数变化时，`cmd/server/main.go` 必须同步构造，否则 main 包编译失败。
- 因此新教程采用“层为主 + 边界最小同步”的方式：服务层章节会同步修改 Handler 中的接口/调用点，但完整 HTTP 端点逻辑集中到 Handler 章节，并明确解释原因。
- `Cursor`、`ListQuery`、Repository 接口仍放在 `service.go`，因为本项目约定“消费者定义接口与参数类型”；存储层章节会说明这一点，不移动代码。

## Proposed Changes

### 新增目录

```text
docs/level2_new/
├── 00-overview.md
├── 01-model-and-migrations.md
├── 02-errors-and-storage.md
├── 03-config-and-platform.md
├── 04-proto-contract.md
├── 05-service-wiring.md
├── 06-service-credentials.md
├── 07-service-password-reset.md
├── 08-query-layer.md
├── 09-service-authorization.md
├── 10-handler-endpoints.md
├── 11-router-and-main.md
├── 12-tests.md
├── 13-run-and-verify.md
└── 14-review.md
```

### 每章内容

#### 00-overview.md

- Level 2 最终 API、最终文件树、分层图。
- 阅读顺序与每章验收标准。
- 说明本教程教“最终代码”，不按历史提交顺序；`docs/level2` 保留作历史对照。
- 迁移版本说明：边做边加迁移时依次到 2/3/4；已完成仓库直接 `up` 到 4。
- 以 `docs/level1` 的 7 段结构作为全系列模板。

#### 01-model-and-migrations.md

- 改 `model.go`：`Role` 字段、`RoleUser`/`RoleAdmin`。
- 新文件 `refresh_token.go`：只写 `RefreshToken` 模型 + `TableName`。
- 新文件 `password_reset.go`：只写 `PasswordResetToken` 模型 + `TableName`。
- 新迁移 000002/000003/000004，完整 up/down SQL。
- 解释表结构、外键级联、哈希字段、role 默认值。
- 检查：`make migrate-up`、`migrate version` 为 4、`go build ./...` 通过。

#### 02-errors-and-storage.md

- 改 `errors.go`：8 个新哨兵错误。
- 改 `service.go`：只新增 `RefreshTokenRepository`、`PasswordResetRepository` 接口；不碰 `Repository.List`。
- 改 `refresh_token.go`：Repository 结构、构造函数、`Create/FindByHash/RevokeAllForUser/Rotate`。
- 改 `password_reset.go`：Repository 结构、构造函数、`Create/Consume`。
- 解释事务轮换、条件 UPDATE、一次性消费、错误翻译。
- 检查：`go build ./...` 通过（Service 还没使用新仓库，允许字段未用）。

#### 03-config-and-platform.md

- 改 `config.go`：`RefreshTTLHours` 与默认值。
- 改 `configs/config.yaml`。
- 新增 `internal/platform/mail/mail.go`。
- 解释 Access 1h / Refresh 7d、开发邮件写日志、生产替换。
- 检查：`go build ./...`。

#### 04-proto-contract.md

- 修改 `proto/user/v1/user.proto` 的全部 Level 2 契约。
- 执行 `buf lint`、`buf generate`。
- 解释为什么契约放在服务层之前：Handler 同步需要生成的字段；Service 本身不依赖 proto。
- 检查：生成代码包含 `Role`、`RefreshToken`、`Filter`、`OrderBy`。

#### 05-service-wiring.md

- 改 `service.go`：最终 `service` 结构（repo、refreshTokens、resetTokens、tokens、mail、refreshTTL）、`MailSender` 接口、最终 `NewService` 签名。
- 改 `cmd/server/main.go`：构造两个新 Repository、`mail.NewLogSender()`、传入 Refresh TTL。
- 解释为什么先接线、再逐个添加业务方法；main 是手动装配，必须同步。
- 检查：`go build ./...` 通过（方法体仍是 V1.0 行为）。

#### 06-service-credentials.md

- 改 `service.go`：
  - `TokenPair`
  - `CreateUser` 默认 `RoleUser`
  - `Register`/`Login` 返回 `*TokenPair`
  - `Refresh`、`issueTokens`
  - `ChangePassword` 最终签名（actorID/targetID，仅本人）+ `RevokeAllForUser`
  - `newOpaqueToken`、`hashToken`
- 边界同步：`handler.go` 的 Service 接口 Register/Login/Refresh/ChangePassword 签名、`tokenResponse`、Register/Login/ChangePassword 调用点、`actorAndTarget`、403 映射。
- 明确：完整 Handler 端点逻辑在第 10 章，这里只做编译所需的最小同步。
- 检查：`go build ./...`。

#### 07-service-password-reset.md

- 改 `service.go`：`resetTokenTTL`、`RequestPasswordReset`、`ResetPassword`；Service 接口补两个方法。
- 不新增 Handler（等第 10 章）。
- 解释邮箱枚举、30 分钟过期、一次性消费、重置后吊销 refresh。
- 检查：`go build ./...`。

#### 08-query-layer.md

- 本层是唯一“存储 + 服务 + Handler 边界”同章讲解的分页查询改造，章内仍按存储→服务→Handler 顺序。
- 改 `service.go`：`Cursor`、`ListQuery`、`filterableFields`/`orderableFields`、最终 `ListUsers`、`parseOrderBy`、`parseFilter`、`sortValueOf`、`formatDBTime`、`pageTokenPayload`/编解码。
- 改 `repository.go`：`Repository.List` 签名与 keyset/过滤/排序 SQL。
- 边界同步：Handler Service 接口 `ListUsers` 签名、读取 `filter`/`order_by`、调用点、400 映射。
- 解释 `(sort, id)` keyset、token 绑定排序、白名单防注入。
- 检查：`go build ./...`、`go test` 暂不运行（测试第 12 章统一补）。

#### 09-service-authorization.md

- 改 `service.go`：`authorizeTarget`、`UpdateUser`/`DeleteUser` 最终签名；`ChangePassword` 已在 06 完成。
- 边界同步：Handler Service 接口 Update/Delete 签名、调用点、`actorAndTarget`（如 06 未加则在此补齐）、403 映射、`userToProto` Role。
- 改 `middleware.go`：确认/修复两个失败分支的 `c.Abort()`；解释 401、403、500 的区别。
- 解释“本人或 admin”、管理员不能改他人密码、Context 缺 userID 返回 500。
- 检查：`go build ./...`。

#### 10-handler-endpoints.md

- 改 `handler.go`：
  - Service 接口补齐 `Refresh`、`RequestPasswordReset`、`ResetPassword`
  - 新增 `Refresh`、`RequestPasswordReset`、`ResetPassword` Handler
  - `mapError` 补齐 refresh/reset 错误
  - 复核 Register/Login/ListUsers/Update/Delete/ChangePassword
- 不新增路由（第 11 章）。
- 解释公开端点、200 响应、401/400 映射。
- 检查：`go build ./...`。

#### 11-router-and-main.md

- 改 `internal/app/router.go`：3 条公开路由。
- 复核 `cmd/server/main.go` 的最终装配顺序。
- 解释为什么 refresh/reset 必须公开。
- 检查：`go build ./...`、`make run` 启动。

#### 12-tests.md

- 改 `service_test.go`：fake refresh/reset/mail、ListQuery fake、凭据/重置/分页/过滤/排序/角色测试。
- 改 `handler_test.go`：测试环境、双 token、刷新、重置、管理员、Abort 链。
- 改 `repository_integration_test.go`：查询/keyset/令牌仓库测试。
- 解释每一类测试覆盖的风险。
- 检查：`go test ./...`、`MYSQL_TEST_DSN` 集成测试。

#### 13-run-and-verify.md

- 迁移、启动、curl 全流程：
  - 注册/登录双 token
  - 刷新与旧 token 401
  - 请求重置、日志取 token、重置、旧密码 401/新密码 200
  - 列表过滤/排序/opaque 翻页
  - 普通用户越权 403
  - 手动 SQL 提升 admin、旧 token 立即生效、管理员改他人密码仍 403
- 预期输出与常见故障。

#### 14-review.md

- 分层图、关键取舍问答、完成清单。
- 说明已有 `docs/architecture.md`、`docs/next-step.md`、`docs/pagination-tokens.md`、README 的同步点。
- 提醒旧 `docs/level2` 保留作历史对照，新入口是 `docs/level2_new/00-overview.md`。

### 写作规范

- 每个代码块必须标明“新增文件”或“改哪个文件、替换哪段”。
- 新文件给完整代码；修改文件给精确前后对比，避免整文件复制。
- 每章末尾给出检查命令与预期输出。
- 常见错误独立成表，至少覆盖：忘记 `buf generate`、迁移 up/down 不成对、Service 接口未同步、Handler 未改调用点、`c.Abort()` 漏掉、日志泄露 token。
- 所有命令使用 PowerShell 写法，路径相对项目根目录。

### 跨章节决策

- `Cursor`/`ListQuery` 不移动文件，仍在 `service.go`；第 08 章解释“消费者定义接口与参数类型”的约定。
- `docs/level2/01` 的历史 bug 叙事不照搬；`c.Abort()` 作为第 09 章的“为什么必须 Abort”知识点出现。
- 测试统一放在第 12 章，与 `docs/level1/13-testing.md` 的组织方式保持一致。
- 不新增 Makefile 目标，不引入新依赖。

## Assumptions & Decisions

1. 新教程覆盖全部 Level 2（含 01 属主校验与 06 角色），按最终代码讲解，不按历史提交顺序。
2. 只新增 `docs/level2_new/`，不改 `docs/level2`、代码、配置；旧文档保留作历史对照。
3. 不修改 `docs/next-step.md` 的链接；新系列入口由 `00-overview.md` 提供。
4. 服务层章节允许对 Handler 做“最小边界同步”，以保证每章结束 `go build ./...` 通过；完整 HTTP 逻辑仍集中在第 10 章。
5. 分页查询的 Repository 与服务改动放在同一章（第 08 章），因为 `Repository.List` 与 `Service.ListUsers` 是编译期成对修改；章内仍按存储→服务→Handler 顺序。
6. 不移动 `ListQuery`/`Cursor`/Repository 接口的文件位置，避免为写文档而改业务代码。
7. Makefile 没有 Level 2 改动；第 13 章只引用现有目标。

## Verification

实现文档后逐项检查：

1. `docs/level2_new/` 下 15 个文件齐全，链接可点击，无死链。
2. 每章按 7 段模板组织，代码块与当前仓库代码一致。
3. 按章节顺序执行检查命令：

```powershell
gofmt -l .
go build ./...
go vet ./...
go test ./...
buf lint
```

4. `buf generate` 后 `gen/go/user/v1/user.pb.go` 无额外 diff。
5. `make migrate-up` 后 `migrate version` 为 4。
6. 按第 13 章 curl 流程完整跑通：双 token、刷新、重置、分页/过滤/排序、403、admin 提升。
7. `git status` 确认只新增 `docs/level2_new/`，没有意外修改 `docs/level2` 或业务代码。
