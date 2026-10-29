package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNamespacePublicAlias(t *testing.T) {
	nc := NacosConfig{Namespace: "public"}
	applyNacosEnvOverrides(&nc)
	if nc.Namespace != "" {
		t.Fatalf("public should map to empty, got %q", nc.Namespace)
	}
	nc2 := NacosConfig{Namespace: "PUBLIC"}
	applyNacosEnvOverrides(&nc2)
	if nc2.Namespace != "" {
		t.Fatalf("PUBLIC should map to empty, got %q", nc2.Namespace)
	}
	nc3 := NacosConfig{Namespace: "e4dec2e2-4711-432f-9110-cc9b67d66b81"}
	applyNacosEnvOverrides(&nc3)
	if nc3.Namespace != "e4dec2e2-4711-432f-9110-cc9b67d66b81" {
		t.Fatalf("autosql id changed: %q", nc3.Namespace)
	}
}

func TestParseServerAddrs(t *testing.T) {
	one, err := parseServerAddrs("10.100.113.151:8848")
	if err != nil || len(one) != 1 || one[0].IpAddr != "10.100.113.151" || one[0].Port != 8848 {
		t.Fatalf("single: %+v err=%v", one, err)
	}
	multi, err := parseServerAddrs("10.0.0.1:8848, 10.0.0.2:8848 ,10.0.0.3:8848")
	if err != nil || len(multi) != 3 {
		t.Fatalf("multi: %+v err=%v", multi, err)
	}
	if multi[1].IpAddr != "10.0.0.2" || multi[2].Port != 8848 {
		t.Fatalf("multi detail: %+v", multi)
	}
	if _, err := parseServerAddrs(""); err == nil {
		t.Fatal("empty should fail")
	}
	if _, err := parseServerAddrs("no-port"); err == nil {
		t.Fatal("no port should fail")
	}
}

func TestLoadMergesNacosHTTPPort(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
nacos:
  enabled: true
  server_addr: "127.0.0.1:8848"
http:
  host: "127.0.0.1"
  port: 8800
  read_timeout: 30
  pid_file: /tmp/x.pid
app:
  jwt_secret: local
db:
  dsn: "postgres://local/db"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	orig := fetchNacosConfig
	fetchNacosConfig = func(NacosConfig) (string, error) {
		return `
app:
  jwt_secret: from-nacos
db:
  dsn: "postgres://nacos/db"
http:
  port: 9000
`, nil
	}
	t.Cleanup(func() { fetchNacosConfig = orig })

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTP.Port != 9000 {
		t.Fatalf("port=%d want 9000 from nacos", cfg.HTTP.Port)
	}
	if cfg.HTTP.Host != "127.0.0.1" {
		t.Fatalf("host=%q should keep local when nacos only sets port", cfg.HTTP.Host)
	}
	if cfg.HTTP.ReadTimeout != 30 || cfg.HTTP.PidFile != "/tmp/x.pid" {
		t.Fatalf("http timeouts/pid wiped: %+v", cfg.HTTP)
	}
}

