# 13 · 测试 —— 单元、Handler、集成三层

## 1. 这一阶段做什么

在 `internal/user/` 包里新增三个测试文件：

| 文件                             | 测试对象                                     | 依赖                      |
| -------------------------------- | -------------------------------------------- | ------------------------- |
| `service_test.go`                | Service 业务规则（手写 fake，无外部依赖）    | 无                        |
| `handler_test.go`                | Handler + 中间件 + protovalidate（httptest） | 无                        |
| `repository_integration_test.go` | Repository + 真实 MySQL                      | 环境变量 `MYSQL_TEST_DSN` |

## 2. 为什么要做

**为什么测试写在这里、写这三个？**
- Service 是业务规则的家（[09](./09-user-service.md)），它的正确性最重要，且它只依赖两个小接口
  （`Repository`、`TokenIssuer`），用手写 fake 就能 100% 隔离测试——毫秒级、无数据库；
- Handler 是协议翻译官（[11](./11-handler-router.md)），要验证"bind→validate→service→mapError"这条流水线和
  JWT 中间件真的按预期工作，用标准库 `net/http/httptest` 发请求即可；
- Repository 的 GORM 查询写对写错，只有真数据库说了算，所以要一条集成测试。

**依赖谁 / 谁依赖它？**
测试文件与被测代码同包（`package user`），可以直接使用同包的 fake、构造函数和领域错误，
不需要导出任何东西。测试不依赖别人——别人也不依赖测试，它们是 [08](./08-user-model-repository.md)/[09](./09-user-service.md)/[11](./11-handler-router.md) 交付质量的证据。

## 3. 手动执行什么命令

无需安装任何测试库——全部用标准库 `testing` + `net/http/httptest`，没有 testify、没有 mockgen。

## 4. 创建什么文件

```text
internal/user/service_test.go
internal/user/handler_test.go
internal/user/repository_integration_test.go
```

## 5. 把什么代码写进去

### `internal/user/service_test.go`

