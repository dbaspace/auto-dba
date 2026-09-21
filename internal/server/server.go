package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/archive"
	"auto-dba/internal/cdc"
	"auto-dba/internal/config"
	"auto-dba/internal/dbmig"
	"auto-dba/internal/flashback"
	"auto-dba/internal/handler"
	"auto-dba/internal/pgdiag"
	"auto-dba/internal/router"
	"auto-dba/internal/store"
	"auto-dba/internal/terminal"
)

func Run(cfg *config.Config) {
	if cfg.App.RunMode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}
	db, err := store.Open(cfg.DB.Driver, cfg.DB.DSN)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

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
	cdcOrch := cdc.NewOrchestrator(db, cfg.App.JWTSecret, cfg.CDC)
	flashback.SetRuntime(&flashback.Runtime{Store: db, Secret: cfg.App.JWTSecret, Cfg: cfg.Flashback})
	fbEngine := flashback.New()
	ticketH := &handler.TicketHandler{Store: db, Secret: cfg.App.JWTSecret, Flashback: fbEngine, Migration: migEngine, Archive: arcEngine, CDC: cdcOrch}
	router.Init(engine, router.Deps{
		Cfg:       cfg,
		Auth:      &handler.AuthHandler{Store: db, Cfg: cfg},
		Inst:      &handler.InstanceHandler{Store: db, Secret: cfg.App.JWTSecret},
		DB:        &handler.DatabaseHandler{Store: db, Secret: cfg.App.JWTSecret},
		Region:    &handler.RegionHandler{Store: db},
		Env:       &handler.EnvironmentHandler{Store: db},
		Connector: &handler.ConnectorHandler{Store: db},
		Args:      &handler.GlobalArgHandler{Store: db},
		Ticket:    ticketH,
		CreateDB:  &handler.CreateDBHandler{Store: db, Secret: cfg.App.JWTSecret},
		Account:   &handler.AccountHandler{Store: db, Secret: cfg.App.JWTSecret},
		Review:    &handler.ReviewRuleHandler{Store: db, Secret: cfg.App.JWTSecret},
		CDC:       &handler.CDCHandler{Store: db, Cfg: cfg, Secret: cfg.App.JWTSecret},
		Slot:      &handler.SlotHandler{Store: db},
		Migration: &handler.MigrationHandler{Store: db, Cfg: cfg, Secret: cfg.App.JWTSecret, Engine: migEngine},
		Flashback: &handler.FlashbackHandler{Store: db, Secret: cfg.App.JWTSecret, Cfg: cfg, Engine: fbEngine},
		Archive:   &handler.ArchiveHandler{Store: db, Secret: cfg.App.JWTSecret, Engine: arcEngine},
		Kafka:     &handler.KafkaHandler{Store: db, Secret: cfg.App.JWTSecret},
		Redis:     &handler.RedisHandler{Store: db, Secret: cfg.App.JWTSecret},
		Insight:   &handler.InsightHandler{Store: db, Secret: cfg.App.JWTSecret},
		Session:   &handler.SessionHandler{Store: db, Secret: cfg.App.JWTSecret},
		Multilang: &handler.MultilangHandler{Store: db},
		External:  &handler.ExternalHandler{Cfg: cfg},
		DCL:       &handler.DCLHandler{Store: db, Secret: cfg.App.JWTSecret},
		RBAC:      &handler.RBACHandler{Store: db, Secret: cfg.App.JWTSecret},
		Ldap:      &handler.LdapHandler{Store: db, Cfg: cfg},
		Sso:       &handler.SsoHandler{Store: db, Cfg: cfg},
		SMTP:      &handler.SMTPHandler{Store: db, Cfg: cfg},
		Notify:    &handler.NotifyHandler{Store: db, Cfg: cfg},
		Terminal:  &terminal.Handler{Store: db, Cfg: cfg, Secret: cfg.App.JWTSecret},
		Static:    staticRoot,
	})

	collectCtx, collectCancel := context.WithCancel(context.Background())
	defer collectCancel()
	go pgdiag.StartCollector(collectCtx, db, cfg.App.JWTSecret)
	migEngine.StartScheduler(collectCtx)
	arcEngine.StartScheduler(collectCtx)
	_ = flashback.Bootstrap(collectCtx)
	go handler.StartIssueAutoCancel(collectCtx, ticketH)

	addr := fmt.Sprintf("%s:%d", cfg.HTTP.Host, cfg.HTTP.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      engine,
		ReadTimeout:  time.Duration(cfg.HTTP.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.HTTP.WriteTimeout) * time.Second,
	}
	go func() {
		fmt.Fprintf(os.Stderr, "auto-dba listening on %s\n", addr)
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
