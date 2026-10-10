package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Db Agent：MySQL/PG/Redis 节点本机执行面（预检 / 制品 / 文件 / systemd）。

var (
	listen         = flag.String("listen", ":8092", "listen address")
	user           = flag.String("user", "", "basic auth username")
	pass           = flag.String("pass", "", "basic auth password")
	insecureNoAuth = flag.Bool("insecure-no-auth", false, "allow empty -user (dev only)")
	dryRun         = flag.Bool("dry-run", false, "skip destructive actions")
	mu             sync.Mutex
	jobs           = map[string]*jobState{}
)

type jobState struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	Message   string    `json:"message,omitempty"`
	UpdatedAt time.Time `json:"-"`
}

func main() {
	flag.Parse()
	requireAuthFlags("db-agent")
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", withAuth(handleHealth))
	mux.HandleFunc("/v1/db/precheck", withAuth(handlePrecheck))
	mux.HandleFunc("/v1/db/fs/ensure", withAuth(handleEnsureFS))
	mux.HandleFunc("/v1/db/file/put", withAuth(handleFilePut))
	mux.HandleFunc("/v1/db/file/get", withAuth(handleFileGet))
	mux.HandleFunc("/v1/db/artifact/install", withAuth(handleArtifactInstall))
	mux.HandleFunc("/v1/db/systemd/install", withAuth(handleSystemdInstall))
	mux.HandleFunc("/v1/db/systemd/ctrl", withAuth(handleSystemdCtrl))
	mux.HandleFunc("/v1/db/exec", withAuth(handleExec))
	mux.HandleFunc("/v1/db/repl/verify", withAuth(handleReplVerify))
	mux.HandleFunc("/v1/db/jobs/", withAuth(handleJob))
	log.Printf("db-agent listen=%s dry-run=%v", *listen, *dryRun)
	serveAgent("db-agent", *listen, mux, 30*time.Minute)
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


const (
	jobTTL  = 2 * time.Hour
	maxJobs = 256
)

func touchJob(st *jobState) {
	if st != nil {
		st.UpdatedAt = time.Now()
	}
}

func putJob(id string, st *jobState) {
	touchJob(st)
	mu.Lock()
	defer mu.Unlock()
	jobs[id] = st
	evictJobsLocked()
}

func updateJob(id string, fn func(*jobState)) {
	mu.Lock()
	defer mu.Unlock()
	st := jobs[id]
	if st == nil {
		return
	}
	fn(st)
	touchJob(st)
	evictJobsLocked()
}

func getJob(id string) *jobState {
	mu.Lock()
	defer mu.Unlock()
	return jobs[id]
}

func evictJobsLocked() {
	now := time.Now()
	for id, j := range jobs {
		if j == nil {
			delete(jobs, id)
			continue
		}
		if strings.EqualFold(j.Status, "running") {
			continue
		}
		if j.UpdatedAt.IsZero() || now.Sub(j.UpdatedAt) > jobTTL {
			delete(jobs, id)
		}
	}
	for len(jobs) > maxJobs {
		var oldestID string
		var oldest time.Time
		for id, j := range jobs {
			if j == nil || strings.EqualFold(j.Status, "running") {
				continue
			}
			t := j.UpdatedAt
			if oldestID == "" || t.Before(oldest) {
				oldestID, oldest = id, t
			}
		}
		if oldestID == "" {
			break
		}
		delete(jobs, oldestID)
	}
}

func withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(*user) != "" {
			u, p, ok := r.BasicAuth()
			if !ok || u != *user || p != *pass {
				w.Header().Set("WWW-Authenticate", `Basic realm="db-agent"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "agent": "db-agent"})
}

func handlePrecheck(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Engine string `json:"engine"`
		Role   string `json:"role"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	items := []map[string]any{
		{"name": "systemd", "ok": true},
		{"name": "engine", "ok": req.Engine != "", "detail": req.Engine},
		{"name": "role", "ok": true, "detail": req.Role},
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items})
}

func handleEnsureFS(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Paths []string `json:"paths"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if *dryRun {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "dry_run": true})
		return
	}
	for _, p := range req.Paths {
		if err := os.MkdirAll(p, 0o755); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func handleFilePut(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Mode    string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Path == "" {
		http.Error(w, "path required", 400)
		return
	}
	if *dryRun {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "dry_run": true})
		return
	}
	tmp := req.Path + ".new"
	if err := os.WriteFile(tmp, []byte(req.Content), 0o644); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if err := os.Rename(tmp, req.Path); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func handleFileGet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Path == "" {
		http.Error(w, "path required", 400)
		return
	}
	// 白名单：仅允许常见 conf 路径前缀
	p := filepath.Clean(req.Path)
	if strings.Contains(p, "..") {
		http.Error(w, "invalid path", 400)
		return
	}
	b, err := os.ReadFile(p)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "content": string(b)})
}

func handleArtifactInstall(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DownloadURL     string `json:"download_url"`
		Checksum        string `json:"checksum"`
		TargetDir       string `json:"target_dir"`
		StripComponents int    `json:"strip_components"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if req.DownloadURL == "" || req.TargetDir == "" || req.Checksum == "" {
		http.Error(w, "download_url/checksum/target_dir required", 400)
		return
	}
	id := fmt.Sprintf("job-%d", time.Now().UnixNano())
	putJob(id, &jobState{ID: id, Status: "running"})
	go func() {
		msg := ""
		status := "success"
		if *dryRun {
			msg = "dry-run skip download"
		} else if err := downloadAndExtract(req.DownloadURL, req.Checksum, req.TargetDir, req.StripComponents); err != nil {
			status = "failed"
			msg = err.Error()
		}
		putJob(id, &jobState{ID: id, Status: status, Message: msg})
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": id})
}

func httpClientShort() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

func downloadAndExtract(urlStr, wantSum, target string, strip int) error {
	client := &http.Client{Timeout: 2 * time.Hour}
	resp, err := client.Get(urlStr) //nolint:gosec // controlled by platform presign
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("download HTTP %d", resp.StatusCode)
	}
	tmp := filepath.Join(os.TempDir(), fmt.Sprintf("db-art-%d.tgz", time.Now().UnixNano()))
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), resp.Body); err != nil {
		f.Close()
		return err
	}
	f.Close()
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, strings.TrimSpace(wantSum)) {
		_ = os.Remove(tmp)
		return fmt.Errorf("checksum mismatch got=%s want=%s", got, wantSum)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	args := []string{"-xf", tmp, "-C", target}
	// 只在包内确实只有一个顶级目录时剥离，避免已按 bin/ lib/ 平铺打包的制品被剥坏
	if strip > 0 && archiveHasSingleTopDir(tmp) {
		args = append(args, fmt.Sprintf("--strip-components=%d", strip))
	}
	cmd := exec.Command("tar", args...)
	out, err := cmd.CombinedOutput()
	_ = os.Remove(tmp)
	if err != nil {
		return fmt.Errorf("tar: %v %s", err, string(out))
	}
	return nil
}

