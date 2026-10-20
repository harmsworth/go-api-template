# 流量防护：Go 独立部署与 Go + nginx 部署的配置指南

本文回答两个部署形态下的问题：**HTTP 流量的防护参数（超时、大小上限）怎么配、为什么这么配**。
代码落点是 `internal/app/server.go` 的 `http.Server` 配置；文末附自测方法与速查清单。

---

## 1. 防的是什么：四类"合法但恶意"的流量

下面这些攻击的共同点：**不靠大流量，靠钻协议规则的空子**，每个都发生在"正常请求"的边界内。

| 威胁 | 手法 | 打击面 |
| --- | --- | --- |
| Slowloris（慢头部） | 头部永远差最后一个 `\r\n`，每 15 秒补一个无关头续命 | 连接占满 → 新用户连不上 |
| Slow Body（慢请求体） | 头部发完，body 以每秒 1 字节的速度滴 | 同上，绕过只防慢头部的超时 |
| 巨型头部/巨型 body | 一次灌 100MB 的 header 或 body | 内存爆炸 |
| 僵尸连接 | 建立连接后一言不发，或 keep-alive 挂着不用 | fd 耗尽（Linux 默认 `ulimit -n` 常见 1024） |

共同规律：**服务端资源按"连接"计费，攻击者用最便宜的方式占坑**。所有防护参数的本质都是给资源占用设定"确定性的到期时间"和"确定性的体积上限"。

---

## 2. Go `http.Server` 参数的精确语义

配之前必须先搞清每个参数管哪一段、从什么时候起算。一个请求的生命周期：

```
连接建立 ──▶ 收头部 ──▶ 收body ──▶ handler处理 ──▶ 写响应 ──▶ 连接空闲(keep-alive) ──▶ 下一个请求
   │          │           │            │              │              │
   │   ReadHeaderTimeout  │            │         WriteTimeout     IdleTimeout
   │          └───── ReadTimeout ──────┘              │
   └──────────────────────────────────────────────────┘
              （连接本身没有总超时，由上述分段超时共同覆盖）
```

| 参数 | 管哪段 | 起算点 | 默认值 |
| --- | --- | --- | --- |
| `ReadHeaderTimeout` | 从连接建立到收完头部 | 连接建立（或上一请求结束） | **0 = 永不超时** |
| `ReadTimeout` | 收完头部 **+** 收完 body | 同上 | 0 |
| `WriteTimeout` | **从读完头部起**，到响应写完 | 读头部结束的时刻 | 0 |
| `IdleTimeout` | 两个请求之间连接空闲的时间 | 上一个请求处理结束 | 0（未设时回落到 ReadTimeout） |
| `MaxHeaderBytes` | 头部体积上限 | — | 1MB |

三个容易踩的坑：

1. **`WriteTimeout` 的起算点是"读完头部"**，不是"开始写响应"。它实际罩住 `body 读取 + handler + 写响应` 全程，所以必须 **大于 `ReadTimeout` + 最慢接口耗时**，否则慢请求会被冤杀（表现为偶发 502/连接重置，极难排查）。
2. **超时全部为 0 不等于"没风险"**，而是"连接何时死不可预测"。半开连接（客户端断电、网线被拔）会永远挂着占 fd。
3. **限时必须配限量**：只有时间上限，攻击者改成"5 秒内灌 1GB body"照样打挂。`MaxHeaderBytes` 管头部，body 上限用 `http.MaxBytesReader`（中间件内）。

---

## 3. 场景 A：Go 独立部署（无反代）

```
互联网用户 ──HTTPS──▶ Go 进程（自己就是最外层防线）
```

此时 Go 进程直接暴露在公网，**所有慢客户端、所有攻击流量都直达它**。防护必须全部由进程自己承担。

### 3.1 推荐配置

```go
// internal/app/server.go —— 独立部署版
func NewServer(addr string, handler http.Handler) *Server {
	return &Server{
		http: &http.Server{
			Addr:    addr,
			Handler: handler,

			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      60 * time.Second,
			IdleTimeout:       120 * time.Second,
			MaxHeaderBytes:    1 << 20,
		},
	}
}
```

### 3.2 数值依据

