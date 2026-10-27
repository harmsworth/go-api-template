# 第二阶段 01：资源属主校验（授权）

> 本文记录 `docs/next-step.md` 中「资源属主校验（授权）」这一项的完整实现过程。
> 只做这一件事：让用户只能修改、删除自己的账号和密码。不引入角色、权限表、Refresh Token、
> Redis、Wire 或任何新基础设施。

---

## 1. 本次升级解决什么问题

### 1.1 一个具体的越权场景

假设数据库里有两个用户：

| id | email | password_hash |
| --- | --- | --- |
| 1 | owner@example.com | （bcrypt 哈希） |
| 2 | other@example.com | （bcrypt 哈希） |

用户 1 正常登录，拿到属于自己的 JWT：

```bash
curl.exe -X POST http://localhost:8080/api/v1/auth/login `
  -H "Content-Type: application/json" `
  -d '{\"email\":\"owner@example.com\",\"password\":\"password123\"}'
```

响应里的 `accessToken` 是一个 HS256 token，payload 的 `sub` 是 `"1"`——**它只证明了一件事：请求者是用户 1。**

接下来用户 1 不去改自己的资料，而是改用户 2 的：

```bash
curl.exe -X PATCH http://localhost:8080/api/v1/users/2 `
  -H "Authorization: Bearer <用户1的token>" `
  -H "Content-Type: application/json" `
  -d '{\"displayName\":\"Hacked\"}'
```

在修改前的代码里，这个请求会**成功**：用户 2 的昵称被改成了 `Hacked`。

### 1.2 为什么仅仅验证 JWT 仍然不够

JWT 中间件做的是这样一件事：

```
Authorization: Bearer xxx
    ↓ 验签 + 检查过期
    ↓ 从 sub 取出 userID = 1
    ↓ 写入 Gin Context
    ↓ 放行到 Handler
```

它回答的问题是「**你是谁**」，也就是认证（Authentication）。它**没有**回答「**你能不能动这个资源**」，也就是授权（Authorization）。

token 里的 `sub = 1` 是一个身份声明，不是一个权限声明。即使将来往 token 里塞了角色（`role: "user"`），也无法表达「user 1 只能改 id=1 的资源」——这个约束依赖**每个请求的具体目标**，只能在实际请求到达时比对。

用一个类比：

| 行为 | 类比 | 对应状态码 |
| --- | --- | --- |
| 认证 | 刷工牌进大楼，证明你是员工 | 401 Unauthorized（你没证明身份） |
| 授权 | 你的工牌能不能开这间办公室的门 | 403 Forbidden（身份没问题，但没这个权限） |

### 1.3 为什么认证失败是 401，权限不足是 403

RFC 9110 对两者的定义非常明确：

- **401 Unauthorized**：「你没有提供有效凭据」。语义上应该带 `WWW-Authenticate` 头告诉客户端「该怎么认证」。修复办法是**重新登录**。
- **403 Forbidden**：「服务端理解你的请求，也知道你是谁，但拒绝执行」。修复办法是**换一个请求**（比如去操作自己的资源），不是重新登录。

这两者混用会造成真实的客户端 bug：如果越权返回 401，前端拦截器会把用户踢出登录页，而用户其实登录得好好的。

所以本项目坚持：**认证失败 401，属主不符 403。**

---

## 2. 修改前的项目状态

这一节描述的是实现本次改动**之前**的真实代码（`git show HEAD:internal/user/handler.go` 可复核），不是设想。

### 2.1 三层各自的职责

| 位置 | 修改前负责什么 |
| --- | --- |
| `internal/user/middleware.go` `JWTAuth` | 解析 Bearer token，验签与过期，把 userID 写入 Gin Context，然后 `c.Next()`；**失败时写 401 但只 `return`，没调 `c.Abort()`**（后果见 2.5） |
| `internal/user/middleware.go` `UserIDFrom` | 从 Context 取 userID，取不到返回 `(0, false)` |
| `internal/user/handler.go` | 绑定请求体 → protovalidate 校验 → 解析路径 id → 调 Service → 写响应 |
| `internal/user/service.go` | 只认 `id`，不关心「谁在调用」 |

### 2.2 关键事实：`UserIDFrom` 写了但没人用

修改前在整个仓库里搜索：

```bash
git grep -n "UserIDFrom" HEAD -- internal/
```

结果只有两行，都在定义处：

```
HEAD:internal/user/middleware.go:43:// UserIDFrom 从 Gin Context 取出 JWTAuth 写入的 userID。
HEAD:internal/user/middleware.go:45:func UserIDFrom(c *gin.Context) (int64, bool) {
```

**没有任何业务代码调用它。** 这意味着 JWT 中间件辛苦解析出来的 userID，只是被写进 Context 然后就被遗忘了——Handler 从头到尾不知道「当前请求是谁发的」。

`JWTAuth` 的注释本身也写明了这个边界：

```go
// 只做“认证”，不做“授权”（谁能访问哪些资源属于业务规则）。
```

注释是对的，但「授权」这一半从来没被补上。

### 2.3 修改前的 Handler 长什么样

以 `UpdateUser` 为例（`PATCH /api/v1/users/:id`）：

```go
// UpdateUser PATCH /v1/users/:id
func (h *Handler) UpdateUser(c *gin.Context) {
	var req userv1.UpdateUserRequest
	if !h.bind(c, &req) {
		return
	}
	// 资源名以 URL 为准：即使请求体里带了 name，也会被覆盖。
	req.Name = resourceName(c.Param("id"))
	if !h.validate(c, &req) {
		return
	}
	id, err := parseResourceID(req.Name)
	if err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid user name")
		return
	}
	u, err := h.service.UpdateUser(c.Request.Context(), id, req.DisplayName)
	if err != nil {
		h.mapError(c, err)
		return
	}
	writeProto(c, http.StatusOK, userToProto(u))
}
```

`DeleteUser` 和 `ChangePassword` 是同一个形状：`parseResourceID` 拿到 id，直接交给 Service。

### 2.4 为什么这里存在越权

把上面三段串起来看：

```
1. JWTAuth 证明「请求者是 user 1」        → userID = 1 写进 Context
2. Handler 解析 URL，得到「要操作 user 2」 → id = 2
3. Handler 直接 service.UpdateUser(ctx, 2, "Hacked")
4. Service 只认 id=2，照做
```

**第 2 步和第 3 步之间少了唯一的一次比对：`1 == 2`？**

后果是三个写接口全部可被任意登录用户操纵：

| 接口 | 修改前的风险 |
| --- | --- |
| `PATCH /api/v1/users/:id` | 改任意用户的昵称 |
| `DELETE /api/v1/users/:id` | 删除任意用户 |
| `POST /api/v1/users/:id/password` | 结合已知的当前密码改他人密码（更隐蔽的账号接管） |

`ChangePassword` 的情况需要多说一句：它要求提供 `currentPassword` 并通过 bcrypt 校验，看起来像一道防线。但 `currentPassword` 是**请求方提供的**，攻击者知道对方密码时就成立；而且它保护的是「不知道密码的人」，不是「不是本人的调用者」。

### 2.5 更严重的一层：无 token 也能写（端到端验证时发现）

上面说的越权至少还要求「攻击者有一个合法账号」。实测发现修改前的情况更糟：**完全不认证也能写入**。

根因在 `JWTAuth`：它写 401 之后只 `return`，没有调用 `c.Abort()`。

```go
// 修改前
if !ok || token == "" {
    writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "missing bearer token")
    return          // ← 只结束了自己这一环
}
```

gin 的执行模型：`engine.handleHTTPRequest` 只调用一次 `c.Next()`，由它按 `c.index` 遍历整条 handler 链。中间件里 `return` 只让自己退出，**engine 的循环会继续推进到下一个 handler**。只有 `c.Abort()`（把 index 推到 63）才能真正终止整条链。

于是无 token 的 `PATCH /api/v1/users/2` 在修改前会走完：

```
JWTAuth 写 401 → return
  ↓ engine 仍然继续
