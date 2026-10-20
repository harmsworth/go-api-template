# 架构说明

本文回答四个问题：请求是怎么流动的？platform 能力怎么被使用？为什么这么组织代码？
以及几个关键取舍（没有 Wire、没有 AutoMigrate、Package by Feature）。

## 1. 一个请求的完整旅程

```text
HTTP 请求
   ↓
Gin Router（internal/app/router.go，手动注册路由）
   ↓
JWT Middleware（internal/user/middleware.go，受保护路由才会经过）
   ↓ 解析 Bearer Token → 得到 userID 写入 Gin Context
Handler（internal/user/handler.go）
   ↓ 参数解析：JSON → proto message（protojson）
   ↓ 校验：protovalidate（规则声明在 proto/user/v1/user.proto）
   ↓ 调用 Service，把领域错误映射成 HTTP 状态码
Service（internal/user/service.go）
   ↓ 业务规则：注册/登录/改密/分页/密码哈希（bcrypt）
   ↓ 依赖 Repository 接口 + TokenIssuer 接口
Repository（internal/user/repository.go）
   ↓ 数据库读写，把 GORM/MySQL 错误翻译成领域错误
GORM（internal/platform/database/mysql.go 提供连接）
   ↓
MySQL
```

依赖方向是单向的：`Handler → Service → Repository`。
上层只依赖下层的**接口**（Repository、TokenIssuer），下层不知道上层的存在。

## 2. Platform 能力

```text
internal/platform/
├── config/    读 YAML 配置，只有 main.go 使用
├── database/  创建 *gorm.DB，只有 main.go 使用
└── jwt/       签发/解析 token，被 Service（经 TokenIssuer 接口）和 Middleware 使用
```

规则：业务 Feature（internal/user）不直接读配置、不自己连数据库，
platform 能力全部由 main.go 构造后作为参数传入。

## 3. 为什么 Package by Feature

第一版只有一个 Feature，所以业务代码都在 `internal/user/`：
model / repository / service / handler / middleware / errors 都在一起。

好处：新增一个 Feature（比如 `internal/todo/`）就是复制一个目录再改；
看代码时，一个 Feature 的所有逻辑在一个包内，不用跨 5 个全局目录跳转。
如果按技术层分包（controllers/ services/ repositories/ models/），
加一个功能要同时改 5 个目录——第一版没有这个必要。

## 4. 为什么没有 Wire

依赖只有一条链：`db → repo → service → handler → router → server`，
在 main.go 里 10 行就能写完，且每个构造函数都是普通 Go 函数。
Wire 解决的是"依赖图复杂到人工维护容易出错"的问题——本项目还没有这个问题。
等依赖多了、组装代码开始变难读时，再考虑引入（docs/next-step.md Level 5）。

## 5. 为什么用 golang-migrate 而不是 GORM AutoMigrate

- AutoMigrate 只会"加字段/加表"，删列、改类型、改索引都很不可靠；
- 迁移是需要评审和回滚的变更，应该是**显式的 SQL 文件**（migrations/），
  进版本库、走 `make migrate-up` 执行，而不是程序启动时的隐式行为；
- 启动即迁移在多实例部署时是竞态源头。

所以本项目：启动 = 连接数据库 + 起 HTTP，仅此而已。

## 6. Proto 与 Gin 的关系

- `proto/user/v1/user.proto` 是 **API Contract**：有哪些接口、参数是什么、
  校验规则是什么（protovalidate）、资源长什么样（AIP 风格的 resource name）；
- Gin 是 **HTTP Runtime**：路由在 `internal/app/router.go` 手动注册。
- Handler 用 protojson 绑定请求、protovalidate 校验，然后把 proto 请求
  翻译成 Service 的普通 Go 函数调用；响应再把领域模型转回 proto 消息。

没有引入 grpc-gateway：那是为了"gRPC 服务自动获得 HTTP 接口"，
本项目没有 gRPC 服务，手动注册 8 条路由更直白。

## 7. 错误处理约定

```text
Repository / Service 返回领域错误（internal/user/errors.go 哨兵错误）
        ↓
Handler 的 mapError 统一映射：
  ErrUserNotFound       → 404 NOT_FOUND
  ErrEmailAlreadyExists → 409 CONFLICT
  ErrInvalidPassword    → 401 UNAUTHENTICATED
  ErrInvalidPageToken   → 400 INVALID_ARGUMENT
  其他                  → 500 INTERNAL（记录日志，不泄露内部细节）
        ↓
统一错误 JSON：{"error": {"code": "...", "message": "..."}}
```

## 8. 一句话总结

> 用最少的文件和最直的依赖方向，把一个"能跑、能测、能讲清楚"的 API 服务组装起来；
> 每引入一个新东西，都要能回答"它解决什么问题、为什么是现在"。
