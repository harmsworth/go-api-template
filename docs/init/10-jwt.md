# 10 · JWT —— platform 层的签发与解析

## 1. 这一阶段做什么

创建 `internal/platform/jwt/` 包（一个文件 `jwt.go`）：
负责签发和解析 JWT Access Token（HS256）。它不认识 User、Gin、数据库，
只接收一个 `userID`，签发/解析出对应的 token。

## 2. 为什么要做

**这个包是什么？**
一段与业务无关的"平台能力"：给我密钥和配置，我就能签发 token、验证 token。
注册/登录时签发（[09](./09-user-service.md) 的 Service 经 `TokenIssuer` 接口调用），
受保护请求进来时验证（[11](./11-handler-router.md) 的 JWTAuth 中间件调用）。

**为什么放 `internal/platform/jwt/` 而不是 `internal/user/`？**
`internal/platform/` 收纳"任何 Feature 都可能用到的通用能力"（config、database、jwt）。
JWT 不包含任何 User 业务规则——它甚至不知道 User 的存在，只处理 int64。
如果哪天加一个 `internal/todo/` Feature 也需要鉴权，直接复用这个包即可。
业务 Feature 也不 import 它：Service 只依赖 [09](./09-user-service.md) 定义的 `TokenIssuer` 接口。

**为什么现在创建？**
[09](./09-user-service.md) 的 Service 已经声明了 `TokenIssuer` 接口，这个包就是它的"真实实现"；
[11](./11-handler-router.md) 的中间件马上要用 `Parse`。

**依赖谁 / 谁依赖它？**

```text
internal/platform/jwt/jwt.go
   依赖：github.com/golang-jwt/jwt/v5（外部库）、config 传入的三个参数
        ↑
        ├── user.Service（经 TokenIssuer 接口使用 Issue，不 import 本包）
        ├── user.JWTAuth 中间件（直接使用 Parse，11 阶段）
        └── 12 的 main.go（构造 Manager 并注入上面两处）
```

## 3. 手动执行什么命令

在项目根目录执行：

```powershell
go get github.com/golang-jwt/jwt/v5@v5.3.1
go mod tidy
```

（[04](./04-mysql.md) 阶段做 `go mod tidy` 时可能已经把它带进 `go.sum`，重复执行也无害。）

## 4. 创建什么文件

```text
internal/platform/jwt/jwt.go
```

## 5. 把什么代码写进去

### `internal/platform/jwt/jwt.go`

```go
// Package jwt 负责签发和解析 JWT Access Token。
// 它是纯粹的 platform 能力：不知道 User、Gin、数据库的存在，
// 只接收一个 userID（int64），签发/解析出对应 Subject。
package jwt

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ErrInvalidToken 表示 token 无效或过期。
// 调用方（middleware）据此返回 401，不需要关心具体原因。
var ErrInvalidToken = errors.New("jwt: invalid token")

// Manager 持有签名密钥和 token 配置。
type Manager struct {
	secret []byte
	ttl    time.Duration
	issuer string
}

// New 创建 Manager。secret 建议足够长且不能泄露到代码里。
func New(secret string, ttl time.Duration, issuer string) *Manager {
	return &Manager{
		secret: []byte(secret),
		ttl:    ttl,
		issuer: issuer,
	}
}

// Issue 为指定 userID 签发 HS256 的 Access Token。
// userID 放在 RegisteredClaims.Subject 中，不自定义 claims 结构，
// 这样第一版保持最简单。
func (m *Manager) Issue(userID int64) (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Issuer:    m.issuer,
		Subject:   strconv.FormatInt(userID, 10),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(m.ttl)),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
}

// Parse 校验签名、算法和过期时间，返回 token 中的 userID。
func (m *Manager) Parse(tokenString string) (int64, error) {
	var claims jwt.RegisteredClaims
	token, err := jwt.ParseWithClaims(
		tokenString,
		&claims,
		func(t *jwt.Token) (any, error) { return m.secret, nil },
		// 只接受 HS256，防止算法替换攻击； WithIssuer 同时校验签发者。
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(m.issuer),
	)
	if err != nil || !token.Valid {
		return 0, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	userID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: bad subject %q", ErrInvalidToken, claims.Subject)
	}
	return userID, nil
}
```

