---
description: 比对 proto 契约与 Gin 路由注册，输出缺失、多余与路径不一致清单（只读）
aliases: ["route-check", "contract-check"]
allowed-tools: Read, Grep, Glob
---

只读检查，不修改任何文件。

1. 从 `proto/user/v1/user.proto` 的 `service UserService` 提取全部 rpc 及其请求/响应类型。
2. 从 `internal/app/router.go` 提取全部 `v1.POST/GET/PATCH/DELETE` 与 `protected.*` 注册。
3. 按下面的已知映射核对，并输出差异：

| rpc | HTTP |
| --- | --- |
| CreateUser | POST /api/v1/users（公开，201） |
| GetUser | GET /api/v1/users/:id（受保护） |
| ListUsers | GET /api/v1/users（受保护） |
| UpdateUser | PATCH /api/v1/users/:id（受保护） |
| DeleteUser | DELETE /api/v1/users/:id（受保护） |
| Register | POST /api/v1/auth/register（公开） |
| Login | POST /api/v1/auth/login（公开） |
| Refresh | POST /api/v1/auth/refresh（公开） |
| ChangePassword | POST /api/v1/users/:id/password（受保护） |
| RequestPasswordReset | POST /api/v1/auth/request-password-reset（公开） |
| ResetPassword | POST /api/v1/auth/reset-password（公开） |

4. 额外检查：
   - 受保护路由必须挂在 `protected` 组（`protected.Use(auth)`）下；`/auth/refresh` 与两个密码重置接口必须保持公开。
   - 资源名是否统一 `users/{id}`，proto 校验是否使用 `^users/[0-9]+$`。
   - Handler 是否为每个 rpc 提供了对应方法，方法名是否匹配（`h.CreateUser` 等）。
   - 成功状态码：仅 `POST /api/v1/users` 为 201，其余为 200。

输出格式：

```text
## 契约与路由对照
| rpc | 期望 HTTP | 实际注册 | 结果 |

## 漂移项
- [缺失/多余/路径不一致/鉴权组错误] 具体位置 → 建议改动

## 结论
一致 / 存在 N 处漂移
```

发现漂移时只给修改建议，不直接编辑 `router.go` 或 proto。