UpdateUser：bind → validate → parseResourceID → service.UpdateUser(2, "Hacked")   ← 真的写库
  ↓
writeProto 试图写 200（header 已冻结，状态码仍是 401，body 被追加）
```

客户端收到 `401` + 两段 JSON 拼接的畸形报文，**但数据已经被改了**。

这条缺陷由本次的**端到端验证**发现：单测只断言了状态码 401，没检查 body，所以一直是绿的。修复见下文「步骤 8」。

---

## 3. 本次设计方案

### 3.1 授权判断放在 Handler，不放 Service

判断逻辑只有一行：

```
JWT 里的 userID  ==  URL 路径里的 id ？
```

放 Handler 的理由：

1. **它只依赖协议层数据。** 左边来自 JWT（HTTP 请求头），右边来自 URL 路径。两边都是 HTTP 概念，只有 Handler 天然拥有它们。
2. **Service 不应该知道 HTTP。** 如果下沉到 Service，签名会变成 `UpdateUser(ctx, callerID, targetID, displayName)`——`callerID` 从哪来？只能靠 Handler 传，等于把判断挪了个位置，还顺便弄脏了业务接口。
3. **将来加 gRPC 入口时这份判断能复用吗？** 能，但复用的是「比对两个 id」这个逻辑，不是 HTTP 上下文。真到那天，把 `authorizeOwner` 拆成一个纯函数 `func allowed(callerID, targetID int64) bool` 即可，不需要现在为它设计抽象。
4. **符合项目现有的分层不变量**：`Service` 接口里没有出现任何 `userv1.*` 类型，也没有 `*gin.Context`。授权检查一旦进 Service，这条不变量就破了。

> 另一条更本质的判据：**这条规则会随接口变化吗？** `POST /v1/users/1/password` 和未来的 `POST /v1/users/1/avatar` 都要做同样的属主校验，这属于「接口层的访问控制」，而不是「业务规则」。业务规则是「密码必须 8-72 字节」这种换个入口也成立的东西。

### 3.2 三个数据来源

| 需要的东西 | 从哪来 | 代码 |
| --- | --- | --- |
| 当前用户 ID | JWT → Gin Context | `UserIDFrom(c)` |
| 目标资源 ID | URL 路径 `:id` | `c.Param("id")` → `resourceName()` → `parseResourceID()` |
| 请求体里的 name | JSON body | **忽略它** |

### 3.3 为什么必须用 URL 路径参数确定目标用户

请求体里也可能有 `name` 字段（`UpdateUserRequest` 和 `ChangePasswordRequest` 都声明了它），但**绝不能采信**：

```go
req.Name = resourceName(c.Param("id"))   // 无条件覆盖 body 里的值
```

原因：

- URL 是**路由匹配的依据**。请求打到 `/users/2` 就是路由到「资源 2」，这是服务器自己的判断，客户端无法伪造。
- body 是**客户端自由填写的**。如果两者不一致（URL 说 2、body 说 1），采信 body 就等于让客户端指定操作对象。
- 现在的覆盖顺序是「先 bind、再覆盖、再 validate」，所以 protovalidate 校验的 `name` 已经是 URL 派生出来的值，pattern 校验（`^users/[0-9]+$`）也就校验在正确的对象上。

这个设计在本次改动之前就存在（`UpdateUser` 的注释写着「资源名以 URL 为准」），本次**沿用不变**。

### 3.4 为什么 Context 里没有 userID 要返回 500

三种可能都没被满足时才会出现 `UserIDFrom` 返回 `false`：

1. 路由挂在了 `protected` 组外面（忘了加 `auth` 中间件）；
2. 中间件顺序错了；
3. `ginContextKeyUserID` 常量被改名但中间件没跟着改。

三者都是**服务端装配错误**，客户端无论怎么做都无法规避。所以：

| 情况 | 该返回 | 说明 |
| --- | --- | --- |
| 有 token 但已过期/伪造 | 401 | 客户端重新登录可解决 |
| 无 token | 401 | 同上 |
| token 有效，但 userID ≠ 目标 id | 403 | 换一个请求可解决 |
| **Context 里根本没有 userID** | **500** | 客户端无解，是服务端 bug |

如果这里返回 403，会把「服务端配置错误」伪装成「你没权限」，线上排查时会朝着完全错误的方向走。所以代码里显式记了一条 `slog.Error`，把 `c.FullPath()` 带出来。

### 3.5 为什么不需要角色、权限表、Wire 或新基础设施

本次的规则是「**资源属主**」——只比较两个整数是否相等。它的输入全部在内存里，没有任何存储访问：

- 不需要查权限表：目标 id 是否属于调用者，用 `==` 就能判断；
- 不需要角色系统：「管理员可以改任何人」是另一个维度，现在没有这个需求（`docs/next-step.md` 明确写了「涉及角色判断（如管理员）则放 service 层」，属于未来项）；
- 不需要 Wire：没有新增依赖，`authorizeOwner` 是包内的纯函数；
- 不需要新的中间件：它需要 `:id` 路径参数，而中间件在路由匹配之前或之后拿到参数的时机容易出错，直接写在 Handler 里最直白。

### 3.6 本次授权范围（明确边界）

| 接口 | 是否属主校验 | 说明 |
| --- | --- | --- |
| `PATCH /api/v1/users/:id` | **是** | 只能改自己的昵称 |
| `DELETE /api/v1/users/:id` | **是** | 只能删自己 |
| `POST /api/v1/users/:id/password` | **是** | 只能改自己的密码 |
| `GET /api/v1/users/:id` | 否 | 保持本次修改前的行为：任意登录用户可查看任意用户 |
| `GET /api/v1/users` | 否 | 保持本次修改前的行为：任意登录用户可列出所有用户 |
| `POST /api/v1/users` | 否 | 公开接口，创建用户 |
| `POST /api/v1/auth/register` | 否 | 公开接口 |
| `POST /api/v1/auth/login` | 否 | 公开接口 |

**`GET /api/v1/users` 和 `GET /api/v1/users/:id` 没有属主授权**，仍然是「登录即可读全部」。这是刻意的范围限制，不是遗漏：只读泄露和写入破坏的风险等级不同，而且「用户列表该不该对所有人可见」是产品决策，不该由这次安全修复顺手改掉。

---

## 4. 手动搭建步骤

> 假设你在一个空白目录里，从零复现这次升级。每一步都给出：做什么 / 为什么 / 改哪个文件 / 代码 / 怎么验证 / 常见错误。

### 步骤 0：先读这四个文件

按依赖顺序读，别跳：

| 顺序 | 文件 | 只关注这些 |
| --- | --- | --- |
| 1 | `internal/user/middleware.go` | `JWTAuth` 往 Context 写了什么键；`UserIDFrom` 的返回值 |
| 2 | `internal/user/handler.go` | `parseResourceID`、`resourceName`、`writeError`、三个待改 Handler |
| 3 | `internal/user/handler_test.go` | `newTestRouter` 注册了哪些路由、`doJSON` 怎么发请求 |
| 4 | `internal/app/router.go` | 哪些路由挂在 `protected` 组下（决定哪些请求一定有 userID） |

读完应该能回答：

- Context 里的键名是什么？→ `ginContextKeyUserID = "userID"`
- 路径参数叫什么？→ `:id`，通过 `c.Param("id")` 取
- `parseResourceID` 期望什么格式？→ `"users/{id}"`，内部会 `TrimPrefix`
- 错误响应长什么样？→ `{"error":{"code":"...","message":"..."}}`（由 `writeError` 统一）

**常见错误**：直接跳到步骤 1 就动手，结果在 `authorizeOwner` 里用了 `c.GetInt64("userID")`——这能编译通过，但如果键名写错会静默返回 0，导致所有请求都 403。用 `UserIDFrom` 就不会有这个风险，因为键名只有一处定义。

### 步骤 1：实现授权辅助函数 `authorizeOwner`

**做什么**：写一个函数，完成「取当前用户 → 取目标用户 → 比对 → 写失败响应」四件事。

**为什么先写它**：三个 Handler 都要用同一个判断。先有函数，后面每处接入只有 5 行，重复逻辑不会散落在三个地方。

**改哪个文件**：`internal/user/handler.go`，放在 `parseResourceID` 之后（因为它依赖 `parseResourceID` 和 `resourceName`，放后面读起来是自顶向下的）。

**代码**：

```go
// authorizeOwner 校验当前登录用户是否有权操作 URL 指向的资源。
// 通过时返回资源 ID；不通过时已经写好响应，调用方直接 return。
//
// 这里只做"属主"判断：JWT 里的 userID 必须等于路径里的 users/{id}。
// 之所以放在 Handler 而不是 Service——这个判断只用到协议层数据
// （JWT userID + 路径参数），不涉及任何业务规则或存储访问；
// 下沉到 Service 只会让业务层被迫接触 HTTP 概念。
func authorizeOwner(c *gin.Context) (int64, bool) {
	currentID, ok := UserIDFrom(c)
	if !ok {
		// 受保护路由必定经过 JWTAuth，取不到说明路由或中间件装配错了。
		// 这是服务端配置问题，不是调用方权限不足，所以返回 500 而不是 403。
		slog.Error("受保护路由缺少 userID，检查路由是否挂载了 JWTAuth", "path", c.FullPath())
		writeError(c, http.StatusInternalServerError, "INTERNAL", "internal error")
		return 0, false
	}

	targetID, err := parseResourceID(resourceName(c.Param("id")))
	if err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid user name")
		return 0, false
	}

	if currentID != targetID {
		writeError(c, http.StatusForbidden, "PERMISSION_DENIED", "permission denied")
		return 0, false
	}
	return targetID, true
}
```

**三个设计细节**：

1. **返回 `(int64, bool)` 而不是 `error`**。为什么不用 `error`？因为三种失败需要三种不同的 HTTP 响应（500 / 400 / 403），如果返回 error，调用方还得再写一次 `switch` 做映射，等于把刚写好的响应逻辑复制三份。现在的约定是「**函数自己负责写响应，只把「过没过」告诉调用方**」——和 `bind`、`validate` 的风格完全一致（它们也是 `bool`）。
2. **成功时返回 `targetID`**。这样调用方不用再调一次 `parseResourceID`，避免重复解析和「两次解析结果不一致」的可能。
3. **400 分支其实很少触发**。因为它前面的 `validate` 已经用 pattern `^users/[0-9]+$` 校验过 `req.Name`。保留它是为了让 `authorizeOwner` **不依赖调用方是否先 validate**，将来新 Handler 直接调用也不会漏。

**怎么验证**：此时 `go build ./...` 应该通过，但函数还没被调用——Go 不会报「未使用的函数」，所以编译通过不代表接对了。

**常见错误**：

| 错误写法 | 后果 |
| --- | --- |
| `currentID, _ := UserIDFrom(c)` | 丢掉 ok，Context 缺失时 currentID=0，所有请求都 403，而且看起来「有权限控制」 |
| 用 `c.GetInt64("userID")` | 键名硬编码，改名后静默失效；且 `GetInt64` 在 key 不存在时返回 0 |
| 把 403 的分支写成 `return 0, true` | 调用方拿到 true 继续执行，等于没校验（最危险的一种） |
| 把缺少 userID 也返回 403 | 混淆服务端 bug 与客户端权限问题，线上排查困难 |

### 步骤 2：接入 `UpdateUser`

**做什么**：把原来的 `parseResourceID` 调用换成 `authorizeOwner`。

**为什么**：原来那 5 行（`parseResourceID` + 错误处理）现在由 `authorizeOwner` 一个函数承担，并且多了比对。

**改哪个文件**：`internal/user/handler.go`

**改动前后对比**：

```go
// ---- 改前 ----
if !h.validate(c, &req) {
    return
}
id, err := parseResourceID(req.Name)
if err != nil {
    writeError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid user name")
    return
}
u, err := h.service.UpdateUser(c.Request.Context(), id, req.DisplayName)

