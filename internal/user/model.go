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
