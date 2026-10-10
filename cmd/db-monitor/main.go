package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"

	"auto-dba/internal/config"
	"auto-dba/internal/server"
)

func main() {
	configPath := flag.String("config", "config/config.yaml", "path to config file")
	flag.Parse()

	cmd := "start"
	if args := flag.Args(); len(args) > 0 {
		cmd = args[0]
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	switch cmd {
	case "start":
		pid := cfg.Services.DbMonitor.Port
		_ = pid
		if cfg.HTTP.PidFile != "" {
			// 独立 pid：在原 pid 旁写 db-monitor 文件，避免与主进程冲突
			p := cfg.HTTP.PidFile + ".db-monitor"
			_ = os.WriteFile(p, []byte(strconv.Itoa(os.Getpid())+"\n"), 0644)
			defer func() { _ = os.Remove(p) }()
		}
		server.RunDbMonitor(cfg, *configPath)
	case "status":
		fmt.Println("check GET http://<host>:<db_monitor.port>/healthz")
	default:
		log.Fatalf("unknown command %q (start)", cmd)
	}
}
