# 11 · Handler、Middleware、Router、Server —— HTTP 层

## 1. 这一阶段做什么

创建 HTTP 层的四个文件：

| 文件                          | 职责                                                                    |
| ----------------------------- | ----------------------------------------------------------------------- |
| `internal/user/handler.go`    | 把 HTTP 请求翻译成 Service 调用（bind → validate → service → mapError） |
| `internal/user/middleware.go` | JWT 鉴权中间件（JWTAuth）与取 userID 的工具（UserIDFrom）               |
| `internal/app/router.go`      | 注册所有 Gin 路由（公开 3 条 / 受保护 5 条 / healthz）                  |
| `internal/app/server.go`      | 包装 `*http.Server`（启动、ReadHeaderTimeout、优雅停机）                |

## 2. 为什么要做

**Handler 是什么？**
协议翻译官。它把"HTTP + JSON + proto 契约"翻译成"对 Service 的普通函数调用"，
再把领域错误翻译回 HTTP 状态码。它不写 SQL、不写业务规则——那些分别在 [08](./08-user-model-repository.md) 和 [09](./09-user-service.md)。

**为什么 handler.go 和 middleware.go 在 `internal/user/`，router.go 和 server.go 在 `internal/app/`？**
JWTAuth（middleware.go）和 User 的 token 息息相关（谁登录、谁是资源属主），属于 User Feature 的一部分；
而"路由怎么注册、Server 怎么起"是整个应用的组装问题，与具体 Feature 无关，所以放
`internal/app/`（应用组装层）。以后加第二个 Feature，router.go 会继续在这里注册它的路由。

**为什么现在创建？**
[09](./09-user-service.md) 的 Service 已经就绪，Handler 是它的第一个 HTTP 消费者；
proto 契约（[07](./07-protovalidate.md)）和 protovalidate 规则到这里终于被执行。

**依赖谁 / 谁依赖它？**

```text
internal/user/handler.go    依赖：Service 接口（本文件定义，消费者侧）、proto 生成代码 userv1、protovalidate、Gin
internal/user/middleware.go 依赖：platform/jwt、Gin（以及同包的 writeError）
internal/app/router.go      依赖：user.Handler、user.JWTAuth、Gin
internal/app/server.go      依赖：仅标准库 net/http
        ↑
        └── 12 的 main.go 依次构造：handler → auth 中间件 → router → server
```

依赖方向仍然单向：app 依赖 user，user 不依赖 app（[13](./13-testing.md) 阶段的 handler 测试正是利用这一点，
在 user 包内自建最小路由，避免 import 成环）。

## 3. 手动执行什么命令

依赖在 [04](./04-mysql.md)–[07](./07-protovalidate.md) 阶段均已就位（Gin、protovalidate、`gen/go/` 生成代码），无需新命令。
若 `go build` 报缺依赖，执行一次 `go mod tidy`。

## 4. 创建什么文件

```text
internal/user/handler.go
internal/user/middleware.go
internal/app/router.go
internal/app/server.go
```

## 5. 把什么代码写进去

### `internal/user/handler.go`

