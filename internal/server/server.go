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

	"auto-dba/internal/alert"
	"auto-dba/internal/archive"
	"auto-dba/internal/awr"
	"auto-dba/internal/backup"
	"auto-dba/internal/cdc"
	"auto-dba/internal/config"
	"auto-dba/internal/copilot"
	"auto-dba/internal/dbdeploy"
	"auto-dba/internal/dbmig"
	"auto-dba/internal/dbmonitor"
	"auto-dba/internal/flashback"
	"auto-dba/internal/handler"
	"auto-dba/internal/ossupload"
	"auto-dba/internal/pgdiag"
	"auto-dba/internal/pgspace"
	"auto-dba/internal/redisrdb"
	"auto-dba/internal/router"
	"auto-dba/internal/runbook"
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
	}

	engine := gin.New()
	engine.Use(gin.Recovery(), gin.Logger())
	migEngine := dbmig.New(db, cfg.App.JWTSecret)
	arcEngine := archive.New(db, cfg.App.JWTSecret)
	bakEngine := backup.New(db, cfg.App.JWTSecret, backup.Config{
		WorkDir:            cfg.Backup.WorkDir,
		AllowEmbedded:      cfg.Backup.AllowEmbedded,
		AgentArtifact:      cfg.Backup.AgentArtifact,
		AgentRemoteDir:     cfg.Backup.AgentRemoteDir,
		AgentListenPort:    cfg.Backup.AgentListenPort,
		SSHUser:            cfg.Terminal.SSHUser,
		SSHIdentityFile:    cfg.Terminal.SSHIdentityFile,
		SSHInsecureHostKey: cfg.Terminal.SSHInsecureHostKey,
	})
	cdcOrch := cdc.NewOrchestrator(db, cfg.App.JWTSecret, cfg.CDC)
	flashback.SetRuntime(&flashback.Runtime{Store: db, Secret: cfg.App.JWTSecret, Cfg: cfg.Flashback})
	fbEngine := flashback.New()
	ticketH := &handler.TicketHandler{Store: db, Secret: cfg.App.JWTSecret, Flashback: fbEngine, Migration: migEngine, Archive: arcEngine, CDC: cdcOrch}
	pgSpaceSvc := pgspace.New(db, cfg.App.JWTSecret)
	redisRDBSvc := redisrdb.New(db, cfg.App.JWTSecret, filepath.Join(os.TempDir(), "auto-dba-redis-rdb"))
	awrSvc := awr.New(db, cfg.App.JWTSecret)
	siSvc := sqlinsight.New(db, sqlinsight.StoreAWRLoader{DB: db})
	ossUploadEng := ossupload.NewEngine(db, cfg.App.JWTSecret)
	starRocksWebH := &handler.StarRocksWebHandler{Store: db, Secret: cfg.App.JWTSecret}
	srEng := srcluster.New(db, cfg.App.JWTSecret, srcluster.Config{
		AgentArtifact:      "deploy/node-agent/bin/node-agent",
		AgentRemoteDir:     "/home/sysadmin/auto-dba/node-agent",
		AgentListenPort:    8091,
		SSHUser:            cfg.Terminal.SSHUser,
		SSHIdentityFile:    cfg.Terminal.SSHIdentityFile,
		SSHInsecureHostKey: cfg.Terminal.SSHInsecureHostKey,
	})
	dbDeployEng := dbdeploy.New(db, cfg.App.JWTSecret, dbdeploy.Config{
		AgentArtifact:      "deploy/db-agent/bin/db-agent",
		AgentRemoteDir:     "/home/sysadmin/auto-dba/db-agent",
		AgentListenPort:    8092,
		SSHUser:            cfg.Terminal.SSHUser,
		SSHIdentityFile:    cfg.Terminal.SSHIdentityFile,
		SSHInsecureHostKey: cfg.Terminal.SSHInsecureHostKey,
	})
	deps := router.Deps{
		Cfg:                cfg,
		Auth:               &handler.AuthHandler{Store: db, Cfg: cfg},
		Inst:               &handler.InstanceHandler{Store: db, Secret: cfg.App.JWTSecret},
		DB:                 &handler.DatabaseHandler{Store: db, Secret: cfg.App.JWTSecret},
		Region:             &handler.RegionHandler{Store: db},
		Env:                &handler.EnvironmentHandler{Store: db},
		Connector:          &handler.ConnectorHandler{Store: db},
		Args:               &handler.GlobalArgHandler{Store: db},
		Ticket:             ticketH,
		CreateDB:           &handler.CreateDBHandler{Store: db, Secret: cfg.App.JWTSecret},
		Account:            &handler.AccountHandler{Store: db, Secret: cfg.App.JWTSecret},
		Review:             &handler.ReviewRuleHandler{Store: db, Secret: cfg.App.JWTSecret},
		CDC:                &handler.CDCHandler{Store: db, Cfg: cfg, Secret: cfg.App.JWTSecret},
		Slot:               &handler.SlotHandler{Store: db},
		Migration:          &handler.MigrationHandler{Store: db, Cfg: cfg, Secret: cfg.App.JWTSecret, Engine: migEngine},
		Flashback:          &handler.FlashbackHandler{Store: db, Secret: cfg.App.JWTSecret, Cfg: cfg, Engine: fbEngine},
		Archive:            &handler.ArchiveHandler{Store: db, Secret: cfg.App.JWTSecret, Engine: arcEngine},
		Kafka:              &handler.KafkaHandler{Store: db, Secret: cfg.App.JWTSecret},
		Redis:              &handler.RedisHandler{Store: db, Secret: cfg.App.JWTSecret},
		Insight:            &handler.InsightHandler{Store: db, Secret: cfg.App.JWTSecret},
		Session:            &handler.SessionHandler{Store: db, Secret: cfg.App.JWTSecret},
		PgSpace:            &handler.PgSpaceHandler{Svc: pgSpaceSvc},
		RedisRDB:           &handler.RedisRDBHandler{Svc: redisRDBSvc},
		AWR:                &handler.AWRHandler{Svc: awrSvc},
		SqlInsightAnalysis: &handler.SqlInsightAnalysisHandler{Svc: siSvc},
		Multilang:          &handler.MultilangHandler{Store: db},
		External:           &handler.ExternalHandler{Cfg: cfg},
		MonitorTarget:      &handler.MonitorTargetHandler{Store: db},
		MonitorTag:         &handler.MonitorLabelTagHandler{Store: db},
		FeAdmin:            &handler.FeAdminHandler{Store: db, Secret: cfg.App.JWTSecret},
		DsAdmin:            &handler.DsAdminHandler{Store: db, Secret: cfg.App.JWTSecret},
		OSSSettings:        &handler.OSSSettingsHandler{Store: db, Secret: cfg.App.JWTSecret},
		OSSUpload:          &handler.OSSUploadHandler{Engine: ossUploadEng},
		StarRocksWeb:       starRocksWebH,
		HA:                 &handler.HAHandler{Store: db, Secret: cfg.App.JWTSecret},
		Proxy:              &handler.ProxyHandler{Store: db, Secret: cfg.App.JWTSecret},
		Backup:             &handler.BackupHandler{Store: db, Secret: cfg.App.JWTSecret, Engine: bakEngine},
		SrCluster:          &handler.SrClusterHandler{Store: db, Secret: cfg.App.JWTSecret, Engine: srEng},
		DbDeploy:           &handler.DbDeployHandler{Store: db, Secret: cfg.App.JWTSecret, Engine: dbDeployEng},
		Cmdb:               &handler.CmdbHandler{Store: db, Secret: cfg.App.JWTSecret},
		Todo:               &handler.TodoHandler{Store: db},
		Alert:              &handler.AlertHandler{Store: db, Secret: cfg.App.JWTSecret},
		Copilot:            &handler.CopilotHandler{Svc: copilot.New(db)},
		Runbook:            &handler.RunbookHandler{Svc: runbook.New(db)},
		DCL:                &handler.DCLHandler{Store: db, Secret: cfg.App.JWTSecret},
		RBAC:               &handler.RBACHandler{Store: db, Secret: cfg.App.JWTSecret},
		Tenant: &handler.TenantHandler{
			Store: db,
			CfgTTL: func() time.Duration {
				return time.Duration(cfg.App.JWTTTLHours) * time.Hour
			},
			Secret: func() string { return cfg.App.JWTSecret },
		},
		Ldap:               &handler.LdapHandler{Store: db, Cfg: cfg},
		Sso:                &handler.SsoHandler{Store: db, Cfg: cfg},
		SMTP:               &handler.SMTPHandler{Store: db, Cfg: cfg},
		Notify:             &handler.NotifyHandler{Store: db, Cfg: cfg},
		Terminal:           &terminal.Handler{Store: db, Cfg: cfg, Secret: cfg.App.JWTSecret},
		Static:             staticRoot,
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
		dbMonSvc := dbmonitor.New(db, cfg.App.JWTSecret)
		deps.DbMonitor = &handler.DbMonitorHandler{Svc: dbMonSvc}
		fmt.Fprintf(os.Stderr, "db-monitor mode=embed\n")
	}
	router.Init(engine, deps)
	log.Printf("boot: routes ready in %s", time.Since(bootStart).Round(time.Millisecond))

	hot := &hotReloadTargets{
		cfg:      cfg,
		db:       db,
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
	go pgdiag.StartCollector(collectCtx, db, &cfg.App.JWTSecret)
	pgspace.StartDailyCron(collectCtx, pgSpaceSvc)
	awr.StartDailyCron(collectCtx, awrSvc)
	sqlinsight.StartDailyCron(collectCtx, siSvc)
	migEngine.StartScheduler(collectCtx)
	arcEngine.StartScheduler(collectCtx)
	bakEngine.StartScheduler(collectCtx)
	_ = flashback.Bootstrap(collectCtx)
	if n := cdc.RecoverStaleReinit(db); n > 0 {
		log.Printf("boot: recovered %d CDC task(s) stuck in reinitializing", n)
	}
	go handler.StartIssueAutoCancel(collectCtx, ticketH)
	go handler.StartAwaitExecStuckNotify(collectCtx, ticketH)
	alert.Start(collectCtx, db, alert.DefaultInterval, cfg.App.JWTSecret)

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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
