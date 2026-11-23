---
name: codebuddy-config
overview: 为 go-api-template 生成根目录 CODEBUDDY.md，并在 .codebuddy/ 下补齐 rules、commands、agents、skills 与 settings.json（gofmt hook），使后续 Agent 能按本项目既有约定工作。
todos:
  - id: write-codebuddy-md
    content: 编写根目录 CODEBUDDY.md：常用命令表与不超过 1600 词的架构说明
    status: completed
  - id: write-rules
    content: 创建 .codebuddy/rules 下架构边界、schema 契约、文档同步三条 RULE.mdc
    status: completed
    dependencies:
      - write-codebuddy-md
  - id: write-commands
    content: 创建 /verify、/gen、/migrate-new、/api-check 四个斜杠命令文件
    status: completed
    dependencies:
      - write-codebuddy-md
  - id: write-agent-skill
    content: 创建 code-reviewer 子代理与 add-feature 技能及其检查清单
    status: completed
    dependencies:
      - write-codebuddy-md
  - id: write-hooks
    content: 创建 .codebuddy/settings.json 与 hooks/gofmt.ps1 并校验 JSON 可解析
    status: completed
  - id: audit-config
    content: 用 [subagent:code-explorer] 审计所有引用的路径、命令与测试名是否真实存在
    status: completed
    dependencies:
      - write-rules
      - write-commands
      - write-agent-skill
      - write-hooks
---

## 用户需求

为 `go-api-template` 仓库建立一套可直接被后续 CodeBuddy Agent 使用的项目级配置，使其理解“本项目是什么、怎么跑、边界在哪”，并复用本项目既有约定而不是套用通用最佳实践。

## 交付内容

1. **根目录 `CODEBUDDY.md`**（官方自动全文加载位置），必须以 `# CODEBUDDY.md This file provides guidance to CodeBuddy when working with code in this repository.` 开头，包含：

- 常用命令：启动依赖、迁移、运行、构建、lint、全量测试、单测、集成测试、proto 校验/生成、新建迁移、检查数据库；每条说明不超过 100 词。
- 架构说明（不超过 1600 词）：启动装配链、请求分层、接口由消费者定义、proto 契约与 Gin 路由人工同步、错误映射与状态码、认证与令牌、迁移与模型契约、配置与 platform 能力、生成代码入库、三层测试、扩展新 Feature 的路径。
- 本项目不可违背的约定（禁 AutoMigrate、迁移 up/down 成对、改 proto 必须重新生成并提交、改行为要同步 docs）。
- 不写通用套话、不罗列可自行发现的文件清单。

2. **`.codebuddy/` 结构化配置**

- `rules/`：3 条项目规则（RULE.mdc，Markdown + frontmatter），分别固化架构边界、schema/契约变更纪律、文档同步要求。
- `commands/`：`/verify`、`/gen`、`/migrate-new`、`/api-check` 四个可直接执行的斜杠命令。
- `agents/`：项目专属 code-reviewer 子代理（agentic，独立上下文）。
- `skills/add-feature/`：新增一个 Feature 的跨层工作流技能（含 references 检查清单）。
- `settings.json` + `hooks/gofmt.ps1`：PostToolUse 对编辑后的 `.go` 文件执行 gofmt，失败不阻塞。

## 验收标准

- 文档中出现的每条命令都能在 Makefile、README 或 docs 中找到依据，不编造路径与参数。
- 所有 frontmatter 字段符合官方规范，JSON 可被解析，hook 脚本在未匹配到 `.go` 文件时静默退出 0。
- 不修改任何业务代码、迁移文件或现有文档。

## 技术方案

### 定位与原则

本任务只新增“给 Agent 读的配置与文档”，不触碰 `cmd/`、`internal/`、`proto/`、`migrations/`、`configs/`。所有内容必须从已核实的源码与文档推导，禁止写入仓库中不存在的能力（如 CI、Docker 镜像、golangci-lint、Redis）。

### 文件格式依据（来自官方文档抓取）

