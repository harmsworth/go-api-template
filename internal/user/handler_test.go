package user

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"go-api-template/internal/platform/jwt"
)

// newTestRouter 为 Handler 测试构建一个最小 Gin 引擎。
// 路由和 app.NewRouter 保持一致；之所以不复用 app.NewRouter，
// 是因为本测试在 user 包内，直接 import internal/app 会形成 import cycle。
func newTestRouter(t *testing.T) (*gin.Engine, *fakeRepository) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	repo := newFakeRepository()
	jwtManager := jwt.New("test-secret", time.Hour, "test")
	svc := NewService(repo, jwtManager)

	h, err := NewHandler(svc)
	if err != nil {
		t.Fatalf("初始化 Handler 失败: %v", err)
	}

	r := gin.New()
	r.POST("/api/v1/users", h.CreateUser)
	r.POST("/api/v1/auth/register", h.Register)
	r.POST("/api/v1/auth/login", h.Login)

	auth := JWTAuth(jwtManager)
	protected := r.Group("/api/v1")
	protected.Use(auth)
	{
		protected.GET("/users", h.ListUsers)
		protected.GET("/users/:id", h.GetUser)
		protected.PATCH("/users/:id", h.UpdateUser)
		protected.DELETE("/users/:id", h.DeleteUser)
		protected.POST("/users/:id/password", h.ChangePassword)
	}

	return r, repo
}

