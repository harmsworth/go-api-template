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
