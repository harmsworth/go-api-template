package user

import (
	"context"
	"encoding/json"
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
