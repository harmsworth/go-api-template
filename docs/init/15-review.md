# 15 · 复盘 —— 把整个项目装进脑子里

## 1. 这一阶段做什么

不写代码、不敲命令。这一阶段是全部教程的复盘：
一张分层图、每层一句话职责、四个关键取舍、一份学习自检清单、一页常见坑。
合上文档能自己讲清楚这些内容，这个模板才真正属于你。

## 2. 为什么要做

"能跑通"和"能复用"之间隔着一层理解。这一阶段把这 8 份教程里散落的取舍
（哨兵错误、消费者接口、proto 契约、手动注入……）重新收拢成一张可以带走的心智模型——
下一个项目里你照着这张图开工，模板的价值才兑现。

## 3. 手动执行什么命令

无。如果想边看边验证，可以重新跑一遍：

```powershell
go build ./...
go test ./...
```

## 4. 创建什么文件

无新文件。本阶段唯一的"产出"是你自己能复述的架构图与答案。

## 5. 全项目的分层图

```text
                        ┌─────────────── 组装（cmd/server/main.go，唯一知道全部的地方）───────────────┐
                        │                                                                            │
HTTP 请求               │                                                                            │
   ↓                    │                                                                            │
Gin Router（internal/app/router.go，手动注册：/healthz + 公开 3 条 + 受保护 5 条）                       │
   ↓                    │                                                                            │
JWT Middleware（internal/user/middleware.go，受保护路由才经过）                                          │
   ↓ 解析 Bearer token → userID 写入 Gin Context                                                        │
Handler（internal/user/handler.go）                                                                     │
   ↓ protojson 绑定 → protovalidate 校验 → 调 Service → mapError 落状态码                               │
Service（internal/user/service.go）                                                                     │
   ↓ 业务规则：注册/登录/改密/分页/bcrypt，只依赖两个接口                                                │
Repository（internal/user/repository.go，接口在消费者侧）                                               │
   ↓ 数据读写 + 错误翻译（gorm.ErrRecordNotFound→ErrUserNotFound、MySQL 1062→ErrEmailAlreadyExists）      │
GORM（internal/platform/database/mysql.go 提供连接）                                                    │
   ↓                                                                                                    │
MySQL（migrations/ 由 golang-migrate 管理，启动不迁移）                                                  │
                        └──────────────────────────────────────────────────────────────────────────────┘

被 main.go 注入的三件 platform 能力：
  config.Load()  → cfg（地址/DSN/JWT 三组配置）──→ 传给 database.New、jwt.New、app.NewServer
  database.New() → *gorm.DB ──────────────────→ 传给 user.NewRepository
  jwt.New()      → *jwt.Manager ───────────────→ 传给 user.NewService（隐式满足 TokenIssuer）
                                                 和 user.JWTAuth（直接用 Parse）
```

**每层一句话职责：**

| 层                    | 一句话职责                                                |
| --------------------- | --------------------------------------------------------- |
| main.go               | 手动组装一切，是唯一的"知道全部"的地方                    |
| Router（app）         | 把 URL/方法注册到 Handler，声明哪些路由需要鉴权           |
| Middleware（JWTAuth） | 只做认证：验证 token、把 userID 放进 Context              |
| Handler               | 协议翻译：JSON↔proto、执行 protovalidate、领域错误→状态码 |
| Service               | 业务规则的唯一住址：注册/登录/改密/分页/密码哈希          |
| Repository            | 数据读写的唯一出口：翻译 GORM/MySQL 错误为领域错误        |
| Model                 | 描述"数据库里怎么存"，与迁移 SQL 人工对齐                 |
| platform/config       | 读配置，业务代码不许自己读文件或环境变量                  |
| platform/database     | 创建 *gorm.DB + 连接池参数，只连接、不迁移                |
| platform/jwt          | 签发/解析 token，不认识任何业务概念                       |

**依赖方向的单向性：**