// doJSON 发起一次测试请求并返回响应记录器。
func doJSON(r *gin.Engine, method, path, bearerToken, body string) *httptest.ResponseRecorder {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// errorBody 用于断言统一错误响应结构。
type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func TestHandlerCreateUser(t *testing.T) {
	r, _ := newTestRouter(t)

	w := doJSON(r, http.MethodPost, "/api/v1/users", "",
		`{"email":"alice@example.com","password":"password123","displayName":"Alice"}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("状态码应为 201，实际 %d，body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if resp["name"] != "users/1" {
		t.Fatalf("name 应为 users/1，实际 %v", resp["name"])
	}
	if resp["email"] != "alice@example.com" {
		t.Fatalf("email 应为 alice@example.com，实际 %v", resp["email"])
	}
}

func TestHandlerCreateUserValidationFailed(t *testing.T) {
	r, _ := newTestRouter(t)

	// 邮箱格式非法，protovalidate 应在 Handler 层拦截，返回 400。
	w := doJSON(r, http.MethodPost, "/api/v1/users", "",
		`{"email":"not-an-email","password":"password123","displayName":"Alice"}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("状态码应为 400，实际 %d，body=%s", w.Code, w.Body.String())
	}
	var eb errorBody
	if err := json.Unmarshal(w.Body.Bytes(), &eb); err != nil {
		t.Fatalf("错误响应不是合法 JSON: %v", err)
	}
	if eb.Error.Code != "INVALID_ARGUMENT" {
		t.Fatalf("错误码应为 INVALID_ARGUMENT，实际 %q", eb.Error.Code)
	}
}

func TestHandlerAuthMiddleware(t *testing.T) {
	r, repo := newTestRouter(t)

	if err := repo.Create(context.Background(), &User{
		Email: "alice@example.com", PasswordHash: "fake-hash", DisplayName: "Alice",
	}); err != nil {
		t.Fatalf("准备数据失败: %v", err)
	}

	// 不带 token：401。
	w := doJSON(r, http.MethodGet, "/api/v1/users/1", "", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("无 token 状态码应为 401，实际 %d", w.Code)
	}

	// 带合法 token：200。
	jm := jwt.New("test-secret", time.Hour, "test")
	token, err := jm.Issue(1)
	if err != nil {
		t.Fatalf("签发测试 token 失败: %v", err)
	}
	w = doJSON(r, http.MethodGet, "/api/v1/users/1", token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("带合法 token 状态码应为 200，实际 %d，body=%s", w.Code, w.Body.String())
	}

	// 带垃圾 token：401。
	w = doJSON(r, http.MethodGet, "/api/v1/users/1", "garbage-token", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("垃圾 token 状态码应为 401，实际 %d", w.Code)
	}
}

func TestHandlerGetUserNotFound(t *testing.T) {
	r, _ := newTestRouter(t)

	jm := jwt.New("test-secret", time.Hour, "test")
	token, _ := jm.Issue(42)

	w := doJSON(r, http.MethodGet, "/api/v1/users/999", token, "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("状态码应为 404，实际 %d，body=%s", w.Code, w.Body.String())
	}
	var eb errorBody
	if err := json.Unmarshal(w.Body.Bytes(), &eb); err != nil {
		t.Fatalf("错误响应不是合法 JSON: %v", err)
	}
	if eb.Error.Code != "NOT_FOUND" {
		t.Fatalf("错误码应为 NOT_FOUND，实际 %q", eb.Error.Code)
	}
}

func TestHandlerLogin(t *testing.T) {
	r, _ := newTestRouter(t)

	// 先注册一个真实用户（走完整 Handler 流程，密码是真实 bcrypt 哈希）。
	w := doJSON(r, http.MethodPost, "/api/v1/auth/register", "",
		`{"email":"bob@example.com","password":"password123","displayName":"Bob"}`)
	// Register 属于 /auth 的凭据颁发类端点，成功返回 200（与 /auth/login 一致），不是 201。
	if w.Code != http.StatusOK {
		t.Fatalf("注册状态码应为 200，实际 %d，body=%s", w.Code, w.Body.String())
	}

	w = doJSON(r, http.MethodPost, "/api/v1/auth/login", "",
		`{"email":"bob@example.com","password":"wrong-password"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("密码错误状态码应为 401，实际 %d", w.Code)
	}

	w = doJSON(r, http.MethodPost, "/api/v1/auth/login", "",
		`{"email":"bob@example.com","password":"password123"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("登录成功状态码应为 200，实际 %d，body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if resp["accessToken"] == "" {
		t.Fatalf("响应应包含 accessToken，实际 %v", resp)
	}
}

func TestHandlerListUsersPagination(t *testing.T) {
	r, repo := newTestRouter(t)

	for i := 1; i <= 3; i++ {
		if err := repo.Create(context.Background(), &User{
			Email:        fmt.Sprintf("u%d@example.com", i),
			PasswordHash: "fake-hash",
			DisplayName:  fmt.Sprintf("U%d", i),
		}); err != nil {
			t.Fatalf("准备数据失败: %v", err)
		}
	}

	jm := jwt.New("test-secret", time.Hour, "test")
	token, _ := jm.Issue(1)

	w := doJSON(r, http.MethodGet, "/api/v1/users?page_size=2", token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("状态码应为 200，实际 %d，body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	users := resp["users"].([]any)
	if len(users) != 2 {
		t.Fatalf("应返回 2 条，实际 %d", len(users))
	}
	if resp["nextPageToken"] == "" {
		t.Fatal("应有 nextPageToken")
	}
}

// ---- 资源属主校验（授权） ----

// registerForTest 走完整注册流程创建一个真实用户（含 bcrypt 哈希），返回其 accessToken。
// fakeRepository 的 ID 从 1 开始自增，所以按注册顺序依次是 users/1、users/2……
func registerForTest(t *testing.T, r *gin.Engine, email, password, displayName string) string {
	t.Helper()

	w := doJSON(r, http.MethodPost, "/api/v1/auth/register", "",
		fmt.Sprintf(`{"email":%q,"password":%q,"displayName":%q}`, email, password, displayName))
	if w.Code != http.StatusOK {
		t.Fatalf("注册 %s 失败：状态码 %d，body=%s", email, w.Code, w.Body.String())
	}
	var resp struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("注册响应不是合法 JSON: %v", err)
	}
	if resp.AccessToken == "" {
		t.Fatal("注册响应缺少 accessToken")
	}
	return resp.AccessToken
}

// TestHandlerOwnershipAllowSelf 验证属主本人可以操作自己的资源：
// 改昵称、改密码、删账号都应成功。
func TestHandlerOwnershipAllowSelf(t *testing.T) {
	r, repo := newTestRouter(t)
	token := registerForTest(t, r, "owner@example.com", "password123", "Owner") // users/1

	// 1. 改自己的昵称。
	w := doJSON(r, http.MethodPatch, "/api/v1/users/1", token, `{"displayName":"OwnerRenamed"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("改自己昵称应为 200，实际 %d，body=%s", w.Code, w.Body.String())
	}
	if u, err := repo.FindByID(context.Background(), 1); err != nil || u.DisplayName != "OwnerRenamed" {
		t.Fatalf("昵称应已更新为 OwnerRenamed，实际 %+v，err=%v", u, err)
	}

	// 2. 改自己的密码。
	w = doJSON(r, http.MethodPost, "/api/v1/users/1/password", token,
		`{"currentPassword":"password123","newPassword":"newpassword456"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("改自己密码应为 200，实际 %d，body=%s", w.Code, w.Body.String())
	}
	if u, err := repo.FindByID(context.Background(), 1); err != nil || !passwordMatches(u.PasswordHash, "newpassword456") {
		t.Fatalf("新密码应已生效，err=%v", err)
	}

	// 3. 删自己。
	w = doJSON(r, http.MethodDelete, "/api/v1/users/1", token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("删自己应为 200，实际 %d，body=%s", w.Code, w.Body.String())
	}
	if _, err := repo.FindByID(context.Background(), 1); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("用户 1 应已被删除，实际 err=%v", err)
	}
}

// TestHandlerOwnershipDenyOthers 验证用户 1 无法操作 用户 2：
// 三个写接口都返回 403 + PERMISSION_DENIED，且用户 2 的数据没有任何变化。
func TestHandlerOwnershipDenyOthers(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"PATCH 他人昵称", http.MethodPatch, "/api/v1/users/2", `{"displayName":"Hacked"}`},
		{"DELETE 他人账号", http.MethodDelete, "/api/v1/users/2", ""},
		{
			"改他人密码", http.MethodPost, "/api/v1/users/2/password",
			`{"currentPassword":"password123","newPassword":"hacked123456"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, repo := newTestRouter(t)
			token1 := registerForTest(t, r, "owner@example.com", "password123", "Owner") // users/1
			registerForTest(t, r, "other@example.com", "password123", "Other")           // users/2

			w := doJSON(r, tc.method, tc.path, token1, tc.body)
			if w.Code != http.StatusForbidden {
				t.Fatalf("越权应返回 403，实际 %d，body=%s", w.Code, w.Body.String())
			}

			var eb errorBody
			if err := json.Unmarshal(w.Body.Bytes(), &eb); err != nil {
				t.Fatalf("错误响应不是合法 JSON: %v", err)
			}
			if eb.Error.Code != "PERMISSION_DENIED" {
				t.Fatalf("错误码应为 PERMISSION_DENIED，实际 %q", eb.Error.Code)
			}

			// 越权请求不能落库：用户 2 仍存在，昵称与密码都未变。
			other, err := repo.FindByID(context.Background(), 2)
			if err != nil {
				t.Fatalf("用户 2 不应被删除: %v", err)
			}
			if other.DisplayName != "Other" {
				t.Fatalf("用户 2 昵称不应被修改，实际 %q", other.DisplayName)
			}
			if !passwordMatches(other.PasswordHash, "password123") {
				t.Fatal("用户 2 的密码不应被修改")
			}
		})
	}
}

// spyService 在真实 Service 外面包一层计数，用来证明授权失败时根本没进入业务层。
// 只覆盖三个受保护方法，其余方法通过嵌入的接口直接代理。
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

func (s *spyService) DeleteUser(ctx context.Context, id int64) error {
	s.deleteCalls++
	return s.Service.DeleteUser(ctx, id)
}

func (s *spyService) ChangePassword(ctx context.Context, id int64, currentPassword, newPassword string) error {
	s.changePasswordCalls++
	return s.Service.ChangePassword(ctx, id, currentPassword, newPassword)
}

// TestHandlerOwnershipDoesNotReachService 直接验证“越权时没有调用 Service 方法”，
// 而不是只看数据有没有被改动。
func TestHandlerOwnershipDoesNotReachService(t *testing.T) {
	gin.SetMode(gin.TestMode)

	repo := newFakeRepository()
	jm := jwt.New("test-secret", time.Hour, "test")
	spy := &spyService{Service: NewService(repo, jm)}

	h, err := NewHandler(spy)
	if err != nil {
		t.Fatalf("初始化 Handler 失败: %v", err)
	}

	r := gin.New()
	auth := JWTAuth(jm)
	protected := r.Group("/api/v1")
	protected.Use(auth)
	{
		protected.PATCH("/users/:id", h.UpdateUser)
		protected.DELETE("/users/:id", h.DeleteUser)
		protected.POST("/users/:id/password", h.ChangePassword)
	}

	ctx := context.Background()
	if err := repo.Create(ctx, &User{Email: "a@example.com", PasswordHash: "fake-hash", DisplayName: "A"}); err != nil {
		t.Fatalf("准备数据失败: %v", err)
	}
	if err := repo.Create(ctx, &User{Email: "b@example.com", PasswordHash: "fake-hash", DisplayName: "B"}); err != nil {
		t.Fatalf("准备数据失败: %v", err)
	}
	token1, err := jm.Issue(1)
	if err != nil {
		t.Fatalf("签发测试 token 失败: %v", err)
	}

	requests := []struct {
		method, path, body string
	}{
		{http.MethodPatch, "/api/v1/users/2", `{"displayName":"Hacked"}`},
		{http.MethodDelete, "/api/v1/users/2", ""},
		{http.MethodPost, "/api/v1/users/2/password", `{"currentPassword":"x","newPassword":"hacked123456"}`},
	}
	for _, req := range requests {
		if w := doJSON(r, req.method, req.path, token1, req.body); w.Code != http.StatusForbidden {
			t.Fatalf("%s %s 应为 403，实际 %d，body=%s", req.method, req.path, w.Code, w.Body.String())
		}
	}

	if spy.updateCalls != 0 || spy.deleteCalls != 0 || spy.changePasswordCalls != 0 {
		t.Fatalf("授权失败时不应调用 Service：update=%d delete=%d changePassword=%d",
			spy.updateCalls, spy.deleteCalls, spy.changePasswordCalls)
	}
}

// TestHandlerOwnershipMissingUserID 验证 Context 里没有 userID 时返回 500 而不是 403：
// 那是路由或中间件装配错误（服务端问题），不是调用方权限不足。
func TestHandlerOwnershipMissingUserID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	repo := newFakeRepository()
	jm := jwt.New("test-secret", time.Hour, "test")
	h, err := NewHandler(NewService(repo, jm))
	if err != nil {
		t.Fatalf("初始化 Handler 失败: %v", err)
	}

	// 故意不挂 JWTAuth：Context 里永远不会出现 userID。
	r := gin.New()
	r.PATCH("/api/v1/users/:id", h.UpdateUser)

	w := doJSON(r, http.MethodPatch, "/api/v1/users/1", "", `{"displayName":"Nobody"}`)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("缺少 userID 应返回 500，实际 %d，body=%s", w.Code, w.Body.String())
	}

	var eb errorBody
	if err := json.Unmarshal(w.Body.Bytes(), &eb); err != nil {
		t.Fatalf("错误响应不是合法 JSON: %v", err)
	}
	if eb.Error.Code != "INTERNAL" {
		t.Fatalf("错误码应为 INTERNAL，实际 %q", eb.Error.Code)
	}
}

// TestHandlerProtectedRouteStillRequiresAuth 锁定“认证与授权不混”：
// 无 token 访问三个受保护写接口，仍必须是 401，而不是被授权逻辑误报为 403。
func TestHandlerProtectedRouteStillRequiresAuth(t *testing.T) {
	r, _ := newTestRouter(t)

	requests := []struct {
		method, path, body string
	}{
		{http.MethodPatch, "/api/v1/users/1", `{"displayName":"X"}`},
		{http.MethodDelete, "/api/v1/users/1", ""},
		{http.MethodPost, "/api/v1/users/1/password", `{"currentPassword":"x","newPassword":"newpassword456"}`},
	}
	for _, req := range requests {
		w := doJSON(r, req.method, req.path, "", req.body)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s 无 token 应为 401，实际 %d，body=%s", req.method, req.path, w.Code, w.Body.String())
		}
	}
}

// TestHandlerAuthFailureStopsHandlerChain 锁定“认证失败必须中止调用链”：
// JWTAuth 里只 return 不 Abort 的话，gin 仍会继续执行 Handler，
// 结果是 401 状态码 + 两段 JSON 拼接的畸形响应，且 Handler 可能继续落库。
func TestHandlerAuthFailureStopsHandlerChain(t *testing.T) {
	r, repo := newTestRouter(t)

	if err := repo.Create(context.Background(), &User{
		Email: "victim@example.com", PasswordHash: "fake-hash", DisplayName: "Victim",
	}); err != nil {
		t.Fatalf("准备数据失败: %v", err)
	}

	cases := []struct {
		name   string
		token  string
		method string
		path   string
		body   string
	}{
		{"无 token 改他人", "", http.MethodPatch, "/api/v1/users/1", `{"displayName":"Hacked"}`},
		{"垃圾 token 删他人", "garbage-token", http.MethodDelete, "/api/v1/users/1", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := doJSON(r, tc.method, tc.path, tc.token, tc.body)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("应为 401，实际 %d，body=%s", w.Code, w.Body.String())
			}

			// 响应体必须是单个完整 JSON。若中间件没 Abort，Handler 会再写一段 body，
			// json.Unmarshal 会因 "invalid character '{' after top-level value" 失败。
			var eb errorBody
			if err := json.Unmarshal(w.Body.Bytes(), &eb); err != nil {
				t.Fatalf("401 响应体必须是单个合法 JSON（可能 Handler 未被中止），实际 %v，body=%s", err, w.Body.String())
			}
			if eb.Error.Code != "UNAUTHENTICATED" {
				t.Fatalf("错误码应为 UNAUTHENTICATED，实际 %q", eb.Error.Code)
			}

			// 认证失败不能产生任何副作用。
			u, err := repo.FindByID(context.Background(), 1)
			if err != nil {
				t.Fatalf("用户不应被删除: %v", err)
			}
			if u.DisplayName != "Victim" {
				t.Fatalf("用户昵称不应被修改，实际 %q", u.DisplayName)
			}
		})
	}
}
