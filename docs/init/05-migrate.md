# 05-migrate.md —— 数据库迁移

## 1. 这一阶段做什么

安装 golang-migrate 命令行工具，写下第一份迁移（创建 `users` 表），执行"升→查→降→再升"的完整循环，并创建项目 `Makefile`。

## 2. 为什么要做

- **为什么需要迁移工具**：数据库结构（schema）和代码一样需要版本管理。迁移工具把每次结构变更写成带版本号的 SQL 文件，任何人、任何环境按同样顺序执行，得到完全相同的表结构——这才是"可重现"。
- **为什么选 golang-migrate**：它足够"傻"——只做一件事：把 `migrations/` 下的 `.up.sql` / `.down.sql` 按序执行或回滚，并把版本号记在数据库里的 `schema_migrations` 表中。没有魔法，出了问题一眼能看出来。
- **为什么现在装**：第 [04](./04-mysql.md) 章数据库已经就绪，这是第一次真正碰 schema；而且第 [12](./12-main-manual-wiring.md) 章服务启动前，表必须已经存在（程序自己不建表）。
- **为什么顺手创建 Makefile**：迁移命令又长（DSN 一大串）又常用，值得起个短名字。Makefile 是项目的"命令速查表"，所有常用操作都收在这里。

## 3. 手动执行什么命令

安装 migrate CLI（编译时带上 `mysql` 标签）：

```powershell
go install -tags 'mysql' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
```

> **`-tags 'mysql'` 是什么、为什么必须带**：migrate 支持 MySQL/Postgres/SQLite 等十几种数据库，但它把每种数据库的驱动做成 Go 的**构建标签（build tag）**——默认一个都不编译进去，二进制更小。不带这个标签装出来的 migrate 执行 `mysql://...` 的 DSN 会报 `Database driver: unknown driver`。装好的二进制位于 `go env GOPATH` 的 `bin` 目录（本教程写作机器上是 `D:\workData\go\bin`），请确保该目录在 `PATH` 里。

> **前置条件**：执行下面的命令前，必须先按第 4、5 节在项目根的 `migrations/` 文件夹里创建 `000001_create_users.up.sql` 和 `000001_create_users.down.sql`。`migrate` 只会执行这个目录里已存在的迁移文件——目录为空（或文件名不符合版本号规则）时命令会直接报错。可以先跳到第 4、5 节把文件建好，再回到这里执行。

执行迁移循环（当前目录：项目根 `go-api-template`）：

```powershell
# 1. 升级到最新版本
migrate -path migrations -database "mysql://root:123456@tcp(localhost:3306)/todo_x?charset=utf8mb4&parseTime=True&loc=UTC" up

# 2. 查看当前版本
migrate -path migrations -database "mysql://root:123456@tcp(localhost:3306)/todo_x?charset=utf8mb4&parseTime=True&loc=UTC" version

# 3. 回滚一个版本（体验 down 文件）
migrate -path migrations -database "mysql://root:123456@tcp(localhost:3306)/todo_x?charset=utf8mb4&parseTime=True&loc=UTC" down 1

# 4. 再升级回去
migrate -path migrations -database "mysql://root:123456@tcp(localhost:3306)/todo_x?charset=utf8mb4&parseTime=True&loc=UTC" up
```

> **参数说明**：`-path migrations` 指迁移文件目录；`-database` 是目标库的 URL——注意前缀 `mysql://` 是 migrate 自己的 URL 格式，去掉它之后的部分与 `config.yaml` 里的 DSN 一模一样（引号必须带，否则 PowerShell 会把 URL 里的 `&` 当成命令语法）。嫌长可以先创建 `Makefile`（第 5 节），然后全部用 `make migrate-up` 等短命令。

## 4. 创建什么文件

```text
go-api-template/migrations/000001_create_users.up.sql
go-api-template/migrations/000001_create_users.down.sql
go-api-template/Makefile
```

## 5. 把什么代码写进去

文件：`go-api-template/migrations/000001_create_users.up.sql`