- Rules：`.codebuddy/rules/<name>/RULE.mdc`，frontmatter `description` / `alwaysApply` / `enabled` / `updatedAt` / `provider`。`alwaysApply: true` 每次会话全文注入；非 always 规则先加载名称与描述。建议 always 规则控制在 3–5 条、单条 ≤500 行。
- Commands：frontmatter `description` / `aliases` / `allowed-tools`；项目级放在 `.codebuddy/commands/*.md`。正文避免依赖未在文档中定义的参数占位符。
- Agents：`.codebuddy/agents/*.md`，frontmatter `name` / `description` / `agentMode` / `enabled` / `enabledAutoRun` / `tools`；正文即 System Prompt。
- Skills：`.codebuddy/skills/<name>/SKILL.md`，必填 `name` / `description`，可选 `allowed-tools` / `disable`；可选 `references/` 存放按需加载的长文档。
- Hooks：`.codebuddy/settings.json`，结构 `{"hooks":{"<Event>":[{"matcher":"<regex>","hooks":[{"type":"command","command":"...","timeout":N}]}]}}`；matcher 支持 CLI/IDE 工具名双向别名；脚本从 stdin 读 JSON，退出码 0 继续、1 警告、2 阻塞。

### 各产物设计

**1. `CODEBUDDY.md`（根目录）**
章节固定为：项目定位 → 常用命令 → 架构总览 → 关键契约与约定 → 修改指南 → CodeBuddy 配置位置。命令用表格或代码块，逐条标注“何时用”；架构部分按“启动装配 → 请求旅程 → 契约与运行时 → 数据与迁移 → 认证 → 配置 → 测试”组织，强调那些必须读多个文件才能得出的结论：

- 接口由消费者定义：`Repository` 在 `service.go`、`Service` 在 `handler.go`，实现结构体不导出。
- proto 是契约、`router.go` 是运行时绑定，新增 RPC 必须同时改两处；Gin 不支持段中冒号，动作型接口用 `/users/:id/password` 这类路径。
- 错误只走 `mapError`，响应体固定 `{"error":{"code","message"}}`；201 只用于集合创建 `POST /users`。
- refresh token 只存 SHA-256、轮换在事务里、改密/重置吊销全部；重置令牌 30 分钟一次性；开发环境邮件写日志。
- 分页是 base64 keyset cursor 且与 `order_by` 绑定，字段走白名单、值走占位符。
- `gen/go` 故意入库；`buf.yaml` 显式排除 3 条 AIP 冲突规则。
- 集成测试未设 `MYSQL_TEST_DSN` 时是 SKIP 而非 PASS。

**2. rules（3 条，均 `alwaysApply: true`）**

- `architecture-boundaries`：分层方向 Handler→Service→Repository；业务包不得直接读配置文件或建连；新增 platform 能力必须由 `main.go` 注入；不引入 Wire/gRPC/Redis 等未落地依赖。
- `schema-and-contract`：禁止 AutoMigrate 与启动迁移；迁移必须 `{version}_{desc}.up/down.sql` 成对且只前进不修改历史文件；改 proto 必须 `buf generate` 并提交 `gen/go`，校验规则写在 proto；model 字段与 SQL、索引名保持一致。
- `docs-sync`：本仓库文档是交付物，改行为要同步 `README.md`、`docs/architecture.md`、对应 `docs/level1` / `docs/level2` 章节；`docs/next-step.md` 的“按需引入”原则不得被提前打破。

**3. commands（4 条）**

- `/verify`：`gofmt -l .`、`go vet ./...`、`go test ./...`、`buf lint`，失败时按“文件→原因→修复建议”汇总；明确说明集成测试可能被跳过。
- `/gen`：先 `buf lint`，再 `buf generate`，然后 `go build ./...` 验证生成代码可编译，最后提示提交 `gen/go`。
- `/migrate-new`：读取 `migrations/` 现有最大版本号，生成 `-seq` 的下一个 up/down 对，up 写变更、down 写可抵消语句，并提醒同步 model 与文档。
- `/api-check`：逐条比对 `proto/user/v1/user.proto` 的 rpc 与 `internal/app/router.go` 注册的路由，输出缺失/多余/路径不一致清单，不自动改代码。