```go
package user

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"buf.build/go/protovalidate"
	"github.com/gin-gonic/gin"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	userv1 "go-api-template/gen/go/user/v1"
)

// resourceType 是 User 资源名的前缀，对应 proto 里的资源名约定 users/{id}。
const resourceType = "users"

// Service 是 Handler 依赖的业务接口，由消费者（Handler）定义在这里。
// 具体实现是 service.go 里的未导出结构体 service，隐式满足本接口。
// 方法集就是 Handler 实际调用的 8 个方法，测试中可被 fake 替换。
type Service interface {
	CreateUser(ctx context.Context, email, password, displayName string) (*User, error)
	GetUser(ctx context.Context, id int64) (*User, error)
	ListUsers(ctx context.Context, pageSize int, pageToken string) ([]*User, string, error)
	UpdateUser(ctx context.Context, id int64, displayName string) (*User, error)
	DeleteUser(ctx context.Context, id int64) error
	Register(ctx context.Context, email, password, displayName string) (string, *User, error)
	Login(ctx context.Context, email, password string) (string, *User, error)
	ChangePassword(ctx context.Context, id int64, currentPassword, newPassword string) error
}

// Handler 负责把 HTTP 请求翻译成 Service 调用：
// 参数解析 → proto 请求绑定 → protovalidate 校验 → 调 Service → 状态码/响应。
// 它不写 SQL，也不写业务规则。
type Handler struct {
	service   Service
	validator protovalidate.Validator
}

// NewHandler 创建 Handler 并初始化 protovalidate 校验器。
func NewHandler(service Service) (*Handler, error) {
	v, err := protovalidate.New()
	if err != nil {
		return nil, err
	}
	return &Handler{service: service, validator: v}, nil
}

// protoJSON 统一响应格式：输出所有字段并缩进，学习时方便肉眼比对。
var protoJSON = protojson.MarshalOptions{EmitUnpopulated: true, Indent: "  "}

// ---- 以下是一组小工具，只服务本 Handler ----

// bind 把 JSON 请求体反序列化到 proto message。
// 用 protojson（而不是 encoding/json），才能和 proto 契约保持一致
// （字段名 camelCase、兼容 snake_case、识别 well-known types）。
func (h *Handler) bind(c *gin.Context, msg proto.Message) bool {
	data, err := io.ReadAll(c.Request.Body)
	if err != nil || len(data) == 0 {
		writeError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid JSON body")
		return false
	}
	if err := protojson.Unmarshal(data, msg); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

// validate 执行 protovalidate：规则在 proto 里声明，这里统一执行。
func (h *Handler) validate(c *gin.Context, msg proto.Message) bool {
	if err := h.validator.Validate(msg); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
		return false
	}
	return true
}

// writeError 输出统一的错误 JSON，例如：
// {"error": {"code": "NOT_FOUND", "message": "user not found"}}
func writeError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}

// writeProto 输出 proto message 对应的 JSON。
func writeProto(c *gin.Context, status int, msg proto.Message) {
	data, err := protoJSON.Marshal(msg)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "INTERNAL", "encode response failed")
		return
	}
	c.Data(status, "application/json", data)
}

// mapError 把领域错误统一映射成 HTTP 状态码。
// 所有 Handler 出错时都走这里，保证全局行为一致。
func (h *Handler) mapError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrUserNotFound):
		writeError(c, http.StatusNotFound, "NOT_FOUND", "user not found")
	case errors.Is(err, ErrEmailAlreadyExists):
		writeError(c, http.StatusConflict, "CONFLICT", "email already exists")
	case errors.Is(err, ErrInvalidPassword):
		writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "email or password is incorrect")
	case errors.Is(err, ErrInvalidPageToken):
		writeError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid page token")
	default:
		slog.Error("internal error", "err", err)
		writeError(c, http.StatusInternalServerError, "INTERNAL", "internal error")
	}
}

// parseResourceID 从 "users/{id}" 中解析数字 id。
// 调用前请求已经通过了 protovalidate 的 pattern 校验。
func parseResourceID(name string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimPrefix(name, resourceType+"/"), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid resource name")
	}
	return id, nil
}

// userToProto 把数据库模型转换成 proto 资源。
// 模型与契约分开：PasswordHash 永远不出现在响应里。
func userToProto(u *User) *userv1.User {
	return &userv1.User{
		Name:        resourceType + "/" + strconv.FormatInt(u.ID, 10),
		Email:       u.Email,
		DisplayName: u.DisplayName,
		CreateTime:  timestamppb.New(u.CreatedAt),
		UpdateTime:  timestamppb.New(u.UpdatedAt),
	}
}

// resourceName 把路径参数拼成资源名 "users/{id}"。
func resourceName(id string) string { return resourceType + "/" + id }

// ---- HTTP Handlers ----

// CreateUser POST /api/v1/users
func (h *Handler) CreateUser(c *gin.Context) {
	var req userv1.CreateUserRequest
	if !h.bind(c, &req) {
		return
	}
	if !h.validate(c, &req) {
		return
	}
	u, err := h.service.CreateUser(c.Request.Context(), req.Email, req.Password, req.DisplayName)
	if err != nil {
		h.mapError(c, err)
		return
	}
	writeProto(c, http.StatusCreated, userToProto(u))
}

// GetUser GET /api/v1/users/:id
func (h *Handler) GetUser(c *gin.Context) {
	req := &userv1.GetUserRequest{Name: resourceName(c.Param("id"))}
	if !h.validate(c, req) {
		return
	}
	id, err := parseResourceID(req.Name)
	if err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid user name")
		return
	}
	u, err := h.service.GetUser(c.Request.Context(), id)
	if err != nil {
		h.mapError(c, err)
		return
	}
	writeProto(c, http.StatusOK, userToProto(u))
}

// ListUsers GET /api/v1/users?page_size=&page_token=
func (h *Handler) ListUsers(c *gin.Context) {
	req := &userv1.ListUsersRequest{PageToken: c.Query("page_token")}
	if v := c.Query("page_size"); v != "" {
		// int 在 64 位机器上比 int32 宽，先做范围检查再转换。
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > int(^uint32(0)>>1) {
			writeError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "page_size must be a non-negative integer")
			return
		}
		req.PageSize = int32(n)
	}
	if !h.validate(c, req) {
		return
	}

	users, next, err := h.service.ListUsers(c.Request.Context(), int(req.PageSize), req.PageToken)
	if err != nil {
		h.mapError(c, err)
		return
	}

	resp := &userv1.ListUsersResponse{
		Users:         make([]*userv1.User, 0, len(users)),
		NextPageToken: next,
	}
	for _, u := range users {
		resp.Users = append(resp.Users, userToProto(u))
	}
	writeProto(c, http.StatusOK, resp)
}

// UpdateUser PATCH /api/v1/users/:id
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

// DeleteUser DELETE /api/v1/users/:id
func (h *Handler) DeleteUser(c *gin.Context) {
	req := &userv1.DeleteUserRequest{Name: resourceName(c.Param("id"))}
	if !h.validate(c, req) {
		return
	}
	id, err := parseResourceID(req.Name)
	if err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid user name")
		return
	}
	if err := h.service.DeleteUser(c.Request.Context(), id); err != nil {
		h.mapError(c, err)
		return
	}
	writeProto(c, http.StatusOK, &emptypb.Empty{})
}

// Register POST /api/v1/auth/register
func (h *Handler) Register(c *gin.Context) {
	var req userv1.RegisterRequest
	if !h.bind(c, &req) {
		return
	}
	if !h.validate(c, &req) {
		return
	}
	token, u, err := h.service.Register(c.Request.Context(), req.Email, req.Password, req.DisplayName)
	if err != nil {
		h.mapError(c, err)
		return
	}
	writeProto(c, http.StatusCreated, &userv1.TokenResponse{AccessToken: token, User: userToProto(u)})
}

// Login POST /api/v1/auth/login
func (h *Handler) Login(c *gin.Context) {
	var req userv1.LoginRequest
	if !h.bind(c, &req) {
		return
	}
	if !h.validate(c, &req) {
		return
	}
	token, u, err := h.service.Login(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		h.mapError(c, err)
		return
	}
	writeProto(c, http.StatusOK, &userv1.TokenResponse{AccessToken: token, User: userToProto(u)})
}

// ChangePassword POST /api/v1/users/:id/password
// userID 必须来自 JWT（router 里挂了 JWTAuth），路径 id 只用来定位资源。
func (h *Handler) ChangePassword(c *gin.Context) {
	var req userv1.ChangePasswordRequest
	if !h.bind(c, &req) {
		return
	}
	req.Name = resourceName(c.Param("id"))
	if !h.validate(c, &req) {
		return
	}
	id, err := parseResourceID(req.Name)
	if err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid user name")
		return
	}
	if err := h.service.ChangePassword(c.Request.Context(), id, req.CurrentPassword, req.NewPassword); err != nil {
		h.mapError(c, err)
		return
	}
	writeProto(c, http.StatusOK, &emptypb.Empty{})
}
```

