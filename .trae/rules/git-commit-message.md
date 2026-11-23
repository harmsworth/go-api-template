---
scene: git_message
---

# Git 提交信息规则

- 使用 Conventional Commits：`type(scope): summary`。
- `type` 只使用：`feat`、`fix`、`docs`、`refactor`、`test`、`chore`。
- `scope` 可选，使用小写模块名，例如 `user`、`migrations`、`proto`、`docs`。
- `summary` 使用英文、祈使句、首字母小写、结尾不加句号，长度不超过 72 个字符。
- 正文说明“为什么”以及用户可见的行为变化；涉及迁移或 proto 变更时必须写明。
- 不把密码、token、API Key 或内部地址写入提交信息。
- 一个提交只做一件事；格式化和无关重构不要混入功能提交。

示例：

```text
feat(user): add refresh token rotation

- add refresh_tokens migration and repository
- issue access + refresh token on register/login
- revoke all refresh tokens on password change

Tests: go test ./...
```