```text
main → app → user(handler.go/middleware.go) → user(service.go) → user(repository.go) → GORM → MySQL
              user/service.go ──(TokenIssuer 接口)←── platform/jwt 的能力，但 service 不 import jwt
```

上层只依赖下层的**接口**（Repository、TokenIssuer），下层不知道上层存在；
platform 不知道业务；业务不读配置、不自连数据库；所有线头只在 main.go 汇合。
任何一层都不能反过来 import 上层——[13](./13-testing.md) 阶段 handler 测试自建路由，正是这条规则的自然结果。

## 6. 四个关键取舍问答

**Q1：为什么没有 Wire 等 DI 框架？**
依赖只有一条链 `db → repo → service → handler → router → server`，main.go 十来行就写完，
每个构造函数都是普通 Go 函数，出错在编译期/启动时立刻暴露。
Wire 解决的是"依赖图复杂到人工维护易出错"——本项目没这个问题。
引入工具的时机是**出现需求**（main.go 长到难以一眼看懂），而不是"显得专业"（见 `docs/next-step.md` Level 5）。

**Q2：为什么用 golang-migrate 而不是 GORM AutoMigrate？**
AutoMigrate 只擅长"加字段/加表"，删列、改类型、改索引都不可靠；
迁移是需要评审、能回滚的变更，应该是 migrations/ 里**显式的 SQL 文件**，进版本库、走
`make migrate-up` 执行。启动即迁移还是多实例部署的竞态源头。
所以本项目启动 = 连接 + 起 HTTP，schema 变更永远显式手动。

**Q3：为什么 Package by Feature（而不是按技术层分包）？**
User 的一切（model.go / errors.go / repository.go / service.go / handler.go / middleware.go 及其测试）都在 `internal/user/`。
加一个 Feature 就是复制一个目录再改；读一个 Feature 不用跨 5 个全局目录跳转。
按层分包（controllers/ services/ repositories/ models/）在小项目里只会让"加一个功能"
变成"同时改 5 个目录"。等单 Feature 内部拥挤了，先拆文件、再考虑拆模块（Level 5）。

**Q4：为什么 proto=契约、Gin=运行时（而不是 grpc-gateway 自动生成路由）？**
proto 文件按 AIP 设计：资源名 `users/{id}`、标准方法、自定义方法 `users/{id}:changePassword`、
protovalidate 规则内嵌。但 Gin 的路由树不支持段中冒号的 `:verb` 写法，
运行时把自定义方法映射成 `/api/v1/users/:id/password`。这造成"契约与路由人工对应"的义务，
换来的是：第一版不引入 grpc-gateway 的学习成本与生成链，8 条路由手动注册反而一目了然。
代价与收益都写在 router.go 的注释里——将来引入 grpc-gateway（出现真正的 gRPC 调用时）
再让路由自动生成。

## 7. 学习自检清单（合上文档回答）

1. 不看代码，说出一次注册请求（`POST /api/v1/auth/register`）经过了哪些层、每层做了什么？
2. 为什么 Handler 里不写 SQL？为什么 Repository 里不出现 HTTP 状态码？
   （提示：各自职责 + 错误的旅程：领域错误在谁那里诞生、在谁那里落地？）
3. `Repository` 接口为什么定义在 user 包（消费者侧）而不是实现所在的文件？
   [13](./13-testing.md) 阶段的测试从这条设计里薅到了什么好处？
4. `ErrInvalidPassword` 为什么同时覆盖"邮箱不存在"和"密码错误"？
   这个取舍牺牲了什么、保护了什么？
5. `page_token` 现在就是明文 offset——它的两个局限是什么？opaque cursor 怎么解决？
6. bcrypt 与 proto 契约里的 `max_len: 72` 是什么关系？如果去掉契约限制会发生什么？
7. `jwt.Manager` 没有写任何"实现 TokenIssuer"的声明，为什么能传给 `user.NewService`？
8. `WithValidMethods([]string{"HS256"})` 防的是哪类攻击？删掉它会怎样？
9. 为什么改密码之后旧 accessToken 仍然有效？这是缺陷还是取舍？怎么升级？
10. [08](./08-user-model-repository.md) 阶段 model 的 gorm tag 里写了 uniqueIndex，但项目从不 AutoMigrate——
    那真正决定表结构的是什么？两者靠什么保持一致？
