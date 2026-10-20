package user

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// TestUserRepositoryIntegration 是 Repository + 真实 MySQL 的集成测试。
//
// 运行前提（详见 docs/init/13-testing.md）：
//  1. 本地 MySQL 已启动（docker compose up -d 或本机 MySQL）
//  2. 迁移已执行（make migrate-up），否则表不存在
//  3. 设置环境变量 MYSQL_TEST_DSN，例如（PowerShell）：
//     $env:MYSQL_TEST_DSN = "root:123456@tcp(localhost:3306)/todo_x?charset=utf8mb4&parseTime=True&loc=UTC"
//
// 未设置 MYSQL_TEST_DSN 时测试自动跳过，保证 go test ./... 在任何机器都能通过。
// 这不是 Testcontainers 那类重量级方案，就是一个“有真实 MySQL 就跑”的普通测试。
func TestUserRepositoryIntegration(t *testing.T) {
	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("未设置 MYSQL_TEST_DSN，跳过集成测试")
	}

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("连接 MySQL 失败: %v", err)
	}

	repo := NewRepository(db)
	ctx := context.Background()

	// 用纳秒时间戳拼邮箱，保证测试可以反复运行。
	email := fmt.Sprintf("it-%d@example.com", time.Now().UnixNano())

	// Create
	u := &User{Email: email, PasswordHash: "fake-hash", DisplayName: "集成测试"}
	if err := repo.Create(ctx, u); err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if u.ID == 0 {
		t.Fatal("Create 后应回填自增主键 ID")
	}

	// FindByEmail
	got, err := repo.FindByEmail(ctx, email)
	if err != nil {
		t.Fatalf("FindByEmail 失败: %v", err)
	}
	if got.ID != u.ID || got.DisplayName != "集成测试" {
		t.Fatalf("FindByEmail 结果不符: %+v", got)
	}

	// FindByID
	byID, err := repo.FindByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("FindByID 失败: %v", err)
	}
	if byID.Email != email {
		t.Fatalf("FindByID 结果不符: %+v", byID)
	}

	// Update
	byID.DisplayName = "已改名"
	if err := repo.Update(ctx, byID); err != nil {
		t.Fatalf("Update 失败: %v", err)
	}
	after, err := repo.FindByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("Update 后查询失败: %v", err)
	}
	if after.DisplayName != "已改名" {
		t.Fatalf("Update 未生效: %+v", after)
	}

	// List
	users, total, err := repo.List(ctx, 0, 10)
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if total < 1 || len(users) < 1 {
		t.Fatalf("List 应至少返回 1 条，实际 total=%d len=%d", total, len(users))
	}

	// 重复邮箱应返回 ErrEmailAlreadyExists。
	if err := repo.Create(ctx, &User{Email: email, PasswordHash: "x", DisplayName: "dup"}); !errors.Is(err, ErrEmailAlreadyExists) {
		t.Fatalf("重复邮箱应返回 ErrEmailAlreadyExists，实际 %v", err)
	}

	// Delete + NotFound
	if err := repo.Delete(ctx, u.ID); err != nil {
		t.Fatalf("Delete 失败: %v", err)
	}
	if _, err := repo.FindByID(ctx, u.ID); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("删除后查询应返回 ErrUserNotFound，实际 %v", err)
	}
}