// ---- 改后 ----
if !h.validate(c, &req) {
    return
}
// 属主校验：只能改自己的昵称。失败时 authorizeOwner 已写好 403/400/500。
id, ok := authorizeOwner(c)
if !ok {
    return
}
u, err := h.service.UpdateUser(c.Request.Context(), id, req.DisplayName)
```

改动完整形态：

```go
// UpdateUser PATCH /v1/users/:id
func (h *Handler) UpdateUser(c *gin.Context) {
	var req userv1.UpdateUserRequest
	if !h.bind(c, &req) {
		return
	}
	// 资源名以 URL 为准：即使请求体里带了 name，也会被覆盖。
	req.Name = resourceName(c.Param("id"))
	if !h.validate(c, &req) {
		return
	}
	// 属主校验：只能改自己的昵称。失败时 authorizeOwner 已写好 403/400/500。
	id, ok := authorizeOwner(c)
	if !ok {
		return
	}
	u, err := h.service.UpdateUser(c.Request.Context(), id, req.DisplayName)
	if err != nil {
		h.mapError(c, err)
		return
	}
	writeProto(c, http.StatusOK, userToProto(u))
}
```

**位置为什么放在 `validate` 之后、Service 之前**：

- 在 `validate` 之后：保持原有「先校验参数格式、再判断权限」的顺序，不带 body 的 `DELETE` 也能共用同一种插入位置；
- 在 Service 之前：这是硬要求——授权不过绝不能产生副作用。

**注意 `err` 变量**：改前 `id, err := parseResourceID(...)` 定义了 `err`，改后这行没了，下面的 `u, err := h.service...` 重新用 `:=` 定义 `u` 和 `err`（两个都是新变量，合法）。如果你改成了 `u, err = ...`（`=` 而不是 `:=`），会编译报 `undefined: err`。

**怎么验证**：

```bash
go build ./...
go test ./internal/user/ -run TestHandlerOwnershipAllowSelf -v   # 此时该测试还不存在，见步骤 6
```

至少先确认编译通过、原有测试 `TestHandlerCreateUser` 仍绿。

**常见错误**：把 `authorizeOwner(c)` 写在 `req.Name = resourceName(...)` **之前**——此时 URL 已可用，所以不会出错；但如果将来 `authorizeOwner` 改成从 `req` 取 name，顺序就会出错。现在的实现直接从 `c.Param("id")` 取，不依赖 Handler 里的 `req`，所以位置相对自由，但仍建议紧跟 `validate` 之后，保持三个 Handler 形状一致。

### 步骤 3：接入 `DeleteUser`

**做什么**：同样替换 `parseResourceID`。

**为什么**：删除是最需要保护的写操作——越权删除不可逆（本项目是硬删除，没有软删除回收站）。

**改哪个文件**：`internal/user/handler.go`

```go
// DeleteUser DELETE /v1/users/:id
func (h *Handler) DeleteUser(c *gin.Context) {
	req := &userv1.DeleteUserRequest{Name: resourceName(c.Param("id"))}
	if !h.validate(c, req) {
		return
	}
	// 属主校验：只能删自己。
	id, ok := authorizeOwner(c)
	if !ok {
		return
	}
	if err := h.service.DeleteUser(c.Request.Context(), id); err != nil {
		h.mapError(c, err)
		return
	}
	writeProto(c, http.StatusOK, &emptypb.Empty{})
}
```

`DeleteUser` 没有请求体（不调 `bind`），所以 `:id` 只来自 URL，天然没有「body 伪造 name」的问题。

**怎么验证**：`go build ./...` 通过。

**常见错误**：把校验放在 `h.validate` **之前**。此时如果 `:id` 是非法值（如 `/users/abc`），`authorizeOwner` 里的 `parseResourceID` 会返回 400，而 `validate` 的 pattern 也会返回 400——两者结果一样，但错误消息不同（`invalid user name` vs protovalidate 的 pattern 消息）。放在 validate 之后可以保证错误消息风格统一。

### 步骤 4：接入 `ChangePassword`

**做什么**：同上，并顺手更新函数注释——原来的注释说「路径 id 只用来定位资源」，现在已经不准确。

**为什么改注释**：注释和代码不一致比没有注释更有害。以后有人读到「只用来定位资源」，会以为这里没有授权检查。

**改哪个文件**：`internal/user/handler.go`

```go
// ChangePassword POST /v1/users/:id/password
// userID 来自 JWT（router 里挂了 JWTAuth），路径 id 既用来定位资源，
// 也用来与 userID 做属主比对——只有本人能改自己的密码。
func (h *Handler) ChangePassword(c *gin.Context) {
	var req userv1.ChangePasswordRequest
	if !h.bind(c, &req) {
		return
	}
	req.Name = resourceName(c.Param("id"))
	if !h.validate(c, &req) {
		return
	}
	// 属主校验：只能改自己的密码。
	id, ok := authorizeOwner(c)
	if !ok {
		return
	}
	if err := h.service.ChangePassword(c.Request.Context(), id, req.CurrentPassword, req.NewPassword); err != nil {
		h.mapError(c, err)
		return
	}
	writeProto(c, http.StatusOK, &emptypb.Empty{})
}
```

**补充说明**：`ChangePassword` 里还有一层业务校验——Service 会比对 `currentPassword`。这层不能替代属主校验：

| 校验 | 防的是 | 攻击者知道对方密码时还有效吗 |
| --- | --- | --- |
| `currentPassword` 比对（Service） | 拿到别人 token 但不知道密码的人 | **无效**，他知道密码就能改 |
| 属主校验（Handler） | 知道密码但不是本人的调用者 | **有效**，直接 403 |

两层各管一段，都不多余。

**常见错误**：以为 `currentPassword` 已经够了，跳过属主校验。这正是修改前的状态。

### 步骤 5：权限失败的统一 JSON 响应

**做什么**：确认 403 走的是现有 `writeError`，不新造响应格式。

**为什么**：全项目所有错误响应都是同一个形状，客户端只需要写一次解析逻辑。

**代码（无需新增，`writeError` 已存在）**：

```go
// writeError 输出统一的错误 JSON，例如：
// {"error": {"code": "NOT_FOUND", "message": "user not found"}}
func writeError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}
```

调用处：

```go
writeError(c, http.StatusForbidden, "PERMISSION_DENIED", "permission denied")
```

实际响应：

```
HTTP/1.1 403 Forbidden
Content-Type: application/json; charset=utf-8