```sql
-- 创建 users 表：第一版唯一的数据表。
-- 字段必须与 internal/user/model.go 中的 GORM Model 保持一致。
CREATE TABLE users (
    id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    email         VARCHAR(255)    NOT NULL,
    password_hash VARCHAR(255)    NOT NULL,
    display_name  VARCHAR(100)    NOT NULL DEFAULT '',
    created_at    TIMESTAMP(6)    NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at    TIMESTAMP(6)    NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY idx_users_email (email)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_unicode_ci;
```

文件：`go-api-template/migrations/000001_create_users.down.sql`

```sql
-- 回滚 000001：删除 users 表。
DROP TABLE IF EXISTS users;
```

文件：`go-api-template/Makefile`

> **注意**：Makefile 里每条命令行开头必须是 **Tab 字符**（不能用空格），这是 Make 的硬性语法。从本教程复制时请确认编辑器保留了 Tab。

```makefile
# 本地开发常用命令。Windows 用户如果没装 make，可以直接复制对应命令到终端执行。

# golang-migrate 使用的 DSN（注意前缀是 mysql://）
MIGRATE_DSN ?= mysql://root:123456@tcp(localhost:3306)/todo_x?charset=utf8mb4&parseTime=True&loc=UTC

.PHONY: buf-lint buf-generate test lint build run migrate-up migrate-down

# 校验 proto 文件
buf-lint:
	buf lint

# 生成 proto 对应的 Go 代码（需要先安装 protoc-gen-go，见 docs/init/06-protobuf-buf.md）
buf-generate:
	buf generate

# 运行所有测试（集成测试需要设置 MYSQL_TEST_DSN，见 docs/init/13-testing.md）
test:
	go test ./...

# 静态检查
lint:
	go vet ./...

# 构建可执行文件到 bin/
build:
	go build -o bin/server.exe ./cmd/server

# 本地启动（先确保 MySQL 已启动且迁移已执行）
run:
	go run ./cmd/server

# 执行迁移（需要先安装 migrate CLI，见 docs/init/05-migrate.md）
migrate-up:
	migrate -path migrations -database "$(MIGRATE_DSN)" up

# 回滚最近一次迁移
migrate-down:
	migrate -path migrations -database "$(MIGRATE_DSN)" down 1
```

## 6. 解释关键代码

### 迁移文件的命名与成对规则

- 文件名格式是 `{版本号}_{描述}.up.sql` / `{版本号}_{描述}.down.sql`。版本号（`000001`）决定执行顺序，migrate 按它排序、按它在 `schema_migrations` 表里记账。
- `up` 是"前进"，`down` 是"后悔药"。**每个 up 必须配一个能抵消它的 down**——只写 up 不写 down 的迁移，等于宣告"这个变更多少都不能撤销"。
- 表名 `users`、字段 `email`/`password_hash`/`display_name`/`created_at`/`updated_at` 与后续章节将创建的 GORM Model（`internal/user/model.go`，见路线图第 [08](./08-user-model-repository.md) 章）**一一对应**：GORM 的命名策略把 Go 的驼峰字段映射成 snake_case 列名（`PasswordHash` → `password_hash`），`TableName()` 方法显式指定表名 `users`；唯一索引名 `idx_users_email` 也与 model 上 `uniqueIndex:idx_users_email` 的声明一致。此刻 model 文件还不存在，先记住这条契约：**改其中一边，必须同时改另一边**。
- 细节上为什么 `created_at`/`updated_at` 用 `TIMESTAMP(6)` + `DEFAULT CURRENT_TIMESTAMP(6)`：微秒精度 + 数据库自动维护时间戳，与 GORM 自动填充的时间一致；`ON UPDATE CURRENT_TIMESTAMP(6)` 让 `updated_at` 在行被 UPDATE 时自动刷新。

### Makefile 逐个 target 解释

