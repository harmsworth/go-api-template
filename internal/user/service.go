package user

import (
	"context"
	"errors"
	"strconv"

	"golang.org/x/crypto/bcrypt"
)

// Repository 是 Service 依赖的存储接口，由消费者（Service）定义在这里。
// 方法集只包含 Service 实际用到的 6 个方法，不暴露 GORM 的任何概念；
// repository.go 里的实现隐式满足它，测试里用 fakeRepository 替换它。
type Repository interface {
	Create(ctx context.Context, u *User) error
	FindByID(ctx context.Context, id int64) (*User, error)
	FindByEmail(ctx context.Context, email string) (*User, error)
	List(ctx context.Context, offset, limit int) ([]*User, int64, error)
	Update(ctx context.Context, u *User) error
	Delete(ctx context.Context, id int64) error
}

// TokenIssuer 是 Service 对“签发 token”能力的最小依赖（一个方法）。
// jwt.Manager 天然满足它；测试里可以用 fake 替代。
// 这不是过度抽象：它让 Service 不 import 具体的 jwt 实现。
type TokenIssuer interface {
	Issue(userID int64) (string, error)
}

const (
	defaultPageSize = 20
	maxPageSize     = 100
)

// service 是业务规则的具体实现（未导出）：
// CRUD 规则、注册、登录、修改密码、密码哈希、调用 Repository 和 TokenIssuer。
// 对外的契约是 handler.go 里的 Service 接口——包外只见接口，不见实现。
type service struct {
	repo   Repository
	tokens TokenIssuer
}

// NewService 是普通的 Go 构造函数，不使用任何 DI 框架。
// 返回接口而不是具体类型：具体类型 service 未导出，包外也写不出它的名字。
func NewService(repo Repository, tokens TokenIssuer) Service {
	return &service{repo: repo, tokens: tokens}
}

// CreateUser 按业务规则创建用户（注册内部也复用它）。
func (s *service) CreateUser(ctx context.Context, email, password, displayName string) (*User, error) {
	hash, err := hashPassword(password)
	if err != nil {
		return nil, err
	}
	u := &User{
		Email:        email,
		PasswordHash: hash,
		DisplayName:  displayName,
	}
	if err := s.repo.Create(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

func (s *service) GetUser(ctx context.Context, id int64) (*User, error) {
	return s.repo.FindByID(ctx, id)
}

// ListUsers 处理分页逻辑：默认页大小、解析 page_token、生成 next_page_token。
// page_token 在第一版就是“偏移量的十进制字符串”，
// 真实的 opaque token 是后续升级方向（见 docs/next-step.md）。
func (s *service) ListUsers(ctx context.Context, pageSize int, pageToken string) ([]*User, string, error) {
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	pageSize = min(pageSize, maxPageSize)

	offset := 0
	if pageToken != "" {
		var err error
		offset, err = strconv.Atoi(pageToken)
		if err != nil || offset < 0 {
			return nil, "", ErrInvalidPageToken
		}
	}

	users, total, err := s.repo.List(ctx, offset, pageSize)
	if err != nil {
		return nil, "", err
	}

	next := ""
	if offset+pageSize < int(total) {
		next = strconv.Itoa(offset + pageSize)
	}
	return users, next, nil
}

func (s *service) UpdateUser(ctx context.Context, id int64, displayName string) (*User, error) {
	u, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	u.DisplayName = displayName
	if err := s.repo.Update(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

func (s *service) DeleteUser(ctx context.Context, id int64) error {
	// 先确认存在，保证“删除不存在的用户”返回 404 而不是静默成功。
	if _, err := s.repo.FindByID(ctx, id); err != nil {
		return err
	}
	return s.repo.Delete(ctx, id)
}

// Register 自助注册：创建用户并直接签发 token（自动登录）。
func (s *service) Register(ctx context.Context, email, password, displayName string) (string, *User, error) {
	u, err := s.CreateUser(ctx, email, password, displayName)
	if err != nil {
		return "", nil, err
	}
	token, err := s.tokens.Issue(u.ID)
	if err != nil {
		return "", nil, err
	}
	return token, u, nil
}

// Login 校验邮箱密码，成功则返回 token。
// “邮箱不存在”与“密码错误”统一返回 ErrInvalidPassword，
// 不向外部泄露某个邮箱是否已注册。
func (s *service) Login(ctx context.Context, email, password string) (string, *User, error) {
	u, err := s.repo.FindByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return "", nil, ErrInvalidPassword
		}
		return "", nil, err
	}
	if !passwordMatches(u.PasswordHash, password) {
		return "", nil, ErrInvalidPassword
	}
	token, err := s.tokens.Issue(u.ID)
	if err != nil {
		return "", nil, err
	}
	return token, u, nil
}

// ChangePassword 校验当前密码后设置新密码。
func (s *service) ChangePassword(ctx context.Context, id int64, currentPassword, newPassword string) error {
	u, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if !passwordMatches(u.PasswordHash, currentPassword) {
		return ErrInvalidPassword
	}
	hash, err := hashPassword(newPassword)
	if err != nil {
		return err
	}
	u.PasswordHash = hash
	return s.repo.Update(ctx, u)
}

// ---- 密码工具函数 ----

// hashPassword 使用 bcrypt 生成密码哈希。
// 注意 bcrypt 的两个事实（因此 proto 契约层限制了 max_len: 72）：
//  1. bcrypt 只使用输入的前 72 字节，更长的部分会被静默截断；
//  2. 超过 72 字节时 x/crypto/bcrypt 会直接返回 ErrPasswordTooLong。
func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// passwordMatches 用常数时间比较校验密码与哈希。
func passwordMatches(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
