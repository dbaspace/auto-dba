package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Nacos     NacosConfig     `yaml:"nacos"`
	App       AppConfig       `yaml:"app"`
	HTTP      HTTPConfig      `yaml:"http"`
	Frontend  FrontendConfig  `yaml:"frontend"` // 开发态 Vite；可放 Nacos，与后端端口一并管理
	Log       LogConfig       `yaml:"log"`
	DB        DBConfig        `yaml:"db"`
	CDC       CDCConfig       `yaml:"cdc"`
	Migration MigrationConfig `yaml:"migration"`
	Flashback FlashbackConfig `yaml:"flashback"`
	Backup    BackupConfig    `yaml:"backup"`
	External  ExternalConfig  `yaml:"external"`
	Terminal  TerminalConfig  `yaml:"terminal"`
	Services  ServicesConfig  `yaml:"services"`
}

// FrontendConfig 前端开发服务（Vite）；生产静态资源不由此端口提供。
type FrontendConfig struct {
	DevHost string `yaml:"dev_host"` // 默认 127.0.0.1
	DevPort int    `yaml:"dev_port"` // 默认 5174；Nacos 可覆盖
}

// NacosConfig 本地引导：只写如何连配置中心，不含业务密钥正文。
// server_addr 支持逗号分隔多节点，单机与集群同一套客户端逻辑。
type NacosConfig struct {
	Enabled    bool   `yaml:"enabled"`
	ServerAddr string `yaml:"server_addr"` // 例 10.100.113.151:8848 或 a:8848,b:8848
	Namespace  string `yaml:"namespace"`   // Namespace ID；public 填 "" 或 public；autosql 填 UUID
	Group      string `yaml:"group"`
	DataID     string `yaml:"data_id"`
	Username   string `yaml:"username"`
	Password   string `yaml:"password"`
	// Watch 为 nil 时默认开启 ListenConfig；显式 false 关闭热更。
	Watch *bool `yaml:"watch"`
	// RestartOnListenChange 为 true 时，http.port/host 变更会 Exec 自重启。
	RestartOnListenChange bool `yaml:"restart_on_listen_change"`
}

// WatchEnabled 默认 true（未配置 watch 时开启监听）。
func (nc NacosConfig) WatchEnabled() bool {
	if nc.Watch == nil {
		return true
	}
	return *nc.Watch
}

// ServicesConfig 可拆分微服务地址（Pod 互通）。
type ServicesConfig struct {
	DbMonitor ServiceEndpoint `yaml:"db_monitor"`
}

