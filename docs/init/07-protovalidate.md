# 07-protovalidate.md —— 契约层请求校验

## 1. 这一阶段做什么

把 `user.proto` 从第 [06](./06-protobuf-buf.md) 章的中间版本升级为最终版：加上 `buf/validate` 校验规则，重新 lint、重新生成，让每个请求字段的合法性直接声明在 API 契约里。

## 2. 为什么要做

- **Protovalidate 是什么**：一套"写在 proto 里的字段校验规则"。它由两部分组成：`buf/validate/validate.proto` 定义规则语法（以字段 option 的形式），`buf.build/go/protovalidate`（第 [02](./02-go-module.md) 章已安装）是 Go 运行时——拿着生成代码里内嵌的规则元数据，在程序里对一个消息对象求值，不合法就返回逐字段的错误。
- **为什么现在做**：第 [06](./06-protobuf-buf.md) 章契约和生成流水线已通，本章只改 proto 一个文件就能看到“规则→生成→运行时可用”的完整效果；而 handler（第 [11](./11-handler-router.md) 章）才是真正调用校验的地方，必须赶在它之前就绪。
- **为什么校验放在 API 契约层，而不是只靠 Service 层**：这是本项目最重要的设计决策之一，第 6 节展开。一句话版本——格式类规则是**接口语义**的一部分，属于契约；写进 proto，所有调用方和实现方看到的是同一份规则。

## 3. 手动执行什么命令

第 [06](./06-protobuf-buf.md) 章之后无需新工具，全部在项目根目录执行：

```powershell
# 1. 第一次已执行过的话可跳过；作用是拉取 buf/validate/validate.proto 依赖并锁定版本
buf dep update

# 2. 按最终版 proto 重新生成 gen/go/user/v1/user.pb.go
buf generate
```

再把生成代码新引入的模块钉进 `go.mod`（它 import 了 protovalidate 规则对应的生成代码）：

```powershell
go get buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go@v1.36.12-20260825204119-511051f7f437.2
```

> **用途**：加了校验规则后，`user.pb.go` 会引用 `buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go` 模块里的包（validate.proto 本身的 Go 生成物）。此刻项目业务代码还没有 import 生成的 `user.pb.go`，`go mod tidy` 会把它当"没人用"删掉，所以这里用 `go get` 显式钉住——版本号与原项目 `go.mod` 一致，执行后 `go.mod` 的 require 块就与原项目完全相同了。

## 4. 创建什么文件

本章不新建文件，只修改：

```text
go-api-template/proto/user/v1/user.proto
```

并由 `buf generate` 更新：

```text
go-api-template/gen/go/user/v1/user.pb.go
```

## 5. 把什么代码写进去

文件：`go-api-template/proto/user/v1/user.proto`（**最终版**，与仓库实际文件完全一致）

```proto
syntax = "proto3";

// user.v1 定义 User 资源和它的 API 契约。
// 本文件是 API Contract：它描述“有什么接口、参数是什么”，
// 实际的 HTTP 路由由 internal/app/router.go 明确注册。
package user.v1;

import "buf/validate/validate.proto";
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
  string email = 1 [(buf.validate.field).string.email = true];
  // bcrypt 只取前 72 字节，这里在契约层就限制长度。
  string password = 2 [(buf.validate.field).string = {min_len: 8, max_len: 72}];
  string display_name = 3 [(buf.validate.field).string = {min_len: 1, max_len: 100}];
}

message GetUserRequest {
  string name = 1 [(buf.validate.field).string.pattern = "^users/[0-9]+$"];
}

// AIP-158 分页：page_size=0 表示使用服务端默认值（20）。
message ListUsersRequest {
  int32 page_size = 1 [(buf.validate.field).int32 = {gte: 0, lte: 100}];
  string page_token = 2;
}

message ListUsersResponse {
  repeated User users = 1;
  string next_page_token = 2;
}

message UpdateUserRequest {
  string name = 1 [(buf.validate.field).string.pattern = "^users/[0-9]+$"];
  string display_name = 2 [(buf.validate.field).string = {min_len: 1, max_len: 100}];
}

message DeleteUserRequest {
  string name = 1 [(buf.validate.field).string.pattern = "^users/[0-9]+$"];
}

// ---- 自定义方法（AIP-136）：不适合套标准方法的动作 ----

message RegisterRequest {
  string email = 1 [(buf.validate.field).string.email = true];
  string password = 2 [(buf.validate.field).string = {min_len: 8, max_len: 72}];
  string display_name = 3 [(buf.validate.field).string = {min_len: 1, max_len: 100}];
}

message LoginRequest {
  string email = 1 [(buf.validate.field).string.email = true];
  string password = 2 [(buf.validate.field).string.min_len = 1];
}

message ChangePasswordRequest {
  string name = 1 [(buf.validate.field).string.pattern = "^users/[0-9]+$"];
  string current_password = 2 [(buf.validate.field).string.min_len = 1];
  string new_password = 3 [(buf.validate.field).string = {min_len: 8, max_len: 72}];
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

## 6. 解释关键代码

### 规则语法逐类拆解

所有规则都以字段 option 的形式挂在字段类型上：`[(buf.validate.field).类型.规则 = 值]`。

- **`[(buf.validate.field).string.email = true]`**：该字符串必须是合法 email 格式。用在 `CreateUserRequest.email`、`RegisterRequest.email`、`LoginRequest.email` 上。
- **`[(buf.validate.field).string = {min_len: 8, max_len: 72}]`**：长度必须在 8~72 **字节**之间（`min_len/max_len` 数的是字节不是字符，对中文昵称要留心，所以 `display_name` 上限给了 100）。密码上限 72 是**深思熟虑的数字**：bcrypt 算法只处理密码的**前 72 字节**，超出部分被静默丢弃——如果契约允许 200 字节的密码，两个"前 72 字节相同、后面任意不同"的密码会哈希出同一个结果，这既是安全隐患也是反直觉行为。所以干脆在契约层就截住：能通过校验的密码，bcrypt 处理时一定是完整的。这个设计的来龙去脉写在 proto 文件的注释里（`// bcrypt 只取前 72 字节，这里在契约层就限制长度。`），规则和解释住在一起。
  - 对比 `LoginRequest.password` 只有 `min_len = 1`：登录时**不该**告诉攻击者"你的密码不够 8 位"——密码错就该统一返回"凭据无效"，长度规则只在注册/创建时强制。这类微妙差异正是"规则写在契约里"的价值：每个接口的宽容度被明确声明。