### `internal/user/middleware.go`

```go
package user

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"go-api-template/internal/platform/jwt"
)

// ginContextKeyUserID 是 userID 在 Gin Context 中的键名。
const ginContextKeyUserID = "userID"

// JWTAuth 是 User Feature 的鉴权 middleware：
//
//	Authorization: Bearer xxx
//	    ↓ 解析 token
//	    ↓ 得到 userID 写入 Gin Context
//	    ↓ c.Next() 进入 Handler
//
// 只做“认证”，不做“授权”（谁能访问哪些资源属于业务规则）。
func JWTAuth(m *jwt.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		authz := c.GetHeader("Authorization")
		token, ok := strings.CutPrefix(authz, "Bearer ")
		if !ok || token == "" {
			writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "missing bearer token")
			return
		}

		userID, err := m.Parse(token)
		if err != nil {
			writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "invalid token")
			return
		}

		c.Set(ginContextKeyUserID, userID)
		c.Next()
	}
}

// UserIDFrom 从 Gin Context 取出 JWTAuth 写入的 userID。
// 取不到时返回 ok=false（例如忘记挂 middleware）。
func UserIDFrom(c *gin.Context) (int64, bool) {
	v, ok := c.Get(ginContextKeyUserID)
	if !ok {
		return 0, false
	}
	id, ok := v.(int64)
	return id, ok
}
```