11. 为什么 handler 测试要自建一个迷你路由，而不是复用 `app.NewRouter`？
12. 为什么 `go test ./...` 在没有 MySQL 的机器上也能全绿？

答不上来的，回到对应阶段（[08](./08-user-model-repository.md)–[14](./14-run-and-check.md)）重读"解释关键代码"一节。

## 8. 常见坑

| 坑                                         | 说明与规避                                                                                                                                                                                                                             |
| ------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| protojson 的字段名是 **camelCase**         | 请求体写 `displayName`（不是 `display_name`，虽然 protojson 也兼容接收 snake_case）；响应读 `accessToken`、`nextPageToken`。写错字段会被当作未知字段静默忽略，然后被 protovalidate 的 min_len 拦截——报错信息不会告诉你是"字段名拼错了" |
| **int32 范围转换**                         | proto 的 `page_size` 是 int32，`strconv.Atoi` 返回平台位宽的 int；64 位机器上 int 比 int32 宽，直接 `int32(n)` 可能静默溢出，所以 Handler 先做范围检查再转换（[11](./11-handler-router.md) 阶段 ListUsers）                            |
| **bcrypt 72 字节**                         | bcrypt 只用输入前 72 字节，超长时 x/crypto/bcrypt 直接报错；契约层用 `max_len: 72` 提前拦截，业务层不再自己截断                                                                                                                        |
| **buf dep update / buf generate 需要网络** | 拉 buf.build 的远端依赖（如 protovalidate）需要联网；公司代理环境要配 HTTPS_PROXY，否则 `buf generate` 卡在拉取远程模块                                                                                                                |
| **PowerShell 内联 JSON 转义**              | PowerShell 里双引号字符串会吞掉/解释 JSON 的引号，导致请求体残缺；一律 `Set-Content ... -Value '{"a":"b"}' -Encoding utf8` 写文件再 `--data "@文件"`，URL 含 `&` 时整体加引号                                                          |
| **Gin 路由不支持段中冒号**                 | 无法注册 AIP 风格的 `users/{id}:changePassword`（Gin 的路由树把 `:` 视为参数前缀），所以运行时用 `/api/v1/users/:id/password`，契约与路由人工对应                                                                                          |
| 忘了执行迁移就启动                         | 服务能启动（只连接不迁移），但一查表就报错——启动成功不代表表存在，先 `make migrate-up`                                                                                                                                                 |
| 集成测试"没跑"                             | `MYSQL_TEST_DSN` 没设置时集成测试是 SKIP 不是 PASS；`$env:` 只对当前 PowerShell 会话生效，新开终端要重新设                                                                                                                             |

## 9. 下一步去哪

学完不是终点，是起点。升级路线按"需求出现才引入"的原则写在
**`docs/next-step.md`**，按 Level 排列：

- **Level 2**：Refresh Token、忘记密码、opaque 分页游标、列表筛选排序；
- **Level 3**：Redis、Email、对象存储——都是先加 `internal/platform/<name>/` 再由 main 注入；
- **Level 4**：结构化日志统一、Prometheus metrics、OpenTelemetry tracing；
- **Level 5**：Wire、gRPC/grpc-gateway、golangci-lint、Feature 拆分。

每引入一项，流程都和本教程一样：proto 契约先行（若影响 API）→
加 platform 包或 Feature 内文件 → main.go 手动注入 → 补测试 → 在 `docs/architecture.md`
写明为什么引入。

> **本阶段成果：** 一张分层图、四个取舍、一份自检清单、一页常见坑。
> 至此 docs/init/ 全部教程完成：你已经从空目录亲手重建了一个
> "能跑、能测、能讲清楚"的 Go Web API 服务——接下来，用它写你自己的项目。
