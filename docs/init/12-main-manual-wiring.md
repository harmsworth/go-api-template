# 12 · main.go —— 手动依赖注入，把一切组装起来

## 1. 这一阶段做什么

创建 `cmd/server/main.go`——整个项目的"组装说明书"。
读取配置、连接 MySQL、构造 JWT Manager，再按依赖顺序手动构造
repo → service → handler → router → server，最后启动并支持 Ctrl+C 优雅停机。

## 2. 为什么要做

**main.go 是什么？**
前面所有阶段创建的都是"零件"（构造函数），main.go 是唯一把零件**连起来**的地方。
依赖注入在这里手动完成：`db → repo → service → handler → router → server`，
一条直线，没有任何 DI 框架（没有 Wire、没有容器、没有反射魔法）。

**为什么在 `cmd/server/`？**
Go 的惯例：`cmd/<二进制名>/main.go` 是程序入口，业务逻辑全部在 `internal/`。
以后如果有第二个入口（比如 `cmd/migrate-runner/`），可以复用同样的零件、
用不同的方式组装——组装策略和业务逻辑是分离的。

**为什么现在创建？**
所有零件已就位。这是"从 0 到能跑"的最后一块拼图，也最适合放在最后：
写它的时候，你会自然复述一遍整个项目的依赖关系。

**依赖谁？**

```text
cmd/server/main.go
   依赖（全部是构造函数，没有全局变量）：
   ├── platform/config   → cfg（02 阶段）
   ├── platform/database → *gorm.DB（04 阶段）
   ├── platform/jwt      → *jwt.Manager（10 阶段）
   ├── user.NewRepository / NewService / NewHandler / JWTAuth（08/09/11 阶段）
   └── app.NewRouter / NewServer（11 阶段）
```

没有任何包反过来依赖 main——它是依赖图的顶端，也是唯一知道"一切"的地方。

## 3. 手动执行什么命令

无需新依赖（automaxprocs 在 [04](./04-mysql.md) 阶段已进入 go.mod；若缺失执行 `go mod tidy`）。

## 4. 创建什么文件

```text
cmd/server/main.go
```

## 5. 把什么代码写进去

### `cmd/server/main.go`

```go
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
```

## 6. 解释关键代码

**初始化顺序 = 依赖方向。**
逐块看 main() 的顺序，它就是 [11](./11-handler-router.md) 阶段依赖图的拓扑排序：

1. `-config` flag：配置文件路径可以从命令行覆盖，默认 `configs/config.yaml`；
2. **automaxprocs**：容器里 cgroup 限制了 CPU 配额（比如只给 1 核），
   但 Go 运行时默认按宿主机核数设置 GOMAXPROCS——32 核宿主跑 1 核配额的容器，
   Go 会创建大量 P 和调度开销。`maxprocs.Set` 把 GOMAXPROCS 调整成 cgroup 配额。
   它的价值就在这一行：导入即生效、失败只警告不影响启动，不需要任何封装。
   （即使较新的 Go 对容器配额的默认处理在不断改进，这行显式调用也无害且可预期。）
3. **配置 → 数据库 → JWT**：三个 platform 能力按依赖顺序就位，全部来自配置；
4. **repo → service → handler**：User Feature 的组装线。注意第 4 步的巧妙之处——
   `jwtManager` 同时被传给了 `user.NewService`（满足 [09](./09-user-service.md) 的 `TokenIssuer` 接口，
   隐式接口，没有适配代码）和 `user.JWTAuth`（直接使用 `Parse`）；
5. **router → server**：handler 和 auth 中间件进路由，路由进 Server；
6. **优雅停机**：`signal.NotifyContext` 监听 Ctrl+C（SIGINT）和 SIGTERM；
   HTTP 服务跑在 goroutine 里，主 goroutine 等信号；收到信号后给 10 秒时限调用
   `Shutdown`（停止接新请求、等存量请求完成）。`http.ErrServerClosed` 是主动停机的
   正常返回值，必须过滤掉，否则会把"正常关闭"当成"异常退出"。

**手动依赖注入的价值。**
看这一个文件就能讲清楚"项目如何组装"：哪些对象存在、谁依赖谁、配置流向哪里。
没有隐藏的 bootstrap、没有容器里自动注册的神秘单例；每个构造函数都是普通 Go 函数，
错误在构造时同步返回，启动失败的原因一眼可见。
本项目依赖只有一条链，Wire 那类工具解决的问题（依赖图复杂到人工维护易错）还没出现
——等出现了再引入（`docs/next-step.md` Level 5）。

**强调：启动不执行迁移。**
main.go 里连接数据库的注释写着"只连接，不迁移"。schema 变更是需要评审、能回滚的
显式操作（`make migrate-up`，见 [05](./05-migrate.md)），永远不要混进服务启动流程——
多实例同时启动时"启动即迁移"是竞态源头。本项目启动 = 连数据库 + 起 HTTP，仅此而已。

## 7. 做一个小检查

前提：MySQL 已启动且库已建（[04](./04-mysql.md) 阶段 `docker compose up -d` 会自动创建 `todo_x` 库），
迁移已执行（[05](./05-migrate.md) 阶段 `make migrate-up`，没装 make 就执行 Makefile 里那条原始命令）：

```powershell
migrate -path migrations -database "mysql://root:123456@tcp(localhost:3306)/todo_x?charset=utf8mb4&parseTime=True&loc=UTC" up
```

然后启动服务：

```powershell
go run ./cmd/server
```

看到这行日志即启动成功：

```text
2026/09/24 12:00:00 INFO HTTP server 启动 addr=:8080
```

另开一个终端验证健康检查：

```powershell
curl.exe -s http://localhost:8080/healthz
```

应返回：

```json
{"status":"ok"}
```

回到服务终端按 Ctrl+C，应看到"收到退出信号，开始优雅停机..."与"服务已退出"。

> **本阶段成果：** 服务能启动了。main.go 用约 60 行把配置、数据库、JWT、User Feature、
> 路由、Server 按依赖顺序手动组装——读完它，你就读完了整个项目的架构。
> 接下来 [13](./13-testing.md) 阶段给这条链补上测试，[14](./14-run-and-check.md) 阶段做端到端冒烟。
