# 01-create-project.md —— 创建项目骨架

## 1. 这一阶段做什么

创建空项目目录 `go-api-template`，并写入第一个文件 `.gitignore`。

## 2. 为什么要做

- 一切工程从目录开始。给项目起一个和 Go module 名一致的名字（`go-api-template`），后面 `go mod init` 时就不容易混。
- `.gitignore` 是第一个该写的文件，而不是最后一个：它决定了"什么该进版本库、什么不该进"。**先定边界再写代码**，就不会出现"构建产物不小心被提交"这种要靠 `git rm --cached` 补救的事故。
- 这个项目的 `.gitignore` 里有一条反直觉的规则：`gen/go/`（自动生成的代码）**故意不忽略**。大部分教程会说"生成代码不要提交"，但本项目刻意相反——生成代码提交进仓库后，使用者 clone 下来不需要先装 buf、先执行 `buf generate` 就能直接编译运行；同时所有人、CI 使用的生成代码完全一致，不会因为本地 protoc-gen-go 版本不同而悄悄产生差异。`buf` 的配置（`buf.yaml` / `buf.gen.yaml`）真正管住"怎么生成"，所以提交生成代码是安全且划算的。

## 3. 手动执行什么命令

在你存放代码的目录（本教程示例为 `D:\workSpace\dd`）下执行：

```powershell
# 创建项目目录并进入；从现在起，所有 go 命令都必须在这个目录下运行
mkdir go-api-template
cd go-api-template
```

Git 是可选但推荐的。如果想从第一行就保留完整历史，执行：

```powershell
git init
```

不执行也没关系，本教程其余步骤不依赖 Git；以后任何时候 `git init` 都来得及。

## 4. 创建什么文件

```text
go-api-template/.gitignore
```

## 5. 把什么代码写进去

文件：`go-api-template/.gitignore`

```text
# 构建产物
bin/
*.test
coverage.out

# 编辑器
.idea/
.vscode/

# 日志
*.log

# 注意：gen/go 下的生成代码是“故意提交”的，
# 这样使用者 clone 后不需要先执行 buf generate 也能编译通过。
```

## 6. 解释关键代码

逐段看这个 `.gitignore` 在忽略什么、为什么忽略：

- **`bin/`**：`make build` 会把编译出的可执行文件放到 `bin/server.exe`。编译产物可以随时由源码重建，不该进版本库。
- **`*.test`**：`go test -c` 编译出的测试二进制的默认后缀，同属可重建产物。
- **`coverage.out`**：`go test -coverprofile=coverage.out ./...` 生成的覆盖率数据文件。
- **`.idea/`、`.vscode/`**：编辑器/IDE 的个人配置，每个人机器上的内容不同，进版本库只会制造无意义 diff。
- **`*.log`**：运行日志属于运行时产物，可能还包含敏感信息。
- **没有忽略 `gen/go/`**：这是刻意的（原因见第 2 节），所以文件末尾专门写了注释防止后人"好心"把它加回去。

和其他文件的关系：此刻 `.gitignore` 是项目里唯一的文件。下一章（[02](./02-go-module.md)）会在根目录生成 `go.mod` / `go.sum`，它们**必须**进版本库（依赖清单与校验和），所以不需要任何额外配置——现有 `.gitignore` 不会误伤它们。

## 7. 做一个小检查

```powershell
Get-ChildItem -Force
```

执行成功后你应该看到：列表里有 `.gitignore`（`-Force` 是为了显示以点开头的文件）。

如果执行了 `git init`，再跑一次：

```powershell
git status
```

你应该看到 `Untracked files: .gitignore`——说明 Git 已经在工作，且忽略规则本身处于待提交状态。

> **本阶段成果：** 项目目录 `go-api-template` 已建立，`.gitignore` 定下了"产物不进库、生成代码进库"的边界。下一章初始化 Go module 并把所有第三方依赖一次性装齐。