{"error":{"code":"PERMISSION_DENIED","message":"permission denied"}}
```

`PERMISSION_DENIED` 这个 code 沿用项目里已有的命名风格（`NOT_FOUND` / `CONFLICT` / `UNAUTHENTICATED` / `INVALID_ARGUMENT` / `INTERNAL`），它对应 Google AIP-193 的标准错误码，gRPC 侧同名（`google.rpc.Code.PERMISSION_DENIED`），将来接 gRPC 不用翻译。

**怎么验证**：

```bash
curl.exe -i -X PATCH http://localhost:8080/api/v1/users/2 `
  -H "Authorization: Bearer <用户1的token>" `
  -H "Content-Type: application/json" `
  -d '{\"displayName\":\"Hacked\"}'
```

期望 `HTTP/1.1 403 Forbidden` + 上面那个 body。

**常见错误**：直接用 `c.JSON(http.StatusForbidden, gin.H{"error": "permission denied"})`——形状和其他错误不一致，客户端的统一错误处理会漏掉它。

### 步骤 6：确保授权失败后不调用 Service

**做什么**：检查每个接入点的控制流。

**为什么**：这是整个改动唯一真正重要的性质——**403 之后不能有任何副作用**。写错成「先调 Service 再判断」的话，测试里状态码可能仍然是对的（因为还没写响应），但数据已经被改了。

**正确形状**：

```go
id, ok := authorizeOwner(c)
if !ok {
    return          // ← 这个 return 是安全边界的全部
}
// 只有走到这里才会产生副作用
if err := h.service.DeleteUser(c.Request.Context(), id); err != nil {
```

**怎么验证**：靠测试，见下一节 `TestHandlerOwnershipDoesNotReachService`——它在 Service 外面包了一层计数器，越权请求后断言三个计数都是 0。

**常见错误**：

| 错误写法 | 后果 |
| --- | --- |
| 先调 Service，再判断 ok | 数据已改，403 只是个幌子 |
| `if !ok { /* 忘了 return */ }` | 拿到 id=0 继续调 Service，最终 404 而不是 403 |
| 校验只写在注释里没写代码 | 最隐蔽的一种 |

### 步骤 7：运行测试确认实现正确

```bash
# 1. 格式化
gofmt -l ./internal ./cmd     # 应输出为空

# 2. 编译
go build ./...

# 3. 静态检查
go vet ./...

# 4. 全量测试
go test ./...

# 5. 只看本次相关
go test ./internal/user/ -run "TestHandlerOwnership|TestHandlerProtectedRouteStillRequiresAuth" -v
```

期望输出（节选）：

```
--- PASS: TestHandlerOwnershipAllowSelf
--- PASS: TestHandlerOwnershipDenyOthers
--- PASS: TestHandlerOwnershipDoesNotReachService
--- PASS: TestHandlerOwnershipMissingUserID
--- PASS: TestHandlerProtectedRouteStillRequiresAuth
--- PASS: TestHandlerAuthFailureStopsHandlerChain
```

### 步骤 8：认证失败必须 Abort（端到端验证时发现）

**做什么**：在 `JWTAuth` 的两个失败分支里补 `c.Abort()`。

**为什么**：见上文 2.5。只 `return` 不 `Abort`，gin 会继续执行 Handler，导致 401 报文被污染，且在修改前会继续写库。这是本次唯一超出「属主校验」范围、但必须一起修的缺陷——不修的话「认证失败返回 401」这条要求只在状态码层面成立，报文层面不成立。

**改哪个文件**：`internal/user/middleware.go`

```go
func JWTAuth(m *jwt.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		authz := c.GetHeader("Authorization")
		token, ok := strings.CutPrefix(authz, "Bearer ")
		if !ok || token == "" {
			writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "missing bearer token")
			c.Abort()
			return
		}

		userID, err := m.Parse(token)
		if err != nil {
			writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "invalid token")
			c.Abort()
			return
		}

		c.Set(ginContextKeyUserID, userID)
		c.Next()
	}
}
```

**怎么验证**：启动服务后，无 token 打一个受保护接口，看完整响应。

修复前（两段 JSON 被拼接）：

```
HTTP/1.1 401 Unauthorized

