---
description: 重新生成 proto 的 Go 代码并验证可编译：buf lint → buf generate → go build
aliases: ["proto-gen", "generate"]
allowed-tools: Bash, Read, Grep, Glob
---

按顺序执行：

```bash
buf lint
buf generate
go build ./...
git --no-pager diff --stat gen/go gen/openapi
```

要求：

1. 先 lint 再 generate；lint 失败时停止，不要生成不合规代码。
2. `buf generate` 使用 `buf.gen.yaml` 的两个本地插件：`protoc-gen-go` 输出 `gen/go`（`paths=source_relative`），`protoc-gen-openapi`（gnostic）输出 `gen/openapi/openapi.yaml`。缺少插件时分别提示 `go install google.golang.org/protobuf/cmd/protoc-gen-go@latest` 与 `go install github.com/google/gnostic/cmd/protoc-gen-openapi@latest`，并确认 `$(go env GOPATH)/bin` 在 PATH。
3. `buf generate` 需要联网拉取 `buf.build/bufbuild/protovalidate` 与 `buf.build/googleapis/googleapis`。网络失败时提示配置 `HTTPS_PROXY` 或 `buf dep update` 后重试，不要伪造生成结果。
4. `gen/go` 与 `gen/openapi` 都是**故意提交入库**的生成物。生成后必须展示变更文件，并提醒用户一并提交；不要把它们加进 `.gitignore`。
5. 若 OpenAPI 文档的 `paths` 为空，说明某个 rpc 缺少 `google.api.http` 注解，先补注解再重新生成。
6. 如果只改了 Go 代码、没有改 proto，直接说明"无需生成"并结束。

输出：lint 结果、生成/删除的文件列表、`go build ./...` 结果、下一步提示（提交 `gen/go` + `gen/openapi` + 同步路由与文档）。
