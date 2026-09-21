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

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	switch cmd {
	case "start":
		if cfg.HTTP.PidFile != "" {
			_ = os.WriteFile(cfg.HTTP.PidFile, []byte(strconv.Itoa(os.Getpid())+"\n"), 0644)
			defer func() { _ = os.Remove(cfg.HTTP.PidFile) }()
		}
		server.Run(cfg)
	case "status":
		fmt.Println("use process manager or check GET /healthz")
	default:
		log.Fatalf("unknown command %q (start)", cmd)
	}
}

func loadConfig(path string) (*config.Config, error) {
	return config.Load(path)
}