- **`[(buf.validate.field).string.pattern = "^users/[0-9]+$"]`**：正则校验。`GetUser`/`UpdateUser`/`DeleteUser`/`ChangePassword` 的 `name` 字段必须形如 `users/1`——这是 AIP 的**资源名**约定（集合名/资源 ID）。用正则把格式定死，第 [11](./11-handler-router.md) 章 handler 解析出 `id` 时就不用担心奇怪输入；`new_password` 复用 8~72 规则，保证改密码和设密码标准一致。
- **`[(buf.validate.field).int32 = {gte: 0, lte: 100}]`**：`page_size` 允许 0（AIP-158 语义：0 表示"用服务端默认值 20"，注释写在了字段上方）但最大 100，防止一页拉走全表。

### 校验的执行链路：Proto Rule → Protovalidate → Service

```text
HTTP 请求进入
   ↓
Handler（第 11 章）：JSON 绑定到生成的 Request 结构体
   ↓
Protovalidate（buf.build/go/protovalidate 运行时，Handler 内调用执行）：逐字段求值校验规则，不合法 → 400 逐字段错误
   ↓
Service（第 09 章）：只处理业务规则——邮箱是否已注册、旧密码是否正确、bcrypt 哈希……
   ↓
Repository（第 08 章）→ MySQL
```

契约规则（proto 里写的）在 **Handler 内**由 Protovalidate 运行时执行（具体调用代码见第 [11](./11-handler-router.md) 章）；Service 收到的请求已经通过了全部格式校验。

### 为什么校验放契约层，而不是只靠 Service

- **快速失败**：格式错误的请求在离入口最近的地方被拒绝，不占用任何业务查询和数据库连接，也不给下游留下"脏数据走了一半"的中间状态。
- **所有客户端一致**：规则在服务端契约里，而不是散落在 iOS/Android/Web 各自的表单校验里——客户端校验只是体验优化，服务端契约校验才是安全边界。换任何语言、任何调用方式（甚至将来上 gRPC），规则都不变。
- **契约即文档**：看一眼 `user.proto` 就知道"email 要合法、密码 8~72 字节、资源名长什么样"，不需要翻实现代码或问后端。协议评审时，校验规则和字段一起被 review。
- **双层防御**：契约层管**格式**（像不像一个 email、长度对不对），Service 层管**业务语义**（这个 email 是否已被注册、旧密码是否匹配）。两层职责不同、缺一不可——把"邮箱是否重复"塞进 proto 正则是做不到的，把"email 格式"写进 Service 又会让每个调用方重复踩坑。本模板 Service 层保留了自己的业务校验（见第 [09](./09-user-service.md) 章），这正是两层的分工示范。

### 与其他文件的关系

- 依赖：规则语法来自 `buf/validate/validate.proto`（`buf.yaml` 的 `deps` + `buf.lock` 已在第 [06](./06-protobuf-buf.md) 章备好）；Go 运行时 `buf.build/go/protovalidate` 已在第 [02](./02-go-module.md) 章安装；本章 `go get` 补齐了规则的 Go 生成物模块。
- 被依赖：第 [11](./11-handler-router.md) 章 handler 调用 protovalidate 执行校验；错误到 HTTP 400 的映射也发生在那里。

## 7. 做一个小检查

```powershell
buf lint
buf generate
Get-Content go.mod
```

执行成功后你应该看到：

- `buf lint` 无输出——带校验注解的 proto 同样通过风格检查（`buf/validate` 是合法的 option 扩展）；
- `buf generate` 无报错，`gen/go/user/v1/user.pb.go` 被重新生成（用 `Select-String -Path gen/go/user/v1/user.pb.go -Pattern "Validate"` 能搜到校验相关代码，对比第 [06](./06-protobuf-buf.md) 章版本是新增的）；
- `go.mod` 的 require 块里出现了 `buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go`——至此与原项目完全一致。

> **本阶段成果：** 请求格式规则（email、密码长度、资源名 pattern、分页上限）全部声明在 API 契约里并已生成进 Go 代码；校验的“声明”到此完成，“执行”将在第 [11](./11-handler-router.md) 章的 Handler 中接上。契约、数据库、校验三块基石就位，接下来进入业务代码：Model/Repository（[08](./08-user-model-repository.md)）→ Service（[09](./09-user-service.md)）→ JWT（[10](./10-jwt.md)）→ Handler 与路由（[11](./11-handler-router.md)）→ 组装启动（[12](./12-main-manual-wiring.md)）。
