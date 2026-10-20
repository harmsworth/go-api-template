# 06-protobuf-buf.md —— API 契约与代码生成

## 1. 这一阶段做什么

安装 protoc-gen-go 插件，写出第一版 `user.proto`（只有消息和服务定义，还没有校验规则），配好 `buf.yaml` / `buf.gen.yaml`，用 buf 拉取依赖并生成 Go 代码到 `gen/go/`。

## 2. 为什么要做

- **什么是 proto / buf / 生成的 Go 代码**：`user.proto` 是项目的 **API 契约**——它声明"系统对外有什么资源、有哪些接口、每个接口收什么参数、返回什么"，与任何具体语言无关。`buf` 是现代的 proto 工具链，负责 lint（风格检查）、管理外部 proto 依赖、调用插件生成代码。生成的 `gen/go/user/v1/user.pb.go` 是契约的 Go 实现：每个 message 变成一个 Go 结构体（带序列化、比较、字符串化等全套方法），handler、service 都基于它编程。
- **为什么用"契约先行"（proto-first）**：先定契约再写实现，接口在动手前就被完整地写了下来；契约文件还能被 lint、被 diff、被 review，"接口长什么样"从此不再藏在 handler 代码里。后面第 [07](./07-protovalidate.md) 章还会看到契约能带校验规则，第 [11](./11-handler-router.md) 章 handler 直接拿生成结构体绑定 JSON。
- **为什么分两步走（本章无校验规则，第 [07](./07-protovalidate.md) 章再加）**：先把“契约 + 生成流水线”这条最小通路跑通，理解 buf 的三个命令各做什么；然后第 [07](./07-protovalidate.md) 章只改 proto 一个文件、重跑一遍，就能聚焦在校验规则本身。这符合本项目"一次只引入一个概念"的教学节奏。
- **为什么放 `proto/user/v1/` 目录**：目录即 proto 包路径——`package user.v1` 对应 `proto/user/v1/`，一眼对上；`v1` 后缀表示 API 的第一个大版本，将来不兼容变更走 `v2` 新目录。

## 3. 手动执行什么命令

安装 protoc-gen-go（buf 会调用它做生成）：

```powershell
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
```

> **用途**：`buf generate` 本身不会写 Go 代码，它读取 `buf.gen.yaml` 后调用名为 `protoc-gen-go` 的可执行文件干活。这个插件同样被安装到 `go env GOPATH` 的 `bin` 目录（本机 `D:\workData\go\bin`），请确保在 `PATH` 里。

在项目根目录执行 buf 三连（都依赖网络，第一次最慢）：

```powershell
# 1. 拉取 buf.yaml 里声明的远程依赖（buf.build/bufbuild/protovalidate），
#    并生成 buf.lock 锁定具体版本
buf dep update

# 2. 按 buf.yaml 的 lint 规则检查 proto 文件风格（无输出 = 通过）
buf lint

# 3. 按 buf.gen.yaml 的配置调用插件，生成 Go 代码到 gen/go/
buf generate
```

## 4. 创建什么文件

```text
go-api-template/proto/user/v1/user.proto
go-api-template/buf.yaml
go-api-template/buf.gen.yaml
go-api-template/buf.lock          # buf dep update 自动生成
go-api-template/gen/go/user/v1/user.pb.go   # buf generate 自动生成
```

## 5. 把什么代码写进去

文件：`go-api-template/proto/user/v1/user.proto`（**中间版本**：先不含校验规则，第 [07](./07-protovalidate.md) 章升级为最终版）

