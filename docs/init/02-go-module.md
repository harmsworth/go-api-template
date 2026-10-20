# 02-go-module.md —— 初始化 module 与依赖

## 1. 这一阶段做什么

在项目根目录执行 `go mod init` 创建 `go.mod`，然后用 `go get` 把项目全部 10 个直接依赖一次性装齐，生成 `go.sum`。

## 2. 为什么要做

- **`go.mod` 是 Go module 的身份证**：声明两件事——模块名（`module go-api-template`）和 Go 语言版本，外加依赖清单。有了它，`go build` / `go test` 才知道代码属于哪个模块、去哪找第三方包。
- **module 名决定 `internal/` 的可见性规则**：`internal/` 是 Go 工具链在**编译期**强制的私有目录——只有与被导入包处在同一个 module 里的代码才能 import 它，外部项目 import 会直接报错 `use of internal package ... not allowed`。本项目的业务代码全部放在 `internal/` 下，靠的就是这条规则。这也解释了为什么 module 名用简单的 `go-api-template` 就够了：它不打算被别人 import，等真正要开源再改成 `github.com/xxx/go-api-template` 也不迟（那是一次全局替换）。
- **为什么现在就 `go get`，而不是写到哪装到哪**：`go get` 只是往 `go.mod` / `go.sum` 里登记依赖并下载到本地模块缓存，**不需要项目里有任何代码**。提前把 10 个依赖钉住（并且钉住版本号），后面每一章写代码时编译器随取随用，还能保证你重建出的 `go.mod` 与原项目一致，避免"最新版行为变了"的干扰。
- **`go.sum` 是什么**：依赖的哈希校验和清单。每次下载模块，Go 都会核对哈希，防止依赖在源头被篡改（供应链攻击）或两次下载内容不一致。它和 `go.mod` 一样必须提交进版本库；**永远不要手工编辑它**，坏了就删掉重跑 `go mod tidy`。

## 3. 手动执行什么命令

确认当前目录是项目根目录（`go` 命令必须在项目根目录运行）：

```powershell
cd go-api-template
```

初始化 module：

```powershell
go mod init go-api-template
```

逐个安装依赖（版本号与原项目 `go.mod` 一致，`@latest` 也可以，但拉到的可能是更新的版本）：

```powershell
go get github.com/gin-gonic/gin@v1.12.0
go get gorm.io/gorm@v1.31.2
go get gorm.io/driver/mysql@v1.6.0
go get github.com/go-sql-driver/mysql@v1.10.1
go get github.com/golang-jwt/jwt/v5@v5.3.1
go get golang.org/x/crypto@v0.57.0
go get google.golang.org/protobuf@v1.36.12
go get buf.build/go/protovalidate@v1.4.0
go get go.uber.org/automaxprocs@v1.6.0
go get gopkg.in/yaml.v3@v3.0.1
```

> 网络报错时先确认代理已设置：`go env -w GOPROXY=https://goproxy.cn,direct`。

## 4. 创建什么文件

`go get` 自动生成（无需手写）：

```text
go-api-template/go.mod
go-api-template/go.sum
```

## 5. 把什么代码写进去

本阶段不手写任何代码文件。执行完上面的命令后，打开 `go.mod`，它的 `require` 块应该与下面一致（`indirect` 块和 `go.sum` 由工具自动维护，行数多，不必逐行核对）：

```text
module go-api-template

go 1.26.2

require (
	buf.build/go/protovalidate v1.4.0
	github.com/gin-gonic/gin v1.12.0
	github.com/go-sql-driver/mysql v1.10.1
	github.com/golang-jwt/jwt/v5 v5.3.1
	go.uber.org/automaxprocs v1.6.0
	golang.org/x/crypto v0.57.0
	google.golang.org/protobuf v1.36.12
	gopkg.in/yaml.v3 v3.0.1
	gorm.io/driver/mysql v1.6.0
	gorm.io/gorm v1.31.2
)
```

> 注意：原项目最终的 `go.mod` 里还有一行 `buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go ...`，它是第 [06](./06-protobuf-buf.md)/[07](./07-protovalidate.md) 章 buf **生成代码** import 的模块，此刻没有生成代码，所以还没有它——这是预期差异，等第 [07](./07-protovalidate.md) 章结束就会补上。

## 6. 解释关键代码

### 每个依赖是什么、解决什么问题、为什么现在需要