### `internal/app/router.go`

```go
// Package app 负责“应用组装”：路由注册和 HTTP Server。
// 它只依赖 user 包的构造函数，不出现任何 DI 框架。
package app

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go-api-template/internal/user"
)

// NewRouter 创建 Gin 引擎并显式注册所有路由。
//
// 注意：proto 文件是 API Contract（描述有哪些接口），
// 这里的路由注册才是真正的 HTTP 运行时绑定，两者需要人工保持一致。
// 这个决定是有意的：第一版不引入 grpc-gateway / gin 自动生成路由。
func NewRouter(h *user.Handler, auth gin.HandlerFunc) *gin.Engine {
	r := gin.New()
	// 显式使用 Logger 和 Recovery，而不是 gin.Default()，
	// 让“中间件有哪些”一眼可见。
	r.Use(gin.Logger(), gin.Recovery())

	// 健康检查：给 docker compose / 负载均衡探活用。
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	v1 := r.Group("/api/v1")

	// 公开接口：创建、注册、登录。
	v1.POST("/users", h.CreateUser)
	v1.POST("/auth/register", h.Register)
	v1.POST("/auth/login", h.Login)

	// 受保护接口：需要 JWT。
	protected := v1.Group("")
	protected.Use(auth)
	{
		protected.GET("/users", h.ListUsers)
		protected.GET("/users/:id", h.GetUser)
		protected.PATCH("/users/:id", h.UpdateUser)
		protected.DELETE("/users/:id", h.DeleteUser)
		protected.POST("/users/:id/password", h.ChangePassword)
	}

	return r
}
```

### `internal/app/server.go`

```go
package app

import (
	"context"
	"net/http"
	"time"
)

// Server 包装 *http.Server，让 main.go 不直接接触标准库细节。
type Server struct {
	http *http.Server
}

// NewServer 创建 HTTP Server。
func NewServer(addr string, handler http.Handler) *Server {
	return &Server{
		http: &http.Server{
			Addr:    addr,
			Handler: handler,
			// Header 超时防止 slowloris 类攻击，这个习惯值得保留。
			ReadHeaderTimeout: 10 * time.Second,
		},
	}
}

// Run 阻塞式启动监听。
// 返回的 http.ErrServerClosed 是正常关闭信号，由 main.go 过滤。
func (s *Server) Run() error {
	return s.http.ListenAndServe()
}

// Shutdown 优雅停机：停止接收新请求，等待存量请求完成（最多 ctx 时限）。
func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}
```

## 6. 解释关键代码

**为什么用 protojson 而不是 `encoding/json`？**
请求体绑定走 `protojson.Unmarshal`，响应走 `protojson.Marshal`：

- proto 契约字段是 snake_case（`display_name`），而 protojson 的标准 JSON 形式是
  **camelCase**（`displayName`）——用它绑定/输出，JSON 契约自动和 proto 一一对应；
  `encoding/json` 需要给每个字段手写 tag，两套真值来源容易漂移；
- protojson 同时兼容接收 snake_case，且能正确处理 well-known types
  （`google.protobuf.Timestamp` ↔ RFC3339 字符串、`Empty` ↔ `{}`）。

响应统一用 `EmitUnpopulated: true, Indent: "  "`：所有字段都输出、带缩进，
学习阶段肉眼比对响应更直观（生产可以收紧）。

**`Service` 接口：消费者定义的依赖声明。**
Handler 是 Service 的消费者，所以 `Service` 接口定义在本文件（而不是 service.go），
方法集就是 Handler 实际调用的 8 个方法。service.go 里的未导出结构体 `service` 隐式满足它——
[09](./09-user-service.md) 章讲过的"消费者定义接口，提供者实现接口"在这里闭环：包外只见 `user.Service` 契约，
实现细节（`service` 结构体、Repository、TokenIssuer）全部藏在包内。

**每个 Handler 的统一流程：bind → validate → service → mapError。**
8 个 Handler 全是同一个形状：

1. `bind`：JSON → proto 请求消息（失败 → 400 INVALID_ARGUMENT）；
2. `validate`：执行 protovalidate（规则写在 proto 里，[07](./07-protovalidate.md) 阶段已经声明；
   执行点只有这一处，所有请求都过同一道闸，失败 → 400 INVALID_ARGUMENT）；
3. 调 Service 的普通 Go 方法（没有任何 HTTP 概念）；
4. 出错统一交给 `mapError`，成功用 `writeProto` 输出。