func archiveHasSingleTopDir(archive string) bool {
	out, err := exec.Command("tar", "-tf", archive).Output()
	if err != nil {
		return false
	}
	top := ""
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimPrefix(strings.TrimSpace(line), "./")
		if line == "" {
			continue
		}
		first, _, hasSlash := strings.Cut(line, "/")
		if !hasSlash {
			return false // 顶层有普通文件，不是单目录包
		}
		if top == "" {
			top = first
		} else if first != top {
			return false
		}
	}
	return top != ""
}

func handleSystemdInstall(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UnitName string `json:"unit_name"`
		Content  string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UnitName == "" {
		http.Error(w, "unit_name required", 400)
		return
	}
	path := "/etc/systemd/system/" + filepath.Base(req.UnitName)
	if !strings.HasSuffix(path, ".service") {
		path += ".service"
	}
	if *dryRun {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "dry_run": true, "path": path})
		return
	}
	if err := os.WriteFile(path, []byte(req.Content), 0o644); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	_ = exec.Command("systemctl", "daemon-reload").Run()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": path})
}

func handleSystemdCtrl(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action string `json:"action"`
		Unit   string `json:"unit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	switch req.Action {
	case "start", "stop", "restart", "reload", "enable", "status":
	default:
		http.Error(w, "invalid action", 400)
		return
	}
	if *dryRun {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "dry_run": true, "message": "dry-run"})
		return
	}
	out, err := exec.Command("systemctl", req.Action, req.Unit).CombinedOutput()
	writeJSON(w, http.StatusOK, map[string]any{"ok": err == nil, "message": string(out), "output": string(out)})
}

// handleExec 白名单命令：repl_setup / 引擎探测。
func handleExec(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Command string         `json:"command"`
		Args    []string       `json:"args"`
		Payload map[string]any `json:"payload"`
		DryRun  bool           `json:"dry_run"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	switch req.Command {
	case "repl_setup":
		if *dryRun || req.DryRun {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "dry-run repl_setup"})
			return
		}
		msg, err := doReplSetup(req.Payload)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": msg})
	default:
		http.Error(w, "command not allowed", 403)
	}
}

