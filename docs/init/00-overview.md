# 00-overview.md —— 项目总览与准备

## 1. 这一阶段做什么

不写一行代码：先看懂 `go-api-template` 是什么、最终长什么样、按什么路线重建、需要准备哪些工具。

## 2. 为什么要做

`go-api-template` 是一个 **Go Web API 学习型模板**，不是企业级脚手架。它的价值在于"小而完整"：

- 它有一个真实的业务对象（User 资源的增删改查 + 注册/登录/改密码）；
- 覆盖了 Web 服务最常见的几件事：配置加载、MySQL 连接、数据库迁移、API 契约、请求校验、JWT 认证、测试；
- 又没有复杂到读不下去——没有微服务、没有消息队列、没有依赖注入框架，所有依赖都在 `cmd/server/main.go` 里手动组装，一条主线读到底。

推荐的做事方式是四个词：**读过 → 思考过 → 复现过 → 优化过**。

1. **读过**：先通读根目录的 `README.md` 和 `docs/architecture.md`，知道项目里有什么；
2. **思考过**：对每个设计问一句"为什么这么做，不这么做行不行"（比如为什么不用 AutoMigrate）；
3. **复现过**：这是本教程要做的事——从一个空文件夹开始，按 [01](./01-create-project.md)~[15](./15-review.md) 的顺序把整个项目**手动**重新搭一遍。读代码会产生"我看懂了"的错觉，只有亲手搭，才会撞上所有真实的坑：PATH 没配对、库忘了建、生成代码缺依赖……这些坑才是学习真正的素材；
4. **优化过**：重建完成后回来对照原项目查差异、想取舍，最后用这个模板写一个你自己的小项目（比如 Todo、博客）。

## 3. 手动执行什么命令

本阶段只需要确认工具是否就绪。**注意：涉及 `go` 的命令以后一律在项目根目录 `go-api-template` 下运行**（本项目暂时还没有根目录，本阶段先在任意位置执行检查命令即可）。

```powershell
# 确认核心工具已安装（括号内是期望看到的版本）
go version        # go1.26.x
git --version     # 任意较新版本
docker --version  # 任意较新版本（第 04 章跑 MySQL 用）
buf --version     # 1.73.0（第 06 章用）

# 查看本机 GOPATH：go install 安装的工具都放在它的 bin 子目录下
# （本教程写作机器上是 D:\workData\go\bin，请确保这个目录已加入 PATH）
go env GOPATH
```

国内网络建议顺手设置 Go 模块代理（已设置过可跳过）：

```powershell
go env -w GOPROXY=https://goproxy.cn,direct
```

工具清单与安装方式：

| 工具              | 用途                                                          | 安装方式                                                                           | 是否必需                                     |
| ----------------- | ------------------------------------------------------------- | ---------------------------------------------------------------------------------- | -------------------------------------------- |
| Go 1.26.x         | 语言与工具链                                                  | 官网 <https://go.dev/dl/> 下载安装包，或 `winget install GoLang.Go`                | 必需                                         |
| Git               | 版本控制                                                      | <https://git-scm.com/> 或 `winget install Git.Git`                                 | 推荐                                         |
| Docker Desktop    | 本地跑 MySQL（第 [04](./04-mysql.md) 章）                     | <https://www.docker.com/products/docker-desktop/>                                  | 必需（或自备本机 MySQL）                     |
| buf v1.73.0       | proto 校验 + 代码生成（第 [06](./06-protobuf-buf.md) 章）     | `go install github.com/bufbuild/buf/cmd/buf@v1.73.0`（或 `scoop install buf`）     | 必需                                         |
| protoc-gen-go     | proto → Go 代码的生成插件（第 [06](./06-protobuf-buf.md) 章） | `go install google.golang.org/protobuf/cmd/protoc-gen-go@latest`                   | 必需                                         |
| migrate CLI       | 数据库迁移（第 [05](./05-migrate.md) 章）                     | `go install -tags 'mysql' github.com/golang-migrate/migrate/v4/cmd/migrate@latest` | 必需                                         |
| make              | 执行 Makefile 快捷命令（第 [05](./05-migrate.md) 章）         | `choco install make` 或 `scoop install make`                                       | 可选（没装就手动复制 Makefile 里的原始命令） |
| 任意 MySQL 客户端 | 手动建库（第 [04](./04-mysql.md) 章）                         | Navicat / DBeaver / 命令行 `mysql` 均可，本教程用 `docker exec`                    | 必需                                         |

> 上面三个 `go install` 安装的工具都会被放到 `go env GOPATH` 指向目录的 `bin` 子目录（本机即 `D:\workData\go\bin`）。执行完如果提示"找不到命令"，先检查这个目录是否在 `PATH` 环境变量里。

