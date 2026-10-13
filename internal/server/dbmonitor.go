package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"auto-dba/internal/config"
	"auto-dba/internal/dbmonitor"
	"auto-dba/internal/handler"
	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

// RunDbMonitor 独立启动数据库监控服务（可单独 Pod 部署）。
// 与主服务共用 JWT Secret + 元数据库，便于互通鉴权与实例发现。
func RunDbMonitor(cfg *config.Config, configPath string) {
	if configPath == "" {
		configPath = "config/config.yaml"
	}
	if cfg.App.RunMode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}
	db, err := store.Open(cfg.DB.Driver, cfg.DB.DSN, store.PoolSettings{
		MaxOpenConns:    cfg.DB.MaxOpenConns,
		MaxIdleConns:    cfg.DB.MaxIdleConns,
		ConnMaxLifetime:  time.Duration(cfg.DB.ConnMaxLifetimeSec) * time.Second,
		ConnMaxIdleTime: time.Duration(cfg.DB.ConnMaxIdleTimeSec) * time.Second,
	})
	if err != nil {
		log.Fatalf("db-monitor open db: %v", err)
	}
	defer db.Close()

	svc := dbmonitor.New(db, cfg.FieldCrypto())
	h := &handler.DbMonitorHandler{Svc: svc}
	hot := &hotReloadTargets{cfg: cfg, db: db, secrets: []*string{&svc.Secret}}
	if err := config.StartNacosWatchOpts(cfg, configPath, config.WatchOptions{
		BeforeApply: hot.beforeApply,
		AfterApply: func(diff config.ReloadDiff) {
			hot.apply(diff)
			if diff.Services {
				log.Printf("nacos reload(db-monitor): services 变更请重启本进程（当前仍绑 %s）", cfg.DbMonitorListenAddr())
				if cfg.Nacos.RestartOnListenChange {
					if err := config.SelfRestart(); err != nil {
						log.Printf("nacos reload(db-monitor): self-restart failed: %v", err)
					}
				}
			}
		},
	}); err != nil {
		log.Printf("nacos watch(db-monitor): start failed: %v", err)
	}

	engine := gin.New()
	engine.Use(gin.Recovery(), gin.Logger())
	engine.Use(cors.New(cors.Config{
		AllowOriginFunc: func(origin string) bool {
			return strings.HasPrefix(origin, "http://localhost:") ||
				strings.HasPrefix(origin, "http://127.0.0.1:") ||
				origin == ""
		},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization", "X-Request-Id"},
		AllowCredentials: true,
	}))

	engine.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "db-monitor"})
	})
	engine.GET("/readyz", func(c *gin.Context) {
		if err := db.Ping(); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_ready", "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready", "service": "db-monitor"})
	})

	auth := middleware.Auth(&cfg.App.JWTSecret, db, "/healthz", "/readyz")
	api := engine.Group("/api/v1")
	api.Use(auth)
	dm := api.Group("/db-monitor")
	dm.GET("/catalog", h.Catalog)
	dm.GET("/instances", h.Instances)
	dm.GET("/overview", h.Overview)
	dm.GET("/panels", h.Panels)
	dm.POST("/query", h.Query)

	addr := cfg.DbMonitorListenAddr()
	srv := &http.Server{
		Addr:         addr,
		Handler:      engine,
		ReadTimeout:  time.Duration(cfg.HTTP.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.HTTP.WriteTimeout) * time.Second,
	}
	go func() {
		fmt.Fprintf(os.Stderr, "auto-dba-db-monitor listening on %s (mode=%s)\n", addr, cfg.Services.DbMonitor.Mode)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("db-monitor listen: %v", err)
		}
	}()

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

// NewDbMonitorReverseProxy 将 /api/v1/db-monitor 转发到独立监控服务。
func NewDbMonitorReverseProxy(targetRoot string) (gin.HandlerFunc, error) {
	root := strings.TrimRight(strings.TrimSpace(targetRoot), "/")
	if root == "" {
		return nil, fmt.Errorf("empty db-monitor url")
	}
	u, err := url.Parse(root)
	if err != nil {
		return nil, err
	}
	proxy := httputil.NewSingleHostReverseProxy(u)
	orig := proxy.Director
	proxy.Director = func(req *http.Request) {
		orig(req)
		req.Host = u.Host
		req.Header.Set("X-Forwarded-Host", req.Header.Get("Host"))
		req.Header.Set("X-Forwarded-Proto", u.Scheme)
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, e error) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"message":"db-monitor unavailable: ` + escapeJSON(e.Error()) + `"}`))
	}
	return func(c *gin.Context) {
		proxy.ServeHTTP(c.Writer, c.Request)
	}, nil
}

func escapeJSON(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}
