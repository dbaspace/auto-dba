package main

import (
	"flag"
	"log"
	"os"
	"strconv"

	"auto-dba/internal/config"
	"auto-dba/internal/server"
)

// 临时起一份后端（覆盖 http.port），用于自测而不打断用户已有 8800 进程。
func main() {
	configPath := flag.String("config", "config/config.yaml", "config path")
	port := flag.Int("port", 18800, "listen port override")
	flag.Parse()
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	cfg.HTTP.Port = *port
	cfg.HTTP.PidFile = ""
	log.Printf("run_on_port listen %s:%d (nacos port overridden)", cfg.HTTP.Host, cfg.HTTP.Port)
	if cfg.HTTP.PidFile != "" {
		_ = os.WriteFile(cfg.HTTP.PidFile, []byte(strconv.Itoa(os.Getpid())+"\n"), 0644)
	}
	server.Run(cfg, *configPath)
}