{"error":{"code":"UNAUTHENTICATED","message":"missing bearer token"}}{"error":{"code":"INTERNAL","message":"internal error"}}
```

修复后（单个 JSON）：

```
HTTP/1.1 401 Unauthorized
Content-Type: application/json; charset=utf-8

{"error":{"code":"UNAUTHENTICATED","message":"missing bearer token"}}
```

服务端日志里 `[GIN-debug] Headers were already written. Wanted to override status code 401 with 500` 这条警告也会一起消失——它是 Handler 在 401 之后又试图写 500 的痕迹。

**常见错误**：

| 错误写法 | 后果 |
| --- | --- |
| 只 `return` 不 `c.Abort()` | Handler 继续执行，401 body 后面追加第二段 JSON |
| 用 `c.AbortWithStatus(401)` 但不写 body | 客户端拿到空 body，错误码丢失 |
| 忘了在两个失败分支都加 | 只修了「无 token」，漏了「token 非法」 |

---

## 5. 测试设计与复现

### 5.1 前置改动：测试路由要补全

`handler_test.go` 里的 `newTestRouter` 原本只注册了三个受保护路由，缺 `DELETE` 和 `POST .../password`。不补的话，请求会命中 404（Gin 找不到路由），测出来的「不是 403」会让人误以为授权逻辑有问题。

补齐后（与 `internal/app/router.go` 保持一致）：

```go
protected := r.Group("/api/v1")
protected.Use(auth)
{
	protected.GET("/users", h.ListUsers)
	protected.GET("/users/:id", h.GetUser)
	protected.PATCH("/users/:id", h.UpdateUser)
	protected.DELETE("/users/:id", h.DeleteUser)
	protected.POST("/users/:id/password", h.ChangePassword)
}
```

### 5.2 测试清单与各自覆盖的风险

| 测试函数 | 覆盖的风险 |
| --- | --- |
| `TestHandlerOwnershipAllowSelf` | 改了校验之后**自己的正常操作没被误杀**（回归风险最高的一项） |
| `TestHandlerOwnershipDenyOthers` | 三个写接口的越权都被拦住，且返回 `PERMISSION_DENIED`，且**数据没被改** |
| `TestHandlerOwnershipDoesNotReachService` | 越权时**根本没有进入 Service 层**（比「数据没变」更强的证据） |
| `TestHandlerOwnershipMissingUserID` | Context 缺 userID 时是 500，不是 403（区分服务端 bug 与权限问题） |
| `TestHandlerProtectedRouteStillRequiresAuth` | 认证逻辑没被授权逻辑污染：无 token 仍是 401 |
| `TestHandlerAuthFailureStopsHandlerChain` | 认证失败后**调用链真的中止了**：401 响应体是单个 JSON，不是两段拼接，且数据零副作用 |

### 5.3 关键断言解释

**（1）注册真实用户而不是手工塞数据**

```go
func registerForTest(t *testing.T, r *gin.Engine, email, password, displayName string) string {
	w := doJSON(r, http.MethodPost, "/api/v1/auth/register", "",
		fmt.Sprintf(`{"email":%q,"password":%q,"displayName":%q}`, email, password, displayName))
	// ... 取出 accessToken 返回
}
```

为什么不直接在 `fakeRepository` 里塞 `PasswordHash: "fake-hash"`？

因为 `ChangePassword` 的成功路径需要真实 bcrypt 哈希才能通过 `passwordMatches`。走注册接口能一次拿到「真实密码哈希 + 合法 JWT」，而且顺便覆盖了注册链路。

`fakeRepository` 的 ID 从 1 自增，所以注册顺序决定了用户 ID：第一个注册的是 `users/1`，第二个是 `users/2`。

**（2）越权测试同时断言「状态码 + 错误码 + 数据未变」**

```go
w := doJSON(r, tc.method, tc.path, token1, tc.body)
if w.Code != http.StatusForbidden { ... }

