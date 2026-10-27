package server

import (
	"fmt"
	"log"
	"strings"
	"time"

	"auto-dba/internal/backup"
	"auto-dba/internal/cdc"
	"auto-dba/internal/config"
	"auto-dba/internal/dbdeploy"
	"auto-dba/internal/flashback"
	"auto-dba/internal/notify"
	"auto-dba/internal/secret"
	"auto-dba/internal/sessionstore"
	"auto-dba/internal/srcluster"
	"auto-dba/internal/sshx"
	"auto-dba/internal/store"
)

// hotReloadTargets 热更时需同步的内存引用（与启动时同一批对象）。
type hotReloadTargets struct {
	cfg      *config.Config
	db       *store.DB
	sessions *sessionstore.Store
	secrets  []*string
	backup   *backup.Engine
	cdc      *cdc.Orchestrator
	sr       *srcluster.Engine
	dbdeploy *dbdeploy.Engine
	// pendingPrevCrypto：JWT 回退加密场景下，热更前旧 FieldCrypto，并入 previous 环
	pendingPrevCrypto string
}

// beforeApply 在写入 live 前执行；DSN 变更时先 Reopen，失败则整次热更中止。
func (t *hotReloadTargets) beforeApply(live, next *config.Config, diff config.ReloadDiff) error {
	if t != nil {
		t.pendingPrevCrypto = ""
	}
	// 未配独立 crypto_secret 时字段加密回退 jwt：JWT 变更前保留旧密钥以便 Decrypt
	if t != nil && live != nil && next != nil && diff.JWTSecret && strings.TrimSpace(next.App.CryptoSecret) == "" {
		oldKey := strings.TrimSpace(config.FieldCryptoFromApp(live.App))
		newKey := strings.TrimSpace(config.FieldCryptoFromApp(next.App))
		if oldKey != "" && oldKey != newKey {
			t.pendingPrevCrypto = oldKey
			if strings.TrimSpace(next.App.CryptoSecretPrevious) == "" {
				next.App.CryptoSecretPrevious = oldKey
				log.Printf("nacos reload: jwt 回退加密场景，已将旧 FieldCrypto 写入 crypto_secret_previous（进程内）")
			} else if strings.TrimSpace(next.App.CryptoSecretPrevious) != oldKey {
				log.Printf("nacos reload: jwt 回退加密场景，将旧 FieldCrypto 并入 previous 环（配置已有 previous）")
			}
		}
	}
	if t == nil || t.db == nil || !diff.DBDSN {
		return nil
	}
	if err := t.db.Reopen(next.DB.Driver, next.DB.DSN, store.PoolSettings{
		MaxOpenConns:    next.DB.MaxOpenConns,
		MaxIdleConns:    next.DB.MaxIdleConns,
		ConnMaxLifetime: time.Duration(next.DB.ConnMaxLifetimeSec) * time.Second,
		ConnMaxIdleTime: time.Duration(next.DB.ConnMaxIdleTimeSec) * time.Second,
	}); err != nil {
		return fmt.Errorf("db.dsn reopen: %w", err)
	}
	log.Printf("nacos reload: db.dsn 已热切并完成 migrate/seed（提交配置前）")
	return nil
}

