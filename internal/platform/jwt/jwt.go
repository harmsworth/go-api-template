// Package jwt 负责签发和解析 JWT Access Token。
// 它是纯粹的 platform 能力：不知道 User、Gin、数据库的存在，
// 只接收一个 userID（int64），签发/解析出对应 Subject。
package jwt

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ErrInvalidToken 表示 token 无效或过期。
// 调用方（middleware）据此返回 401，不需要关心具体原因。
var ErrInvalidToken = errors.New("jwt: invalid token")

// Manager 持有签名密钥和 token 配置。
type Manager struct {
	secret []byte
	ttl    time.Duration
	issuer string
}

// New 创建 Manager。secret 建议足够长且不能泄露到代码里。
func New(secret string, ttl time.Duration, issuer string) *Manager {
	return &Manager{
		secret: []byte(secret),
		ttl:    ttl,
		issuer: issuer,
	}
}

// Issue 为指定 userID 签发 HS256 的 Access Token。
// userID 放在 RegisteredClaims.Subject 中，不自定义 claims 结构，
// 这样第一版保持最简单。
func (m *Manager) Issue(userID int64) (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Issuer:    m.issuer,
		Subject:   strconv.FormatInt(userID, 10),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(m.ttl)),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
}

// Parse 校验签名、算法和过期时间，返回 token 中的 userID。
func (m *Manager) Parse(tokenString string) (int64, error) {
	var claims jwt.RegisteredClaims
	token, err := jwt.ParseWithClaims(
		tokenString,
		&claims,
		func(t *jwt.Token) (any, error) { return m.secret, nil },
		// 只接受 HS256，防止算法替换攻击； WithIssuer 同时校验签发者。
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(m.issuer),
	)
	if err != nil || !token.Valid {
		return 0, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	userID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: bad subject %q", ErrInvalidToken, claims.Subject)
	}
	return userID, nil
}