var eb errorBody
json.Unmarshal(w.Body.Bytes(), &eb)
if eb.Error.Code != "PERMISSION_DENIED" { ... }

// 越权请求不能落库：用户 2 仍存在，昵称与密码都未变。
other, err := repo.FindByID(context.Background(), 2)
if err != nil { t.Fatalf("用户 2 不应被删除: %v", err) }
if other.DisplayName != "Other" { ... }
if !passwordMatches(other.PasswordHash, "password123") { ... }
```

只断言 403 是不够的——曾经出现过的 bug 是「先写响应再干活」，状态码对了但数据被改了。所以必须回查存储。

**（3）用 `spyService` 证明没进业务层**

```go
type spyService struct {
	Service
	updateCalls         int
	deleteCalls         int
	changePasswordCalls int
}

func (s *spyService) UpdateUser(ctx context.Context, id int64, displayName string) (*User, error) {
	s.updateCalls++
	return s.Service.UpdateUser(ctx, id, displayName)
}
// DeleteUser / ChangePassword 同理
```

这里有一个 Go 技巧值得注意：**嵌入接口类型** `Service`，然后只覆盖三个方法，其余五个方法自动代理到真实实现。这样不用实现整个接口（8 个方法），也不会因为接口将来新增方法而编译失败。

断言：

```go
if spy.updateCalls != 0 || spy.deleteCalls != 0 || spy.changePasswordCalls != 0 {
	t.Fatalf("授权失败时不应调用 Service：update=%d delete=%d changePassword=%d", ...)
}
```

**（4）500 场景要绕过 JWTAuth**

```go
// 故意不挂 JWTAuth：Context 里永远不会出现 userID。
r := gin.New()
r.PATCH("/api/v1/users/:id", h.UpdateUser)

