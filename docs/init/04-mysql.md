# 04-mysql.md —— MySQL 环境与 GORM 连接

## 1. 这一阶段做什么

用 docker-compose 启动本地 MySQL、手动创建 `todo_x` 库，并写好项目的数据库连接包 `internal/platform/database/mysql.go`。

## 2. 为什么要做

- **为什么先搞环境再写代码**：第 [05](./05-migrate.md) 章要执行的数据库迁移、第 [08](./08-user-model-repository.md) 章的 Repository、第 [13](./13-testing.md) 章的集成测试，全都以"MySQL 正在运行且 `todo_x` 库存在"为前提。环境这一步错了，后面全是连环报错。
- **为什么用 docker-compose 而不是本机安装 MySQL**：一条命令起、一条命令删（`docker compose down`），版本固定为 `mysql:8.0`，不污染本机。`docker-compose.yml` 放在项目根目录，因为它描述的是"这个项目需要的本地基础设施"，跟着仓库走，谁 clone 谁能用。
- **为什么用 GORM**：学习型模板选 ORM 而不是裸 `database/sql`，是为了把注意力放在分层结构上，而不是手写扫描行到结构体的样板代码。GORM 是 Go 生态最常用的 ORM，语法即文档。
- **为什么这一步只"连接"、不建表**：建表是 schema 变更，属于第 [05](./05-migrate.md) 章 golang-migrate 的职责。连接和迁移分开，是本项目最重要的原则之一（第 [05](./05-migrate.md) 章细讲为什么）。

## 3. 手动执行什么命令

进入项目根目录（所有命令都在这里执行）：

```powershell
cd go-api-template
```

启动 MySQL 容器：

```powershell
docker compose up -d
```

> 用途：`docker compose` 读取项目根目录的 `docker-compose.yml`，在后台（`-d`）创建并启动 MySQL 8.0 容器。首次执行会拉取镜像，需要网络。

**手动创建数据库**（golang-migrate 和程序本身都不会自动建库）。用任意 MySQL 客户端执行都可以；这里用 `docker exec` 直接在容器里执行：

```powershell
docker exec -it go-api-template-mysql mysql -uroot -p123456 -e "CREATE DATABASE todo_x CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;"
```

> 如果容器是首次启动，compose 里的 `MYSQL_DATABASE: todo_x` 环境变量已经自动建过同名库，这条命令会报"数据库已存在"——跳过即可；但只要你是复用已初始化过的数据卷或本机 MySQL，库就不一定存在，手动执行一次最稳妥。

## 4. 创建什么文件

```text
go-api-template/docker-compose.yml
go-api-template/internal/platform/database/mysql.go
```

## 5. 把什么代码写进去

文件：`go-api-template/docker-compose.yml`

```yaml
# 本地开发环境：只包含 MySQL，其他中间件以后按需再加（见 docs/next-step.md）。
# 启动: docker compose up -d
# 停止: docker compose down
services:
  mysql:
    image: mysql:8.0
    container_name: go-api-template-mysql
    environment:
      MYSQL_ROOT_PASSWORD: "123456"
      MYSQL_DATABASE: todo_x
    ports:
      - "3306:3306"
    command:
      - --character-set-server=utf8mb4
      - --collation-server=utf8mb4_unicode_ci
    volumes:
      - mysql-data:/var/lib/mysql

volumes:
  mysql-data:
```

文件：`go-api-template/internal/platform/database/mysql.go`

```go
// Package database 负责创建 MySQL 连接（*gorm.DB）。
// 连接参数来自 config，业务代码只接收 *gorm.DB，不关心 DSN 细节。
package database

import (
	"fmt"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"go-api-template/internal/platform/config"
)

// New 打开 MySQL 连接并设置连接池参数。
// 注意：这里只“连接”，不执行任何 migration（见 docs/architecture.md）。
func New(cfg *config.Config) (*gorm.DB, error) {
	db, err := gorm.Open(mysql.Open(cfg.Database.DSN), &gorm.Config{
		// 慢查询和错误会以 Warn 级别打到标准输出，便于学习时观察 SQL。
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("database: 连接 MySQL 失败: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("database: 获取底层 sql.DB 失败: %w", err)
	}
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(time.Hour)

	return db, nil
}
```

