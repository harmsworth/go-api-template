// Package database 负责创建 MySQL 连接（*gorm.DB）。
// 连接参数来自 config，业务代码只接收 *gorm.DB，不关心 DSN 细节。
package database

import (
	"fmt"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"go-api-template/internal/platform/config"
)

// New 打开 MySQL 连接并设置连接池参数。
// 注意：这里只“连接”，不执行任何 migration（见 docs/architecture.md）。
func New(cfg *config.Config) (*gorm.DB, error) {
	db, err := gorm.Open(mysql.Open(cfg.Database.DSN), &gorm.Config{
		// 慢查询和错误会以 Warn 级别打到标准输出，便于学习时观察 SQL。
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("database: 连接 MySQL 失败: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("database: 获取底层 sql.DB 失败: %w", err)
	}
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(time.Hour)

	return db, nil
}
