package user

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"go-api-template/internal/platform/jwt"
)

// ginContextKeyUserID 是 userID 在 Gin Context 中的键名。
const ginContextKeyUserID = "userID"

// JWTAuth 是 User Feature 的鉴权 middleware：
//
//	Authorization: Bearer xxx
//	    ↓ 解析 token
//	    ↓ 得到 userID 写入 Gin Context
//	    ↓ c.Next() 进入 Handler
//
// 只做“认证”，不做“授权”（谁能访问哪些资源属于业务规则）。
//
// 注意：认证失败时必须调用 c.Abort()，不能只 return。
// gin 的 c.Next() 在 engine 层遍历整条 handler 链，中间件单纯 return
// 只是结束自己这一环，后面的 Handler 照样会被执行——实测会写出
// “401 响应体 + 500 响应体”拼接的畸形报文，且 Handler 可能继续落库。
func JWTAuth(m *jwt.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		authz := c.GetHeader("Authorization")
		token, ok := strings.CutPrefix(authz, "Bearer ")
		if !ok || token == "" {
			writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "missing bearer token")
			c.Abort()
			return
		}

		userID, err := m.Parse(token)
		if err != nil {
			writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "invalid token")
			c.Abort()
			return
		}

		c.Set(ginContextKeyUserID, userID)
		c.Next()
	}
}

// UserIDFrom 从 Gin Context 取出 JWTAuth 写入的 userID。
// 取不到时返回 ok=false（例如忘记挂 middleware）。
func UserIDFrom(c *gin.Context) (int64, bool) {
	v, ok := c.Get(ginContextKeyUserID)
	if !ok {
		return 0, false
	}
	id, ok := v.(int64)
	return id, ok
}