| 依赖                                     | 它是什么                     | 解决什么问题                                                                         | 为什么现在就装                                                                                              |
| ---------------------------------------- | ---------------------------- | ------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------- |
| `github.com/gin-gonic/gin` v1.12.0       | HTTP Web 框架                | 路由注册、中间件、JSON 参数绑定与响应                                                | 第 [12](./12-main-manual-wiring.md) 章启动 HTTP 服务、第 [11](./11-handler-router.md) 章写 handler 时的地基 |
| `gorm.io/gorm` v1.31.2                   | ORM 核心库                   | 结构体 ↔ 数据库表的映射、链式查询、事务                                              | 第 [08](./08-user-model-repository.md) 章 model/repository 直接使用                                         |
| `gorm.io/driver/mysql` v1.6.0            | GORM 的 MySQL 方言驱动       | 把底层 MySQL 驱动接入 GORM（`gorm.Open(mysql.Open(dsn))`）                           | 第 [04](./04-mysql.md) 章建立数据库连接                                                                     |
| `github.com/go-sql-driver/mysql` v1.10.1 | `database/sql` 的 MySQL 驱动 | 真正用二进制协议和 MySQL 通信的实现；既是上一个包的底层，也是 migrate CLI 使用的驱动 | 第 [04](./04-mysql.md)/[05](./05-migrate.md) 章都需要，DSN 语法也是它定义的                                 |
| `github.com/golang-jwt/jwt/v5` v5.3.1    | JWT 库                       | 签发与解析 HS256 Access Token                                                        | 第 [10](./10-jwt.md) 章 JWT 平台包                                                                          |
| `golang.org/x/crypto` v0.57.0            | Go 官方扩展密码学库          | `bcrypt` 密码哈希（注册/登录的核心）                                                 | 第 [09](./09-user-service.md) 章 service 层                                                                 |
| `google.golang.org/protobuf` v1.36.12    | protobuf 的 Go 运行时        | proto 生成代码（消息序列化、描述符）依赖的运行库                                     | 第 [06](./06-protobuf-buf.md) 章生成代码一落盘就要能编译                                                    |
| `buf.build/go/protovalidate` v1.4.0      | Protovalidate 的 Go 运行时   | 在 Go 里执行 proto 中声明的字段校验规则                                              | 第 [07](./07-protovalidate.md) 章启用契约校验、第 [11](./11-handler-router.md) 章 handler 调用              |
| `go.uber.org/automaxprocs` v1.6.0        | 容器 CPU 探测库              | 把 `GOMAXPROCS` 校正为容器 cgroup 的 CPU 配额，避免调度开销                          | 第 [12](./12-main-manual-wiring.md) 章 `main.go` 里一行接入                                                 |
| `gopkg.in/yaml.v3` v3.0.1                | YAML 解析库                  | 读取 `configs/config.yaml`                                                           | 第 [03](./03-config.md) 章配置包                                                                            |

### 关于 go.mod / go.sum 的三个要点

- `go get` 记录的是"模块路径 @ 版本"，`go.sum` 记录的是"这些版本的哈希"。两者合起来才构成可重现构建。
- `require` 块里带 `// indirect` 注释的依赖不是你直接 import 的，而是工具根据依赖关系推出来的，不用管、也不要手动改。
- **此刻没有业务代码也能 `go get`**：`go get` 操作的是依赖清单与本地缓存，与源码无关。唯一的效果是 `go.mod` 里这些依赖暂时"还没人用"——这没关系，等代码写好后它们自然派上用场。中途也不要随手执行 `go mod tidy`，它会把"暂时没人用"的依赖从 `go.mod` 里删掉，打乱我们钉好的版本。

与其他文件的关系：`go.mod` 定义的模块名 `go-api-template` 将被第 [03](./03-config.md) 章的 `internal/platform/config` 用作自身包路径的前缀，也是第 [06](./06-protobuf-buf.md) 章 proto 里 `go_package` 选项（`go-api-template/gen/go/...`）的来源；`go.sum` 则伴随每一次 `go build` 被校验。

## 7. 做一个小检查

```powershell
Get-Content go.mod
go env GOMOD
```

执行成功后你应该看到：

- `go.mod` 内容与第 5 节一致（10 个直接依赖、版本号相同）；
- `go env GOMOD` 输出当前目录下 `go.mod` 的绝对路径，说明 `go` 命令确实工作在项目根目录；
- 项目根目录下出现了 `go.sum`，文件里每个依赖对应两行哈希。

> **本阶段成果：** `go.mod` + `go.sum` 就位，10 个直接依赖钉在原项目相同版本上。下一章写第一段真正的 Go 代码：配置加载。
