# 本地开发常用命令。Windows 用户如果没装 make，可以直接复制对应命令到终端执行。

# golang-migrate 使用的 DSN（注意前缀是 mysql://）
MIGRATE_DSN ?= mysql://root:123456@tcp(localhost:3306)/todo_x?charset=utf8mb4&parseTime=True&loc=UTC

.PHONY: buf-lint buf-generate test lint build run migrate-up migrate-down

# 校验 proto 文件
buf-lint:
	buf lint

# 生成 proto 对应的 Go 代码（需要先安装 protoc-gen-go，见 docs/init/06-protobuf-buf.md）
buf-generate:
	buf generate

# 运行所有测试（集成测试需要设置 MYSQL_TEST_DSN，见 docs/init/13-testing.md）
test:
	go test ./...

# 静态检查
lint:
	go vet ./...

# 构建可执行文件到 bin/
build:
	go build -o bin/server.exe ./cmd/server

# 本地启动（先确保 MySQL 已启动且迁移已执行）
run:
	go run ./cmd/server

# 执行迁移（需要先安装 migrate CLI，见 docs/init/05-migrate.md）
migrate-up:
	migrate -path migrations -database "$(MIGRATE_DSN)" up

# 回滚最近一次迁移
migrate-down:
	migrate -path migrations -database "$(MIGRATE_DSN)" down 1