```go
package user

import (
	"context"
	"strconv"
	"testing"
)

// fakeRepository 是手写的内存版 Repository，专门用于测试。
// 不引入 mockgen 等自动生成工具——这么小的接口，手写更直观。
type fakeRepository struct {
	users  map[int64]*User
	emails map[string]int64 // email -> id
	nextID int64
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		users:  map[int64]*User{},
		emails: map[string]int64{},
	}
}

func (f *fakeRepository) Create(_ context.Context, u *User) error {
	if _, exists := f.emails[u.Email]; exists {
		return ErrEmailAlreadyExists
	}
	f.nextID++
	clone := *u
	clone.ID = f.nextID
	f.users[clone.ID] = &clone
	f.emails[clone.Email] = clone.ID
	u.ID = clone.ID
	return nil
}

func (f *fakeRepository) FindByID(_ context.Context, id int64) (*User, error) {
	u, ok := f.users[id]
	if !ok {
		return nil, ErrUserNotFound
	}
	clone := *u
	return &clone, nil
}

func (f *fakeRepository) FindByEmail(_ context.Context, email string) (*User, error) {
	id, ok := f.emails[email]
	if !ok {
		return nil, ErrUserNotFound
	}
	clone := *f.users[id]
	return &clone, nil
}

func (f *fakeRepository) List(_ context.Context, offset, limit int) ([]*User, int64, error) {
	total := int64(len(f.users))
	var out []*User
	for id := int64(1); id <= f.nextID; id++ {
		u, ok := f.users[id]
		if !ok {
			continue
		}
		if offset > 0 {
			offset--
			continue
		}
		if limit >= 0 && len(out) == limit {
			break
		}
		clone := *u
		out = append(out, &clone)
	}
	return out, total, nil
}

func (f *fakeRepository) Update(_ context.Context, u *User) error {
	if _, ok := f.users[u.ID]; !ok {
		return ErrUserNotFound
	}
	clone := *u
	f.users[u.ID] = &clone
	return nil
}

func (f *fakeRepository) Delete(_ context.Context, id int64) error {
	u, ok := f.users[id]
	if !ok {
		return ErrUserNotFound
	}
	delete(f.users, id)
	delete(f.emails, u.Email)
	return nil
}

// fakeTokenIssuer 是手写的 TokenIssuer，让 Service 测试不依赖真实 JWT。
type fakeTokenIssuer struct{}

func (fakeTokenIssuer) Issue(userID int64) (string, error) {
	return "token-" + strconv.FormatInt(userID, 10), nil
}

// newTestService 创建被测 Service（fake repo + fake token）。
func newTestService() Service {
	return NewService(newFakeRepository(), fakeTokenIssuer{})
}

func TestServiceRegisterSuccess(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	token, u, err := svc.Register(ctx, "alice@example.com", "password123", "Alice")
	if err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	if token == "" {
		t.Fatal("注册成功应返回 token")
	}
	if u.ID != 1 {
		t.Fatalf("第一个用户 ID 应为 1，实际 %d", u.ID)
	}
	if u.PasswordHash == "password123" || u.PasswordHash == "" {
		t.Fatal("密码必须被哈希，不能存明文")
	}
}

func TestServiceRegisterDuplicateEmail(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	if _, _, err := svc.Register(ctx, "bob@example.com", "password123", "Bob"); err != nil {
		t.Fatalf("第一次注册失败: %v", err)
	}
	if _, _, err := svc.Register(ctx, "bob@example.com", "password456", "Bob2"); err == nil {
		t.Fatal("重复邮箱注册应返回错误")
	} else if err != ErrEmailAlreadyExists {
		t.Fatalf("错误应为 ErrEmailAlreadyExists，实际 %v", err)
	}
}

func TestServiceLoginSuccess(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	if _, _, err := svc.Register(ctx, "carl@example.com", "password123", "Carl"); err != nil {
		t.Fatalf("注册失败: %v", err)
	}

	token, u, err := svc.Login(ctx, "carl@example.com", "password123")
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	if token == "" || u.ID != 1 {
		t.Fatalf("登录应返回 token 和用户，实际 token=%q id=%d", token, u.ID)
	}
}

func TestServiceLoginWrongPassword(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	if _, _, err := svc.Register(ctx, "dave@example.com", "password123", "Dave"); err != nil {
		t.Fatalf("注册失败: %v", err)
	}

	// 密码错误和邮箱不存在都必须返回同一个 ErrInvalidPassword，
	// 避免泄露“邮箱是否已注册”。
	if _, _, err := svc.Login(ctx, "dave@example.com", "wrong-password"); err != ErrInvalidPassword {
		t.Fatalf("密码错误应返回 ErrInvalidPassword，实际 %v", err)
	}
	if _, _, err := svc.Login(ctx, "nobody@example.com", "password123"); err != ErrInvalidPassword {
		t.Fatalf("邮箱不存在也应返回 ErrInvalidPassword，实际 %v", err)
	}
}

func TestServiceChangePassword(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	if _, _, err := svc.Register(ctx, "erin@example.com", "old-password", "Erin"); err != nil {
		t.Fatalf("注册失败: %v", err)
	}

	if err := svc.ChangePassword(ctx, 1, "wrong-current", "new-password"); err != ErrInvalidPassword {
		t.Fatalf("当前密码错误应返回 ErrInvalidPassword，实际 %v", err)
	}
	if err := svc.ChangePassword(ctx, 1, "old-password", "new-password"); err != nil {
		t.Fatalf("修改密码失败: %v", err)
	}

	// 旧密码登录失败，新密码登录成功。
	if _, _, err := svc.Login(ctx, "erin@example.com", "old-password"); err != ErrInvalidPassword {
		t.Fatalf("旧密码应失效，实际 %v", err)
	}
	if _, _, err := svc.Login(ctx, "erin@example.com", "new-password"); err != nil {
		t.Fatalf("新密码应能登录: %v", err)
	}
}

func TestServiceListUsersPagination(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		email := "p" + strconv.Itoa(i) + "@example.com"
		if _, _, err := svc.Register(ctx, email, "password123", "P"+strconv.Itoa(i)); err != nil {
			t.Fatalf("注册失败: %v", err)
		}
	}

	users, next, err := svc.ListUsers(ctx, 2, "")
	if err != nil {
		t.Fatalf("第一页查询失败: %v", err)
	}
	if len(users) != 2 || next == "" {
		t.Fatalf("第一页应返回 2 条且有 next token，实际 %d 条 next=%q", len(users), next)
	}
	if users[0].ID != 1 {
		t.Fatalf("应按 id 升序返回，实际第一条 ID=%d", users[0].ID)
	}

	users, next, err = svc.ListUsers(ctx, 2, next)
	if err != nil {
		t.Fatalf("第二页查询失败: %v", err)
	}
	if len(users) != 2 || next == "" {
		t.Fatalf("第二页应返回 2 条且有 next token，实际 %d 条 next=%q", len(users), next)
	}

	users, next, err = svc.ListUsers(ctx, 2, next)
	if err != nil {
		t.Fatalf("第三页查询失败: %v", err)
	}
	if len(users) != 1 || next != "" {
		t.Fatalf("最后一页应返回 1 条且没有 next token，实际 %d 条 next=%q", len(users), next)
	}

	// 非法 page_token 返回领域错误。
	if _, _, err := svc.ListUsers(ctx, 2, "not-a-number"); err != ErrInvalidPageToken {
		t.Fatalf("非法 page_token 应返回 ErrInvalidPageToken，实际 %v", err)
	}
}
```

