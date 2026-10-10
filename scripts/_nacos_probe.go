package main
import (
  "fmt"
  "auto-dba/internal/config"
)
func main() {
  cfg, err := config.Load("/tmp/nacos_probe.yaml")
  if err != nil {
    fmt.Println("ERR:", err)
    return
  }
  fmt.Println("OK jwt=", cfg.App.JWTSecret, "dsn_set=", cfg.DB.DSN != "")
}
