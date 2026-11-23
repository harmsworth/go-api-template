---
description: 新建一对版本号自增的 up/down 迁移文件，并提示同步 model 与文档
aliases: ["new-migration", "migration"]
allowed-tools: Bash, Read, Grep, Glob
---

目标：在 `migrations/` 下新增 `{version}_{name}.up.sql` 与 `.down.sql`，不改动已有迁移。

步骤：

1. 读取 `migrations/` 现有文件名，取最大版本号（当前为 `000004`）。
2. 如果用户没有给出名称，先询问一个 snake_case 描述（例如 `add_user_avatar`），不要自行编造业务字段。
3. 执行：

```bash
migrate create -ext sql -dir migrations -seq <name>
```

`-seq` 会基于现有最大版本号生成下一个 6 位版本号，并同时创建 up/down 两个空文件。

4. 填写内容前先确认改动属于哪类，并给出对应模板：

```sql
-- up：只包含向前变更
ALTER TABLE users ADD COLUMN avatar_url VARCHAR(512) NOT NULL DEFAULT '';

-- down：必须能完全抵消 up
ALTER TABLE users DROP COLUMN avatar_url;
```

5. 强制检查：
   - down 是否真的能抵消 up（建表↔删表、加列↔删列、加索引↔删索引）。
   - 新列/索引是否与 `internal/user/model.go` 的 GORM tag 一致（长度、`not null`、默认值、索引名）。
   - 是否需要同步 `docs/level1/05-migrate.md`、相关 level2 章节与 `docs/architecture.md`。
6. 不要自动执行 `make migrate-up`；迁移应在用户确认 SQL 后显式执行。若用户要求执行，再运行并核对：

```bash
make migrate-up
```

输出：新文件路径、up/down 内容、需要同步的 model 字段与文档清单。
