package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/archive"
	"auto-dba/internal/awr"
	"auto-dba/internal/backup"
	"auto-dba/internal/cdc"
	"auto-dba/internal/config"
	"auto-dba/internal/copilot"
	"auto-dba/internal/dbdeploy"
	"auto-dba/internal/dbmig"
	"auto-dba/internal/dbmonitor"
	"auto-dba/internal/dns"
	"auto-dba/internal/flashback"
	"auto-dba/internal/handler"
	"auto-dba/internal/jobboot"
	"auto-dba/internal/jobreg"
	"auto-dba/internal/notify"
	"auto-dba/internal/ossupload"
	"auto-dba/internal/pgspace"
	"auto-dba/internal/redisrdb"
	"auto-dba/internal/router"
	"auto-dba/internal/runbook"
	"auto-dba/internal/secret"
	"auto-dba/internal/sessionstore"
	"auto-dba/internal/sqlinsight"
	"auto-dba/internal/srcluster"
	"auto-dba/internal/sshx"
	"auto-dba/internal/store"
	"auto-dba/internal/terminal"
)

func Run(cfg *config.Config, configPath string) {
	bootStart := time.Now()
	if configPath == "" {
		configPath = "config/config.yaml"
	}
	if cfg.App.RunMode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}
	sshx.SetDefaults(cfg.Terminal.SSHHostKeyPolicy, cfg.Terminal.SSHKnownHostsFile)
	log.Printf("ssh host key policy=%s known_hosts=%s", cfg.Terminal.SSHHostKeyPolicy, cfg.Terminal.SSHKnownHostsFile)
	db, err := store.Open(cfg.DB.Driver, cfg.DB.DSN, store.PoolSettings{
		MaxOpenConns:    cfg.DB.MaxOpenConns,
		MaxIdleConns:    cfg.DB.MaxIdleConns,
		ConnMaxLifetime: time.Duration(cfg.DB.ConnMaxLifetimeSec) * time.Second,
		ConnMaxIdleTime: time.Duration(cfg.DB.ConnMaxIdleTimeSec) * time.Second,
	})
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()
	log.Printf("boot: store ready in %s", time.Since(bootStart).Round(time.Millisecond))

	if raw, err := db.GetGlobalArg("ticket_instance_types"); err == nil {
		if warns := dbdeploy.DiffAgainstInstanceTypes(dbdeploy.ParseTicketInstanceTypeValues(raw)); len(warns) > 0 {
			for _, w := range warns {
				log.Printf("dbdeploy registry: %s", w)
			}
		}
	}

	staticRoot := ""
	if cfg.HTTP.ServeStatic {
		staticRoot = "frontend/dist"
		if abs, err := filepath.Abs(staticRoot); err == nil {
			staticRoot = abs
		}
		indexPath := filepath.Join(staticRoot, "index.html")
		if _, err := os.Stat(indexPath); err == nil {
			log.Printf("boot: serve_static root=%s index=ok", staticRoot)
		} else {
			log.Printf("boot: WARN serve_static root=%s index.html missing (check cwd / package layout): %v", staticRoot, err)
		}
	}

	engine := gin.New()
	engine.Use(gin.Recovery(), gin.Logger())
	cryptoLive := cfg.FieldCrypto()
	fieldCrypto := cryptoLive
	// 审计：失败与遗留明文必记；成功密文解密默认不刷屏（采集路径很频繁）
	secret.SetAudit(func(ok bool, format string) {
		if !ok || format == "plain" {
			log.Printf("secret_decrypt ok=%v format=%s", ok, format)
		}
	})
	warnIfInstanceSecretsUnreadable(db, fieldCrypto)
	migEngine := dbmig.New(db, fieldCrypto)
	arcEngine := archive.New(db, fieldCrypto)
	bakEngine := backup.New(db, fieldCrypto, backup.Config{
		WorkDir:            cfg.Backup.WorkDir,
		AllowEmbedded:      cfg.Backup.AllowEmbedded,
		AgentArtifact:      cfg.Backup.AgentArtifact,
		AgentRemoteDir:     cfg.Backup.AgentRemoteDir,
		AgentListenPort:    cfg.Backup.AgentListenPort,
		SSHUser:            cfg.Terminal.SSHUser,
		SSHIdentityFile:    cfg.Terminal.SSHIdentityFile,
		SSHInsecureHostKey: cfg.Terminal.SSHInsecureHostKey,
	})
	cdcOrch := cdc.NewOrchestrator(db, fieldCrypto, cfg.CDC)
	if n, err := cdcOrch.RecoverInflight(); err != nil {
		log.Printf("cdc recover inflight: %v", err)
	} else if n > 0 {
		log.Printf("cdc recover inflight: marked %d tasks failed", n)
	}
	flashback.SetRuntime(&flashback.Runtime{Store: db, Secret: fieldCrypto, Cfg: cfg.Flashback})
	fbEngine := flashback.New()
	ticketH := &handler.TicketHandler{Store: db, Secret: fieldCrypto, Flashback: fbEngine, Migration: migEngine, Archive: arcEngine, CDC: cdcOrch, Backup: bakEngine}
	pgSpaceSvc := pgspace.New(db, fieldCrypto)
	redisRDBSvc := redisrdb.New(db, fieldCrypto, filepath.Join(os.TempDir(), "auto-dba-redis-rdb"))
	awrSvc := awr.New(db, fieldCrypto)
	siSvc := sqlinsight.New(db, sqlinsight.StoreAWRLoader{DB: db})
	ossUploadEng := ossupload.NewEngine(db, fieldCrypto)
	starRocksWebH := &handler.StarRocksWebHandler{Store: db, Secret: fieldCrypto}
	srEng := srcluster.New(db, fieldCrypto, srcluster.Config{
		AgentArtifact:      "deploy/node-agent/bin/node-agent",
		AgentRemoteDir:     "/home/sysadmin/auto-dba/node-agent",
		AgentListenPort:    8091,
		SSHUser:            cfg.Terminal.SSHUser,
		SSHIdentityFile:    cfg.Terminal.SSHIdentityFile,
		SSHInsecureHostKey: cfg.Terminal.SSHInsecureHostKey,
	})
	dbDeployEng := dbdeploy.New(db, fieldCrypto, dbdeploy.Config{
		AgentArtifact:      "deploy/db-agent/bin/db-agent",
		AgentRemoteDir:     "/home/sysadmin/auto-dba/db-agent",
		AgentListenPort:    8092,
		SSHUser:            cfg.Terminal.SSHUser,
		SSHIdentityFile:    cfg.Terminal.SSHIdentityFile,
		SSHInsecureHostKey: cfg.Terminal.SSHInsecureHostKey,
		DNSDSN:             cfg.DNS.DSN,
		DNSNodeUser:        cfg.DNS.NodeUser,
		DNSNodePassword:    cfg.DNS.NodePassword,
		DNSNodeDB:          cfg.DNS.NodeDB,
	})
	dnsH := &handler.DNSHandler{
		Store: db, Secret: fieldCrypto, Engine: dbDeployEng,
		DNS: &dns.Provider{DSN: cfg.DNS.DSN, NodeUser: cfg.DNS.NodeUser, NodePass: cfg.DNS.NodePassword, NodeDB: cfg.DNS.NodeDB},
	}
	ticketH.DNS = dnsH
	sessStore := sessionstore.New(cfg.Redis, cfg.App.JWTTTLHours)
	if cfg.Redis.Enabled {
		sessStore.Reconfigure(cfg.Redis, cfg.App.JWTTTLHours, true)
	}
	authH := &handler.AuthHandler{Store: db, Cfg: cfg, Sessions: sessStore}
	loginSessH := &handler.LoginSessionHandler{Store: db, Sessions: sessStore}
	cdcH := &handler.CDCHandler{Store: db, Cfg: cfg, Secret: fieldCrypto}
	slotH := &handler.SlotHandler{Store: db, Secret: fieldCrypto}
	jobReg := jobreg.New(db)
	jobboot.RegisterPlatformJobs(jobReg, jobboot.Deps{
		DB: db, Sessions: sessStore, PgSpace: pgSpaceSvc, AWR: awrSvc, SQLInsight: siSvc,
		Migration: migEngine, Archive: arcEngine, Backup: bakEngine,
		Ticket: ticketH, CDC: cdcH, Slot: slotH, AlertSecret: fieldCrypto, PgSecret: &cryptoLive,
		DNS: dnsH,
	})
	jobReg.OnFailure = func(jobKey, jobName, runID, errMsg string) {
		notify.JobFailed(db, fieldCrypto, jobKey, jobName, runID, errMsg)
	}
	deps := router.Deps{
		Cfg:                cfg,
		Auth:               authH,
		LoginSession:       loginSessH,
		Inst:               &handler.InstanceHandler{Store: db, Secret: fieldCrypto},
		DB:                 &handler.DatabaseHandler{Store: db, Secret: fieldCrypto},
		AccessDomain:       &handler.AccessDomainHandler{Store: db},
		Region:             &handler.RegionHandler{Store: db},
		Env:                &handler.EnvironmentHandler{Store: db},
		Connector:          &handler.ConnectorHandler{Store: db},
		Args:               &handler.GlobalArgHandler{Store: db},
		Jobs:               &handler.JobsHandler{Reg: jobReg, Store: db},
		Ticket:             ticketH,
		CreateDB:           &handler.CreateDBHandler{Store: db, Secret: fieldCrypto},
		Account:            &handler.AccountHandler{Store: db, Secret: fieldCrypto},
		Review:             &handler.ReviewRuleHandler{Store: db, Secret: fieldCrypto},
		CDC:                cdcH,
		Slot:               slotH,
		Migration:          &handler.MigrationHandler{Store: db, Cfg: cfg, Secret: fieldCrypto, Engine: migEngine},
		Flashback:          &handler.FlashbackHandler{Store: db, Secret: fieldCrypto, Cfg: cfg, Engine: fbEngine},
		Archive:            &handler.ArchiveHandler{Store: db, Secret: fieldCrypto, Engine: arcEngine},
		Kafka:              &handler.KafkaHandler{Store: db, Secret: fieldCrypto},
		Redis:              &handler.RedisHandler{Store: db, Secret: fieldCrypto},
		Insight:            &handler.InsightHandler{Store: db, Secret: fieldCrypto},
		InstanceOverview:   &handler.OverviewHandler{Store: db, Secret: fieldCrypto},
		Session:            &handler.SessionHandler{Store: db, Secret: fieldCrypto},
		PgSpace:            &handler.PgSpaceHandler{Svc: pgSpaceSvc},
		RedisRDB:           &handler.RedisRDBHandler{Svc: redisRDBSvc},
		AWR:                &handler.AWRHandler{Svc: awrSvc},
		SqlInsightAnalysis: &handler.SqlInsightAnalysisHandler{Svc: siSvc},
		Multilang:          &handler.MultilangHandler{Store: db},
		External:           &handler.ExternalHandler{Cfg: cfg},
		MonitorTarget:      &handler.MonitorTargetHandler{Store: db},
		MonitorTag:         &handler.MonitorLabelTagHandler{Store: db},
		FeAdmin:            &handler.FeAdminHandler{Store: db, Secret: fieldCrypto},
		DsAdmin:            &handler.DsAdminHandler{Store: db, Secret: fieldCrypto},
		OSSSettings:        &handler.OSSSettingsHandler{Store: db, Secret: fieldCrypto},
		OSSUpload:          &handler.OSSUploadHandler{Engine: ossUploadEng},
		StarRocksWeb:       starRocksWebH,
		HA:                 &handler.HAHandler{Store: db, Secret: fieldCrypto},
		Proxy:              &handler.ProxyHandler{Store: db, Secret: fieldCrypto},
		Backup:             &handler.BackupHandler{Store: db, Secret: fieldCrypto, Engine: bakEngine},
		K8s:                func() *handler.K8sHandler { h := handler.NewK8sHandler(db, fieldCrypto); h.StartProbeLoop(); return h }(),
		SrCluster:          &handler.SrClusterHandler{Store: db, Secret: fieldCrypto, Engine: srEng},
		DbDeploy:           &handler.DbDeployHandler{Store: db, Secret: fieldCrypto, Engine: dbDeployEng},
		DNS:                dnsH,
		Cmdb:               &handler.CmdbHandler{Store: db, Secret: fieldCrypto},
		Todo:               &handler.TodoHandler{Store: db},
		Alert:              &handler.AlertHandler{Store: db, Secret: fieldCrypto, DNS: dnsH.DNS},
		Copilot:            &handler.CopilotHandler{Svc: func() *copilot.Service { s := copilot.New(db); s.Secret = fieldCrypto; return s }()},
		Runbook:            &handler.RunbookHandler{Svc: runbook.New(db)},
		DCL:                &handler.DCLHandler{Store: db, Secret: fieldCrypto},
		ResourceApply:      &handler.ResourceApplyHandler{Store: db, Secret: fieldCrypto},
		RBAC:               &handler.RBACHandler{Store: db, Secret: fieldCrypto, Sessions: sessStore},
		Tenant: &handler.TenantHandler{
			Store: db,
			CfgTTL: func() time.Duration {
				return time.Duration(cfg.App.JWTTTLHours) * time.Hour
			},
			Secret:   func() string { return cfg.App.JWTSecret },
			Sessions: sessStore,
		},
		Ldap:     &handler.LdapHandler{Store: db, Cfg: cfg},
		Sso:      &handler.SsoHandler{Store: db, Cfg: cfg},
		SMTP:     &handler.SMTPHandler{Store: db, Cfg: cfg},
		Notify:   &handler.NotifyHandler{Store: db, Cfg: cfg},
		Terminal: &terminal.Handler{Store: db, Cfg: cfg, Secret: fieldCrypto},
		Static:   staticRoot,
	}

	// 数据库监控：remote 代理到独立服务；embed 内嵌；off 关闭
	switch strings.ToLower(strings.TrimSpace(cfg.Services.DbMonitor.Mode)) {
	case "remote":
		remote := cfg.DbMonitorRemoteURL()
		if remote == "" {
			log.Fatalf("services.db_monitor.mode=remote 但未配置 url（可用环境变量 AUTO_DBA_DB_MONITOR_URL）")
		}
		proxy, err := NewDbMonitorReverseProxy(remote)
		if err != nil {
			log.Fatalf("db-monitor proxy: %v", err)
		}
		deps.DbMonitorProxy = proxy
		fmt.Fprintf(os.Stderr, "db-monitor mode=remote → %s\n", remote)
	case "off":
		fmt.Fprintf(os.Stderr, "db-monitor mode=off\n")
	default: // embed
		dbMonSvc := dbmonitor.New(db, fieldCrypto)
		deps.DbMonitor = &handler.DbMonitorHandler{Svc: dbMonSvc}
		fmt.Fprintf(os.Stderr, "db-monitor mode=embed\n")
	}
	router.Init(engine, deps)
	log.Printf("boot: routes ready in %s", time.Since(bootStart).Round(time.Millisecond))

	notify.InitProviders(cfg.Notify)
	if ncfg, err := db.GetNotifyConfig(); err == nil {
		store.ApplyAlertNotifyDefaultsOnLoad(ncfg)
		log.Printf("boot: alert notify defaults max=%d window=%s repeat=%s",
			ncfg.AlertNotifyMaxFire, ncfg.AlertNotifyWindow, ncfg.AlertNotifyRepeat)
	} else {
		store.ApplyAlertNotifyDefaultsOnLoad(nil)
	}
	log.Printf("boot: notify providers ready lark=%v alert_lark=%v wecom=%v",
		cfg.Notify.LarkConfigured(), cfg.Notify.AlertLarkConfigured(), cfg.Notify.WechatConfigured())

	hot := &hotReloadTargets{
		cfg:      cfg,
		db:       db,
		sessions: sessStore,
		backup:   bakEngine,
		cdc:      cdcOrch,
		sr:       srEng,
		dbdeploy: dbDeployEng,
		secrets: []*string{
			&migEngine.Secret, &arcEngine.Secret, &bakEngine.Secret,
			&deps.Inst.Secret, &deps.DB.Secret, &deps.Ticket.Secret,
			&deps.CreateDB.Secret, &deps.Account.Secret, &deps.Review.Secret,
			&deps.CDC.Secret, &deps.Migration.Secret, &deps.Flashback.Secret,
			&deps.Archive.Secret, &deps.Kafka.Secret, &deps.Redis.Secret,
			&deps.Insight.Secret, &deps.Session.Secret, &deps.FeAdmin.Secret,
			&deps.DsAdmin.Secret, &deps.OSSSettings.Secret,
			&deps.HA.Secret, &deps.Proxy.Secret, &deps.Backup.Secret,
			&deps.SrCluster.Secret, &deps.DbDeploy.Secret, &deps.Cmdb.Secret,
			&deps.DCL.Secret, &deps.RBAC.Secret, &deps.Terminal.Secret,
			&pgSpaceSvc.Secret, &redisRDBSvc.Secret, &awrSvc.Secret,
			&ossUploadEng.Secret, &starRocksWebH.Secret,
			&srEng.Secret, &dbDeployEng.Secret,
			&cryptoLive,
		},
	}
	if deps.DbMonitor != nil && deps.DbMonitor.Svc != nil {
		hot.secrets = append(hot.secrets, &deps.DbMonitor.Svc.Secret)
	}
	if err := config.StartNacosWatchOpts(cfg, configPath, config.WatchOptions{
		BeforeApply: hot.beforeApply,
		AfterApply:  hot.apply,
	}); err != nil {
		log.Printf("nacos watch: start failed: %v", err)
	}

	collectCtx, collectCancel := context.WithCancel(context.Background())
	defer collectCancel()
	_ = flashback.Bootstrap(collectCtx)
	if n := cdc.RecoverStaleReinit(db); n > 0 {
		log.Printf("boot: recovered %d CDC task(s) stuck in reinitializing", n)
	}
	jobReg.Start(collectCtx)

	addr := fmt.Sprintf("%s:%d", cfg.HTTP.Host, cfg.HTTP.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      engine,
		ReadTimeout:  time.Duration(cfg.HTTP.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.HTTP.WriteTimeout) * time.Second,
	}
	go func() {
		fmt.Fprintf(os.Stderr, "auto-dba listening on %s (boot %s)\n", addr, time.Since(bootStart).Round(time.Millisecond))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	jobReg.Stop()
	collectCancel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