// ServiceEndpoint 独立服务监听/发现配置。
// Mode:
//   - "" / "embed"：主进程内嵌（本地开发默认）
//   - "remote"：主进程反向代理到 URL（K8s/多 Pod）
//   - "off"：关闭监控 API
type ServiceEndpoint struct {
	Mode string `yaml:"mode"`
	URL  string `yaml:"url"`  // 例：http://auto-dba-db-monitor:8801
	Host string `yaml:"host"` // 独立进程监听
	Port int    `yaml:"port"` // 独立进程端口，默认 8801
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
	Driver             string `yaml:"driver"`
	DSN                string `yaml:"dsn"`
	MaxOpenConns       int    `yaml:"max_open_conns"`         // 默认 50；0 用默认
	MaxIdleConns       int    `yaml:"max_idle_conns"`         // 默认 10
	ConnMaxLifetimeSec int    `yaml:"conn_max_lifetime_sec"`  // 默认 1800
	ConnMaxIdleTimeSec int    `yaml:"conn_max_idle_time_sec"` // 默认 300
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

// BackupConfig 备份控制面。
type BackupConfig struct {
	WorkDir         string `yaml:"workdir"`
	AllowEmbedded   bool   `yaml:"allow_embedded"` // 无 Agent 时是否允许平台本机执行 dump
	AgentArtifact   string `yaml:"agent_artifact"` // 平台侧 linux 制品，如 deploy/backup-agent/bin/backup-agent
	AgentRemoteDir  string `yaml:"agent_remote_dir"`
	AgentListenPort int    `yaml:"agent_listen_port"`
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
	// SSHPrivateKey Nacos/远程配置中的 PEM 正文；Load 后物化为 ssh_identity_file，内存中清空
	SSHPrivateKey      string `yaml:"ssh_private_key,omitempty"`
	SSHInsecureHostKey bool   `yaml:"ssh_insecure_host_key"` // true：忽略 known_hosts（仅开发），等同 ssh_host_key_policy=insecure
	SSHHostKeyPolicy   string `yaml:"ssh_host_key_policy"`   // accept-new（默认）| strict | insecure
	SSHKnownHostsFile  string `yaml:"ssh_known_hosts_file"`  // 平台自管 known_hosts，默认 config/terminal/known_hosts
	ScriptsDir         string `yaml:"scripts_dir"`
	MaxLifetimeSec     int    `yaml:"max_lifetime_sec"`
	StaticDir          string `yaml:"static_dir"`
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
	applyNacosEnvOverrides(&cfg.Nacos)
	if cfg.Nacos.Enabled {
		boot := cfg.Nacos
		// 远程可能只改 http.port，Unmarshal 会把同段其它字段置零；先记下再回填
		localHTTP := cfg.HTTP
		localFE := cfg.Frontend
		content, err := fetchNacosConfig(boot)
		if err != nil {
			return nil, fmt.Errorf("nacos: %w", err)
		}
		if strings.TrimSpace(content) == "" {
			return nil, fmt.Errorf("nacos: data_id %q group %q 内容为空", boot.DataID, boot.Group)
		}
		if err := yaml.Unmarshal([]byte(content), cfg); err != nil {
			return nil, fmt.Errorf("nacos: parse yaml: %w", err)
		}
		// 远程 DataId 不应嵌套 nacos 引导；始终保留本地/环境变量引导
		cfg.Nacos = boot
		preserveHTTPFromLocal(&cfg.HTTP, localHTTP)
		preserveFrontendFromLocal(&cfg.Frontend, localFE)
	}
	applyDefaults(cfg, path)
	if err := materializeSSHPrivateKey(cfg, path); err != nil {
		return nil, err
	}
	BindLive(cfg)
	PublishJWT(cfg.App.JWTSecret)
	return cfg, nil
}

func applyDefaults(cfg *Config, path string) {
	if cfg.HTTP.Host == "" {
		cfg.HTTP.Host = "0.0.0.0"
	}
	if cfg.HTTP.Port == 0 {
		cfg.HTTP.Port = 8800
	}
	if cfg.Frontend.DevHost == "" {
		cfg.Frontend.DevHost = "127.0.0.1"
	}
	if cfg.Frontend.DevPort <= 0 {
		cfg.Frontend.DevPort = 5174
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
	if cfg.DB.MaxOpenConns <= 0 {
		cfg.DB.MaxOpenConns = 50
	}
	if cfg.DB.MaxIdleConns <= 0 {
		cfg.DB.MaxIdleConns = 10
	}
	if cfg.DB.MaxIdleConns > cfg.DB.MaxOpenConns {
		cfg.DB.MaxIdleConns = cfg.DB.MaxOpenConns
	}
	if cfg.DB.ConnMaxLifetimeSec <= 0 {
		cfg.DB.ConnMaxLifetimeSec = 1800
	}
	if cfg.DB.ConnMaxIdleTimeSec <= 0 {
		cfg.DB.ConnMaxIdleTimeSec = 300
	}
	if cfg.Flashback.WorkDir == "" {
		cfg.Flashback.WorkDir = "/tmp/auto-dba-flashback"
	}
	if cfg.Flashback.MaxWALBytes <= 0 {
		cfg.Flashback.MaxWALBytes = 1 << 30
	}
	if cfg.Backup.WorkDir == "" {
		cfg.Backup.WorkDir = "/tmp/auto-dba-backup"
		// 未配置 backup 段时默认允许内嵌执行（开发友好）；生产请显式 allow_embedded: false + Agent
		cfg.Backup.AllowEmbedded = true
	}
	if cfg.Backup.AgentRemoteDir == "" {
		cfg.Backup.AgentRemoteDir = "/home/sysadmin/auto-dba/platform-agent"
	}
	if cfg.Backup.AgentListenPort <= 0 {
		cfg.Backup.AgentListenPort = 8090
	}
	if cfg.Backup.AgentArtifact == "" {
		// 优先新名，构建脚本同时产出 platform-agent 与 backup-agent
		cfg.Backup.AgentArtifact = "deploy/backup-agent/bin/platform-agent"
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
	applyDbMonitorServiceDefaults(cfg)
	applyTerminalDefaults(cfg, path)
}

func applyDbMonitorServiceDefaults(cfg *Config) {
	ep := &cfg.Services.DbMonitor
	if ep.Host == "" {
		ep.Host = "0.0.0.0"
	}
	if ep.Port <= 0 {
		ep.Port = 8801
	}
	if v := strings.TrimSpace(os.Getenv("AUTO_DBA_DB_MONITOR_URL")); v != "" {
		ep.URL = v
	}
	if v := strings.TrimSpace(os.Getenv("AUTO_DBA_DB_MONITOR_MODE")); v != "" {
		ep.Mode = strings.ToLower(v)
	}
	if v := strings.TrimSpace(os.Getenv("AUTO_DBA_DB_MONITOR_PORT")); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			ep.Port = p
		}
	}
	mode := strings.ToLower(strings.TrimSpace(ep.Mode))
	if mode == "" {
		if strings.TrimSpace(ep.URL) != "" {
			mode = "remote"
		} else {
			mode = "embed"
		}
	}
	ep.Mode = mode
}

// DbMonitorRemoteURL 主服务应反向代理的监控服务根地址；空表示不走远端。
func (c *Config) DbMonitorRemoteURL() string {
	if c == nil {
		return ""
	}
	ep := c.Services.DbMonitor
	if strings.ToLower(strings.TrimSpace(ep.Mode)) != "remote" {
		return ""
	}
	return strings.TrimRight(strings.TrimSpace(ep.URL), "/")
}

// DbMonitorListenAddr 独立监控进程监听地址。
func (c *Config) DbMonitorListenAddr() string {
	if c == nil {
		return "0.0.0.0:8801"
	}
	ep := c.Services.DbMonitor
	host := ep.Host
	if host == "" {
		host = "0.0.0.0"
	}
	port := ep.Port
	if port <= 0 {
		port = 8801
	}
	return fmt.Sprintf("%s:%d", host, port)
}

func applyTerminalDefaults(cfg *Config, configPath string) {
	root := repoRootFromConfigPath(configPath)
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
	if t.SSHKnownHostsFile == "" {
		t.SSHKnownHostsFile = filepath.Join(root, "config", "terminal", "known_hosts")
	}
	t.SSHHostKeyPolicy = strings.ToLower(strings.TrimSpace(t.SSHHostKeyPolicy))
	if t.SSHInsecureHostKey {
		t.SSHHostKeyPolicy = "insecure"
	}
	switch t.SSHHostKeyPolicy {
	case "accept-new", "strict", "insecure":
	default:
		t.SSHHostKeyPolicy = "accept-new"
	}
}
