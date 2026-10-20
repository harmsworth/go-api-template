package user

import (
	"context"
	"errors"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

// repository 是基于 GORM 的 MySQL 实现。
// 它实现的 Repository 接口由消费者（service.go）定义——
// "消费者定义接口，提供者实现接口"，本文件只关心怎么读写 MySQL。
type repository struct {
	db *gorm.DB
}

// NewRepository 创建 Repository 实现。
// 它被 main.go 显式调用，依赖关系一目了然。
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
	if err := r.db.WithContext(ctx).First(&u, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &u, nil
}

func (r *repository) FindByEmail(ctx context.Context, email string) (*User, error) {
	var u User
	if err := r.db.WithContext(ctx).Where("email = ?", email).First(&u).Error; err != nil {
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
