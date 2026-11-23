# 引入 gnostic OpenAPI 3 文档 + 查询参数 camelCase 化

## 1. Summary

本计划做三件事，并保持全仓一致：

1. **接入 gnostic `protoc-gen-openapi`**，从 `proto/user/v1/user.proto` 生成 `gen/openapi/openapi.yaml`（OpenAPI 3.0.3），产物提交入库。
2. **把 proto 的 12 个 RPC 改为带 `google.api.http` 注解的块形式**（单行 `;` → `{ option ... }`），让文档路径与 `internal/app/router.go` 的真实路由一致。注解只服务文档，不改变运行时。
3. **统一 camelCase 命名**：`ListUsers` 查询参数从 `page_size/page_token/order_by` 改为 `pageSize/pageToken/orderBy`（只保留 camelCase），同步 Handler、测试、curl 示例与全部文档。

范围已确认：查询参数不做 snake_case 兼容；生成物提交；文档全量同步（README / Makefile / docs / AGENTS.md / CODEBUDDY.md / `.trae` 与 `.codebuddy` 规则）。

## 2. Current State Analysis

已确认的现状（均来自实际读文件/实测）：

- [buf.gen.yaml](file:///d:/workSpace/dd/go-api-template/buf.gen.yaml) 只有本地 `protoc-gen-go`，输出 `gen/go`，`paths=source_relative`。
- [buf.yaml](file:///d:/workSpace/dd/go-api-template/buf.yaml) 只有 `buf.build/bufbuild/protovalidate`，无 `googleapis`，因此现在 import `google/api/annotations.proto` 会报 `imported file does not exist`。
- [user.proto](file:///d:/workSpace/dd/go-api-template/proto/user/v1/user.proto#L119-L132) 的 12 个 RPC 都是单行 `;` 形式，没有 `google.api.http` 注解。
- 已在本机实测：
  - 无注解时 gnostic 输出 `paths: {}`（空文档），**没有** openapiv2 那种 `generate_unbound_methods` 兜底；
  - 加注解后 12 条路径正确生成，`{name=users/*}` 渲染为 `/api/v1/users/{user}`；
  - `default_response=false` 可去掉 `google.rpc.Status` 错误体；
  - 生成物固定命名 `openapi.yaml`，插件无改文件名选项；
  - 带注解重新生成后 `user.pb.go` 多一个空导入 `_ "google.golang.org/genproto/googleapis/api/annotations"`，该模块已在 [go.mod](file:///d:/workSpace/dd/go-api-template/go.mod#L55) 中作为 indirect 存在；
  - `protoc-gen-openapi` 已安装在 `D:\workData\go\bin\protoc-gen-openapi.exe`。
- 查询参数现状：Handler 读 `page_size/page_token/order_by`；`filter` 名称不变。`order_by` 的**值**（如 `create_time desc`）使用 proto 字段名，本次不改。
- 文档里出现 `page_size/page_token/order_by` 的位置已定位：`README` 未出现，但 `docs/level1/11`、`13`、`14`，`docs/level2/00`、`04`、`08`、`10`、`12`、`13`，`docs/pagination-tokens.md` 均有。
- [architecture.md](file:///d:/workSpace/dd/go-api-template/docs/architecture.md#L79-L80) 第 6 节写着"手动注册 8 条路由"，实际是 12 条，属既有陈旧描述，本次一并修正。

## 3. Proposed Changes

### 3.1 契约层（proto / buf）

**文件：`buf.yaml`**

- `deps` 增加 `buf.build/googleapis/googleapis`。
- 原因：`google/api/annotations.proto` 来自该模块，否则 lint/generate 解析 import 失败。
- 不改 `lint.except` 三条。

**文件：`buf.gen.yaml`**

在 `protoc-gen-go` 之后新增第二个插件：

```yaml
version: v2
plugins:
  # 使用本地 protoc-gen-go 插件（go install google.golang.org/protobuf/cmd/protoc-gen-go@latest）
  - local: protoc-gen-go
    out: gen/go
    opt: paths=source_relative

  # OpenAPI 3 文档：路径来自 proto 的 google.api.http 注解
  # 需要 go install github.com/google/gnostic/cmd/protoc-gen-openapi@latest
  - local: protoc-gen-openapi
    out: gen/openapi
    opt:
      - naming=json
      - fq_schema_naming=true
      - title=go-api-template API
      - version=0.1.0
      - default_response=false
```

选项含义（写进 docs/level1/06 的解释段）：

| 选项 | 原因 |
| --- | --- |
| `naming=json` | schema 字段用 `displayName` 等 JSON 名，与 protojson 响应一致 |
| `fq_schema_naming=true` | schema 名带包名（`user.v1.User`），避免未来重名 |
| `title` / `version` | 默认值分别是 proto 文件名与 `version not set`，不可接受 |
| `default_response=false` | 否则每个 operation 挂 `google.rpc.Status`，与本仓库 `{"error":{...}}` 不符 |

> **不要加 `yaml=true` 或 `output_format=yaml`**：gnostic 的选项只有 `version` / `title` / `description` / `naming` / `fq_schema_naming` / `enum_type` / `depth` / `default_response`，YAML 是唯一输出格式。`output_format=yaml` 属于 grpc-gateway 的 `protoc-gen-openapiv2`，换插件后不再适用，写了会被当作未知参数导致生成失败。

**文件：`proto/user/v1/user.proto`**

1. 增加 import：

```proto
import "google/api/annotations.proto";
```

2. 12 个 RPC 全部改为块形式并加注解（路径必须与 `router.go` 一一对应）：

```proto
service UserService {
  rpc CreateUser(CreateUserRequest) returns (User) {
    option (google.api.http) = { post: "/api/v1/users" body: "*" };
  }
  rpc GetUser(GetUserRequest) returns (User) {
    option (google.api.http) = { get: "/api/v1/{name=users/*}" };
  }
  rpc ListUsers(ListUsersRequest) returns (ListUsersResponse) {
    option (google.api.http) = { get: "/api/v1/users" };
  }
  rpc UpdateUser(UpdateUserRequest) returns (User) {
    option (google.api.http) = { patch: "/api/v1/{name=users/*}" body: "*" };
  }
  rpc DeleteUser(DeleteUserRequest) returns (google.protobuf.Empty) {
    option (google.api.http) = { delete: "/api/v1/{name=users/*}" };
  }
  rpc Register(RegisterRequest) returns (TokenResponse) {
    option (google.api.http) = { post: "/api/v1/auth/register" body: "*" };
  }
  rpc Login(LoginRequest) returns (TokenResponse) {
    option (google.api.http) = { post: "/api/v1/auth/login" body: "*" };
  }
  rpc Refresh(RefreshTokenRequest) returns (TokenResponse) {
    option (google.api.http) = { post: "/api/v1/auth/refresh" body: "*" };
  }
  rpc Logout(LogoutRequest) returns (google.protobuf.Empty) {
    option (google.api.http) = { post: "/api/v1/auth/logout" body: "*" };
  }
  rpc ChangePassword(ChangePasswordRequest) returns (google.protobuf.Empty) {
    option (google.api.http) = { post: "/api/v1/{name=users/*}/password" body: "*" };
  }
  rpc RequestPasswordReset(RequestPasswordResetRequest) returns (google.protobuf.Empty) {
    option (google.api.http) = { post: "/api/v1/auth/request-password-reset" body: "*" };
  }
  rpc ResetPassword(ResetPasswordRequest) returns (google.protobuf.Empty) {
    option (google.api.http) = { post: "/api/v1/auth/reset-password" body: "*" };
  }
}
```

`ListUsersRequest` 等 message 块不改（字段名保持 snake_case，JSON 由 protojson 输出 camelCase）。

**生成物（不手写）**

- `buf.lock`：`buf dep update` 追加 googleapis 记录。
- `gen/go/user/v1/user.pb.go`：`buf generate` 重新生成，接受新增空导入。
- `gen/openapi/openapi.yaml`：新增并提交。

### 3.2 运行时（Handler 与测试）

**文件：`internal/user/handler.go`**

1. `ListUsers`（约 230-245 行）——只读 camelCase：

```go
// ListUsers GET /api/v1/users?pageSize=&pageToken=&filter=&orderBy=
func (h *Handler) ListUsers(c *gin.Context) {
	req := &userv1.ListUsersRequest{
		PageToken: c.Query("pageToken"),
		Filter:    c.Query("filter"),
		OrderBy:   c.Query("orderBy"),
	}
	if v := c.Query("pageSize"); v != "" {
		// int 在 64 位机器上比 int32 宽，先做范围检查再转换。
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > int(^uint32(0)>>1) {
			writeError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "pageSize must be a non-negative integer")
			return
		}
		req.PageSize = int32(n)
	}
	...
```

2. `mapError` 第 121 行：用户可见文案 `"invalid order_by"` → `"invalid orderBy"`。`internal/user/errors.go` 的哨兵错误文本 `user: invalid order_by` **不改**（内部日志/`errors.Is` 语义不变）。

**文件：`internal/user/handler_test.go`**

- 236 行：`?page_size=2` → `?pageSize=2`
- 257 行：`?page_size=2&page_token=` → `?pageSize=2&pageToken=`
- 303 行：`?order_by=email%20desc` → `?orderBy=email%20desc`
- 317 行：`?order_by=password%20desc` → `?orderBy=password%20desc`
- 316 行注释同步改为 `orderBy`。

### 3.3 构建与依赖

**文件：`Makefile`**

`buf-generate` 目标保持 `buf generate`（一个模板同时产出 go 与 openapi），只更新注释，避免新增目标：

```make
# 生成 proto 产物：gen/go（Go 代码）+ gen/openapi/openapi.yaml（OpenAPI 3 文档）
# 需要 protoc-gen-go 与 protoc-gen-openapi，见 docs/level1/06-protobuf-buf.md
buf-generate:
	buf generate
```

**文件：`go.mod` / `go.sum`**

- 执行 `go mod tidy`，把 `google.golang.org/genproto/googleapis/api` 从 indirect 提升为直接依赖。不手工编辑。

### 3.4 文档（全量同步）

**README.md / README.en.md**

- 技术栈表新增一行：`gnostic protoc-gen-openapi | 从 google.api.http 注解生成 OpenAPI 3 文档`（英文版同步）。
- 前置条件：在 `protoc-gen-go` 后补 `protoc-gen-openapi`（改 proto 时才需要）。
- 目录结构：`gen/go/` 下方补 `gen/openapi/  # OpenAPI 3 文档（故意提交）`。
- 常用命令：把 `make buf-lint / make buf-generate` 的说明改为"生成 `gen/go` 与 `gen/openapi/openapi.yaml`"。
- "刻意没有引入" 段落补一句：`google.api.http` 注解只用于生成文档，运行时仍是 Gin 手写路由，没有 grpc-gateway。

**docs/architecture.md**

- 第 6 节：补充"proto 额外携带 `google.api.http` 注解，仅供 `buf generate` 产出 `gen/openapi/openapi.yaml`；运行时真相仍是 `router.go`"。
- 同一节把"手动注册 8 条路由"改为 12 条。
- 追加一句命名约定：JSON 响应与查询参数统一 camelCase（`pageSize/pageToken/orderBy`），`filter`/`orderBy` 的**值**仍使用 proto 字段名（`display_name`、`create_time`）。

**docs/level1/06-protobuf-buf.md**

- 第 3 节：新增 `go install github.com/google/gnostic/cmd/protoc-gen-openapi@latest`，说明两个插件分工。
- 第 4 节产物清单补 `gen/openapi/openapi.yaml`。
- 第 5 节：`buf.yaml` 增 `googleapis`；`buf.gen.yaml` 用双插件版本；`user.proto` 加 import 与注解（本章为"中间版本"，注解同样加上，避免第 11 章才补）。
- 第 6 节新增小节解释：为什么必须有注解（无注解时 gnostic 输出 `paths: {}`）、为什么产物固定叫 `openapi.yaml`、`default_response=false` 的原因、`{name=users/*}` 如何映射到 `/api/v1/users/:id`。
- 第 7 节检查步骤补 `Get-ChildItem gen/openapi` 与 `Select-String ... 'openapi:'`。
- 在"为什么 gen/go 提交"小节后补一句：`gen/openapi` 同样提交。

**docs/level1/07-protovalidate.md**

- 第 5 节最终版 proto 片段：补 `import "google/api/annotations.proto";` 与 8 个 RPC 的注解块（与本章实际 RPC 数量一致），保证教程前后一致。

**docs/level1/11-handler-router.md**

- `ListUsers` 代码片段（243-254 行附近）：注释与 `c.Query` 改 camelCase，错误文案改 `pageSize`。

**docs/level1/13-testing.md**

- 504 行测试 URL：`?page_size=2` → `?pageSize=2`。

**docs/level1/14-run-and-check.md**

- 210、240 行 curl：`page_size` → `pageSize`、`page_token` → `pageToken`。
- 317 行表格行同步。

**docs/level2/00-overview.md**

- 141 行文件清单补 `gen/openapi/openapi.yaml`。
- 163 行端点说明：`page_size`、`page_token`、`order_by` → `pageSize`、`pageToken`、`orderBy`。

**docs/level2/04-proto-contract.md**

- 第 3 节命令后补一句：`buf generate` 同时产出 `gen/openapi/openapi.yaml`。
- 第 4 节文件清单补 `gen/openapi/openapi.yaml`。
- 5.2 之后新增 5.x 小节：`service` 块必须携带 `google.api.http` 注解，否则文档为空。
- 6.3 示例 `order_by=create_time desc` → `orderBy=create_time desc`，并说明值仍是 proto 字段名。
- 6.4 表格下方补一句：查询参数同样使用 camelCase（`pageSize`/`pageToken`/`orderBy`）。

**docs/level2/08-query-layer.md**

- 45-46 行示例：`order_by=` → `orderBy=`。
- 401-408 行 Handler 片段：注释、`c.Query`、错误文案改 camelCase。
- 429 行说明 `invalid orderBy`。

**docs/level2/10-handler-endpoints.md**

- `ListUsers` 传参说明（202 行附近）改为 `pageSize`/`pageToken`/`filter`/`orderBy`。

**docs/level2/12-tests.md**

- 358 行：`order_by=email desc` → `orderBy=email desc`。

**docs/level2/13-run-and-verify.md**

- 223、235、249、256 行 curl：`page_size` → `pageSize`、`page_token` → `pageToken`、`order_by` → `orderBy`（`filter` 不变）。
- 新增可选项：`Get-ChildItem gen/openapi` 核对文档已生成。

**docs/pagination-tokens.md**

- 208 行 `/api/v1/users?page_token=` → `?pageToken=`。

**docs/next-step.md**

- Level 2 表格新增一行：`OpenAPI 3 文档 | 已完成 | 由 proto 的 google.api.http 注解生成 gen/openapi/openapi.yaml，与 router.go 路由保持一致`。

### 3.5 仓库内的 AI 规则/说明（保持与新事实一致）

- `AGENTS.md`
  - 目录块增加 `gen/openapi/`。
  - 常用命令处注明 `buf generate` 同时生成 Go 代码与 OpenAPI 文档，并给出 `protoc-gen-openapi` 安装命令。
  - 硬约束第 1 条扩展为：改 proto 后必须 `buf generate`，同时提交 `gen/go` 与 `gen/openapi/openapi.yaml`；新增/修改 rpc 时必须补 `google.api.http` 注解并与 `router.go` 对齐。
  - 完成定义增加"OpenAPI 文档已重新生成且路径与路由一致"。
- `CODEBUDDY.md`
  - "生成代码"段（104 行）：补第二个插件与 `gen/openapi` 产物。
  - "不可违背的约定"第 2 条（119 行）：补注解与 openapi 提交要求。
- `.trae/rules/project-rules.md`
  - "契约与数据库"节：新增"RPC 必须携带 `google.api.http` 注解且与 `router.go` 一致；改路由要同步注解"。
  - "测试与验证"节：`buf generate` 后确认 `gen/openapi/openapi.yaml` 变更。
- `.trae/skills/go-api-template/SKILL.md`
  - "项目不变量"增加注解约束。
  - "契约先行"第 2-3 步：生成后确认 `gen/go` 与 `gen/openapi` 都更新。
- `.codebuddy/rules/schema-and-contract/RULE.mdc`
  - "proto 契约"节：补注解规则与 `gen/openapi` 提交。
  - "路由同步"节：把"与 proto 没有代码生成关系"改为"运行时仍手写，但 proto 的 `google.api.http` 注解必须与之逐条对齐（用于生成文档）"。
- `.codebuddy/commands/gen.md`
  - 说明改为两个本地插件、两类产物；`gen/openapi` 不进 `.gitignore`。
- `.gitignore`：不改（两个生成目录都提交）。

## 4. Assumptions & Decisions

1. **只保留 camelCase 查询参数**（用户已选）。这是对外不兼容变更，模板尚无外部使用者，接受；不写 snake_case 回退分支。
2. **`filter` 参数名不变**；`filter` 与 `orderBy` 的**值**仍用 proto 字段名（`display_name`、`create_time`），不改成 JSON 名。原因：值由 `service.go` 白名单解析，属于查询 DSL，与 JSON 命名无关。
3. **错误文案与参数名保持一致**：HTTP 响应文案用 `pageSize`/`orderBy`；内部哨兵错误文本不动。
4. **生成物提交**（用户已选）：`gen/openapi/openapi.yaml` 与 `gen/go` 同等对待，clone 即可查阅文档。
5. **文档全量同步**（用户已选），包括从零重建的 level1 教程，避免教程与仓库不一致。
6. **不新增 Makefile 目标**：一个 `buf generate` 出两类产物，避免两套入口；只更新注释。
7. **不使用远程插件**：gnostic 不在 BSR 上，只能 `local:`；CI 需要预装该插件（写入 AGENTS.md 常用命令）。
8. **不改 `buf.yaml` 的三条 lint except**。
9. **不传任何输出格式选项**：gnostic 只输出 YAML，`yaml=true` / `output_format=yaml` 是 openapiv2 的写法，不适用于本方案。
10. **已知限制写入文档**：
   - protovalidate 规则（`email`、`min_len`、`pattern`）不会出现在 OpenAPI schema；
   - `google.protobuf.Empty` 响应生成 `content: {}`；
   - gnostic 每次调用固定写 `openapi.yaml`，未来出现第二个 proto 目录会撞名，需要 `buf generate --path ...` 分别生成；
   - 文档路径参数是 `{user}`，运行时是 `:id`，语义等价。

## 5. Verification

按顺序执行：

```powershell
buf dep update
buf lint
buf generate
gofmt -l .
go mod tidy
go build ./...
go vet ./...
go test ./...
```

逐项确认：

1. `buf lint` 无新增规则报错（三条 known except 保持排除）。
2. `gen/go/user/v1/user.pb.go` 有 `_ "google.golang.org/genproto/googleapis/api/annotations"` 空导入。
3. `gen/openapi/openapi.yaml` 包含 12 条路径：

```powershell
Select-String -Path gen/openapi/openapi.yaml -Pattern '^    /|^openapi:|operationId'
```

应看到 `/api/v1/users`、`/api/v1/users/{user}`、`/api/v1/users/{user}/password` 与 6 条 `/api/v1/auth/*`；且不出现 `google.rpc.Status`。
4. `go.mod` 中 `google.golang.org/genproto/googleapis/api` 进入主 require 块（非 indirect）。
5. 查询参数只认 camelCase：

```powershell
go test ./internal/user/ -run TestHandlerListUsers -v
```

6. 手工冒烟（可选，需要 MySQL）：

```powershell
curl.exe -s "http://localhost:8080/api/v1/users?pageSize=2" -H "Authorization: Bearer $token"
```

返回 200；换成 `page_size=2` 时应按默认值返回 20 条（参数被忽略，不再报错）。

7. 全文检索不再有旧参数名残留（允许出现在 proto 字段定义、`errors.go` 内部错误文本与 `filter` 值示例中）：

```powershell
Select-String -Path README.md,README.en.md,docs\**\*.md,Makefile,AGENTS.md,CODEBUDDY.md -Pattern 'page_size=|page_token=|order_by='
```

8. `git status` 应包含：`buf.yaml`、`buf.lock`、`buf.gen.yaml`、`proto/user/v1/user.proto`、`gen/go/user/v1/user.pb.go`、`gen/openapi/openapi.yaml`、`internal/user/handler.go`、`internal/user/handler_test.go`、`go.mod`/`go.sum`、Makefile、两份 README、docs 相关篇目、AGENTS.md、CODEBUDDY.md 与规则文件。