w := doJSON(r, http.MethodPatch, "/api/v1/users/1", "", `{"displayName":"Nobody"}`)
if w.Code != http.StatusInternalServerError { ... }
```

注意 body 必须是**合法**的 `{"displayName":"Nobody"}`。如果写成非法 body，请求会先被 `bind`/`validate` 拦成 400，测不到授权分支。

**（5）用 `json.Unmarshal` 本身来证明调用链中止了**

这是最巧妙的一个断言，值得单独说：

```go
// 响应体必须是单个完整 JSON。若中间件没 Abort，Handler 会再写一段 body，
// json.Unmarshal 会因 "invalid character '{' after top-level value" 失败。
var eb errorBody
if err := json.Unmarshal(w.Body.Bytes(), &eb); err != nil {
	t.Fatalf("401 响应体必须是单个合法 JSON（可能 Handler 未被中止），实际 %v，body=%s", err, w.Body.String())
}
```

为什么不用「断言 body 里不包含 `INTERNAL`」这类写法？因为那样只能抓到当前这个具体症状（Handler 走到了 `authorizeOwner` 并写 500）。而 `json.Unmarshal` 报错是**结构性的**：只要 Handler 往同一个响应里追加了任何内容，body 就不再是一个合法 JSON 文档，断言就会红。将来即使 Handler 改成往 body 里写别的东西，这个测试依然有效。

验证测试本身有效的方法：把 `c.Abort()` 注掉跑一次，应该看到

```
handler_test.go:508: 401 响应体必须是单个合法 JSON（可能 Handler 未被中止），实际 invalid character '{' after top-level value，body={"error":{"code":"UNAUTHENTICATED","message":"missing bearer token"}}{"error":{"code":"INTERNAL","message":"internal error"}}
```

这个报错里的 body 就是线上实际观测到的那串拼接报文。

### 5.4 运行命令

```bash
# 只跑本次相关
go test ./internal/user/ -run "TestHandlerOwnership|TestHandlerProtectedRouteStillRequiresAuth|TestHandlerAuthFailureStopsHandlerChain" -v

# 全量
go test ./...