| 参数 | 值 | 理由 |
| --- | --- | --- |
| `ReadHeaderTimeout` 5s | 收紧 | 头部几百字节，真实网络 1 秒内完成，5s 是 5 倍冗余；slowloris 的"每 15 秒续命"在 5s 窗口内直接失败。这是防 slowloris 的**主防线** |
| `ReadTimeout` 30s | 适中 | 必须罩住真实慢客户端的 body：手机弱网上传 5MB 头像，30s 合理。**这是防 slow body 的主防线** |
| `WriteTimeout` 60s | 最宽 | 罩住 `body + handler + 写响应` 全程 = ≥ `ReadTimeout`(30s) + 最慢接口耗时。有超过 30s 的操作？做异步任务，不要拉长超时 |
| `IdleTimeout` 120s | 放宽 | 浏览器 keep-alive 默认约 115s（Chrome），服务端略长可让浏览器主动复用连接，减少握手。僵尸 keep-alive 由它兜底回收 |
| `MaxHeaderBytes` 1MB | 默认 | 头部没有业务理由超 1MB |

### 3.3 独立部署还必须补的两件事（不在 http.Server 里）

**TLS**：公网裸 HTTP 是不合格的。Go 自带方案是 `golang.org/x/crypto/acme/autocert`（Let's Encrypt 自动续期），或者用 Caddy 之类只在 TLS 层工作的极简反代——但那已经不是"独立部署"了。

**body 大小上限**：在中间件里按全局或按路由设置：

```go
// 全局中间件：10MB 上限（本项目 API 全是 KB 级 JSON，10MB 已是千倍富余）
r.Use(func(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 10<<20)
	c.Next()
})
```

---

## 4. 场景 B：Go + nginx（Docker Compose 同机部署）

```
互联网用户 ──HTTPS──▶ nginx ──内网HTTP──▶ Go ──内网──▶ MySQL
                （第一道防线）        （第二道防线）
```

本项目的定位（`docker compose` 装 nginx + app + mysql）。此时**慢客户端、slowloris、大 body 全被 nginx 挡下**，Go 看到的对端是 nginx（数据中心级内网连接）——Go 侧超时可以整体收紧一档，职责变成纵深防御的第二层。

### 4.1 Go 侧配置

```go
func NewServer(addr string, handler http.Handler) *Server {
	return &Server{
		http: &http.Server{
			Addr:    addr,
			Handler: handler,

			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      60 * time.Second,
			IdleTimeout:       120 * time.Second,
			MaxHeaderBytes:    1 << 20,
		},
	}
}
```

与独立部署数值相同，但**含义变了**：它们不再是主防线，而是"nginx 失误时的保险"。例如 nginx 重载配置的瞬间、或有人绕过 nginx 直连容器端口（所以 **app 容器绝不能映射 8080 到宿主机公网 IP**）。

### 4.2 nginx 侧配置（第一道防线，主力）

```nginx
server {
    listen 443 ssl;
    server_name example.com;

    # ---- TLS 终止 ----
    ssl_certificate     /etc/nginx/certs/fullchain.pem;
    ssl_certificate_key /etc/nginx/certs/privkey.pem;

    # ---- 对客户端（外网）的防护：主力防线 ----
    client_header_timeout 10s;      # 防外网 slowloris（主防线）
    client_body_timeout   30s;      # 防外网 slow body（主防线）
    client_max_body_size  10m;      # body 体积上限（与 Go 侧 MaxBytesReader 成对）
    keepalive_timeout     65s;      # 客户端 keep-alive 空闲回收

    # ---- 转发到 Go ----
    location /api/ {
        proxy_pass http://app:8080;          # compose 服务名，Docker 内置 DNS
        proxy_http_version 1.1;

        # 接力规则：proxy 超时 > Go 侧最慢处理时间
        proxy_connect_timeout 5s;            # 建立到 Go 的 TCP 连接（内网秒成）
        proxy_send_timeout    30s;           # 把请求发给 Go 的限时
        proxy_read_timeout    60s;           # 等 Go 响应的限时，必须 ≥ Go 最慢接口
    }

    location /healthz {
        proxy_pass http://app:8080;
        proxy_read_timeout 5s;               # 探活要快速失败，LB 才能及时摘节点
    }
}
```

### 4.3 两侧的接力关系（核心规则）

**每一层"下一跳"的超时 ≤ 上一跳**，让超时总是由外层先触发——这样用户拿到的是结构化的 4xx/5xx，而不是 nginx 兜底的 504：