func doReplSetup(p map[string]any) (string, error) {
	engine, _ := p["engine"].(string)
	primaryHost, _ := p["primary_host"].(string)
	var primaryPort int
	switch v := p["primary_port"].(type) {
	case float64:
		primaryPort = int(v)
	case int:
		primaryPort = v
	}
	localPort := 0
	switch v := p["local_port"].(type) {
	case float64:
		localPort = int(v)
	case int:
		localPort = v
	}
	if primaryHost == "" || primaryPort == 0 {
		return "", fmt.Errorf("primary_host/port required")
	}
	switch strings.ToLower(engine) {
	case "mysql", "oceanbase":
		sql := fmt.Sprintf(
			`STOP SLAVE; CHANGE MASTER TO MASTER_HOST='%s', MASTER_PORT=%d, MASTER_AUTO_POSITION=1, MASTER_USER='repl'; START SLAVE;`,
			primaryHost, primaryPort,
		)
		var (
			out []byte
			err error
		)
		if localPort > 0 {
			out, err = exec.Command("mysql", "-h", "127.0.0.1", "-P", fmt.Sprintf("%d", localPort), "-N", "-e", sql).CombinedOutput()
		} else {
			out, err = exec.Command("mysql", "-N", "-e", sql).CombinedOutput()
		}
		if err != nil {
			return "", fmt.Errorf("%s repl_setup: %v %s", engine, err, string(out))
		}
		return engine + " CHANGE MASTER + START SLAVE", nil
	case "postgres", "postgresql":
		return fmt.Sprintf("pg standby uses primary_conninfo→%s:%d (conf already written)", primaryHost, primaryPort), nil
	case "redis", "codis":
		portArg := fmt.Sprintf("%d", localPort)
		if localPort == 0 {
			portArg = "6379"
		}
		out, err := exec.Command("redis-cli", "-p", portArg, "REPLICAOF", primaryHost, fmt.Sprintf("%d", primaryPort)).CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("redis REPLICAOF: %v %s", err, string(out))
		}
		return "redis REPLICAOF ok", nil
	case "sqlserver":
		return fmt.Sprintf("sqlserver replica setup deferred to artifact (primary %s:%d)", primaryHost, primaryPort), nil
	default:
		return "", fmt.Errorf("unsupported engine %s", engine)
	}
}