```proto
syntax = "proto3";

// user.v1 定义 User 资源和它的 API 契约。
// 本文件是 API Contract：它描述“有什么接口、参数是什么”，
// 实际的 HTTP 路由由 internal/app/router.go 明确注册。
package user.v1;

import "google/protobuf/empty.proto";
import "google/protobuf/timestamp.proto";

option go_package = "go-api-template/gen/go/user/v1;userv1";

// User 是本模板唯一的资源类型。
// name 是资源名，格式固定为 "users/{id}"（AIP resource name 约定）。
message User {
  string name = 1;  // 例如: users/1
  string email = 2;
  string display_name = 3;
  google.protobuf.Timestamp create_time = 4;
  google.protobuf.Timestamp update_time = 5;
}

// ---- 标准方法：Create / Get / List / Update / Delete（AIP-131~135）----

message CreateUserRequest {
  string email = 1;
  // bcrypt 只取前 72 字节，这里在契约层就限制长度。
  string password = 2;
  string display_name = 3;
}

message GetUserRequest {
  string name = 1;
}

// AIP-158 分页：page_size=0 表示使用服务端默认值（20）。
message ListUsersRequest {
  int32 page_size = 1;
  string page_token = 2;
}

message ListUsersResponse {
  repeated User users = 1;
  string next_page_token = 2;
}

message UpdateUserRequest {
  string name = 1;
  string display_name = 2;
}

message DeleteUserRequest {
  string name = 1;
}

// ---- 自定义方法（AIP-136）：不适合套标准方法的动作 ----

message RegisterRequest {
  string email = 1;
  string password = 2;
  string display_name = 3;
}

message LoginRequest {
  string email = 1;
  string password = 2;
}

message ChangePasswordRequest {
  string name = 1;
  string current_password = 2;
  string new_password = 3;
}

// TokenResponse：注册/登录成功后返回访问令牌和用户信息。
message TokenResponse {
  string access_token = 1;
  User user = 2;
}

service UserService {
  rpc CreateUser(CreateUserRequest) returns (User);
  rpc GetUser(GetUserRequest) returns (User);
  rpc ListUsers(ListUsersRequest) returns (ListUsersResponse);
  rpc UpdateUser(UpdateUserRequest) returns (User);
  rpc DeleteUser(DeleteUserRequest) returns (google.protobuf.Empty);
  rpc Register(RegisterRequest) returns (TokenResponse);
  rpc Login(LoginRequest) returns (TokenResponse);
  rpc ChangePassword(ChangePasswordRequest) returns (google.protobuf.Empty);
}
```

文件：`go-api-template/buf.yaml`

```yaml
version: v2
modules:
  - path: proto
deps:
  - buf.build/bufbuild/protovalidate
lint:
  use:
    - STANDARD
  # AIP 风格的 RPC 直接返回资源本身（如 CreateUser/GetUser/UpdateUser 都返回 User），
  # 与 buf 的 STANDARD 命名与唯一性规则冲突，以下三条规则需要显式排除。
  except:
    - RPC_REQUEST_STANDARD_NAME
    - RPC_RESPONSE_STANDARD_NAME
    - RPC_REQUEST_RESPONSE_UNIQUE
breaking:
  use:
    - FILE
```

文件：`go-api-template/buf.gen.yaml`

```yaml
version: v2
plugins:
  # 使用本地 protoc-gen-go 插件（go install google.golang.org/protobuf/cmd/protoc-gen-go@latest）
  - local: protoc-gen-go
    out: gen/go
    opt: paths=source_relative
```

## 6. 解释关键代码

### user.proto 的骨架

- **`syntax = "proto3"` + `package user.v1`**：proto3 语法；包名是消息类型的命名空间，也对应目录 `proto/user/v1/`。
- **`option go_package = "go-api-template/gen/go/user/v1;userv1"`**：分号前是生成 Go 代码的 **import 路径**（与第 [02](./02-go-module.md) 章 module 名 `go-api-template` 拼在一起），分号后是 Go **包名** `userv1`。第 [11](./11-handler-router.md) 章的 handler 将 `import "go-api-template/gen/go/user/v1"` 然后用 `userv1.User` 这类类型。
- **`User` message**：注意它只有 `name`（资源名，`users/{id}`）、`email`、`display_name`、两个时间戳——**没有 `password`**。密码只在请求里出现，永远不会在响应结构里回传；数据库里存的也是哈希（第 [08](./08-user-model-repository.md) 章 model 的 `PasswordHash`）。字段编号（`1`、`2`…）一旦发布就不能复用，这是 proto 的兼容性铁律。
- **标准方法与自定义方法**：Create/Get/List/Update/Delete 五个是 AIP（Google API Improvement Proposals）定义的标准方法模板，Register/Login/ChangePassword 是不适合硬套标准方法的"自定义方法"（AIP-136）。本项目没跑 gRPC，`service UserService` 块更多是给"接口一共有哪些操作"一个权威清单，第 [11](./11-handler-router.md) 章路由按它注册。

### buf.yaml：lint 为什么要排除三条规则

