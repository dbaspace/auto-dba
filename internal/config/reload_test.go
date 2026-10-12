package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiffConfigAllSections(t *testing.T) {
	old := &Config{
		App:      AppConfig{JWTSecret: "a", Name: "x"},
		HTTP:     HTTPConfig{Host: "0.0.0.0", Port: 8800, ReadTimeout: 30},
		Frontend: FrontendConfig{DevHost: "127.0.0.1", DevPort: 5174},
		Log:      LogConfig{Level: "info"},
		DB:       DBConfig{DSN: "dsn1", MaxOpenConns: 10},
		CDC:      CDCConfig{Enabled: false},
		Terminal: TerminalConfig{MaxLifetimeSec: 1800, Enabled: true},
		External: ExternalConfig{StarRocksFEURL: ""},
		Backup:   BackupConfig{WorkDir: "/tmp/a"},
		Copilot:  CopilotConfig{BaseURL: "https://api.cursor.com", APIKey: "old"},
	}
	next := &Config{
		App:      AppConfig{JWTSecret: "b", Name: "x"},
		HTTP:     HTTPConfig{Host: "0.0.0.0", Port: 8804, ReadTimeout: 60},
		Frontend: FrontendConfig{DevHost: "127.0.0.1", DevPort: 5175},
		Log:      LogConfig{Level: "debug"},
		DB:       DBConfig{DSN: "dsn1", MaxOpenConns: 20},
		CDC:      CDCConfig{Enabled: true, DebeziumURL: "http://x"},
		Terminal: TerminalConfig{MaxLifetimeSec: 28000, Enabled: true},
		External: ExternalConfig{StarRocksFEURL: "http://fe"},
		Backup:   BackupConfig{WorkDir: "/tmp/b"},
		Copilot:  CopilotConfig{BaseURL: "https://api.cursor.com", APIKey: "new"},
	}
	d := DiffConfig(old, next)
	if !d.Any() {
		t.Fatal("expected Any")
	}
	if !d.App || !d.JWTSecret || !d.HTTP || !d.ListenAddr || !d.Frontend || !d.FrontendDev ||
		!d.Log || !d.DB || !d.DBPool || !d.CDC || !d.Terminal || !d.External || !d.Backup || !d.Copilot {
		t.Fatalf("diff=%+v sections=%v", d, d.Sections())
	}
	// 仅改 http.read_timeout，也应命中 http 段（整段比较）
	onlyTO := *old
	onlyTO.HTTP.ReadTimeout = 90
	d2 := DiffConfig(old, &onlyTO)
	if !d2.HTTP || d2.ListenAddr {
		t.Fatalf("read_timeout-only: %+v", d2)
	}
	// 仅改 max_lifetime_sec
	onlyLife := *old
	onlyLife.Terminal.MaxLifetimeSec = 9999
	d3 := DiffConfig(old, &onlyLife)
	if !d3.Terminal || !d3.Any() {
		t.Fatalf("lifetime-only: %+v", d3)
	}
}

func TestMergeRemoteYAMLPreservesBoot(t *testing.T) {
	dir := t.TempDir()
	local := filepath.Join(dir, "config.yaml")
	content := []byte(`
nacos:
  enabled: true
  server_addr: "127.0.0.1:8848"
  namespace: ""
  group: DEFAULT_GROUP
  data_id: auto-dba.yaml
  username: nacos
  password: "x"
http:
  host: "0.0.0.0"
  port: 8800
frontend:
  dev_port: 5174
app:
  name: t
  jwt_ttl_hours: 1
db:
  driver: postgres
  dsn: "postgres://u:p@127.0.0.1:5432/db?sslmode=disable"
log:
  level: info
`)
	if err := os.WriteFile(local, content, 0o600); err != nil {
		t.Fatal(err)
	}
	remote := `
http:
  port: 8809
app:
  name: auto-dba
  jwt_secret: "remote-secret-at-least-32-chars!!"
  jwt_ttl_hours: 24
frontend:
  dev_port: 5199
db:
  driver: postgres
  dsn: "postgres://u:p@127.0.0.1:5432/db?sslmode=disable"
log:
  level: debug
terminal:
  max_lifetime_sec: 28000
`
	next, err := MergeRemoteYAML(local, remote)
	if err != nil {
		t.Fatal(err)
	}
	if next.HTTP.Port != 8809 {
		t.Fatalf("port=%d", next.HTTP.Port)
	}
	if next.Frontend.DevPort != 5199 {
		t.Fatalf("dev_port=%d", next.Frontend.DevPort)
	}
	if next.Terminal.MaxLifetimeSec != 28000 {
		t.Fatalf("lifetime=%d", next.Terminal.MaxLifetimeSec)
	}
	if next.Nacos.ServerAddr != "127.0.0.1:8848" || !next.Nacos.Enabled {
		t.Fatalf("boot lost: %+v", next.Nacos)
	}
}
