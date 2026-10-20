# 14 · 从零到跑通 —— 运行手册与 curl 全流程冒烟

## 1. 这一阶段做什么

不写任何新代码。这一阶段是一份**运行手册**：把 [08](./08-user-model-repository.md)–[12](./12-main-manual-wiring.md) 搭好的服务真正跑起来，
再用 curl 把 8 个接口全部冒烟一遍，亲眼确认每一条错误分支的响应。

## 2. 为什么要做

能编译 ≠ 能运行，接口写了 ≠ 行为正确。这一阶段把前面所有阶段串成一条真实的请求链路：
MySQL → GORM → Repository → Service → Handler → Gin → curl。
每一条响应都和 [11](./11-handler-router.md) 阶段的 mapError 映射表、[07](./07-protovalidate.md) 阶段的 proto 契约一一对应——
看懂了这些响应，你才算真正"验收"了这个项目。

## 3. 手动执行什么命令

以下命令全部在**项目根目录**的 PowerShell 中执行。
JSON 请求体一律先用 `Set-Content` 写进文件、再用 `curl.exe --data "@文件名"` 发送——
PowerShell 内联 JSON 的引号转义非常容易出错，文件方式最稳。

### 第 1 步：启动 MySQL

```powershell
docker compose up -d
```

`docker-compose.yml` 会启动 MySQL 8.0 并自动创建 `todo_x` 库（`MYSQL_DATABASE: todo_x`）。
如果你用的是本机已有 MySQL 而不是 Docker，请手动建库：
`CREATE DATABASE todo_x DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;`

### 第 2 步：执行迁移（只需在新库上执行一次）

```powershell
migrate -path migrations -database "mysql://root:123456@tcp(localhost:3306)/todo_x?charset=utf8mb4&parseTime=True&loc=UTC" up
```

成功输出类似 `1/u create_users`。服务启动**不会**自动执行迁移，这一步必须手动做。

### 第 3 步：启动服务（保持这个终端运行）

```powershell
go run ./cmd/server
```

看到 `HTTP server 启动 addr=:8080` 即成功。**另开一个终端**继续下面的冒烟。

### 第 4 步：healthz

```powershell
curl.exe -s http://localhost:8080/healthz
```

预期响应：

```json
{"status":"ok"}
```

## 4. 创建什么文件

只在工作目录产生几个临时 JSON 文件（冒烟完可以删）：

```text
reg.json        bad-email.json    login.json
new-login.json  patch.json        chpwd.json    chpwd-new.json
```

## 5. 冒烟全流程：把什么命令跑一遍

### 5.1 注册成功（201）

```powershell
Set-Content -Path reg.json -Value '{"email":"smoke@example.com","password":"password123","displayName":"Smoke"}' -Encoding utf8
curl.exe -s -i -X POST http://localhost:8080/api/v1/auth/register -H "Content-Type: application/json" --data "@reg.json"
```

预期 `HTTP/1.1 201 Created`，响应体（protojson 缩进输出、字段为 camelCase）：

```json
{
  "accessToken": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJnby1hcGktdGVtcGxhdGUiLCJzdWIiOiIxIiwiaWF0IjoxNzU4NjkwMjAwLCJleHAiOjE3NTg3NzY2MDB9.2fHqXk0iZ0a2yE5pWl6m1s8vFJ0zKcPqQbY3nO9uLxM",
  "user": {
    "name": "users/1",
    "email": "smoke@example.com",
    "displayName": "Smoke",
    "createTime": "2026-09-24T08:30:00.123456Z",
    "updateTime": "2026-09-24T08:30:00.123456Z"
  }
}
```

三个细节：资源名是 `users/1`（AIP 资源名，不是裸数字 id）；`accessToken` 是 [09](./09-user-service.md) 的
Service 经 `TokenIssuer` 签发的 JWT；`createTime`/`updateTime` 是 UTC 的 RFC3339
（结尾 `Z`，因为 DSN 里 `loc=UTC`）。**你的 token 与时间戳会和示例不同，这是正常的。**

顺手保存 token（把示例替换成你响应里的 accessToken）：

```powershell
$token = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9....（粘贴你的 accessToken）"
```

### 5.2 非法邮箱（400）