func (t *hotReloadTargets) apply(diff config.ReloadDiff) {
	if t == nil || t.cfg == nil {
		return
	}
	// 调用方已持有 config 写锁；live 已 ApplyInPlace。

	if diff.JWTSecret {
		log.Printf("nacos reload: jwt_secret 已更新（旧 token 将失效）")
	}
	if diff.CryptoSecret || (diff.JWTSecret && strings.TrimSpace(t.cfg.App.CryptoSecret) == "") {
		config.SyncFieldCrypto(t.cfg)
		if t.pendingPrevCrypto != "" {
			secret.MergePreviousSecrets([]string{t.pendingPrevCrypto})
			t.pendingPrevCrypto = ""
		}
		sec := t.cfg.FieldCrypto()
		for _, p := range t.secrets {
			if p != nil {
				*p = sec
			}
		}
		log.Printf("nacos reload: field crypto 已更新（crypto_secret / previous / key_id）")
	}

	if diff.DBPool && !diff.DBDSN && t.db != nil {
		t.db.ApplyPool(store.PoolSettings{
			MaxOpenConns:    t.cfg.DB.MaxOpenConns,
			MaxIdleConns:    t.cfg.DB.MaxIdleConns,
			ConnMaxLifetime: time.Duration(t.cfg.DB.ConnMaxLifetimeSec) * time.Second,
			ConnMaxIdleTime: time.Duration(t.cfg.DB.ConnMaxIdleTimeSec) * time.Second,
		})
		log.Printf("nacos reload: db pool 已更新 open=%d idle=%d", t.cfg.DB.MaxOpenConns, t.cfg.DB.MaxIdleConns)
	}
	// DBDSN：已在 beforeApply 成功 Reopen，此处不再重复。

	if diff.Terminal || diff.SSHKey {
		sshx.SetDefaults(t.cfg.Terminal.SSHHostKeyPolicy, t.cfg.Terminal.SSHKnownHostsFile)
		log.Printf("nacos reload: terminal 已更新 max_lifetime_sec=%d enabled=%v identity=%s",
			t.cfg.Terminal.MaxLifetimeSec, t.cfg.Terminal.Enabled, t.cfg.Terminal.SSHIdentityFile)
	}

	if diff.Flashback || diff.CryptoSecret || (diff.JWTSecret && strings.TrimSpace(t.cfg.App.CryptoSecret) == "") {
		flashback.ApplyConfig(t.cfg.Flashback, t.cfg.FieldCrypto())
		if diff.Flashback {
			log.Printf("nacos reload: flashback 配置已同步")
		}
	}

	if (diff.Backup || diff.Terminal || diff.SSHKey) && t.backup != nil {
		t.backup.Cfg.WorkDir = t.cfg.Backup.WorkDir
		t.backup.Cfg.AllowEmbedded = t.cfg.Backup.AllowEmbedded
		t.backup.Cfg.AgentArtifact = t.cfg.Backup.AgentArtifact
		t.backup.Cfg.AgentRemoteDir = t.cfg.Backup.AgentRemoteDir
		t.backup.Cfg.AgentListenPort = t.cfg.Backup.AgentListenPort
		t.backup.Cfg.SSHUser = t.cfg.Terminal.SSHUser
		t.backup.Cfg.SSHIdentityFile = t.cfg.Terminal.SSHIdentityFile
		t.backup.Cfg.SSHInsecureHostKey = t.cfg.Terminal.SSHInsecureHostKey
		t.backup.Cfg.ResolveIdentity = t.cfg.Terminal.ResolveSSHIdentity
		log.Printf("nacos reload: backup 引擎配置已同步")
	}

	if (diff.Terminal || diff.SSHKey) && t.sr != nil {
		t.sr.Cfg.SSHUser = t.cfg.Terminal.SSHUser
		t.sr.Cfg.SSHIdentityFile = t.cfg.Terminal.SSHIdentityFile
		t.sr.Cfg.SSHInsecureHostKey = t.cfg.Terminal.SSHInsecureHostKey
		t.sr.Cfg.ResolveIdentity = t.cfg.Terminal.ResolveSSHIdentity
		log.Printf("nacos reload: srcluster SSH 已同步")
	}
	if (diff.Terminal || diff.SSHKey) && t.dbdeploy != nil {
		t.dbdeploy.Cfg.SSHUser = t.cfg.Terminal.SSHUser
		t.dbdeploy.Cfg.SSHIdentityFile = t.cfg.Terminal.SSHIdentityFile
		t.dbdeploy.Cfg.SSHInsecureHostKey = t.cfg.Terminal.SSHInsecureHostKey
		t.dbdeploy.Cfg.ResolveIdentity = t.cfg.Terminal.ResolveSSHIdentity
		log.Printf("nacos reload: dbdeploy SSH 已同步")
	}

	if (diff.CDC || diff.CryptoSecret || (diff.JWTSecret && strings.TrimSpace(t.cfg.App.CryptoSecret) == "")) && t.cdc != nil {
		t.cdc.Cfg = t.cfg.CDC
		if diff.CryptoSecret || (diff.JWTSecret && strings.TrimSpace(t.cfg.App.CryptoSecret) == "") {
			t.cdc.Secret = t.cfg.FieldCrypto()
		}
		if diff.CDC {
			log.Printf("nacos reload: cdc 配置已同步 enabled=%v", t.cfg.CDC.Enabled)
		}
	}

	if diff.Log {
		log.Printf("nacos reload: log.level=%s（标准库 log 无动态级别；需重启进程才能改输出过滤）", t.cfg.Log.Level)
	}
	if diff.External {
		log.Printf("nacos reload: external 已更新")
	}
	if diff.Migration {
		log.Printf("nacos reload: migration 已更新 enabled=%v", t.cfg.Migration.Enabled)
	}
	if diff.App && !diff.JWTSecret {
		log.Printf("nacos reload: app 已更新 name=%s run_mode=%s jwt_ttl_hours=%d（run_mode 改 Gin 模式需重启）",
			t.cfg.App.Name, t.cfg.App.RunMode, t.cfg.App.JWTTTLHours)
	}
	if diff.HTTP && !diff.ListenAddr {
		log.Printf("nacos reload: http 非监听字段已写入内存（read/write_timeout 对已 Listen 的 Server 需重启）")
	}
	if diff.FrontendDev {
		log.Printf("nacos reload: frontend.dev 已变为 %s（请重启 Vite）", diff.NewFrontendDev)
	} else if diff.Frontend {
		log.Printf("nacos reload: frontend 已更新")
	}
	if diff.ListenAddr {
		log.Printf("nacos reload: http 监听地址 %s → %s（需重启进程才能重新绑端口）",
			diff.OldListen, diff.NewListen)
		if t.cfg.Nacos.RestartOnListenChange {
			if err := config.SelfRestart(); err != nil {
				log.Printf("nacos reload: self-restart failed: %v", err)
			}
		}
	}
	if diff.Services {
		log.Printf("nacos reload: services 已更新；db_monitor 模式/端口变更请重启相关进程")
	}
	if diff.Notify {
		notify.InitProviders(t.cfg.Notify)
		log.Printf("nacos reload: notify providers 已重建 lark=%v wecom=%v",
			t.cfg.Notify.LarkConfigured(), t.cfg.Notify.WechatConfigured())
	}
	if diff.Copilot {
		log.Printf("nacos reload: copilot 已更新")
	}
	if diff.Redis && t.sessions != nil {
		t.sessions.Reconfigure(t.cfg.Redis, t.cfg.App.JWTTTLHours, diff.RedisConn)
		log.Printf("nacos reload: redis session store 已同步 enabled=%v addr=%s",
			t.cfg.Redis.Enabled, t.cfg.Redis.Addr)
	}
}
