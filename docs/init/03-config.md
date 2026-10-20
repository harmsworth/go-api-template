# 03-config.md —— 配置加载

## 1. 这一阶段做什么

创建项目第一个 Go 包：`internal/platform/config`（配置结构体 + YAML 加载），以及它对应的配置文件 `configs/config.yaml`。

## 2. 为什么要做

- **配置集中**：监听地址、数据库 DSN、JWT 密钥——所有"因环境而异"的信息必须只有**一个**入口。如果每个模块自己读环境变量、自己拼 DSN，环境一换（本地/测试/生产）就要满项目找漏网之鱼。本项目把读取配置的权力收归 `config` 包，其他代码只接收 `*config.Config`，不允许自己读配置文件或环境变量。
- **为什么放 `internal/platform/config`**：`platform/` 收纳与具体业务无关的"平台能力"。配置加载是第一个能力，也是后面每一章的依赖——第 [04](./04-mysql.md) 章的数据库连接要用 `cfg.Database.DSN`，第 [10](./10-jwt.md) 章的 JWT 要用 `cfg.JWT.Secret`，第 [12](./12-main-manual-wiring.md) 章 `main.go` 第一件事就是 `config.Load(...)`。所以它必须在连接数据库之前就绪。
- **为什么现在创建**：它是第一个没有外部前置条件的 Go 包（module 已就绪、yaml 依赖已安装），也是让第 [04](./04-mysql.md) 章能动手的最短路径。
- **为什么用 YAML 而不是环境变量或 `.env`**：本地开发时 YAML 能一眼看全所有配置、能写注释、能进版本库（敏感值留给环境变量注入，见文件头注释）。这是学习型模板的简单取舍，生产做法见 `docs/next-step.md`。

## 3. 手动执行什么命令

本阶段没有新命令，只需确认工作目录：

```powershell
cd go-api-template
```

> Windows PowerShell 直接创建 `internal` 这类多级目录：`mkdir internal\platform\config` 和 `mkdir configs`（当然也可以用编辑器直接新建文件，目录会自动创建）。

## 4. 创建什么文件

```text
go-api-template/internal/platform/config/config.go
go-api-template/configs/config.yaml
```

## 5. 把什么代码写进去

文件：`go-api-template/internal/platform/config/config.go`

```go
// Package config 负责读取启动配置。
// 所有环境相关的信息（监听地址、DSN、JWT 密钥）都集中在这里，
// 业务代码不允许自己读配置文件或环境变量。
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config 是配置文件的顶层结构，对应 configs/config.yaml。
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	JWT      JWTConfig      `yaml:"jwt"`
}

type ServerConfig struct {
	// Addr 是 HTTP 监听地址，例如 ":8080"。
	Addr string `yaml:"addr"`
}

type DatabaseConfig struct {
	// DSN 是 go-sql-driver/mysql 的连接字符串。
	DSN string `yaml:"dsn"`
}

type JWTConfig struct {
	Secret   string `yaml:"secret"`
	TTLHours int    `yaml:"ttl_hours"`
	Issuer   string `yaml:"issuer"`
}

// Load 从指定路径读取 YAML 配置并填充默认值。
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: 读取配置文件失败: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: 解析配置文件失败: %w", err)
	}
	setDefaults(&cfg)
	return &cfg, nil
}

// setDefaults 给必填但可能缺失的配置一个可用的默认值。
// 显式写在这里，而不是散落在使用方代码里。
func setDefaults(cfg *Config) {
	if cfg.Server.Addr == "" {
		cfg.Server.Addr = ":8080"
	}
	if cfg.JWT.TTLHours <= 0 {
		cfg.JWT.TTLHours = 24
	}
	if cfg.JWT.Issuer == "" {
		cfg.JWT.Issuer = "go-api-template"
	}
}
```

文件：`go-api-template/configs/config.yaml`

```yaml
# 本地开发配置。
# 生产环境的敏感信息（如 DSN 密码、JWT secret）不应放在配置文件里，
# 见 docs/next-step.md 的升级方向。
server:
  addr: ":8080"

database:
  dsn: "root:123456@tcp(localhost:3306)/todo_x?charset=utf8mb4&parseTime=True&loc=UTC"

jwt:
  secret: "dev-only-secret-change-me"
  ttl_hours: 24
  issuer: "go-api-template"
```

## 6. 解释关键代码

### 结构体与 YAML 的映射

- `Config` 三个字段对应 YAML 的三个顶层键，靠的是结构体 tag（`` `yaml:"server"` ``）：`yaml.Unmarshal` 按 tag 把 YAML 键填进字段。tag 名和 YAML 键**必须逐字符一致**，这是结构体和配置文件之间唯一的纽带。
- `ServerConfig.Addr`、`DatabaseConfig.DSN`、`JWTConfig` 三层各自只描述自己那一段——将来加 Redis、加日志配置，就加新的子结构体，`Load` 的骨架不用动。
- DSN（Data Source Name）是 `go-sql-driver/mysql` 定义的连接字符串语法：`用户名:密码@tcp(主机:端口)/库名?参数`。`parseTime=True` 让 DATE/TIMESTAMP 列扫描进 `time.Time`（GORM 需要），`loc=UTC` 约定时间解释时区，`charset=utf8mb4` 与第 [04](./04-mysql.md) 章建库时的字符集一致。这一串现在看不懂没关系，第 [04](./04-mysql.md) 章还会遇到它。

### setDefaults 的意义

`setDefaults` 在 `Load` 里统一执行，它保证的是：**配置文件里漏写一个键，程序仍能以合理默认值启动，而不是拿着零值往下跑**。比如 `ttl_hours` 少写了，JWT 过期时间就会是 0（token 立刻过期）；`addr` 少写了就监听 `""` 直接报错。把这些兜底集中在一处，而不是让第 [10](./10-jwt.md) 章、第 [12](./12-main-manual-wiring.md) 章的代码各自 `if x == 0`——默认值本身也是配置语义的一部分，应该跟配置结构住在一起。

### 错误处理的小习惯

`fmt.Errorf("config: ...: %w", err)`：前缀标明错误来源的包，`%w` 把底层错误包进去，调用方将来可以用 `errors.Is/As` 拆开看。全项目都遵循这个风格。

### 与其他文件的关系

- 依赖：只依赖标准库和第 [02](./02-go-module.md) 章安装的 `gopkg.in/yaml.v3`，不依赖项目里任何其他包——所以它能成为第一个编译通过的包。
- 被依赖：第 [04](./04-mysql.md) 章 `database.New(cfg)`、第 [10](./10-jwt.md) 章 `jwt.New(...)`、第 [12](./12-main-manual-wiring.md) 章 `main.go` 全都以 `*config.Config` 为输入。它是依赖树的根。

## 7. 做一个小检查

```powershell
go build ./...
go vet ./...
```

执行成功后你应该看到：**两条命令都没有任何输出**（Go 的习惯：没有消息就是好消息），说明 `config` 包编译通过、静态检查无告警。此刻项目里只有一个 Go 包，`go build ./...` 就是专门编译它。

> **本阶段成果：** 第一个 Go 包就位——配置集中在 `config` 包，YAML 文件提供本地值，`setDefaults` 兜底漏写项。下一章用其中的 DSN 把 MySQL 跑起来。