```powershell
Set-Content -Path bad-email.json -Value '{"email":"not-an-email","password":"password123","displayName":"Smoke"}' -Encoding utf8
curl.exe -s -X POST http://localhost:8080/api/v1/auth/register -H "Content-Type: application/json" --data "@bad-email.json"
```

预期 400，错误体（message 形如 `email: value must be a valid email address`）：

```json
{
  "error": {
    "code": "INVALID_ARGUMENT",
    "message": "email: value must be a valid email address"
  }
}
```

这是 protovalidate 在 Handler 层拦截的——规则来自 proto 文件，服务端代码里没有一行 email 正则。

### 5.3 重复邮箱（409）

把 5.1 的命令原样再执行一次，预期 409：

```json
{
  "error": {
    "code": "CONFLICT",
    "message": "email already exists"
  }
}
```

链路：MySQL 唯一键 1062 → Repository 翻译成 `ErrEmailAlreadyExists` → mapError 映射 409。

### 5.4 登录成功（200）

```powershell
Set-Content -Path login.json -Value '{"email":"smoke@example.com","password":"password123"}' -Encoding utf8
curl.exe -s -X POST http://localhost:8080/api/v1/auth/login -H "Content-Type: application/json" --data "@login.json"
```

预期 200，响应结构与注册相同（`accessToken` + `user`）。可以刷新 `$token` 变量。

### 5.5 无 token 访问受保护接口（401）

```powershell
curl.exe -s http://localhost:8080/api/v1/users/1
```

预期 401：

```json
{
  "error": {
    "code": "UNAUTHENTICATED",
    "message": "missing bearer token"
  }
}
```

这是 JWTAuth 中间件在 Handler 之前拦截的（受保护路由组的 5 条接口都会经过它）。

### 5.6 带 token 查询用户（200）

```powershell
curl.exe -s http://localhost:8080/api/v1/users/1 -H "Authorization: Bearer $token"
```

预期 200：

```json
{
  "name": "users/1",
  "email": "smoke@example.com",
  "displayName": "Smoke",
  "createTime": "2026-09-24T08:30:00.123456Z",
  "updateTime": "2026-09-24T08:30:00.123456Z"
}
```

注意响应里**没有** `passwordHash` 字段——模型和 proto 资源是两个结构，转换函数把它挡在了外面。

### 5.7 修改昵称（200）

```powershell
Set-Content -Path patch.json -Value '{"displayName":"Smoke-2"}' -Encoding utf8
curl.exe -s -X PATCH http://localhost:8080/api/v1/users/1 -H "Authorization: Bearer $token" -H "Content-Type: application/json" --data "@patch.json"
```

预期 200，`displayName` 已更新、`updateTime` 变化（`createTime` 不变）：

```json
{
  "name": "users/1",
  "email": "smoke@example.com",
  "displayName": "Smoke-2",
  "createTime": "2026-09-24T08:30:00.123456Z",
  "updateTime": "2026-09-24T08:31:10.654321Z"
}
```

### 5.8 分页（200）

先多注册两个用户（改一下 `reg.json` 的 email/displayName 后重复执行注册即可，
比如 `page1@example.com`、`page2@example.com`），然后：

```powershell
curl.exe -s "http://localhost:8080/api/v1/users?page_size=2" -H "Authorization: Bearer $token"
```

预期 200：按 id 升序返回前 2 条，并给出下一页令牌（URL 含 `&` 时必须给整个 URL 加引号）：

```json
{
  "users": [
    {
      "name": "users/1",
      "email": "smoke@example.com",
      "displayName": "Smoke-2",
      "createTime": "2026-09-24T08:30:00.123456Z",
      "updateTime": "2026-09-24T08:31:10.654321Z"
    },
    {
      "name": "users/2",
      "email": "page1@example.com",
      "displayName": "Page1",
      "createTime": "2026-09-24T08:35:00.223456Z",
      "updateTime": "2026-09-24T08:35:00.223456Z"
    }
  ],
  "nextPageToken": "2"
}
```

用 `nextPageToken` 翻下一页：

```powershell
curl.exe -s "http://localhost:8080/api/v1/users?page_size=2&page_token=2" -H "Authorization: Bearer $token"
```