`use: [STANDARD]` 启用 buf 的标准风格检查，但它预设的是 Google 官方"每个 RPC 配对独立命名的请求/响应类型"风格（如请求消息必须叫 `UserServiceCreateUserRequest`、响应必须叫 `UserServiceCreateUserResponse`、且每对类型全局唯一）。而 AIP 风格（AIP-131~135）恰恰相反：**标准方法直接复用资源类型**——`CreateUser` 返回 `User`，`GetUser` 和 `DeleteUser` 共用请求消息里只需要一个 `name` 字段的形态。两种风格冲突，所以显式排除：

- `RPC_REQUEST_STANDARD_NAME`：请求消息名不叫 `UserServiceCreateUserRequest` 这种格式；
- `RPC_RESPONSE_STANDARD_NAME`：响应消息名同理；
- `RPC_REQUEST_RESPONSE_UNIQUE`：多个 RPC 共用同一请求/响应类型时触发（本模板多个方法共用 `User`、`ListUsersResponse`）。

**buf 不认识 AIP，但它允许你声明例外**——排除三条、其余全保留，比干脆关掉 lint 好得多：消息字段命名、包版本后缀、import 规范等大量检查仍在生效。

- **`deps: [buf.build/bufbuild/protovalidate]`**：声明本模块依赖 Buf Schema Registry 上的 protovalidate 模块（它提供 `buf/validate/validate.proto`）。本章生成的代码还没用到它，但第 [07](./07-protovalidate.md) 章 import 校验规则时就需要，且 `buf lint` 解析 import 也要它，所以现在就声明。
- **`breaking: use: [FILE]`**：启用破坏性变更检查（对比旧版本，防止删字段/改编号），本教程不展开，知道有这层保险即可。

### buf.gen.yaml 与生成物

- **`local: protoc-gen-go`**：使用第 3 节安装的本地插件。
- **`out: gen/go` + `opt: paths=source_relative`**：生成代码输出到 `gen/go/`，并按 proto 文件自身的目录结构落盘（`proto/user/v1/user.proto` → `gen/go/user/v1/user.pb.go`）。这样 `go_package` 里的路径与磁盘路径一致，import 不混乱。
- **`buf.lock` 是什么**：`buf dep update` 生成的依赖锁文件，记录所依赖的远程模块在 BSR 上的具体 commit 和内容摘要，作用相当于 `go.sum`——保证任何人任何时候拉到的 protovalidate proto 内容完全一致。它与 `buf.yaml` 一起提交进仓库。

### 为什么生成代码（gen/go）故意提交进仓库

`.gitignore`（第 [01](./01-create-project.md) 章）里那句注释就是为它写的：提交生成代码后，clone 即编译，不强制每个使用者先装 buf；同时生成物只有 buf 版本这一个变量，review 时还能直接看到契约变化对生成代码的影响。主流 Go 项目（包括 etcd）大多也这么做。

### 与其他文件的关系

- 依赖：`go_package` 引用第 [02](./02-go-module.md) 章的 module 名；生成代码的运行时依赖 `google.golang.org/protobuf` 已在第 [02](./02-go-module.md) 章安装。
- 被依赖：第 [07](./07-protovalidate.md) 章在校验规则里继续改这个文件；第 [08](./08-user-model-repository.md) 章 model 与 `User` message 字段对应；第 [11](./11-handler-router.md) 章 handler 用生成结构体绑定请求、构造响应。

## 7. 做一个小检查

```powershell
buf lint
buf generate
go build ./...
Get-ChildItem gen/go -Recurse -File
```

执行成功后你应该看到：

- `buf lint` **没有任何输出**（buf 的约定：没有消息就是通过）；
- `buf generate` 无报错，`Get-ChildItem` 显示 `gen/go/user/v1/user.pb.go`；
- `go build ./...` 无输出——生成的 Go 代码能直接编译（此刻它只依赖已安装的 `google.golang.org/protobuf`）；
- 项目根目录多了 `buf.lock`，打开能看到 `buf.build/bufbuild/protovalidate` 的 commit 与 digest。

> **本阶段成果：** 契约文件、buf 配置、生成流水线全部就绪，`gen/go` 里有了第一份生成代码。下一章给契约加上请求校验规则，让 API 契约真正"活"起来。
