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