预期返回剩下的 1 条（`users/3`），且 `nextPageToken` 为空串 `""`（表示没有更多页；
因为响应开启了 `EmitUnpopulated`，空字段也会输出）。

### 5.9 修改密码：旧密码失效、新密码可登录（200 / 401 / 200）

改密（`currentPassword`/`newPassword` 是 proto 里 `current_password`/`new_password`
的 camelCase 形式）：

```powershell
Set-Content -Path chpwd.json -Value '{"currentPassword":"password123","newPassword":"newpass456"}' -Encoding utf8
curl.exe -s -i -X POST http://localhost:8080/api/v1/users/1/password -H "Authorization: Bearer $token" -H "Content-Type: application/json" --data "@chpwd.json"
```

预期 200，响应体就是两个大括号（`google.protobuf.Empty` 的 JSON 形式）：

```json
{}
```

用**旧密码**登录，预期 401：

```powershell
curl.exe -s -X POST http://localhost:8080/api/v1/auth/login -H "Content-Type: application/json" --data "@login.json"
```

```json
{
  "error": {
    "code": "UNAUTHENTICATED",
    "message": "email or password is incorrect"
  }
}
```

用**新密码**登录，预期 200：

```powershell
Set-Content -Path new-login.json -Value '{"email":"smoke@example.com","password":"newpass456"}' -Encoding utf8
curl.exe -s -X POST http://localhost:8080/api/v1/auth/login -H "Content-Type: application/json" --data "@new-login.json"
```

响应含新的 `accessToken` 与 `user`，与 5.4 结构相同。

### 5.10 停机

回到服务终端按 Ctrl+C，看到"收到退出信号，开始优雅停机..."与"服务已退出"。

## 6. 解释关键观察点

这一轮冒烟实际上把全链路的每个环节都亲眼确认了一遍：

| 观察                                         | 背后的设计                                                                                                   |
| -------------------------------------------- | ------------------------------------------------------------------------------------------------------------ |
| 响应字段全是 camelCase、带缩进、空字段也输出 | protojson `MarshalOptions{EmitUnpopulated, Indent}`（[11](./11-handler-router.md)）                          |
| 错误体统一是 `{"error":{"code","message"}}`  | Handler 的 `writeError` + `mapError`（[11](./11-handler-router.md)）                                         |
| `users/1` 资源名、密码哈希不外泄             | AIP 资源名约定 + 模型/契约分离（[11](./11-handler-router.md)）                                               |
| 409/401/404/400 各归其位                     | 领域错误在 Repository 诞生、Handler 落地（[08](./08-user-model-repository.md)/[11](./11-handler-router.md)） |
| 分页令牌是明文 offset                        | 第一版刻意简化，升级方向见 `docs/next-step.md`                                                               |
| 改密后**旧 token 仍然有效**                  | 无状态 JWT 没有"改密即吊销"，这是已知的 Level 2 待办（Refresh Token/黑名单）                                 |

## 7. 做一个小检查

把上面 5.1–5.9 的状态码记下来，与下表对照，全部一致即冒烟通过：

| 请求                                 | 预期状态码 |
| ------------------------------------ | ---------- |
| `GET /healthz`                       | 200        |
| `POST /api/v1/auth/register`（合法）     | 201        |
| `POST /api/v1/auth/register`（非法邮箱） | 400        |
| `POST /api/v1/auth/register`（重复邮箱） | 409        |
| `POST /api/v1/auth/login`（正确密码）    | 200        |
| `GET /api/v1/users/1`（无 token）        | 401        |
| `GET /api/v1/users/1`（带 token）        | 200        |
| `PATCH /api/v1/users/1`                  | 200        |
| `GET /api/v1/users?page_size=2`（两页）  | 200        |
| `POST /api/v1/users/1/password`          | 200        |
| 旧密码登录                           | 401        |
| 新密码登录                           | 200        |

> **本阶段成果：** 服务从零跑通，8 个接口全部冒烟通过。你现在拥有一条被亲眼验证过的
> 请求链路：curl → Gin → JWTAuth → Handler（protojson + protovalidate）→ Service →
> Repository → GORM → MySQL。剩下最后一件事：复盘（[15](./15-review.md)）。
