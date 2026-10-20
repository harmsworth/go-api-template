// Package app 负责“应用组装”：路由注册和 HTTP Server。
// 它只依赖 user 包的构造函数，不出现任何 DI 框架。
package app

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go-api-template/internal/user"
)

// NewRouter 创建 Gin 引擎并显式注册所有路由。
//
// 注意：proto 文件是 API Contract（描述有哪些接口），
// 这里的路由注册才是真正的 HTTP 运行时绑定，两者需要人工保持一致。
// 这个决定是有意的：第一版不引入 grpc-gateway / gin 自动生成路由。
func NewRouter(h *user.Handler, auth gin.HandlerFunc) *gin.Engine {
	r := gin.New()
	// 显式使用 Logger 和 Recovery，而不是 gin.Default()，
	// 让“中间件有哪些”一眼可见。
	r.Use(gin.Logger(), gin.Recovery())

	// 健康检查：给 docker compose / 负载均衡探活用。
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// /api 前缀：为前后端共用域名留出隔离（静态站点走 /，API 走 /api/**）。
	v1 := r.Group("/api/v1")

	// 公开接口：创建、注册、登录。
	v1.POST("/users", h.CreateUser)
	v1.POST("/auth/register", h.Register)
	v1.POST("/auth/login", h.Login)

	// 受保护接口：需要 JWT。
	protected := v1.Group("")
	protected.Use(auth)
	{
		protected.GET("/users", h.ListUsers)
		protected.GET("/users/:id", h.GetUser)
		protected.PATCH("/users/:id", h.UpdateUser)
		protected.DELETE("/users/:id", h.DeleteUser)
		protected.POST("/users/:id/password", h.ChangePassword)
	}

	return r
}
