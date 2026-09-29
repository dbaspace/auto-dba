package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// MHA Manager Agent：部署在 masterha_manager 同机，将 HTTP 转为白名单 masterha_* 调用。
// 平台登记控制器 Base URL 指向本服务；不替代 masterha_manager 自动 failover。

var (
	listen         = flag.String("listen", ":8088", "listen address")
	user           = flag.String("user", "", "basic auth username")
	pass           = flag.String("pass", "", "basic auth password")
	insecureNoAuth = flag.Bool("insecure-no-auth", false, "allow empty -user (dev only)")
	confRoot       = flag.String("conf-root", "/etc/mha", "allowed conf directory")
	workdir        = flag.String("workdir", "/tmp/auto-dba-mha-agent", "package download directory")
	dryRun         = flag.Bool("dry-run", false, "do not exec masterha_*; return ok stubs")
	mu             sync.Mutex
)

func main() {
	flag.Parse()
	requireAuthFlags("mha-agent")
	_ = os.MkdirAll(*workdir, 0o750)
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", withAuth(handleHealth))
	mux.HandleFunc("/v1/mha/status", withAuth(handleStatus))
	mux.HandleFunc("/v1/mha/repl", withAuth(handleRepl))
	mux.HandleFunc("/v1/mha/switch/planned", withAuth(handleSwitchPlanned))
	mux.HandleFunc("/v1/mha/switch/dead", withAuth(handleSwitchDead))
	mux.HandleFunc("/v1/mha/conf", withAuth(handlePutConf))
	mux.HandleFunc("/v1/mha/manager/status", withAuth(handleMHAManagerStatus))
	mux.HandleFunc("/v1/mha/manager/start", withAuth(handleMHAManagerCtrl))
	mux.HandleFunc("/v1/mha/manager/stop", withAuth(handleMHAManagerCtrl))
	mux.HandleFunc("/v1/mha/manager/restart", withAuth(handleMHAManagerCtrl))
	mux.HandleFunc("/v1/mha/package/install", withAuth(handleMHAPackageInstall))
	mux.HandleFunc("/v1/mha/package/jobs/", withAuth(handleMHAPackageJob))
	log.Printf("mha-agent listen=%s conf-root=%s dry-run=%v", *listen, *confRoot, *dryRun)
	serveAgent("mha-agent", *listen, mux, 10*time.Minute)
}

func requireAuthFlags(agentName string) {
	if strings.TrimSpace(*user) == "" {
		if !*insecureNoAuth {
			log.Fatalf("%s: -user/-pass 必填（本地调试可加 -insecure-no-auth）", agentName)
		}
		log.Printf("WARNING: %s 以无鉴权模式启动（-insecure-no-auth）", agentName)
		return
	}
	if *pass == "" {
		log.Fatalf("%s: 设置 -user 时必须同时设置 -pass", agentName)
	}
}

func serveAgent(name, addr string, handler http.Handler, writeTimeout time.Duration) {
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       120 * time.Second,
	}
	log.Printf("%s http.Server timeouts: readHeader=10s read=60s write=%v idle=120s", name, writeTimeout)
	log.Fatal(srv.ListenAndServe())
}

func withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(*user) != "" {
			u, p, ok := r.BasicAuth()
			if !ok || u != *user || p != *pass {
				w.Header().Set("WWW-Authenticate", `Basic realm="mha-agent"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next(w, r)
	}
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	conf, err := resolveConf(r.URL.Query().Get("conf"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	out, code, err := runMasterha(2*time.Minute, "masterha_check_status", "--conf="+conf)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": err.Error(), "output": out, "exit_code": code})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "check_status ok", "output": out})
}

func handleRepl(w http.ResponseWriter, r *http.Request) {
	conf, err := resolveConf(r.URL.Query().Get("conf"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	out, code, err := runMasterha(2*time.Minute, "masterha_check_repl", "--conf="+conf)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": err.Error(), "output": out, "exit_code": code})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "check_repl ok", "output": out})
}

func handleSwitchPlanned(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Conf          string `json:"conf"`
		NewMasterHost string `json:"new_master_host"`
		NewMasterPort int    `json:"new_master_port"`
		TimeoutSec    int    `json:"timeout_seconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	conf, err := resolveConf(req.Conf)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	if strings.TrimSpace(req.NewMasterHost) == "" || req.NewMasterPort <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "new_master_host/port required"})
		return
	}
	timeout := time.Duration(req.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	args := []string{
		"--conf=" + conf,
		"--master_state=alive",
		"--new_master_host=" + req.NewMasterHost,
		fmt.Sprintf("--new_master_port=%d", req.NewMasterPort),
		"--interactive=0",
	}
	out, code, err := runMasterha(timeout, "masterha_master_switch", args...)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": err.Error(), "output": trimOut(out), "exit_code": code})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "planned switch ok", "output": trimOut(out)})
}

func handlePutConf(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Conf    string `json:"conf"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "content required"})
		return
	}
	conf, err := resolveConf(req.Conf)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	if *dryRun {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "dry-run write skipped", "conf": conf, "bytes": len(req.Content)})
		return
	}
	dir := filepath.Dir(conf)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": "mkdir: " + err.Error()})
		return
	}
	tmp := conf + ".tmp." + fmt.Sprintf("%d", time.Now().UnixNano())
	if err := os.WriteFile(tmp, []byte(req.Content), 0o600); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": "write: " + err.Error()})
		return
	}
	if err := os.Rename(tmp, conf); err != nil {
		_ = os.Remove(tmp)
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": "rename: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "conf written", "conf": conf, "bytes": len(req.Content)})
}

func resolveConf(raw string) (string, error) {
	p := strings.TrimSpace(raw)
	if p == "" {
		p = filepath.Join(*confRoot, "app.cnf")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	root, err := filepath.Abs(*confRoot)
	if err != nil {
		return "", err
	}
	sep := string(os.PathSeparator)
	if abs != root && !strings.HasPrefix(abs, root+sep) {
		return "", fmt.Errorf("conf outside allowed root: %s", abs)
	}
	return abs, nil
}

func runMasterha(timeout time.Duration, bin string, args ...string) (string, int, error) {
	mu.Lock()
	defer mu.Unlock()
	if *dryRun {
		return fmt.Sprintf("dry-run %s %s", bin, strings.Join(args, " ")), 0, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		code := -1
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
		return string(out), code, fmt.Errorf("%s failed: %w", bin, err)
	}
	return string(out), 0, nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func trimOut(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 4000 {
		return s[len(s)-4000:]
	}
	return s
}