- **`MIGRATE_DSN ?= ...`**：变量定义，`?=` 表示"没在外面指定时才用这个默认值"。注意它和 `config.yaml` 里 DSN 的唯一区别是前缀 `mysql://`——migrate 用 URL 格式，go-sql-driver 用纯 DSN 格式。
- **`.PHONY: ...`**：声明这些名字是"命令"而不是文件名，防止目录里恰好存在同名文件时 Make 拒绝执行。
- **`buf-lint` / `buf-generate`**：proto 校验与生成，第 [06](./06-protobuf-buf.md)/[07](./07-protovalidate.md) 章登场。
- **`test` / `lint`**：`go test ./...` 与 `go vet ./...`，第 [13](./13-testing.md) 章详细展开。
- **`build` / `run`**：构建到 `bin/server.exe`（所以 `.gitignore` 忽略 `bin/`）；`run` 直接 `go run`，是本地开发最常用的命令。
- **`migrate-up` / `migrate-down`**：把第 3 节那两条长命令变成两个词。`down 1` 只回滚一个版本——回滚永远小步走。
- **没装 make？** 完全没关系：打开 Makefile，把对应命令里的 `$(MIGRATE_DSN)` 替换成变量值，直接复制到 PowerShell 执行即可（文件第一行注释写的就是这件事）。

### 为什么禁止启动时自动迁移、为什么不用 GORM AutoMigrate

- **启动即变更 = 部署即冒险**：如果程序启动时自动改表，那么"发布一个新版本"和"修改生产数据库结构"就混在同一个瞬间发生，没有任何人事先 review 过 DDL，也没有回滚预案。迁移文件把这两件事拆开：代码归代码，schema 变更必须**显式**执行 `migrate up`。
- **多实例会打架**：生产环境起三个副本，三个进程同时启动、同时判断"表要不要改"，DDL 竞争轻则报错重则锁表。迁移应该由部署流水线**执行一次**，而不是每个实例各来一次。
- **AutoMigrate 能力不足**：GORM 的 `AutoMigrate` 只会加列加索引，**不会删列、不会改类型、不写 down**；表结构会随着时间悄悄漂移，最后没人说得清数据库到底长什么样。而迁移文件是代码审查的对象——你能在 PR 里看到 `ADD COLUMN` 和对应的 `DROP COLUMN`。
- **团队协作的一致性**：新同事 clone 项目后执行 `migrate up`，得到与你完全一致的库。这正是 `go.sum` 对依赖做的事，只不过对象换成了 schema。

本项目把这个约束写进了两处代码注释（`mysql.go` 的 `New` 和 `main.go`），防止后人图省事加回 `AutoMigrate`。

### 与其他文件的关系

- 依赖：第 [04](./04-mysql.md) 章的 MySQL 容器和 `todo_x` 库是前提。
- 被依赖：第 [08](./08-user-model-repository.md) 章 model 与这里的 SQL 逐字段对应；第 [12](./12-main-manual-wiring.md) 章服务启动的前提是“迁移已执行”；第 [13](./13-testing.md) 章集成测试用同一条 DSN 的思路建测试库。

## 7. 做一个小检查

```powershell
migrate -path migrations -database "mysql://root:123456@tcp(localhost:3306)/todo_x?charset=utf8mb4&parseTime=True&loc=UTC" version
docker exec -it local-mysql mysql -uroot -p123456 -e "SHOW TABLES FROM todo_x; DESCRIBE todo_x.users;"
```

执行成功后你应该看到：

- `version` 输出 `1`（紧跟第 3 节第 4 步"再升级回去"之后）；如果装了 make，用 `make migrate-up` / `make migrate-down` 重跑一遍，效果相同；
- `SHOW TABLES` 里有两张表：`schema_migrations`（migrate 的记账表）和 `users`；
- `DESCRIBE users` 里能看到 `email` 为 `varchar(255)` 且 `UNI`（唯一）、`display_name` 为 `varchar(100)`。

> **本阶段成果：** `users` 表由迁移文件创建并记录在案（版本 1），down 文件验证过可回滚，Makefile 收齐了项目所有常用命令。数据库这一侧到此闭环。下一章转向 API 契约：proto + buf。