### `internal/user/handler_test.go`

```go
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
```

### `internal/user/repository_integration_test.go`

```go
package user

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// TestUserRepositoryIntegration 是 Repository + 真实 MySQL 的集成测试。
//
// 运行前提（详见 docs/init/13-testing.md）：
//  1. 本地 MySQL 已启动（docker compose up -d 或本机 MySQL）
//  2. 迁移已执行（make migrate-up），否则表不存在
//  3. 设置环境变量 MYSQL_TEST_DSN，例如（PowerShell）：
//     $env:MYSQL_TEST_DSN = "root:123456@tcp(localhost:3306)/todo_x?charset=utf8mb4&parseTime=True&loc=UTC"
//
// 未设置 MYSQL_TEST_DSN 时测试自动跳过，保证 go test ./... 在任何机器都能通过。
// 这不是 Testcontainers 那类重量级方案，就是一个“有真实 MySQL 就跑”的普通测试。
func TestUserRepositoryIntegration(t *testing.T) {
	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("未设置 MYSQL_TEST_DSN，跳过集成测试")
	}

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("连接 MySQL 失败: %v", err)
	}

	repo := NewRepository(db)
	ctx := context.Background()

	// 用纳秒时间戳拼邮箱，保证测试可以反复运行。
	email := fmt.Sprintf("it-%d@example.com", time.Now().UnixNano())

	// Create
	u := &User{Email: email, PasswordHash: "fake-hash", DisplayName: "集成测试"}
	if err := repo.Create(ctx, u); err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if u.ID == 0 {
		t.Fatal("Create 后应回填自增主键 ID")
	}

	// FindByEmail
	got, err := repo.FindByEmail(ctx, email)
	if err != nil {
		t.Fatalf("FindByEmail 失败: %v", err)
	}
	if got.ID != u.ID || got.DisplayName != "集成测试" {
		t.Fatalf("FindByEmail 结果不符: %+v", got)
	}

	// FindByID
	byID, err := repo.FindByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("FindByID 失败: %v", err)
	}
	if byID.Email != email {
		t.Fatalf("FindByID 结果不符: %+v", byID)
	}

	// Update
	byID.DisplayName = "已改名"
	if err := repo.Update(ctx, byID); err != nil {
		t.Fatalf("Update 失败: %v", err)
	}
	after, err := repo.FindByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("Update 后查询失败: %v", err)
	}
	if after.DisplayName != "已改名" {
		t.Fatalf("Update 未生效: %+v", after)
	}

	// List
	users, total, err := repo.List(ctx, 0, 10)
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if total < 1 || len(users) < 1 {
		t.Fatalf("List 应至少返回 1 条，实际 total=%d len=%d", total, len(users))
	}

	// 重复邮箱应返回 ErrEmailAlreadyExists。
	if err := repo.Create(ctx, &User{Email: email, PasswordHash: "x", DisplayName: "dup"}); !errors.Is(err, ErrEmailAlreadyExists) {
		t.Fatalf("重复邮箱应返回 ErrEmailAlreadyExists，实际 %v", err)
	}

	// Delete + NotFound
	if err := repo.Delete(ctx, u.ID); err != nil {
		t.Fatalf("Delete 失败: %v", err)
	}
	if _, err := repo.FindByID(ctx, u.ID); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("删除后查询应返回 ErrUserNotFound，实际 %v", err)
	}
}
```

## 6. 解释关键代码