## 6. 解释关键代码

### docker-compose.yml 的几个关键点

- **`container_name: go-api-template-mysql`**：给容器固定名字，`docker exec` 时好找。
- **`MYSQL_ROOT_PASSWORD` / `MYSQL_DATABASE`**：MySQL 官方镜像约定的环境变量——前者设置 root 密码，后者在**数据卷首次初始化时**自动建库。
- **`command` 的两个参数**：把服务器级默认字符集/排序规则设为 `utf8mb4` / `utf8mb4_unicode_ci`，与第 [03](./03-config.md) 章 DSN 里的 `charset=utf8mb4` 对应，避免中文和 emoji 存不进去。
- **`volumes: mysql-data`**：命名卷把数据存在 Docker 管理的卷里，容器删了数据还在；想彻底重来就 `docker compose down -v`（`-v` 会删卷，数据清空）。
- **密码 `123456` 只属于本地开发**：文件头注释明确了这一点，别学进生产。

### mysql.go 的关键点

- **`gorm.Open(mysql.Open(cfg.Database.DSN), ...)`**：`mysql.Open` 只是包装 DSN，`gorm.Open` 才真正建连接（内部默认会 ping 一次），失败立刻返回错误——所以这个函数返回成功，就意味着 MySQL 是通的。
- **`logger.Default.LogMode(logger.Warn)`**：平时安静，出现慢查询或 SQL 错误才打印，学习时能直接看到 GORM 生成的 SQL。
- **连接池三件套**（通过 `db.DB()` 拿到底层 `*sql.DB` 再设置）：
  - `SetMaxOpenConns(10)`：最多同时开 10 条连接，防止把 MySQL 打满；
  - `SetMaxIdleConns(5)`：空闲时保留 5 条复用，避免每次请求都重新握手；
  - `SetConnMaxLifetime(time.Hour)`：一条连接最长用 1 小时就换新，让 MySQL 端断连（如重启、`wait_timeout`）能被平滑吸收。
  - 这些数字对学习项目足够，真正调优要结合负载与 MySQL 的 `max_connections`。
- **为什么这里只连接不迁移**：见第 [05](./05-migrate.md) 章"为什么禁止启动时自动迁移"。一句话版本——schema 变更必须是显式、可审查、可回滚的动作，不能藏在"程序启动"里。

### 与其他文件的关系

- 依赖：`internal/platform/config`（第 [03](./03-config.md) 章）提供 DSN；`gorm.io/driver/mysql`、`gorm.io/gorm`（第 [02](./02-go-module.md) 章安装）提供能力。
- 被依赖：第 [12](./12-main-manual-wiring.md) 章 `main.go` 调 `database.New(cfg)` 拿到 `*gorm.DB`，传给第 [08](./08-user-model-repository.md) 章的 Repository；第 [13](./13-testing.md) 章集成测试也用同样方式建连接。业务代码永远只见 `*gorm.DB`，不见 DSN。

## 7. 做一个小检查

```powershell
docker compose ps
docker exec -it go-api-template-mysql mysql -uroot -p123456 -e "SHOW DATABASES;"
go build ./...
```

执行成功后你应该看到：

- `docker compose ps` 里 `go-api-template-mysql` 状态为 `running`（或 `Up`），端口映射 `0.0.0.0:3306->3306`；
- `SHOW DATABASES` 的输出里有 `todo_x`（还有 `information_schema` 等系统库）；
- `go build ./...` 无输出——`config`、`database` 两个包都编译通过。

> **本阶段成果：** MySQL 8.0 在 Docker 里运行、`todo_x` 库（utf8mb4）已就绪，项目里有了可复用的 GORM 连接工厂。表还没有——下一章用 golang-migrate 把第一张表建出来。
