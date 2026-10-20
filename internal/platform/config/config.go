// Package config 负责读取启动配置。
// 所有环境相关的信息（监听地址、DSN、JWT 密钥）都集中在这里，
// 业务代码不允许自己读配置文件或环境变量。
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config 是配置文件的顶层结构，对应 configs/config.yaml。
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	JWT      JWTConfig      `yaml:"jwt"`
}

type ServerConfig struct {
	// Addr 是 HTTP 监听地址，例如 ":8080"。
	Addr string `yaml:"addr"`
}

type DatabaseConfig struct {
	// DSN 是 go-sql-driver/mysql 的连接字符串。
	DSN string `yaml:"dsn"`
}

type JWTConfig struct {
	Secret   string `yaml:"secret"`
	TTLHours int    `yaml:"ttl_hours"`
	Issuer   string `yaml:"issuer"`
}

// Load 从指定路径读取 YAML 配置并填充默认值。
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: 读取配置文件失败: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: 解析配置文件失败: %w", err)
	}
	setDefaults(&cfg)
	return &cfg, nil
}

// setDefaults 给必填但可能缺失的配置一个可用的默认值。
// 显式写在这里，而不是散落在使用方代码里。
func setDefaults(cfg *Config) {
	if cfg.Server.Addr == "" {
		cfg.Server.Addr = ":8080"
	}
	if cfg.JWT.TTLHours <= 0 {
		cfg.JWT.TTLHours = 24
	}
	if cfg.JWT.Issuer == "" {
		cfg.JWT.Issuer = "go-api-template"
	}
}
