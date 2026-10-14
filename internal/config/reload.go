package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// ReloadDiff 一次 Nacos 推送相对当前生效配置的变更摘要。
// 顶层业务段用整段比较：该段内任意字段变更都会命中，无需再为单字段补洞。
type ReloadDiff struct {
	App       bool
	HTTP      bool
	Frontend  bool
	Log       bool
	DB        bool
	CDC       bool
	Migration bool
	Flashback bool
	Backup    bool
	External  bool
	Terminal  bool
	Services  bool
	Copilot   bool

	// 派生：供 Reloader 做副作用（换密钥/换库/重启等）
	JWTSecret    bool
	CryptoSecret bool // crypto_secret / previous / key_id 或回退后的 FieldCrypto 材料变更
	DBDSN        bool
	DBPool       bool
	SSHKey       bool // terminal 内 SSH 相关
	ListenAddr   bool // http.host / http.port
	FrontendDev  bool // frontend.dev_*

	OldListen      string
	NewListen      string
	OldFrontendDev string
	NewFrontendDev string
}

// Any 任一业务段有变更即为 true。
func (d ReloadDiff) Any() bool {
	return d.App || d.HTTP || d.Frontend || d.Log || d.DB || d.CDC ||
		d.Migration || d.Flashback || d.Backup || d.External || d.Terminal || d.Services || d.Copilot
}

// Sections 返回有变更的段名（日志用）。
func (d ReloadDiff) Sections() []string {
	var out []string
	add := func(name string, on bool) {
		if on {
			out = append(out, name)
		}
	}
	add("app", d.App)
	add("http", d.HTTP)
	add("frontend", d.Frontend)
	add("log", d.Log)
	add("db", d.DB)
	add("cdc", d.CDC)
	add("migration", d.Migration)
	add("flashback", d.Flashback)
	add("backup", d.Backup)
	add("external", d.External)
	add("terminal", d.Terminal)
	add("services", d.Services)
	add("copilot", d.Copilot)
	return out
}

// MergeRemoteYAML 以本地引导文件为底，叠加 Nacos DataId 正文，得到下一份完整配置（不修改 live）。
func MergeRemoteYAML(configPath, remoteContent string) (*Config, error) {
	b, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read local config: %w", err)
	}
	next := &Config{}
	if err := yaml.Unmarshal(b, next); err != nil {
		return nil, fmt.Errorf("parse local config: %w", err)
	}
	applyNacosEnvOverrides(&next.Nacos)
	boot := next.Nacos
	localHTTP := next.HTTP
	localFE := next.Frontend
	if strings.TrimSpace(remoteContent) == "" {
		return nil, fmt.Errorf("nacos: remote content empty")
	}
	if err := yaml.Unmarshal([]byte(remoteContent), next); err != nil {
		return nil, fmt.Errorf("parse remote yaml: %w%s", err, yamlSSHPrivateKeyHint(remoteContent, err))
	}
	next.Nacos = boot
	preserveHTTPFromLocal(&next.HTTP, localHTTP, remoteContent)
	preserveFrontendFromLocal(&next.Frontend, localFE)
	applyDefaults(next, configPath)
	if err := materializeSSHPrivateKey(next, configPath); err != nil {
		return nil, err
	}
	return next, nil
}

func preserveFrontendFromLocal(dst *FrontendConfig, local FrontendConfig) {
	if dst.DevHost == "" {
		dst.DevHost = local.DevHost
	}
	if dst.DevPort <= 0 {
		dst.DevPort = local.DevPort
	}
}

// DiffConfig 按 Config 全部业务顶层段比较（不含 nacos 引导）。
func DiffConfig(old, next *Config) ReloadDiff {
	var d ReloadDiff
	if old == nil || next == nil {
		return d
	}
	d.App = old.App != next.App
	d.HTTP = old.HTTP != next.HTTP
	d.Frontend = old.Frontend != next.Frontend
	d.Log = old.Log != next.Log
	d.DB = old.DB != next.DB
	d.CDC = old.CDC != next.CDC
	d.Migration = old.Migration != next.Migration
	d.Flashback = old.Flashback != next.Flashback
	d.Backup = old.Backup != next.Backup
	d.External = old.External != next.External
	d.Terminal = old.Terminal != next.Terminal
	d.Services = old.Services != next.Services
	d.Copilot = old.Copilot != next.Copilot

	d.JWTSecret = old.App.JWTSecret != next.App.JWTSecret
	d.CryptoSecret = FieldCryptoFromApp(old.App) != FieldCryptoFromApp(next.App) ||
		old.App.CryptoSecretPrevious != next.App.CryptoSecretPrevious ||
		old.App.CryptoKeyID != next.App.CryptoKeyID
	d.DBDSN = old.DB.DSN != next.DB.DSN || old.DB.Driver != next.DB.Driver
	d.DBPool = old.DB.MaxOpenConns != next.DB.MaxOpenConns ||
		old.DB.MaxIdleConns != next.DB.MaxIdleConns ||
		old.DB.ConnMaxLifetimeSec != next.DB.ConnMaxLifetimeSec ||
		old.DB.ConnMaxIdleTimeSec != next.DB.ConnMaxIdleTimeSec
	d.SSHKey = old.Terminal.SSHIdentityFile != next.Terminal.SSHIdentityFile ||
		old.Terminal.SSHUser != next.Terminal.SSHUser ||
		old.Terminal.SSHHostKeyPolicy != next.Terminal.SSHHostKeyPolicy ||
		old.Terminal.SSHInsecureHostKey != next.Terminal.SSHInsecureHostKey ||
		old.Terminal.SSHKnownHostsFile != next.Terminal.SSHKnownHostsFile ||
		old.Terminal.SSHKeyFingerprint != next.Terminal.SSHKeyFingerprint
	d.ListenAddr = old.HTTP.Host != next.HTTP.Host || old.HTTP.Port != next.HTTP.Port
	d.OldListen = fmt.Sprintf("%s:%d", old.HTTP.Host, old.HTTP.Port)
	d.NewListen = fmt.Sprintf("%s:%d", next.HTTP.Host, next.HTTP.Port)
	d.FrontendDev = old.Frontend.DevHost != next.Frontend.DevHost || old.Frontend.DevPort != next.Frontend.DevPort
	d.OldFrontendDev = fmt.Sprintf("%s:%d", old.Frontend.DevHost, old.Frontend.DevPort)
	d.NewFrontendDev = fmt.Sprintf("%s:%d", next.Frontend.DevHost, next.Frontend.DevPort)
	return d
}

// ApplyInPlace 把 next 的业务字段写入 live（保留 live 的 nacos 引导）。
func ApplyInPlace(live, next *Config) {
	if live == nil || next == nil {
		return
	}
	boot := live.Nacos
	*live = *next
	live.Nacos = boot
}
