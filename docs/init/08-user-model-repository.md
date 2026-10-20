# 08 · User：Model 与 Repository —— 业务 Feature 的地基

> 前置：01–07 已经完成（[01](./01-create-project.md) 创建项目、[02](./02-go-module.md) go module、[03](./03-config.md) config、[04](./04-mysql.md) MySQL 连接、[05](./05-migrate.md) golang-migrate、[06](./06-protobuf-buf.md) proto + buf、[07](./07-protovalidate.md) protovalidate，
> `gen/go/` 里已有生成的 Go 代码）。从这一阶段开始，我们进入业务代码。

## 1. 这一阶段做什么

创建 `internal/user/` 包的前三个文件：

| 文件                          | 职责                                                     |
| ----------------------------- | -------------------------------------------------------- |
| `internal/user/errors.go`     | 定义 User Feature 的领域错误（哨兵错误）                 |
| `internal/user/model.go`      | 定义 `User` 的 GORM Model（数据库里长什么样）            |
| `internal/user/repository.go` | 定义 `Repository` 接口 + GORM/MySQL 实现（数据怎么读写） |

这一阶段只做**数据层**。业务规则（注册、登录、改密码）是 [09](./09-user-service.md) 的事，HTTP 层（handler/router）是 [11](./11-handler-router.md) 的事。

## 2. 为什么要做

**领域错误（errors.go）是什么？**
数据库驱动和 GORM 会返回它们自己的错误（`gorm.ErrRecordNotFound`、MySQL 错误码 1062）。
如果上层直接判断这些错误，整个项目就和 GORM、MySQL 绑死了。所以我们先定义自己的错误：
`ErrUserNotFound`、`ErrEmailAlreadyExists`……上层只认识它们，不认识 GORM。

**为什么放 `internal/user/`（Package by Feature）？**
与 User 有关的代码（model、repository、service、handler、middleware、错误）全部收敛在同一个包里，
而不是分散到全局的 `models/`、`services/`、`repositories/` 目录。
好处：加一个新 Feature（比如 `internal/todo/`）就是复制一个目录再改；
读代码时一个 Feature 的所有逻辑在一个包里，不用跨五个目录跳转。

**为什么现在创建？**
错误和模型是最底层的"词汇表"：[09](./09-user-service.md) 的 Service、[11](./11-handler-router.md) 的 Handler、[12](./12-main-manual-wiring.md) 的 main.go 都要引用它们，
所以必须最先落地。

**依赖谁 / 谁依赖它？**

```text
internal/user/errors.go      ← 不依赖任何项目内代码
internal/user/model.go       ← 依赖 GORM（04 阶段已 go get）
internal/user/repository.go  ← 依赖 gorm、go-sql-driver/mysql，以及同包的 model 和 errors
        ↑
        ├── 09 的 Service（只依赖 Repository 接口，不依赖实现）
        └── 12 的 main.go（调用 NewRepository，传入 04 阶段 database.New 返回的 *gorm.DB）
```

依赖方向是单向的：Repository 不知道 Service 和 Handler 的存在。

## 3. 手动执行什么命令

在**项目根目录**执行（PowerShell）：

```powershell
# 创建 user 包目录
New-Item -ItemType Directory -Force internal\user
```

不需要再安装任何依赖：`gorm.io/gorm`、`gorm.io/driver/mysql`、`github.com/go-sql-driver/mysql`
在 [04](./04-mysql.md)（MySQL）阶段已经写入 `go.mod`。若 `go build` 报缺依赖，执行一次 `go mod tidy` 即可。

## 4. 创建什么文件

```text
internal/user/errors.go
internal/user/model.go
internal/user/repository.go
```

## 5. 把什么代码写进去

### `internal/user/errors.go`

