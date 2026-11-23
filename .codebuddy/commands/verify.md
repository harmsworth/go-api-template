---
description: 依次执行 gofmt 检查、go vet、go test 与 buf lint，汇总失败项与修复建议
aliases: ["check", "ci"]
allowed-tools: Bash, Read, Grep, Glob
---

按顺序执行以下检查，任一步失败也继续执行后续步骤，最后统一汇总：

```bash
gofmt -l .
go vet ./...
go test ./...
buf lint
```

执行要求：

1. `gofmt -l .` 会递归列出未格式化的 Go 文件。不要自动改写文件，先列出清单。
2. `go test ./...` 中集成测试在未设置 `MYSQL_TEST_DSN` 时是 **SKIP**。汇总时要明确写出"集成测试已跳过"，不要把 SKIP 当作通过，也不要把 `ok` 直接当成全部覆盖。
3. 若用户希望连真实 MySQL 一起验证，再执行（PowerShell，仅当前会话生效）：

```powershell
$env:MYSQL_TEST_DSN = "root:123456@tcp(localhost:3306)/todo_x?charset=utf8mb4&parseTime=True&loc=UTC"
go test ./internal/user/ -run "TestUserRepositoryIntegration|TestUserRepositoryQueryIntegration|TestTokenRepositoriesIntegration" -v
```

4. `buf lint` 失败时只报告 `buf.yaml` 规则与 proto 行号；本仓库的 3 条 except 是刻意保留的，不要建议删除。

输出格式：

```text
## 检查结果
| 步骤 | 结果 | 失败项 |
| --- | --- | --- |

## 失败详情
- 文件:行 — 原因 → 修复建议
```

只做检查与建议，不要在当前命令里顺手改代码；如果用户明确要求修复，再逐项处理并重新执行对应步骤。