**为什么手写 fake，而不用 mockgen？**
`Repository` 只有 6 个方法、`TokenIssuer` 只有 1 个方法——接口这么小，手写内存实现
（两个 map）比"装代码生成工具 → 学它的 DSL → 读生成的 mock"直观得多。
fake 还能精确复现领域错误（重复邮箱返回 `ErrEmailAlreadyExists`、查不到返回
`ErrUserNotFound`），让 Service 测试聚焦在"规则对不对"。工具是好东西，但要在
接口大到"手写开始痛苦"时才引入——这也是 `docs/next-step.md` 的按需引入原则。

**Service 测试覆盖的场景。**

| 测试                                | 场景                | 验证的规则                                                             |
| ----------------------------------- | ------------------- | ---------------------------------------------------------------------- |
| `TestServiceRegisterSuccess`        | 注册成功            | 返回 token、ID 从 1 自增、**密码被哈希**（不存明文）                   |
| `TestServiceRegisterDuplicateEmail` | 重复邮箱            | 返回 `ErrEmailAlreadyExists`                                           |
| `TestServiceLoginSuccess`           | 登录成功            | 返回 token 与用户                                                      |
| `TestServiceLoginWrongPassword`     | 密码错 / 邮箱不存在 | **两者都返回同一个** `ErrInvalidPassword`（防用户枚举）                |
| `TestServiceChangePassword`         | 改密                | 旧密码错→报错；成功后旧密码失效、新密码可登录                          |
| `TestServiceListUsersPagination`    | 分页                | 三页翻完、id 升序、`next` 到底为空、非法 token → `ErrInvalidPageToken` |

注意 fake 的一个细节：它返回的是 `User` 的**副本**（`clone := *u`），
模拟真实数据库"每次查询都是新对象"，避免测试内部因共享指针而假通过。

**Handler 测试为什么在本包内自建最小路由？**
`newTestRouter` 看起来和 `app.NewRouter` 重复，其实必须如此：
本测试在 `package user` 里，而 `internal/app` import 了 `internal/user`——
如果测试再 import `internal/app` 就会形成 **import cycle**，编译都过不了。
所以测试自建一个只挂 6 条路由的最小引擎（并注释提醒与 `app.NewRouter` 保持一致）。
`httptest.NewRequest` + `httptest.NewRecorder` + `r.ServeHTTP(w, req)` 的组合
让测试不需要监听端口就能跑完整 Gin 管线（中间件、路由、handler 全部真实执行）。

**Handler 测试覆盖的场景。**
创建成功（201 + `name=users/1`，验证 protojson 的 camelCase 输出与资源名格式）、
protovalidate 拦截非法邮箱（400 + `INVALID_ARGUMENT`）、
中间件三态（无 token 401 / 合法 token 200 / 垃圾 token 401）、
查不到的用户（404 + `NOT_FOUND`）、登录（错误密码 401、正确密码 200 且含 `accessToken`）、
分页（2 条 + `nextPageToken`）。

**集成测试的环境变量跳过机制。**
第一行就检查 `MYSQL_TEST_DSN`：没设置就 `t.Skip`。
这样 `go test ./...` 在任何机器（包括 CI 的纯编译阶段）都能通过，
而在有 MySQL 的机器上才真正跑数据库断言。它要求**迁移已执行**（表必须存在），
测试邮箱用纳秒时间戳拼出来，保证可以反复运行。
说明：这不是 Testcontainers 那类自动起容器的重量级方案——就是一个
"有真实 MySQL 就跑"的普通测试，简单、可控、零依赖。

## 7. 做一个小检查

先跑不需要 MySQL 的全部测试：

```powershell
go test ./...
```

预期输出（集成测试显示 SKIP）：

```text
ok      go-api-template/internal/user   1.2s
```

要看细节可以加 `-v`（能看到每个 `Test*` 的 PASS/SKIP）。

再让集成测试真正运行——PowerShell 里设置环境变量（只对当前会话生效）：

```powershell
$env:MYSQL_TEST_DSN = "root:123456@tcp(localhost:3306)/todo_x?charset=utf8mb4&parseTime=True&loc=UTC"
go test ./internal/user/ -run TestUserRepositoryIntegration -v
```

MySQL 已启动且迁移已执行时，应看到：

```text
=== RUN   TestUserRepositoryIntegration
--- PASS: TestUserRepositoryIntegration (0.05s)
PASS
```

> **本阶段成果：** 三层测试就位：Service 用手写 fake 验证全部业务规则（毫秒级），
> Handler 用 httptest 验证协议流水线与 JWT 中间件，Repository 用环境变量门控的
> 真实 MySQL 集成测试兜底。`go test ./...` 随时可跑、永不假红。