```go
// Package user 是本模板的第一个业务 Feature（Package by Feature）。
// 与 User 相关的 model、repository、service、handler、middleware、错误
// 全部收敛在这个包里，而不是分散到全局的 models/、services/ 目录。
package user

import "errors"

// 领域错误：由 Service / Repository 返回，Handler 统一映射成 HTTP 状态码。
// 定义成哨兵错误（sentinel errors），上层用 errors.Is 判断，不耦合具体实现。
var (
	// ErrUserNotFound 用户不存在（或被删除）。
	ErrUserNotFound = errors.New("user: user not found")

	// ErrEmailAlreadyExists 注册/创建时邮箱已被占用。
	ErrEmailAlreadyExists = errors.New("user: email already exists")

	// ErrInvalidPassword 密码错误。
	// 登录时“邮箱不存在”和“密码错误”都返回这个错误，
	// 避免向外部泄露“某邮箱是否已注册”。
	ErrInvalidPassword = errors.New("user: invalid password")

	// ErrInvalidPageToken 分页令牌不合法。
	ErrInvalidPageToken = errors.New("user: invalid page token")
)
```

### `internal/user/model.go`

```go
package user

import "time"

// User 是 User 资源的数据库持久化结构（GORM Model）。
// 它描述“数据库里怎么存”，不承载业务规则；业务规则在 service.go。
//
// 注意：字段必须与 migrations/000001_create_users.up.sql 保持一致。
// 表结构由 golang-migrate 管理，这里永远不会调用 AutoMigrate。
type User struct {
	// ID 是数据库自增主键。
	ID int64 `gorm:"primaryKey;autoIncrement"`

	// Email 登录邮箱，全表唯一。
	Email string `gorm:"size:255;not null;uniqueIndex:idx_users_email"`

	// PasswordHash 是 bcrypt 哈希后的密码。
	// 数据库里永远不存明文密码。
	PasswordHash string `gorm:"size:255;not null"`

	// DisplayName 用户昵称。
	DisplayName string `gorm:"size:100;not null;default:''"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName 显式指定表名，避免依赖 GORM 的复数推导。
func (User) TableName() string { return "users" }
```

### `internal/user/repository.go`

```go
package user