**4. agents/code-reviewer**
`agentMode: agentic`、`enabled: true`、`enabledAutoRun: false`（审查只读，不改文件）、`tools: Read, Grep, Glob`。正文给出本项目专用检查清单：分层越界、`mapError` 覆盖、SQL 拼接与白名单、游标与排序绑定、令牌哈希与事务、权限判断位置、迁移成对性、proto/路由漂移、测试层级是否匹配、文档是否同步。输出按“阻断 / 建议 / 说明”分级并附文件行号。

**5. skills/add-feature**
`description` 明确触发条件：“新增或修改一个用户可见的 API 能力时使用”。正文按顺序给出步骤：改 proto → `buf generate` → 新增迁移对 → 更新 model → 在 `service.go` 定义/扩展接口 → 实现 repository → 实现 service 规则 → handler 绑定与错误映射 → `router.go` 注册 → 补 service/handler 测试（必要时集成测试）→ 同步文档。`references/checklist.md` 存放逐项勾选清单，避免 SKILL.md 膨胀。

**6. hooks**

- `settings.json` 仅注册 `PostToolUse`，matcher 为 `Write|Edit|write_to_file|replace_in_file`，command 使用 `powershell -NoProfile -ExecutionPolicy Bypass -File "$CODEBUDDY_PROJECT_DIR/.codebuddy/hooks/gofmt.ps1"`，timeout 20。
- `gofmt.ps1`：从 stdin 读取 JSON，兼容 `tool_input.file_path` / `path` / `target_file` 三种字段；仅处理 `.go` 且文件存在的情况；调用 `gofmt -w`；任何异常写 stderr 并 `exit 0`，绝不阻塞编辑。

### 实施注意事项

- 写入前先确认 `.codebuddy` 不存在，避免覆盖用户配置；创建目录时只建实际使用的子目录。
- `CODEBUDDY.md` 中文书写，代码/路径/命令保持原样；不重复 README 已有的大段内容，只保留 Agent 决策所需结论。
- 现有 git 工作区存在未提交的 `docs/init → docs/level1` 重命名，新增文件不得触碰这些改动。
- 文档中引用路径一律用仓库相对路径，命令区分 PowerShell 语法（`$env:`）与 make 语法。
- hooks 依赖 Windows PowerShell；在 `CODEBUDDY.md` 中注明非 Windows 环境需替换该条 command，不额外引入跨平台脚本。

### 目录结构

```text
go-api-template/
├── CODEBUDDY.md                                  # [NEW] 项目总指南：命令 + 架构 + 契约 + 配置索引
└── .codebuddy/
    ├── settings.json                             # [NEW] PostToolUse gofmt hook 注册
    ├── hooks/
    │   └── gofmt.ps1                             # [NEW] 读取 stdin JSON，对 .go 文件执行 gofmt -w，异常不阻塞
    ├── rules/
    │   ├── architecture-boundaries/RULE.mdc      # [NEW] 分层边界、依赖注入方式、禁止提前引入组件
    │   ├── schema-and-contract/RULE.mdc          # [NEW] 禁 AutoMigrate、迁移成对、proto 与 gen/go 同步
    │   └── docs-sync/RULE.mdc                    # [NEW] 行为变更必须同步 README/architecture/level1/level2
    ├── commands/
    │   ├── verify.md                             # [NEW] gofmt + go vet + go test + buf lint 并汇总
    │   ├── gen.md                                # [NEW] buf lint → generate → go build 验证
    │   ├── migrate-new.md                        # [NEW] 生成下一个版本的 up/down 迁移对
    │   └── api-check.md                          # [NEW] 比对 proto rpc 与 router 注册，输出漂移清单
    ├── agents/
    │   └── code-reviewer.md                      # [NEW] 项目专属审查代理（agentic，只读工具）
    └── skills/
        └── add-feature/
            ├── SKILL.md                          # [NEW] 跨层新增能力的标准工作流
            └── references/
                └── checklist.md                  # [NEW] 逐层勾选清单，按需加载
```

## Agent Extensions

### SubAgent

- **code-explorer**
- Purpose: 在配置写入后做一次只读审计，核对 `CODEBUDDY.md` 与 commands/rules 中引用的每个路径、Makefile target、测试函数名是否真实存在。
- Expected outcome: 输出一份“引用不存在或已过期”的清单；若无问题则明确确认全部引用可解析。