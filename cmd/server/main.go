// main.go 是整个项目的“组装说明书”。
// 所有依赖关系都在这里手动创建、手动传入，不使用 Wire 等 DI 框架：
//
//	读取配置
//	 ↓
//	automaxprocs（让 GOMAXPROCS 匹配容器配额）
//	 ↓
//	连接 MySQL
//	 ↓
//	创建 JWT Manager
//	 ↓
//	创建 User Repository（依赖 db）
//	 ↓
//	创建 User Service（依赖 repo + jwt）
//	 ↓
//	创建 User Handler（依赖 service）
//	 ↓
//	创建 Router（依赖 handler + auth middleware）
//	 ↓
//	启动 HTTP Server
//
// 注意：这里不会执行数据库 migration。
// schema 变更必须显式执行 migrate（make migrate-up），见 docs/init/05-migrate.md。
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/automaxprocs/maxprocs"

	"go-api-template/internal/app"
	"go-api-template/internal/platform/config"
	"go-api-template/internal/platform/database"
	"go-api-template/internal/platform/jwt"
	"go-api-template/internal/user"
)

func main() {
	// -config 支持从任意路径读配置，默认项目根目录下的 configs/config.yaml。
	configPath := flag.String("config", "configs/config.yaml", "配置文件路径")
	flag.Parse()

	// automaxprocs 解决“容器里 GOMAXPROCS 看的是宿主机 CPU 核数”的问题。
	// 它会把 GOMAXPROCS 调整成 cgroup 的 CPU 配额，避免过多调度开销。
	// 只需要这一行，不需要围绕它做任何封装。
	if _, err := maxprocs.Set(maxprocs.Logger(slog.Info)); err != nil {
		slog.Warn("automaxprocs 调整失败（不影响启动）", "err", err)
	}

	// 1. 读取配置。
	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("读取配置失败", "err", err)
		os.Exit(1)
	}

	// 2. 连接数据库（只连接，不迁移）。
	db, err := database.New(cfg)
	if err != nil {
		slog.Error("连接数据库失败", "err", err)
		os.Exit(1)
	}

	// 3. 创建 JWT Manager。
	jwtManager := jwt.New(cfg.JWT.Secret, time.Duration(cfg.JWT.TTLHours)*time.Hour, cfg.JWT.Issuer)

	// 4. 组装 User Feature：repo → service → handler。
	repo := user.NewRepository(db)
	service := user.NewService(repo, jwtManager)
	handler, err := user.NewHandler(service)
	if err != nil {
		slog.Error("初始化 handler 失败", "err", err)
		os.Exit(1)
	}

	// 5. 创建路由与 Server。
	auth := user.JWTAuth(jwtManager)
	router := app.NewRouter(handler, auth)
	server := app.NewServer(cfg.Server.Addr, router)

	// 6. 启动并支持 Ctrl+C 优雅停机。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("HTTP server 启动", "addr", cfg.Server.Addr)
		if err := server.Run(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP server 异常退出", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("收到退出信号，开始优雅停机...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("优雅停机失败", "err", err)
		os.Exit(1)
	}
	slog.Info("服务已退出")
}
