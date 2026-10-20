# 分页令牌：offset token 与 cursor token

本文解释 `internal/user/service.go` 中 `page_token` 的两种实现方式的原理、取舍与升级路径。
契约层的背景见 [09-user-service.md](./init/09-user-service.md)（AIP 分页契约）与 [next-step.md](./next-step.md)（分页增强条目）。

---

## 1. 概念分层：三个词不在同一层

```
契约层（API 长什么样）：   page_token / next_page_token    ← AIP-158 标准字段名
载体层（token 是什么）：   opaque token，客户端不解析内容
实现层（怎么定位数据）：   offset 定位  vs  cursor 定位    ← 真正的区别在这里
```

- **offset 和 cursor** 是两种"定位实现"——回答"下一批数据从哪来"
- **token** 是"封装载体"——把定位信息装进一个字符串递给客户端
- **next_page_token** 只是 AIP 给这个载体起的标准字段名

AIP-158 对 token 内容刻意不表态：客户端必须把 token 当不透明字符串原样传回。
因此服务端可以随时把信封里的 offset 换成 cursor，契约纹丝不动。

## 2. 两种定位实现

### offset 定位："给我从第 N 行开始的 20 条"

```sql
SELECT * FROM users ORDER BY id LIMIT 20 OFFSET 100;
```

定位依据是**行号**——相对于结果集开头的绝对位置。

### cursor 定位："给我排在 id=87 之后的 20 条"

```sql
SELECT * FROM users WHERE id > 87 ORDER BY id LIMIT 20;
```

定位依据是**锚点**——上一批最后一条记录的真实坐标。

### 对比

| | offset token | cursor token |
| --- | --- | --- |
| 定位方式 | 第 N 行（绝对位置） | 锚点之后（相对某条记录） |
| 翻页窗口内数据插入 | 重数据（结果集后移） | 稳定 |
| 翻页窗口内数据删除 | 漏数据 | 稳定 |
| 深翻页性能 | O(N)：OFFSET 100000 要先扫过 10 万行 | 恒定：`(id)` 走索引直达 |
| 跳页能力 | 支持（第 50 页 = offset 980） | 不支持，只能顺次前进 |
| 排序键要求 | 无 | 唯一且可比较（通常是自增 id） |
| token 内容 | `"100"` | `"87"`，或 base64 / 签名串 |
| 可调试性 | 直观（肉眼可读） | 黑盒 |
| 使用者 | 传统后台管理系统 | GitHub GraphQL、Stripe、Slack、Firestore |

## 3. 本项目第一版：offset token

`service.go` 的 `ListUsers` 就是全部实现，三段各司其职：

**解析（进）**：page_token 是偏移量的十进制字符串

```go
offset := 0
if pageToken != "" {
    offset, err = strconv.Atoi(pageToken)
    if err != nil || offset < 0 {
        return nil, "", ErrInvalidPageToken   // 非数字、负数都拒绝
    }
}
```

**查询（中）**：换算成 offset/limit 交给 Repository

```go
users, total, err := s.repo.List(ctx, offset, pageSize)
```

**生成（出）**：还有剩余数据就把下一页偏移量写进 next_page_token

```go
next := ""
if offset+pageSize < int(total) {
    next = strconv.Itoa(offset + pageSize)
}
```

一轮闭环：响应给 `next_page_token = "40"`，客户端下一轮原样带回，`Atoi("40")` 还原 `offset=40`。

### 这个版本的能力边界

- `total` 只在服务端内部用于判断"还有没有下一页"，不进响应体（省掉客户端对 total 的依赖，AIP 也不要求返回总数）
- token 是明文 offset，客户端可以伪造 `page_token=0`——当前无权限敏感数据时无害，但升级方向之一是加签名
- 翻页窗口内有人增删用户时，后续页会重/漏数据（见下文升级动机）

## 4. 升级：cursor token

### 什么时候该升级

