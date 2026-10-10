// print_dev_api_proxy 与后端相同路径 Load 配置，向 stdout 打印一行 JSON：
//   {"api_target":"http://127.0.0.1:8804","vite_host":"127.0.0.1","vite_port":5174}
// 用法（仓库根目录）：go run ./scripts/print_dev_api_proxy [-config config/config.yaml]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"auto-dba/internal/config"
)

type out struct {
	APITarget string `json:"api_target"`
	ViteHost  string `json:"vite_host"`
	VitePort  int    `json:"vite_port"`
}

func main() {
	cfgPath := flag.String("config", "config/config.yaml", "local config path (nacos bootstrap)")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "print_dev_api_proxy: %v\n", err)
		os.Exit(1)
	}

	host := strings.TrimSpace(cfg.HTTP.Host)
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	port := cfg.HTTP.Port
	if port <= 0 {
		port = 8800
	}

	vh := strings.TrimSpace(cfg.Frontend.DevHost)
	if vh == "" || vh == "0.0.0.0" {
		vh = "127.0.0.1"
	}
	vp := cfg.Frontend.DevPort
	if vp <= 0 {
		vp = 5174
	}

	b, err := json.Marshal(out{
		APITarget: fmt.Sprintf("http://%s:%d", host, port),
		ViteHost:  vh,
		VitePort:  vp,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "print_dev_api_proxy: encode: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(b))
}