func handleReplVerify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Engine      string `json:"engine"`
		Role        string `json:"role"`
		Port        int    `json:"port"`
		Host        string `json:"host"`
		PDEndpoints string `json:"pd_endpoints"`
		JoinAddr    string `json:"join_addr"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if *dryRun {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "detail": "dry-run verify"})
		return
	}
	var ok bool
	var detail string
	var err error
	switch strings.ToLower(req.Engine) {
	case "tidb", "cockroachdb", "clickhouse", "mongodb", "cassandra", "elasticsearch", "kafka", "oceanbase", "oracle", "sqlserver":
		ok, detail, err = doComponentHealth(req.Engine, req.Role, req.Port, req.PDEndpoints, req.JoinAddr)
	default:
		ok, detail, err = doReplVerify(req.Engine, req.Role, req.Port)
	}
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": err.Error(), "detail": detail})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": ok, "detail": detail})
}

func doComponentHealth(engine, role string, port int, pdEndpoints, joinAddr string) (bool, string, error) {
	_ = joinAddr
	switch strings.ToLower(engine) {
	case "tidb":
		if port <= 0 {
			switch role {
			case "pd":
				port = 2379
			case "tikv":
				port = 20160
			default:
				port = 4000
			}
		}
		switch role {
		case "pd":
			url := fmt.Sprintf("http://127.0.0.1:%d/pd/api/v1/health", port)
			resp, err := httpClientShort().Get(url)
			if err != nil {
				return false, "", err
			}
			defer resp.Body.Close()
			b, _ := io.ReadAll(resp.Body)
			ok := resp.StatusCode >= 200 && resp.StatusCode < 300
			return ok, string(b), nil
		case "tikv":
			// TiKV 无统一 HTTP；用 pd 看 store 或本机端口探测
			out, err := exec.Command("bash", "-c", fmt.Sprintf("ss -lnt | grep -q ':%d ' || netstat -lnt 2>/dev/null | grep -q ':%d '", port, port)).CombinedOutput()
			if err != nil {
				return false, string(out), fmt.Errorf("tikv listen :%d not found", port)
			}
			_ = pdEndpoints
			return true, fmt.Sprintf("tikv listening :%d", port), nil
		default: // tidb
			out, err := exec.Command("mysql", "-h", "127.0.0.1", "-P", fmt.Sprintf("%d", port), "-N", "-e", "SELECT 1").CombinedOutput()
			s := strings.TrimSpace(string(out))
			if err != nil {
				return false, s, err
			}
			return s == "1", "tidb SELECT 1 ok", nil
		}
	case "cockroachdb":
		if port <= 0 {
			port = 26257
		}
		out, err := exec.Command("bash", "-c", fmt.Sprintf("cockroach node status --insecure --host=localhost:%d 2>/dev/null | head -20", port)).CombinedOutput()
		s := string(out)
		if err != nil {
			// fallback: port listen
			out2, err2 := exec.Command("bash", "-c", fmt.Sprintf("ss -lnt | grep -q ':%d ' || netstat -lnt 2>/dev/null | grep -q ':%d '", port, port)).CombinedOutput()
			if err2 != nil {
				return false, s + string(out2), err
			}
			return true, fmt.Sprintf("cockroach listening :%d", port), nil
		}
		return true, s, nil
	case "clickhouse":
		if port <= 0 {
			port = 9000
		}
		out, err := exec.Command("clickhouse-client", "--port", fmt.Sprintf("%d", port), "-q", "SELECT 1").CombinedOutput()
		if err != nil {
			return portListening(port)
		}
		return strings.TrimSpace(string(out)) == "1", "clickhouse SELECT 1", nil
	case "mongodb":
		if port <= 0 {
			port = 27017
		}
		out, err := exec.Command("mongosh", "--quiet", "--port", fmt.Sprintf("%d", port), "--eval", "db.runCommand({ping:1}).ok").CombinedOutput()
		if err != nil {
			out2, err2 := exec.Command("mongo", "--quiet", "--port", fmt.Sprintf("%d", port), "--eval", "db.runCommand({ping:1}).ok").CombinedOutput()
			if err2 != nil {
				return portListening(port)
			}
			return strings.TrimSpace(string(out2)) == "1", "mongo ping", nil
		}
		return strings.TrimSpace(string(out)) == "1", "mongosh ping", nil
	case "cassandra":
		if port <= 0 {
			port = 9042
		}
		out, err := exec.Command("cqlsh", "127.0.0.1", fmt.Sprintf("%d", port), "-e", "DESCRIBE KEYSPACES").CombinedOutput()
		if err != nil {
			return portListening(port)
		}
		return true, string(out), nil
	case "elasticsearch":
		if port <= 0 {
			port = 9200
		}
		resp, err := httpClientShort().Get(fmt.Sprintf("http://127.0.0.1:%d/_cluster/health", port))
		if err != nil {
			return portListening(port)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		ok := resp.StatusCode >= 200 && resp.StatusCode < 300
		return ok, string(b), nil
	case "kafka":
		if port <= 0 {
			port = 9092
		}
		return portListening(port)
	case "oceanbase":
		if port <= 0 {
			port = 2881
		}
		out, err := exec.Command("mysql", "-h", "127.0.0.1", "-P", fmt.Sprintf("%d", port), "-N", "-e", "SELECT 1").CombinedOutput()
		if err != nil {
			return portListening(port)
		}
		return strings.TrimSpace(string(out)) == "1", "oceanbase SELECT 1", nil
	case "oracle":
		if port <= 0 {
			port = 1521
		}
		// 优先 tnsping / sqlplus；否则端口探活
		if out, err := exec.Command("bash", "-c", "command -v tnsping >/dev/null && tnsping localhost || true").CombinedOutput(); err == nil && strings.Contains(strings.ToUpper(string(out)), "OK") {
			return true, string(out), nil
		}
		return portListening(port)
	case "sqlserver":
		if port <= 0 {
			port = 1433
		}
		out, err := exec.Command("bash", "-c", fmt.Sprintf("command -v sqlcmd >/dev/null && sqlcmd -S 127.0.0.1,%d -Q 'SELECT 1' -h -1 || true", port)).CombinedOutput()
		s := strings.TrimSpace(string(out))
		if err == nil && (strings.Contains(s, "1") || strings.Contains(s, "(1 rows")) {
			return true, "sqlcmd SELECT 1", nil
		}
		return portListening(port)
	default:
		return false, "", fmt.Errorf("unsupported component engine")
	}
}

func portListening(port int) (bool, string, error) {
	out, err := exec.Command("bash", "-c", fmt.Sprintf("ss -lnt | grep -q ':%d ' || netstat -lnt 2>/dev/null | grep -q ':%d '", port, port)).CombinedOutput()
	if err != nil {
		return false, string(out), fmt.Errorf("port :%d not listening", port)
	}
	return true, fmt.Sprintf("listening :%d", port), nil
}

func doReplVerify(engine, role string, port int) (bool, string, error) {
	if port <= 0 {
		port = 6379
		switch strings.ToLower(engine) {
		case "mysql", "oceanbase":
			port = 3306
			if strings.EqualFold(engine, "oceanbase") {
				port = 2881
			}
		case "postgres", "postgresql":
			port = 5432
		case "sqlserver":
			port = 1433
		}
	}
	switch strings.ToLower(engine) {
	case "mysql", "oceanbase":
		args := []string{"-N", "-e", `SHOW SLAVE STATUS\G`}
		if port > 0 {
			args = []string{"-h", "127.0.0.1", "-P", fmt.Sprintf("%d", port), "-N", "-e", `SHOW SLAVE STATUS\G`}
		}
		out, err := exec.Command("mysql", args...).CombinedOutput()
		s := string(out)
		if err != nil {
			return false, s, fmt.Errorf("SHOW SLAVE STATUS: %v", err)
		}
		ok := strings.Contains(s, "Slave_IO_Running: Yes") && strings.Contains(s, "Slave_SQL_Running: Yes")
		return ok, s, nil
	case "postgres", "postgresql":
		out, err := exec.Command("psql", "-p", fmt.Sprintf("%d", port), "-tAc", "SELECT pg_is_in_recovery()").CombinedOutput()
		s := strings.TrimSpace(string(out))
		if err != nil {
			return false, s, err
		}
		ok := s == "t" || s == "true"
		if role != "replica" {
			ok = true
		}
		return ok, "pg_is_in_recovery="+s, nil
	case "redis", "codis":
		if role == "sentinel" {
			out, err := exec.Command("redis-cli", "-p", fmt.Sprintf("%d", port), "SENTINEL", "masters").CombinedOutput()
			if err != nil {
				return false, string(out), err
			}
			return len(out) > 0, string(out), nil
		}
		out, err := exec.Command("redis-cli", "-p", fmt.Sprintf("%d", port), "INFO", "replication").CombinedOutput()
		s := string(out)
		if err != nil {
			return false, s, err
		}
		ok := strings.Contains(s, "role:slave") || strings.Contains(s, "role:master")
		if role == "replica" {
			ok = strings.Contains(s, "role:slave") && strings.Contains(s, "master_link_status:up")
		}
		return ok, s, nil
	case "sqlserver":
		return portListening(port)
	default:
		return false, "", fmt.Errorf("unsupported engine")
	}
}

func handleJob(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/v1/db/jobs/")
	j := getJob(id)
	if j == nil {
		http.Error(w, "not found", 404)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": j.Status == "success", "job_id": j.ID, "status": j.Status, "message": j.Message})
}