## 6. 解释关键代码

**HS256 与密钥。**
HS256 是对称签名：签发和验证用同一个 secret。secret 来自 [02](./02-go-module.md) 阶段的配置
（`configs/config.yaml` 的 `jwt.secret`），由 [12](./12-main-manual-wiring.md) 阶段的 main.go 传给 `New`。
对比 RS256（非对称）：HS256 配置最简单，单体服务第一版足够；secret 泄露 = 全部 token 可伪造，
所以它不能写死在代码里（当前模板放本地 dev 配置，生产应走环境变量/密钥管理，
见 `docs/next-step.md`）。

**`RegisteredClaims`：用标准 claims，不自定义结构。**
JWT 标准字段里，`iss`（签发者）填配置的 issuer，`sub`（Subject）填 userID 的十进制字符串，
`iat`/`exp`（签发/过期时间）由 TTL 计算。
不自定义 claims 结构体，第一版保持最简单——反正我们只需要"这个 token 属于哪个 userID"。
`Parse` 里再用 `strconv.ParseInt` 把 Subject 转回 int64。

**`WithValidMethods`：防算法替换攻击。**
如果只传 key func，攻击者可以把 token header 里的 `alg` 改掉
（例如改成 `none`，或换成期望你用公钥验证的算法）来绕过签名校验。
`jwt.WithValidMethods([]string{"HS256"})` 让库在验证前先检查 alg，
非 HS256 直接拒绝。这是使用 jwt 库时最容易被忽略、后果最严重的一个选项。

**`WithIssuer` 与 TTL。**
`WithIssuer` 要求 token 的 `iss` 与本服务配置一致，防止"拿别的服务签发的合法 token 冒充"。
TTL 由 main.go 从配置的 `ttl_hours` 换算成 `time.Duration` 传入——
过期 token 在 `Parse` 里直接失败，错误统一包装成 `ErrInvalidToken`，
调用方（[11](./11-handler-router.md) 的中间件）不需要区分"过期"还是"签名不对"，一律 401。

**为什么放 platform 层 + 与 [09](./09-user-service.md) 的隐式接口呼应。**
这个包只依赖 `golang-jwt/v5`，不 import 项目里任何业务代码；
业务侧的 user 包也不 import 它（Service 走 `TokenIssuer` 接口）。
两边只在 [12](./12-main-manual-wiring.md) 阶段的 main.go 会合：

```text
jwtManager := jwt.New(cfg.JWT.Secret, ...)   // platform
service := user.NewService(repo, jwtManager) // jwtManager 隐式满足 user.TokenIssuer
```

`jwt.Manager` 的方法签名 `Issue(userID int64) (string, error)` 与 [09](./09-user-service.md) 定义的接口逐字吻合，
Go 的结构化类型系统让它自动满足接口，不需要 `implements` 声明、不需要适配器。
这就是"消费者定义接口、提供者天然实现"的双向好处。

## 7. 做一个小检查

在项目根目录执行：

```powershell
go build ./...
```

没有输出即通过。此时 `jwt.Manager` 已具备 `Issue`/`Parse` 能力，
但在 [11](./11-handler-router.md)、[12](./12-main-manual-wiring.md) 阶段之前还没有调用方——这是正常的：本教程按依赖方向自底向上搭建，
每个阶段只保证"编译通过 + 职责单一"。

> **本阶段成果：** `internal/platform/jwt/` 有了完整的签发与解析能力：HS256、
> 标准 claims（Subject 存 userID）、算法白名单、issuer 校验、TTL 过期。
> 它同时服务 Service（经 TokenIssuer 接口）和 Middleware（直接调用），
> 自己却对两者一无所知。