## 4. 创建什么文件

本阶段不创建任何文件。`docs/init/` 下的教程文档本身就是这个阶段的产物。

## 5. 把什么代码写进去

本阶段没有代码要写。但重建之前必须先记住"终点长什么样"——下面是项目**最终的完整目录树**（与仓库实际结构一致）：

```text
go-api-template/
├── cmd/
│   └── server/
│       └── main.go             # 程序入口：所有依赖在这里手动组装（无 Wire）
├── configs/
│   └── config.yaml             # 本地开发配置
├── docs/
│   ├── init/                   # ★ 本教程：从零重建本项目的分步文档
│   │   ├── 00-overview.md
│   │   ├── 01-create-project.md
│   │   ├── 02-go-module.md
│   │   ├── 03-config.md
│   │   ├── 04-mysql.md
│   │   ├── 05-migrate.md
│   │   ├── 06-protobuf-buf.md
│   │   ├── 07-protovalidate.md
│   │   ├── 08-user-model-repository.md
│   │   ├── 09-user-service.md
│   │   ├── 10-jwt.md
│   │   ├── 11-handler-router.md
│   │   ├── 12-main-manual-wiring.md
│   │   ├── 13-testing.md
│   │   ├── 14-run-and-check.md
│   │   └── 15-review.md
│   ├── architecture.md         # 架构说明与设计取舍
│   └── next-step.md            # 后续升级路线（按需引入，不要提前引入）
├── gen/
│   └── go/
│       └── user/
│           └── v1/
│               └── user.pb.go  # buf 生成的 Go 代码（故意提交进仓库）
├── internal/
│   ├── app/
│   │   ├── router.go           # 路由注册
│   │   └── server.go           # HTTP Server 与优雅停机
│   ├── platform/
│   │   ├── config/
│   │   │   └── config.go       # 配置加载
│   │   ├── database/
│   │   │   └── mysql.go        # GORM MySQL 连接
│   │   └── jwt/
│   │       └── jwt.go          # JWT 签发与校验
│   └── user/
│       ├── errors.go           # 业务错误定义
│       ├── handler.go          # HTTP 层：参数绑定、校验、响应
│       ├── handler_test.go     # handler 单元测试
│       ├── middleware.go       # JWT 认证中间件
│       ├── model.go            # GORM Model（字段与迁移 SQL 一一对应）
│       ├── repository.go       # 数据访问层
│       ├── repository_integration_test.go  # 仓库集成测试
│       ├── service.go          # 业务逻辑层
│       └── service_test.go     # service 单元测试
├── migrations/
│   ├── 000001_create_users.down.sql
│   └── 000001_create_users.up.sql
├── proto/
│   └── user/
│       └── v1/
│           └── user.proto      # API 契约（含请求校验规则）
├── .gitignore
├── buf.gen.yaml                # buf 代码生成配置
├── buf.lock                    # buf 依赖锁定（buf dep update 自动生成）
├── buf.yaml                    # buf lint / breaking 配置
├── docker-compose.yml          # 本地开发环境（只有 MySQL）
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

## 6. 解释关键代码

### 目录设计的几条主线

- **`cmd/server/main.go` 是组装说明书**：所有依赖（config → db → jwt → repo → service → handler → router → server）在这里按顺序手动创建、手动传入。不用 Wire 这类依赖注入框架，是因为依赖总共不到十个，手写反而更直观、出错更好查。
- **`internal/` 是 Go 的编译器级封装边界**：`internal/` 下的包只允许被同一个 module 内的代码导入，外部项目 import 会直接编译报错。业务代码全部住在 `internal/` 里，等于对外宣布"这些代码是项目私有的"。
- **`internal/` 内部又分两种**：`platform/` 放与业务无关的通用能力（config / database / jwt），`user/` 是按功能分包（Package by Feature）的业务模块——一个 feature 的 model、repository、service、handler、测试全在一个包里，改一个需求只动一个目录。
- **`proto/` + `gen/go/` 是"契约先行"（proto-first）**：API 长什么样先在 proto 里定死（包括校验规则），Go 代码由 buf 生成到 `gen/go/`。生成代码故意提交进仓库，使用者 clone 下来不装 buf 也能编译。
- **`migrations/` 是数据库结构的唯一真相**：表结构的每次变化都是一个带版本号的 SQL 文件，靠 golang-migrate 显式执行，绝不在程序启动时偷偷改表。

### 重建路线图（全 16 篇）

| 章节 | 内容                                             |
| ---- | ------------------------------------------------ |
| 00   | 总览与准备（本篇）                               |
| [01](./01-create-project.md)   | 创建项目与 .gitignore                            |
| [02](./02-go-module.md)   | 初始化 go module 与依赖                          |
| [03](./03-config.md)   | 配置加载（config）                               |
| [04](./04-mysql.md)   | MySQL 环境（docker-compose + GORM 连接）         |
| [05](./05-migrate.md)   | 数据库迁移（golang-migrate + Makefile）          |
| [06](./06-protobuf-buf.md)   | Protobuf + Buf（契约与代码生成）                 |
| [07](./07-protovalidate.md)   | Protovalidate（契约层请求校验）                  |
| [08](./08-user-model-repository.md)   | User：Model 与 Repository（业务 Feature 的地基） |
| [09](./09-user-service.md)   | User Service（业务规则的核心）                   |
| [10](./10-jwt.md)   | JWT（platform 层的签发与解析）                   |
| [11](./11-handler-router.md)   | Handler、Middleware、Router、Server（HTTP 层）   |
| [12](./12-main-manual-wiring.md)   | main.go —— 手动依赖注入，把一切组装起来          |
| [13](./13-testing.md)   | 测试 —— 单元、Handler、集成三层                  |
| [14](./14-run-and-check.md)   | 从零到跑通 —— 运行手册与 curl 全流程冒烟         |
| [15](./15-review.md)   | 复盘 —— 把整个项目装进脑子里                     |

### 8 个里程碑

| #   | 里程碑         | 怎么算达成                                              | 对应章节 |
| --- | -------------- | ------------------------------------------------------- | -------- |
| 1   | 服务能启动     | `go run ./cmd/server` 后访问 `/healthz` 有响应          | [12](./12-main-manual-wiring.md)       |
| 2   | 连上 MySQL     | 服务启动时数据库连接建立、无连接错误                    | [04](./04-mysql.md)-[05](./05-migrate.md)    |
| 3   | 迁移执行       | `migrate ... version` 显示版本 1，`users` 表存在        | [05](./05-migrate.md)       |
| 4   | 生成 protobuf  | `buf generate` 后 `gen/go/user/v1/user.pb.go` 出现      | [06](./06-protobuf-buf.md)       |
| 5   | 注册用户       | 调注册接口返回 access_token 和用户信息                  | [14](./14-run-and-check.md)       |
| 6   | 登录拿 JWT     | 登录接口返回 JWT                                        | [14](./14-run-and-check.md)       |
| 7   | 访问受保护接口 | 带 `Authorization: Bearer <token>` 请求成功，不带则 401 | [14](./14-run-and-check.md)       |
| 8   | 测试全绿       | `go test ./...` 全部通过                                | [13](./13-testing.md)-[14](./14-run-and-check.md)    |

### 本项目刻意不用的东西

| 不引入                | 一句话原因                                                               |
| --------------------- | ------------------------------------------------------------------------ |
| Wire（依赖注入框架）  | 依赖总共不到十个，手动 new 更直观，框架的学习收益此刻为负                |
| GORM AutoMigrate      | schema 变更需要可审查、可回滚的 SQL 文件，AutoMigrate 只加不减、不可审计 |
| Redis                 | 现阶段没有任何缓存需求，等真出现再加                                     |
| gRPC / grpc-gateway   | 本模板只提供 JSON over HTTP，proto 在这里只当契约和校验载体              |
| OpenTelemetry / Kafka | 没有可观测性和异步消息的需求，不要为了简历写代码                         |
| 自动 Mock 工具        | 测试规模还小，手写 fake 接口足以覆盖                                     |

判断标准只有一条：**需求出现了再引入**，引入顺序见 `docs/next-step.md`。

## 7. 做一个小检查

回到第 3 节的命令，逐条执行。执行成功后你应该看到：

- `go version` 输出 `go version go1.26.x windows/amd64`；
- `buf --version` 输出 `1.73.0`；
- `go env GOPATH` 输出一个真实存在的路径（本教程写作机器上是 `D:\workData\go`），并且它的 `bin` 子目录已在 `PATH` 中；
- `go env GOPROXY` 输出 `https://goproxy.cn,direct`。

> 另外提醒：Windows PowerShell 里 `curl` 是 `Invoke-WebRequest` 的别名，行为与 Linux 的 curl 不同。本教程涉及 HTTP 请求的地方统一写 `curl.exe`（PowerShell 会优先使用真正的 curl）。

> **本阶段成果：** 你已经知道这个项目的全貌、目录里每个东西的职责、8 个里程碑各自的验收标准，以及本机工具是否就绪。接下来从第 [01](./01-create-project.md) 篇开始，在空目录里亲手把它建出来。
