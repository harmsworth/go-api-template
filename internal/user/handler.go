package user

import (
	"context"
	"errors"
	"fmt"
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
	// TODO: 临时调试，观察完删除下面两段
	// data 的类型是 []byte（字节切片）：HTTP body 的原始字节集合（字节数组），不是字符串。
	// io.ReadAll 把 body 流整体读成它；% x 逐字节按十六进制打印（1 字节 = 2 个十六进制字符）。
	// 对照：%v 打印是十进制数字（[123 13 10 ...]），%s / string(data) 才是文本视图。
	fmt.Printf("bind: data hex = % x\n", data)
	slog.Info(
		"bind: raw request body",
		"len", len(data),
		"bytes", data, // []byte 原值（slog 会转成字符串，故与 text 相同）
		"text", string(data), // 转成 string 后的文本
	)
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

// CreateUser POST /v1/users
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

// GetUser GET /v1/users/:id
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

// ListUsers GET /v1/users?page_size=&page_token=
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

// Register POST /v1/auth/register
//
// 成功返回 200 而不是 201：本端点属于 /auth 命名空间，定位是"注册并立即换发凭据"。
// 客户端拿到 accessToken 就去访问受保护接口，不会去跟进新建的 user 资源；
// 因此它与 /auth/login 归为同一类（凭据颁发），共用 200，客户端无需按端点分支。
// "资源已创建"的语义由面向集合的 POST /v1/users（201）承担。
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
	writeProto(c, http.StatusOK, &userv1.TokenResponse{AccessToken: token, User: userToProto(u)})
}

// Login POST /v1/auth/login
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