import (
	"context"
	"errors"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

// repository 是基于 GORM 的 MySQL 实现。
// 它实现的 Repository 接口由消费者（service.go，09 章）定义——
// "消费者定义接口，提供者实现接口"，本文件只关心怎么读写 MySQL。
type repository struct {
	db *gorm.DB
}

// NewRepository 创建 Repository 实现。
// 它被 main.go 显式调用，依赖关系一目了然。
// 返回类型 Repository 是 service.go（09 章）定义的接口——同一个包，直接引用。
func NewRepository(db *gorm.DB) Repository {
	return &repository{db: db}
}

// Create 插入一条用户记录。
// 唯一键冲突（重复邮箱）会被翻译成领域错误 ErrEmailAlreadyExists。
func (r *repository) Create(ctx context.Context, u *User) error {
	if err := r.db.WithContext(ctx).Create(u).Error; err != nil {
		if isDuplicateKeyError(err) {
			return ErrEmailAlreadyExists
		}
		return err
	}
	return nil
}

// FindByID 按主键查询。
// gorm.ErrRecordNotFound 统一翻译成 ErrUserNotFound，
// Service 层就不需要知道 GORM 的存在。
func (r *repository) FindByID(ctx context.Context, id int64) (*User, error) {
	var u User
	err := r.db.WithContext(ctx).First(&u, id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &u, nil
}

func (r *repository) FindByEmail(ctx context.Context, email string) (*User, error) {
	var u User
	err := r.db.WithContext(ctx).Where("email = ?", email).First(&u).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &u, nil
}

// List 分页查询，返回当前页和总条数。
// offset/limit 的换算由 Service 负责。
func (r *repository) List(ctx context.Context, offset, limit int) ([]*User, int64, error) {
	var (
		users []*User
		total int64
	)
	if err := r.db.WithContext(ctx).Model(&User{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := r.db.WithContext(ctx).
		Order("id ASC").
		Offset(offset).
		Limit(limit).
		Find(&users).Error; err != nil {
		return nil, 0, err
	}
	return users, total, nil
}

// Update 保存整个结构体（v1 只有 display_name/password_hash 会被修改）。
func (r *repository) Update(ctx context.Context, u *User) error {
	if err := r.db.WithContext(ctx).Save(u).Error; err != nil {
		return err
	}
	return nil
}

func (r *repository) Delete(ctx context.Context, id int64) error {
	if err := r.db.WithContext(ctx).Delete(&User{}, id).Error; err != nil {
		return err
	}
	return nil
}

// isDuplicateKeyError 判断是否是 MySQL 唯一键冲突（错误码 1062）。
func isDuplicateKeyError(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}
```

## 6. 解释关键代码

**哨兵错误 + `errors.Is`。**
四个错误都是包级变量（哨兵错误）。上层判断时永远写 `errors.Is(err, ErrUserNotFound)`，
而不是 `err == ErrUserNotFound`——`errors.Is` 能穿透 `fmt.Errorf("%w", err)` 的包装，
也让"判断某个错误"不再依赖错误的类型或字符串内容。Handler（[11](./11-handler-router.md) 阶段）据此统一映射状态码；
测试（[13](./13-testing.md) 阶段）也直接断言这些错误。

**Model 与迁移 SQL 的一一对应。**
对照 `migrations/000001_create_users.up.sql`：

| GORM 字段                 | SQL 列                              | 对应关系                                           |
| ------------------------- | ----------------------------------- | -------------------------------------------------- |
| `ID int64`                | `id BIGINT UNSIGNED AUTO_INCREMENT` | GORM 约定 `ID` 是主键                              |
| `Email`                   | `email VARCHAR(255) UNIQUE`         | tag 里 `uniqueIndex:idx_users_email` 对应唯一键名  |
| `PasswordHash`            | `password_hash VARCHAR(255)`        | GORM 默认蛇形转换 `PasswordHash` → `password_hash` |
| `DisplayName`             | `display_name VARCHAR(100)`         | 同上                                               |
| `CreatedAt` / `UpdatedAt` | `created_at` / `updated_at`         | GORM 约定字段名，自动填充时间                      |

注意一个细节：`gorm:"..."` 里的 tag（size、not null、uniqueIndex）主要用于 AutoMigrate 建表。
本项目**永远不调用 AutoMigrate**（表结构由 golang-migrate 的 SQL 文件管理，见 [05](./05-migrate.md)），
所以这些 tag 的实际作用是"文档化"——让人读 model 时知道列的约束。
真正决定表结构的，是 migrations 目录里的 SQL；两边必须人工保持一致。

**为什么 `Repository` 接口定义在消费者侧？**
`Repository` 接口不定义在本文件，而是定义在 `service.go`（消费者 Service 所在的文件，[09](./09-user-service.md) 章登场）。
这是 Go 的惯用法："消费者定义接口，提供者实现接口"。好处有两个：

1. `repository` 实现（本文件里的私有 struct）不需要 import 任何"接口包"——Go 接口是隐式满足的；
2. [13](./13-testing.md) 阶段的测试可以在这个包里手写一个内存版 fakeRepository 直接替换实现，Service 一行代码都不用改。

**错误翻译：把基础设施错误变成领域错误。**
Repository 是唯一认识 GORM 和 MySQL 的地方，它在边界上做两次翻译：

- `errors.Is(err, gorm.ErrRecordNotFound)` → `ErrUserNotFound`：
  查不到 = 用户不存在。翻译之后，Service 和 Handler 都不需要 import GORM。
- MySQL 错误码 `1062`（Duplicate entry，唯一键冲突）→ `ErrEmailAlreadyExists`：
  用 `errors.As` 把错误链里的 `*mysql.MySQLError` 取出来再比较 `Number`。
  为什么靠错误码而不是靠"先查一遍再插入"？因为"查询+插入"不是原子的，
  并发注册同一邮箱时先查后插仍然会撞唯一键，唯一键才是最终防线。

**依赖与被依赖。**
`NewRepository(db *gorm.DB)` 的参数来自 [04](./04-mysql.md) 阶段 `internal/platform/database` 的 `New()`，
[12](./12-main-manual-wiring.md) 阶段的 main.go 会把两者接起来。此刻 repository.go 自己不知道 config、main 的存在——
它只接收一个 `*gorm.DB`。

## 7. 做一个小检查

在项目根目录执行：

```powershell
go build ./...
```

没有任何输出即通过（本阶段还没有 main.go，`go build ./...` 会编译目前存在的所有包，
包括 internal/user）。如果有拼写或 import 错误，Go 会在这里直接指出来。

> **本阶段成果：** `internal/user/` 包有了"词汇表"——四个领域错误、一个与迁移 SQL 对齐的
> GORM Model、一个消费者侧的 Repository 接口和 GORM 实现。Service、Handler、main
> 都将建立在这三个文件之上。
