package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	App       AppConfig       `yaml:"app"`
	HTTP      HTTPConfig      `yaml:"http"`
	Log       LogConfig       `yaml:"log"`
	DB        DBConfig        `yaml:"db"`
	CDC       CDCConfig       `yaml:"cdc"`
	Migration MigrationConfig `yaml:"migration"`
	Flashback FlashbackConfig `yaml:"flashback"`
	External  ExternalConfig  `yaml:"external"`
	Terminal  TerminalConfig  `yaml:"terminal"`
}

type AppConfig struct {
	Name        string `yaml:"name"`
	RunMode     string `yaml:"run_mode"`
	JWTSecret   string `yaml:"jwt_secret"`
	JWTTTLHours int    `yaml:"jwt_ttl_hours"`
}

type HTTPConfig struct {
	Host         string `yaml:"host"`
	Port         int    `yaml:"port"`
	ReadTimeout  int    `yaml:"read_timeout"`
	WriteTimeout int    `yaml:"write_timeout"`
	PidFile      string `yaml:"pid_file"`
	ServeStatic  bool   `yaml:"serve_static"`
}

type LogConfig struct {
	Level string `yaml:"level"`
}

type DBConfig struct {
	Driver string `yaml:"driver"`
	DSN    string `yaml:"dsn"`
}

type CDCConfig struct {
	Enabled      bool   `yaml:"enabled"`
	DebeziumURL  string `yaml:"debezium_url"`
	KafkaBrokers string `yaml:"kafka_brokers"`
}

type MigrationConfig struct {
	Enabled     bool   `yaml:"enabled"`
	OSSEndpoint string `yaml:"oss_endpoint"`
}

type FlashbackConfig struct {
	WorkDir          string `yaml:"workdir"`
	ArchiveDir       string `yaml:"archive_dir"`
	MaxWALBytes      int64  `yaml:"max_wal_bytes"`
	TencentSecretID  string `yaml:"tencent_secret_id"`
	TencentSecretKey string `yaml:"tencent_secret_key"`
	TencentRegion    string `yaml:"tencent_region"`
}

type ExternalConfig struct {
	DolphinSchedulerURL string `yaml:"dolphinscheduler_url"`
	StarRocksFEURL      string `yaml:"starrocks_fe_url"`
}

// TerminalConfig WebTTY / SSH 终端（迁入 neptune-terminal 精简核心）
type TerminalConfig struct {
	Enabled         bool   `yaml:"enabled"`
	SSHUser         string `yaml:"ssh_user"`
	SSHIdentityFile string `yaml:"ssh_identity_file"`
	ScriptsDir      string `yaml:"scripts_dir"`
	MaxLifetimeSec  int    `yaml:"max_lifetime_sec"`
	StaticDir       string `yaml:"static_dir"`
}

func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	cfg := &Config{}
	if err := yaml.Unmarshal(b, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.HTTP.Host == "" {
		cfg.HTTP.Host = "0.0.0.0"
	}
	if cfg.HTTP.Port == 0 {
		cfg.HTTP.Port = 8800
	}
	if cfg.HTTP.ReadTimeout <= 0 {
		cfg.HTTP.ReadTimeout = 30
	}
	if cfg.HTTP.WriteTimeout <= 0 {
		cfg.HTTP.WriteTimeout = 120
	}
	if cfg.App.JWTTTLHours <= 0 {
		cfg.App.JWTTTLHours = 24
	}
	if cfg.App.JWTSecret == "" {
		cfg.App.JWTSecret = "auto-dba-dev-secret"
	}
	if cfg.DB.Driver == "" {
		cfg.DB.Driver = "postgres"
	}
	if cfg.Flashback.WorkDir == "" {
		cfg.Flashback.WorkDir = "/tmp/auto-dba-flashback"
	}
	if cfg.Flashback.MaxWALBytes <= 0 {
		cfg.Flashback.MaxWALBytes = 1 << 30
	}
	if v := strings.TrimSpace(os.Getenv("FLASHBACK_TENCENT_SECRET_ID")); v != "" {
		cfg.Flashback.TencentSecretID = v
	}
	if v := strings.TrimSpace(os.Getenv("FLASHBACK_TENCENT_SECRET_KEY")); v != "" {
		cfg.Flashback.TencentSecretKey = v
	}
	if v := strings.TrimSpace(os.Getenv("FLASHBACK_TENCENT_REGION")); v != "" {
		cfg.Flashback.TencentRegion = v
	}
	applyTerminalDefaults(cfg, path)
	return cfg, nil
}

func applyTerminalDefaults(cfg *Config, configPath string) {
	root := "."
	if abs, err := filepath.Abs(filepath.Dir(configPath)); err == nil {
		// config/config.yaml → repo root
		if filepath.Base(abs) == "config" {
			root = filepath.Dir(abs)
		} else {
			root = abs
		}
	}
	t := &cfg.Terminal
	if t.SSHUser == "" {
		t.SSHUser = "sysadmin"
	}
	if t.ScriptsDir == "" {
		t.ScriptsDir = filepath.Join(root, "scripts", "terminal")
	}
	if t.StaticDir == "" {
		t.StaticDir = filepath.Join(root, "assets", "terminal")
	}
	if t.MaxLifetimeSec <= 0 {
		t.MaxLifetimeSec = 1800
	}
	if t.SSHIdentityFile == "" {
		t.SSHIdentityFile = filepath.Join(root, "config", "terminal", "id_rsa")
	}
}
