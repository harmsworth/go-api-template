package app

import (
	"context"
	"net/http"
	"time"
)

// Server 包装 *http.Server，让 main.go 不直接接触标准库细节。
type Server struct {
	http *http.Server
}

// NewServer 创建 HTTP Server。
func NewServer(addr string, handler http.Handler) *Server {
	return &Server{
		http: &http.Server{
			Addr:    addr,
			Handler: handler,
			// Header 超时防止 slowloris 类攻击，这个习惯值得保留。
			ReadHeaderTimeout: 10 * time.Second,
		},
	}
}

// Run 阻塞式启动监听。
// 返回的 http.ErrServerClosed 是正常关闭信号，由 main.go 过滤。
func (s *Server) Run() error {
	return s.http.ListenAndServe()
}

// Shutdown 优雅停机：停止接收新请求，等待存量请求完成（最多 ctx 时限）。
func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}