| 链路位置 | nginx 侧 | Go 侧 | 关系 |
| --- | --- | --- | --- |
| 收头部 | `client_header_timeout 10s` | `ReadHeaderTimeout 5s` | 内层更紧：外网慢连接死在 nginx，到不了 Go |
| 收 body | `client_body_timeout 30s` | `ReadTimeout 30s` | 同级。注意 nginx 默认**缓冲完整个 body** 才转发（`proxy_request_buffering on`），Go 收 body 是内网全速，30s 只是极端兜底 |
| 等响应 | `proxy_read_timeout 60s` | `WriteTimeout 60s` | 同级偏松。nginx 的必须 ≥ Go 最慢接口，否则 Go 还在算、nginx 先掐断返回 504 |
| keep-alive | `keepalive_timeout 65s`（对客户端） | `IdleTimeout 120s` | Go 略大于 nginx 的 upstream 空闲回收周期，让 nginx 主动关连接，避免两边同时关的竞态 |
| body 大小 | `client_max_body_size 10m` | `MaxBytesReader 10MB` | 成对相等或内层略小；**只限时不限量 = 白防** |

---

## 5. 两种形态对比与选择

| 维度 | A：独立部署 | B：Go + nginx |
|---|---|---|
| 主防线在哪 | Go 进程内（四个超时都是主力） | nginx（Go 是第二层） |
| TLS | autocert 自管 | nginx 终止（Go 只说 HTTP） |
| 防外网慢客户端 | `ReadHeaderTimeout`/`ReadTimeout` 直接承担 | `client_header_timeout`/`client_body_timeout` 承担 |
| 静态前端文件 | Go 自己 `http.FileServer`（别用业务进程干这事） | nginx 天职 |
| 慢接口被掐时用户看到 | Go 的结构化错误 | 若接力正确：Go 的错误透传；若配错：nginx 504 |
| 适用 | 内网服务、客户内网交付 | 一切公网生产环境 |

> **本项目选择 B**（compose 装 nginx + app + mysql）。当某天去掉 nginx 裸跑时，把 4.1 的配置原样保留即可独立成 A——这正是"进程内防护永远要配"的原因：**它不依赖部署形态，是代码自带的底线**。

---

## 6. 例外：流式响应与超时的冲突

SSE、大文件下载、WebSocket 场景下，`WriteTimeout` 会在超时点**掐断正常的长连接**（表现为流每隔固定时间断一次）。三种处理方式，按推荐顺序：

1. **该类路由单独部署**一个不配 `WriteTimeout` 的 Server（不同端口，nginx 按 path 分流到它）；
2. 全局不配 `WriteTimeout`，在需要保护的普通路由内用 `http.NewResponseController(w).SetWriteDeadline(...)` 按请求微调；
3. （不推荐）拉长全局 `WriteTimeout` 到最大流时长——普通接口的防护窗口被白白放大。

本项目 v1 没有 SSE/下载接口，无需处理；加这类接口时回来读这一节。

---

## 7. 自测方法（配完必须验）

```powershell
# 1. slowloris 模拟（需安装 slowhttptest，或用任何慢速请求工具）
slowhttptest -c 200 -H -i 10 -r 200 -u http://localhost:8080/healthz -p 3
#   -H 慢头部模式；预期：Go 侧 5 秒内掐断，无连接堆积

# 2. 观察连接堆积（另一窗口）
ss -tan state established '( dport = :8080 or sport = :8080 )' | Measure-Object -Line
#   或 Windows: netstat -an | findstr :8080
#   正常压测中该数字稳定；被 slowloris 打挂时只增不减

# 3. 巨型 body 验证 MaxBytesReader
curl.exe -X POST http://localhost:8080/api/v1/auth/register `
  -H "Content-Type: application/json" `
  --data-binary "@10mb-garbage.bin"
#   预期：413 Request Entity Too Large（或 400），而非连接挂死

# 4. 接力验证：临时把 Go 的 WriteTimeout 调到 2s、nginx proxy_read_timeout 调到 60s
#   请求一个 sleep 3s 的接口 → 用户应在 ~2s 收到 Go 的错误（说明内层先触发）
```

---

## 8. 速查清单

- [ ] `ReadHeaderTimeout` 已配（防 slowloris 的底线，gosec G114 会查）
- [ ] `ReadTimeout` 已配（防 slow body）
- [ ] `WriteTimeout` 已配且 > `ReadTimeout` + 最慢接口耗时
- [ ] `IdleTimeout` 已配（回收僵尸 keep-alive）
- [ ] `MaxHeaderBytes` 显式写出
- [ ] body 上限：`http.MaxBytesReader` +（有 nginx 时）`client_max_body_size` 成对
- [ ] 有反代时：每层"下一跳超时 ≤ 上一跳"，`proxy_read_timeout` ≥ Go 最慢接口
- [ ] app 容器端口不映射公网；mysql 无 `ports:` 映射
- [ ] 独立部署时：TLS（autocert）已配
- [ ] 有 SSE/下载接口时：重新审读第 6 节
- [ ] 配完跑第 7 节的自测
