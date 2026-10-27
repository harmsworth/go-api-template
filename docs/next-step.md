# 后续升级路线（按需引入）

核心原则：

> **需求出现以后，再引入工具解决需求；而不是先把所有工具装上。**

每个 Level 里的每一项，都应该在你真正遇到对应需求的那天再加入。
加入之前先问：它解决什么问题？能不能用更少的代码解决？

---

## Level 1：基础模板（V1.0，已完成）

- User CRUD（Create / Get / List / Update / Delete）
- Register / Login / Change Password
- JWT Access Token（HS256，单 token，无刷新）
- golang-migrate 管理 schema
- proto-first 契约 + protovalidate

## 当前进度

| 项                        | 状态         | 说明                                                                                                                              |
| ------------------------- | ------------ | --------------------------------------------------------------------------------------------------------------------------------- |
| V1.0 基础模板             | 已完成       | tag `v0.1.0`：User CRUD + Register/Login/ChangePassword + JWT + golang-migrate + proto 契约                                         |
| 资源属主校验（授权）      | **已完成**   | 仅三个写接口：`PATCH /users/:id`、`DELETE /users/:id`、`POST /users/:id/password`。实现记录见 [second/01-ownership-authorization.md](./second/01-ownership-authorization.md) |
| Refresh Token             | 待实现       | —                                                                                                                                 |
| Password Reset            | 待实现       | —                                                                                                                                 |
| 分页增强（opaque token）  | 待实现       | —                                                                                                                                 |
| Filtering / Ordering      | 待实现       | —                                                                                                                                 |
| 角色 / 管理员权限         | 待实现       | 属主校验只覆盖"本人"，不覆盖"管理员改别人"                                                                                        |

> 只读接口 `GET /users` 和 `GET /users/:id` 仍保持"登录即可读"，未做属主授权；是否收紧属于产品决策。

## Level 2：认证与查询的补全

| 项                         | 什么时候引入                                                                                                                                                                                                                                                                   |
| -------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Refresh Token              | Access Token 变短、需要"记住登录"但不延长泄露窗口时。做法：新增 refresh_tokens 表 + 签发/轮换接口                                                                                                                                                                              |
| 资源属主校验（授权）       | **已完成**（仅三个写接口）。`PATCH /users/:id`、`DELETE /users/:id`、`POST /users/:id/password` 在 Handler 层用 `UserIDFrom` 比对路径 id 与 JWT 身份，不符返回 403 `PERMISSION_DENIED`；Context 缺少 userID 视为服务端装配错误，返回 500。`GET /users` 与 `GET /users/:id` 未加属主限制。实现记录见 [second/01-ownership-authorization.md](./second/01-ownership-authorization.md)。后续引入角色（如管理员）时，判断变为"属主或管理员"，应下沉到 service 层返回领域错误 |
| Password Reset（忘记密码） | 有真实用户、能收邮件之后。涉及 reset token 持久化、过期控制、邮件服务，是完整的子项目，第一版刻意没做                                                                                                                                                                          |
| 分页增强                   | 列表变大后把 page_token 换成 opaque token（base64 编码的 cursor），而不是现在的明文 offset。原理与改法见 [pagination-tokens.md](./pagination-tokens.md)                                                                                                                        |
| Filtering / Ordering       | 列表接口真的需要筛选排序时，在 proto 里加 filter/order_by 字段并在 Repository 层拼接（注意防注入）                                                                                                                                                                             |

## Level 3：外部依赖

| 项             | 什么时候引入                                                                                      |
| -------------- | ------------------------------------------------------------------------------------------------- |
| Redis          | 需要"跨请求缓存"或"存储会话黑名单"时。加 `internal/platform/redis/`，依赖注入方式与 database 相同 |
| Email          | 做 Password Reset 或通知时。先抽象成一个很小的 `MailSender` 接口，开发环境用 log 实现             |
| Object Storage | 有头像/附件需求时。同样从一个小接口开始                                                           |

## Level 4：可观测性

| 项                       | 什么时候引入                                                    |
| ------------------------ | --------------------------------------------------------------- |
| 结构化日志统一           | 现在是 slog + gin.Logger 混用；请求量大后可把 gin 日志接入 slog |
| Metrics（Prometheus）    | 需要看 QPS/延迟/错误率时。注意：引入即意味着要跑一个 Prometheus |
| Tracing（OpenTelemetry） | 出现跨服务调用、需要追踪一条请求经过哪些层时                    |

## Level 5：工程化升级

| 项                               | 什么时候引入                                            |
| -------------------------------- | ------------------------------------------------------- |
| Wire 等 DI 框架                  | main.go 的组装代码长到难以一眼看懂时                    |
| gRPC / grpc-gateway              | 出现非 HTTP 的服务间调用时                              |
| golangci-lint                    | 想要更严格的静态检查时（当前只有 go vet）               |
| http.Server 全量超时             | 去掉反代裸跑公网时，按 [traffic-protection.md](./traffic-protection.md) 场景 A 补全四个超时与 body 上限 |
| 多 Feature 拆分 / 更细的模块边界 | 单个 Feature 内部开始拥挤时，先考虑拆文件，再考虑拆模块 |

---

## 引入新东西时的通用步骤

1. 在 proto 契约层先设计（如果它影响 API）；
2. 加 `internal/platform/<name>/` 或 Feature 内部的新文件；
3. 在 main.go 手动构造并注入——这个流程与现有模板完全一致；
4. 补一条 `docs/architecture.md` 的说明，说明为什么引入。