**`mapError`：领域错误 → HTTP 状态码映射表。**

| 领域错误（[08](./08-user-model-repository.md) 定义） | 状态码 | code             |  |
| ---------------------------------------------------- | ------ | ---------------- |
| `ErrUserNotFound`                                    | 404    | NOT_FOUND        |
| `ErrEmailAlreadyExists`                              | 409    | CONFLICT         |
| `ErrInvalidPassword`                                 | 401    | UNAUTHENTICATED  |
| `ErrInvalidPageToken`                                | 400    | INVALID_ARGUMENT |
| 其他（未知错误）                                     | 500    | INTERNAL         |

`default` 分支值得注意：未知错误只打日志（`slog.Error`），对外只说 "internal error"——
内部细节（SQL、堆栈）不进响应体。这就是 [08](./08-user-model-repository.md) 哨兵错误模式的终点：
错误在 Repository 诞生、经 Service 传递、在 Handler 统一落地为状态码。

**资源名 `"users/{id}"` 与路径参数的互相转换。**
proto 契约按 AIP 风格用资源名（`name = "users/1"`，并有 `^users/[0-9]+$` 的 pattern 校验）。
两个小工具负责换向：`resourceName("1")` 把 Gin 的路径参数 `c.Param("id")` 拼成 `"users/1"`
（先过 protovalidate 的 pattern），`parseResourceID("users/1")` 再拆回 `int64` 给 Service。
`userToProto` 则把数据库模型转成 proto 资源——**PasswordHash 永远不进入 proto 资源**，
模型和契约是两个结构，这正是转换函数存在的意义。

**JWTAuth 中间件流程。**
`Bearer → Parse → c.Set → Next`：截取 `Authorization: Bearer xxx` 里的 token；
`m.Parse` 校验签名/算法/过期；成功后 `c.Set("userID", userID)` 写入 Gin Context，
`c.Next()` 放行进入 Handler；任何失败都直接写 401 并 return（不调用 `c.Next()`）。
它只做**认证**（你是谁），不做**授权**（你能干什么）——`UserIDFrom` 就是留给后续授权规则的
取值入口（例如"只允许修改自己的资料"），当前模板的授权策略刻意从简，直接按路径 id 操作资源。

**路由分组：公开 3 条 / 受保护 5 条 / healthz。**
`/healthz` 给探活用；公开组挂 `POST /users`、`/auth/register`、`/auth/login`；
受保护组 `protected.Use(auth)` 一行就让组内 5 条路由全部经过 JWT 校验——
新增受保护接口时只要写进这个组，鉴权自动生效。

**为什么 proto 是一种路由、Gin 是另一种？**
proto 契约按 AIP 自定义方法设计，改密码应注册为 `POST /api/v1/users/{id}:changePassword`
（冒号动词是 AIP 惯例）。但 Gin 的路由树（httprouter 风格）**不支持在同一个路径段里
使用冒号参数**（`:id` 只能独占一个段，段中出现 `:` 会被当作参数前缀解析失败），
字面的 `users/{id}:changePassword` 注册不了。所以运行时把它映射成
`POST /api/v1/users/:id/password`：契约（proto）描述"有什么接口、参数是什么"，
Gin 描述"HTTP 请求实际打到哪"，两者人工对应——这正是"proto=契约、Gin=运行时"取舍的体现，
router.go 顶部的注释就是在提醒这一点。

**server.go：ReadHeaderTimeout 与优雅停机。**
`ReadHeaderTimeout: 10s` 防 slowloris 攻击（客户端慢慢发请求头占住连接）——
Go 官方甚至会在漏配时打印提醒。`Run`/`Shutdown` 只是把标准库 `http.Server` 包了一层：
`ListenAndServe` 被主动停机时返回 `http.ErrServerClosed`，这是正常信号，
[12](./12-main-manual-wiring.md) 阶段的 main.go 会过滤它；`Shutdown(ctx)` 停止接收新请求、等待存量请求完成。

## 7. 做一个小检查

在项目根目录执行：

```powershell
go build ./...
```

没有输出即通过。此刻所有零件都已备齐，只差 main.go 把它们组装起来（[12](./12-main-manual-wiring.md) 阶段）。

> **本阶段成果：** HTTP 层完工：Handler 用 protojson + protovalidate 统一处理 8 个接口，
> 错误到状态码的映射只剩一个函数；JWTAuth 让受保护路由一行挂载鉴权；
> 路由和 Server 在 app 包显式组装。距离能跑，只差一份"组装说明书"。