# 静态检查
go vet ./...
```

实际结果（本地执行）：

```
--- PASS: TestHandlerOwnershipAllowSelf
--- PASS: TestHandlerOwnershipDenyOthers
--- PASS: TestHandlerOwnershipDoesNotReachService
--- PASS: TestHandlerOwnershipMissingUserID
--- PASS: TestHandlerProtectedRouteStillRequiresAuth
--- PASS: TestHandlerAuthFailureStopsHandlerChain
ok  	go-api-template/internal/user
```

`internal/user` 的集成测试（`repository_integration_test.go`）在无数据库环境下会自动跳过，`go test ./...` 仍然通过。

有数据库时另跑真实 MySQL 集成测试：

```powershell
$env:MYSQL_TEST_DSN = "root:123456@tcp(localhost:3306)/todo_x?charset=utf8mb4&parseTime=True&loc=UTC"
go test ./internal/user/ -run TestUserRepositoryIntegration -v
```

输出里的 `Error 1062 ... Duplicate entry` 和 `record not found` 是测试**故意**触发的（验证重复邮箱、验证删除后查询），不是失败。

---

## 6. 本次修改的文件清单

### 已修改

| 文件 | 为什么改 | 改了什么 |
| --- | --- | --- |
| `internal/user/handler.go` | 实现授权本体 | 新增 `authorizeOwner`（约 29 行，含注释）；`UpdateUser` / `DeleteUser` / `ChangePassword` 各把 `parseResourceID` 调用替换为 `authorizeOwner`；`ChangePassword` 注释同步更新 |
| `internal/user/handler_test.go` | 覆盖授权行为 | `newTestRouter` 补 2 条路由；新增 `registerForTest`、`spyService` 和 6 个测试函数 |
| `internal/user/middleware.go` | 修复端到端验证发现的认证绕过 | 两个失败分支各补 `c.Abort()`；注释说明为何“只 return”不够 |
| `docs/next-step.md` | 反映实际进度 | 新增「当前进度」区；把「资源属主校验」标为已完成并更新描述 |
| `docs/second/01-ownership-authorization.md` | 学习文档 | 新建（本文件） |

### 刻意未修改

| 文件 | 原因 |
| --- | --- |
| `internal/user/service.go` | 授权判断不进业务层，Service 签名与实现一行没动 |
| `internal/user/errors.go` | 403 不需要新的领域错误：它不是「业务失败」，是「请求被拒绝」，直接由 Handler 写响应 |
| `internal/app/router.go` | 路由本身不用变（本次没有新增端点）；`protected` 组已经覆盖了三个接口 |
| `internal/user/model.go` | 无字段变化 |
| `proto/user/v1/user.proto` | 契约不变：没有新增接口、字段或校验规则 |
| `migrations/*.sql` | 无数据库变更 |

### 新增文件

只有一个：`docs/second/01-ownership-authorization.md`（本文档）。代码侧没有新增文件——`authorizeOwner` 放在已有的 `handler.go` 里，不新建 `authz.go`，因为它只有 20 行且只服务这一个 Handler 文件。

---

## 7. 本次复现任务

以下是给你的独立练习清单。**先不看上面的实现，自己写一遍**，再对照检查。

### 练习 1：编写属主校验辅助函数

要求：

- 放在 `internal/user/handler.go`，位置在 `parseResourceID` 之后；
- 签名形如 `func authorizeOwner(c *gin.Context) (int64, bool)`；
- 三种失败分别返回 500 / 400 / 403；
- 500 分支要记日志；
- 通过时返回资源 ID，让调用方不用二次解析；
- 写中文注释说明「为什么放 Handler 不放 Service」。

**完成标准**：

- [ ] `go build ./...` 通过
- [ ] 不会出现「忘了 return」导致的 0 值穿透
- [ ] 403 的错误码是 `PERMISSION_DENIED`

### 练习 2：在三个 Handler 中调用它

要求：

- 只改 `UpdateUser`、`DeleteUser`、`ChangePassword`；
- 调用位置在 `validate` 之后、Service 之前；
- 删掉原来那 5 行 `parseResourceID` + 错误处理；
- 保留 `req.Name = resourceName(c.Param("id"))` 的覆盖逻辑；
- 更新 `ChangePassword` 的注释。

**完成标准**：

- [ ] 三个 Handler 的授权接入形状一致
- [ ] `go build ./...` 通过，无 `undefined: err` 之类的变量错误
- [ ] 原有的 `TestHandlerCreateUser`、`TestHandlerLogin` 等仍然通过

### 练习 3：编写越权测试

要求：

- 用现有 `httptest` + `doJSON` + `fakeRepository`，不引新框架；
- 先补 `newTestRouter` 里的两条路由；
- 至少覆盖：本人成功（PATCH/改密/DELETE）、他人 403（三个接口）、错误码、数据未变、缺 userID 返 500、无 token 返 401；
- 越权场景要回查存储，不能只断言状态码。

**完成标准**：

- [ ] 5 个测试函数全部 PASS
- [ ] 故意把 `authorizeOwner` 改成 `return 0, true`，`TestHandlerOwnershipDenyOthers` 必须变红
- [ ] 故意把 403 改成 400，测试必须变红（证明断言真的在起作用）

### 练习 4：执行测试

```bash
gofmt -l ./internal ./cmd
go build ./...
go vet ./...
go test ./...
```

**完成标准**：

- [ ] `gofmt -l` 输出为空
- [ ] `go vet` 无输出
- [ ] `go test ./...` 全绿

### 练习 5：对照检查

写完再看本文件，逐条确认：

| 检查点 | 你的版本 | 参考实现 |
| --- | --- | --- |
| 缺 userID 时的状态码 | ? | 500（服务端配置错误） |
| 越权时的状态码 | ? | 403 |
| 越权时的错误码 | ? | `PERMISSION_DENIED` |
| 授权检查与 Service 调用的先后 | ? | 授权在前 |
| 目标用户 ID 的来源 | ? | URL 路径，覆盖 body 的 name |
| 是否在 Service 里做授权 | ? | 否，只在 Handler |
| 是否新增了角色/权限表 | ? | 否 |

任何一格和参考实现不一致，都回到对应章节看它的理由——**理由比代码重要**，因为换一个项目、换一种接口，代码会变而理由不变。

### 练习 6（选做）：用真实服务验证 401 报文

单元测试断言的是 `httptest.ResponseRecorder` 里的 body，和真实网络响应还有差别（`Content-Type`、是否被二次写入）。启动服务手工验证一次：

```powershell
go build -buildvcs=false -o bin/server.exe ./cmd/server
.\bin\server.exe
# 另开一个终端：
curl.exe -i -X PATCH http://localhost:8080/api/v1/users/1 -H "Content-Type: application/json" -d "{\"displayName\":\"X\"}"
```

**完成标准**：

- [ ] 状态码 401
- [ ] body 是**一个** JSON 对象，不是两段拼接
- [ ] 服务端日志里没有 `Headers were already written`

如果看到两段 JSON 拼接，说明 `c.Abort()` 没加或没加全——这正是本次端到端验证抓到的缺陷。

### 一条自查题

如果将来的需求是「管理员可以修改任何用户的昵称」，现在的 `authorizeOwner` 该怎么改？

> 提示：它需要知道调用者的角色，而角色信息应该在 JWT 里或从存储查。`docs/next-step.md` 里写的方向是「涉及角色判断（如管理员）则放 service 层返回领域错误」。
> 这个问题的答案不是本次任务的范围，但值得先想清楚：为什么「属主」能留在 Handler，而「角色」可能不行。

