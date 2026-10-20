# 09 · User Service —— 业务规则的核心

## 1. 这一阶段做什么

在 `internal/user/` 包里新增一个文件 `service.go`：
承载 User Feature 的全部业务规则——CRUD 约束、注册、登录、修改密码、密码哈希、分页。

## 2. 为什么要做

**Service 是什么？**
它是"业务规则住的地方"。Handler 只负责 HTTP 协议的事（解析请求、返回状态码），
Repository 只负责"把数据存进去/读出来"，两者都不该写业务规则。
比如"密码必须 bcrypt 哈希"、"登录失败统一报 invalid password"、"page_size 默认 20 上限 100"，
这些规则只应该存在一份，就在 Service 里。

**为什么在 `internal/user/` 里？**
业务规则属于 Feature 本身，和 model/repository 放在同一个包（Package by Feature）。
这样读一个 Feature 时，"数据长什么样 → 怎么存 → 规则是什么"一气呵成。

**为什么现在创建？**
[08](./08-user-model-repository.md) 已经定义了领域错误，Service 是它们的第一个消费者；`Repository` 接口也由本文件定义
（消费者侧接口，“我要存储提供什么”由我声明）；[11](./11-handler-router.md) 的 Handler 又以 Service 为唯一入口。
它在依赖链的中间，必须先于 Handler 出现。

**依赖谁 / 谁依赖它？**

```text
internal/user/service.go
   依赖（都是接口）：
   ├── Repository   —— 本文件定义的存储接口（消费者侧；实现在 repository.go，12 阶段由 main 注入）
   └── TokenIssuer  —— 本文件定义的单方法接口（10 阶段的 jwt.Manager 天然满足）
        ↑
        └── 11 的 Handler 只调用 Service，12 的 main.go 组装它们
```

注意：Service **不 import** gin、proto 生成代码、jwt 包——它只是普通 Go 函数的集合。

## 3. 手动执行什么命令

无需安装新依赖：`golang.org/x/crypto/bcrypt` 在 [04](./04-mysql.md) 阶段已经随依赖进入 `go.mod`
（本教程里如果 `go mod tidy` 提示缺失，执行一次 `go mod tidy` 即可）。

## 4. 创建什么文件

```text
internal/user/service.go
```

## 5. 把什么代码写进去

### `internal/user/service.go`

```go
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
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}

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
```

## 6. 解释关键代码

**`TokenIssuer`：只有一个方法的消费者侧接口。**
Service 需要的能力其实只有一个："给我 userID，还我 token 字符串"。
所以接口只声明这一个方法，并且定义在 Service 旁边（消费者侧）：

- Service 文件里没有出现 `import .../jwt`，它完全不知道 JWT 的存在；
- [10](./10-jwt.md) 阶段的 `jwt.Manager` 拥有 `Issue(userID int64) (string, error)` 方法，
  Go 的隐式接口让它**自动满足** `TokenIssuer`，不需要任何声明和适配代码；
- [13](./13-testing.md) 阶段的测试用 `fakeTokenIssuer`（返回 `"token-1"` 这种假串）替换它，
  Service 的测试就不需要构造真实 JWT。

这就是"接口越小越好"的落地示例：接口只描述消费者此刻需要的能力。

**`Repository`：同一原则的完整版。**
`Repository` 接口也定义在消费者侧（本文件开头），方法集是 Service 实际用到的 6 个
存储操作——没有任何 GORM 概念。它和 `TokenIssuer` 一起构成 Service 的全部依赖面：
实现可以换成 GORM、内存版 fake、未来的任何存储，Service 一行不改。

**为什么结构体叫小写 `service`、接口叫大写 `Service`？**
一个包里接口和具体类型不能同名。这里把可见性对调：**契约导出、实现隐藏**——
`Service` 接口定义在 handler.go（[11](./11-handler-router.md) 章，因为 Handler 是它的消费者），
具体结构体 `service` 未导出，`NewService` 返回接口。包外（main.go、其他 Feature）
只能拿到契约面，写不出实现类型的名字；测试同样通过接口注入 fake。

> **按章节顺序执行的注意**：`Service` 接口在 [11](./11-handler-router.md) 章的 handler.go 里才出现。
> 如果你严格跟随章节顺序、此刻还没有 handler.go，可先让 `NewService` 临时返回
> `*service`（编译能过，只是包外能看到具体类型），等 [11](./11-handler-router.md) 章建好接口后再改回返回 `Service`。
> 本仓库是完整代码，两种写法最终等价。

**bcrypt 与 72 字节限制。**
`hashPassword` 用 `bcrypt.GenerateFromPassword` + `bcrypt.DefaultCost`（cost=10）。
bcrypt 有一个历史包袱：算法只使用输入的**前 72 字节**。所以 [07](./07-protovalidate.md) 阶段的 proto 契约
给 password 设了 `max_len: 72`——在入口（Handler 的 protovalidate）就拒绝超长密码，
而不是让它在深处被静默截断或报错。契约层和业务层在这里互相呼应。

**Login 为什么把"邮箱不存在"也报成 `ErrInvalidPassword`？**
如果"邮箱不存在"和"密码错误"返回不同错误，攻击者就能批量探测某个邮箱是否注册过
（用户枚举攻击）。统一映射成同一个 `ErrInvalidPassword`（→ HTTP 401）后，
外部无法区分两种情况。代价是真实用户输错邮箱时的提示不够精确——这是安全上的刻意取舍。

**分页：`page_token` 第一版就是 offset。**
`ListUsers` 做三件事：兜底 pageSize（默认 20、上限 100，与 proto 的
`page_size {gte:0, lte:100}` 双保险）；把 `page_token` 当作十进制 offset 解析；
算出 `next_page_token`（`offset+pageSize`，翻到底就返回空串）。
这个实现简单直白，但有两个已知局限：token 明文可读可篡改（可以跳页），
且数据删除后 offset 会漂移（某一页可能漏掉/重复记录）。
升级方向是 opaque cursor（base64 编码的游标），写在 `docs/next-step.md` 的 Level 2 里，
第一版刻意不做。

**与其他文件的关系。**
- `CreateUser` 被 `Register` 复用——注册就是"创建用户 + 签发 token"两步；
- `DeleteUser` 先 `FindByID` 再 `Delete`，保证"删除不存在的用户"返回 `ErrUserNotFound`
  （→ 404），而不是静默成功；
- 所有错误都是 [08](./08-user-model-repository.md) 定义的领域错误，Service 从头到尾没有出现过 HTTP 状态码。

## 7. 做一个小检查

在项目根目录执行：

```powershell
go build ./...
go vet ./...
```

两个命令都没有输出即通过。`go vet` 会顺带检查 printf 类格式串、
无法到达的代码等静态问题——这是本教程约定俗成的最低限度静态检查。

> **本阶段成果：** User 的全部业务规则集中到了 `service.go`：注册/登录/改密/分页/密码哈希。
> 它只依赖两个小接口（`Repository`、`TokenIssuer`），不认识 HTTP、JWT、数据库的任何细节——
> 这让它在 [13](./13-testing.md) 阶段可以脱离一切外部依赖被完整测试。