出现以下任一信号时：

1. 列表数据高频增删，且调用方要求"不重不漏"（消息流、动态、订单流水）
2. 表变大，深翻页变慢（offset 代价随深度线性增长）
3. 需要全量遍历（数据同步、导出），而 offset 深翻页不可承受
4. token 需要防伪造（编码 + 签名）

### Repository 层的改动

Repository 接口把 `List(ctx, offset, limit)` 换成锚点语义：

```go
// afterID 为 0 表示第一页；非 0 表示"取 id > afterID 的下一批"
List(ctx context.Context, afterID int64, limit int) ([]*User, int64, error)
```

```go
func (r *repository) List(ctx context.Context, afterID int64, limit int) ([]*User, error) {
    q := r.db.WithContext(ctx).Model(&User{}).Order("id ASC").Limit(limit)
    if afterID > 0 {
        q = q.Where("id > ?", afterID)
    }
    var users []*User
    if err := q.Find(&users).Error; err != nil {
        return nil, err
    }
    return users, nil
}
```

### Service 层的改动

token 里装的不再是"第几行"，而是"上一批最后一条的 id"，用 base64 薄薄包一层维持 opaque 语义：

```go
// 解析：base64 → lastID
func decodeCursor(token string) (int64, error) {
    raw, err := base64.RawURLEncoding.DecodeString(token)
    if err != nil {
        return 0, ErrInvalidPageToken
    }
    id, err := strconv.ParseInt(string(raw), 10, 64)
    if err != nil || id < 0 {
        return 0, ErrInvalidPageToken
    }
    return id, nil
}

// 生成：最后一条的 id → base64
func encodeCursor(lastID int64) string {
    return base64.RawURLEncoding.EncodeString([]byte(strconv.FormatInt(lastID, 10)))
}

func (s *service) ListUsers(ctx context.Context, pageSize int, pageToken string) ([]*User, string, error) {
    // pageSize 默认值与钳制逻辑不变
    var afterID int64
    if pageToken != "" {
        var err error
        if afterID, err = decodeCursor(pageToken); err != nil {
            return nil, "", err
        }
    }
    users, err := s.repo.List(ctx, afterID, pageSize)
    if err != nil {
        return nil, "", err
    }
    next := ""
    if len(users) == pageSize {   // 拿满一页才可能有下一页
        next = encodeCursor(users[len(users)-1].ID)
    }
    return users, next, nil
}
```

注意两个细节：

- **"还有没有下一页"的判断变了**：cursor 式不再查 total（省掉 COUNT），用"本页拿满 pageSize 条"作为启发条件。最后一页恰好凑满时响应会多一次空页请求——这是用一次廉价请求换掉一次昂贵的 COUNT
- **契约零变化**：proto 里 `page_token` / `next_page_token` 字段名不动，客户端（只当 token 是不透明串）一行不用改

### 进阶：防伪造的签名 cursor

cursor 若需要防篡改（防止客户端构造 token 绕过服务端过滤条件），在编码内容里附加 HMAC：

```
payload = lastID + "|" + filterFingerprint
token   = base64(payload) + "." + base64(HMAC-SHA256(payload, secret))
```

服务端验签失败即返回 `ErrInvalidPageToken`。签名密钥可复用 config 里的 JWT Secret 或单独加一项。

## 5. 选型决策

问一个问题：**调用方需不需要"随机跳到任意一页"？**

- **需要**（运营后台、el-pagination 页码跳转）→ 页码式 page/total，不适合 token 式
- **不需要**，只是顺次消费（App 列表、无限下拉、微服务调用、数据同步）→ token 式，小表用 offset、大表或高频增删用 cursor

两种门面可以并存：Repository 层保持 offset/limit 或锚点语义，service 之上出两个 handler（AIP 式 `/api/v1/users?page_token=` 与页码式 `/admin/users?page=`），互不干扰。