func TestLoadMergesNacosOverLocal(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	local := `
nacos:
  enabled: true
  server_addr: "127.0.0.1:8848"
  namespace: "ns-test"
  data_id: auto-dba.yaml
app:
  jwt_secret: local-secret
db:
  dsn: "postgres://local/db"
terminal:
  ssh_user: localuser
`
	if err := os.WriteFile(cfgPath, []byte(local), 0o600); err != nil {
		t.Fatal(err)
	}
	_, bare := testRSAMaterial(t)
	// 模拟生产：裸 Base64 多行；物化后应写成 RSA PEM
	var b64lines []string
	for i := 0; i < len(bare); i += 64 {
		end := i + 64
		if end > len(bare) {
			end = len(bare)
		}
		b64lines = append(b64lines, "    "+bare[i:end])
	}
	remote := `
app:
  jwt_secret: nacos-secret
db:
  dsn: "postgres://nacos/db"
terminal:
  ssh_user: nacosuser
  ssh_private_key: |
` + strings.Join(b64lines, "\n") + "\n"
	orig := fetchNacosConfig
	fetchNacosConfig = func(nc NacosConfig) (string, error) {
		if nc.Namespace != "ns-test" || nc.ServerAddr != "127.0.0.1:8848" {
			t.Fatalf("boot=%+v", nc)
		}
		return remote, nil
	}
	t.Cleanup(func() { fetchNacosConfig = orig })

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.App.JWTSecret != "nacos-secret" || cfg.DB.DSN != "postgres://nacos/db" {
		t.Fatalf("merge failed: jwt=%q dsn=%q", cfg.App.JWTSecret, cfg.DB.DSN)
	}
	if cfg.Terminal.SSHUser != "nacosuser" {
		t.Fatalf("ssh_user=%q", cfg.Terminal.SSHUser)
	}
	if cfg.Terminal.SSHPrivateKey != "" {
		t.Fatal("pem should be cleared after materialize")
	}
	if cfg.Terminal.SSHKeyFingerprint == "" {
		t.Fatal("fingerprint should be set")
	}
	if !strings.HasSuffix(cfg.Terminal.SSHIdentityFile, ".runtime_id_ed25519") {
		t.Fatalf("identity=%q", cfg.Terminal.SSHIdentityFile)
	}
	b, err := os.ReadFile(cfg.Terminal.SSHIdentityFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "BEGIN RSA PRIVATE KEY") {
		t.Fatalf("pem file: %q", b[:min(80, len(b))])
	}
	st, err := os.Stat(cfg.Terminal.SSHIdentityFile)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm()&0o077 != 0 {
		t.Fatalf("perm=%o want 0600", st.Mode().Perm())
	}
	// 引导未被远程覆盖
	if !cfg.Nacos.Enabled || cfg.Nacos.Namespace != "ns-test" {
		t.Fatalf("nacos boot=%+v", cfg.Nacos)
	}
}

func TestLoadNacosDisabledKeepsLocal(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
nacos:
  enabled: false
app:
  jwt_secret: only-local
db:
  dsn: "postgres://only/local"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	orig := fetchNacosConfig
	fetchNacosConfig = func(NacosConfig) (string, error) {
		t.Fatal("should not call nacos")
		return "", nil
	}
	t.Cleanup(func() { fetchNacosConfig = orig })

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.App.JWTSecret != "only-local" {
		t.Fatalf("got %q", cfg.App.JWTSecret)
	}
}

func TestLoadNacosFetchError(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
nacos:
  enabled: true
  server_addr: "10.100.113.151:8848"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	orig := fetchNacosConfig
	fetchNacosConfig = func(NacosConfig) (string, error) {
		return "", fmt.Errorf("unreachable")
	}
	t.Cleanup(func() { fetchNacosConfig = orig })

	_, err := Load(cfgPath)
	if err == nil || !strings.Contains(err.Error(), "nacos") {
		t.Fatalf("err=%v", err)
	}
}

func TestNacosCacheFileName(t *testing.T) {
	if got := nacosCacheFileName("auto-dba.yaml", "DEFAULT_GROUP", ""); got != "auto-dba.yaml@@DEFAULT_GROUP@@" {
		t.Fatalf("got %q", got)
	}
	if got := nacosCacheFileName("auto-dba.yaml", "", "public"); got != "auto-dba.yaml@@DEFAULT_GROUP@@" {
		t.Fatalf("public ns: %q", got)
	}
}

func TestReadLatestNacosDiskCache(t *testing.T) {
	root := t.TempDir()
	old := nacosDiskCacheRoot
	nacosDiskCacheRoot = func() string { return root }
	t.Cleanup(func() { nacosDiskCacheRoot = old })

	nc := NacosConfig{DataID: "auto-dba.yaml", Group: "DEFAULT_GROUP"}
	if _, _, err := readLatestNacosDiskCache(nc); err == nil {
		t.Fatal("want miss")
	}
	dir := filepath.Join(root, "public-1", "cache", "config")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := nacosCacheFileName(nc.DataID, nc.Group, nc.Namespace)
	body := "http:\n  port: 8800\napp:\n  jwt_secret: from-cache\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got, path, err := readLatestNacosDiskCache(nc)
	if err != nil || !strings.Contains(got, "from-cache") || path == "" {
		t.Fatalf("got=%q path=%q err=%v", got, path, err)
	}
}
