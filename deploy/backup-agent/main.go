package main

import (
	"context"
	"crypto/rand"
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

// Backup Agent / 平台 Agent：部署在可直连数据库的跳板机，白名单执行备份与 Redis RDB / HA 环境检查。

var (
	listen         = flag.String("listen", ":8090", "listen address")
	user           = flag.String("user", "", "basic auth username")
	pass           = flag.String("pass", "", "basic auth password")
	insecureNoAuth = flag.Bool("insecure-no-auth", false, "allow empty -user (dev only)")
	workdir        = flag.String("workdir", "/tmp/auto-dba-platform-agent", "local dump directory")
	confRoot       = flag.String("conf-root", "/etc/mha", "allowed MHA conf directory for /v1/mha/*")
	dryRun         = flag.Bool("dry-run", false, "skip real dump; write stub file")
	mu             sync.Mutex
	jobs           = map[string]*jobState{}
)

type jobState struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	Message   string    `json:"message,omitempty"`
	LocalPath string    `json:"local_path,omitempty"`
	ObjectKey string    `json:"object_key,omitempty"`
	SizeBytes int64     `json:"size_bytes"`
	Checksum  string    `json:"checksum,omitempty"`
	SetLabel  string    `json:"set_label,omitempty"`
	ExtraJSON string    `json:"extra_json,omitempty"`
	UpdatedAt time.Time `json:"-"`
}

func main() {
	flag.Parse()
	requireAuthFlags("platform-agent")
	_ = os.MkdirAll(*workdir, 0o750)
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", withAuth(handleHealth))
	mux.HandleFunc("/v1/capabilities", withAuth(handleCapabilities))
	mux.HandleFunc("/v1/ha/env-check", withAuth(handleHAEnvCheck))
	mux.HandleFunc("/v1/mha/status", withAuth(handleMHAStatus))
	mux.HandleFunc("/v1/mha/repl", withAuth(handleMHARepl))
	mux.HandleFunc("/v1/mha/switch/planned", withAuth(handleMHASwitchPlanned))
	mux.HandleFunc("/v1/mha/switch/dead", withAuth(handleMHASwitchDead))
	mux.HandleFunc("/v1/mha/conf", withAuth(handleMHAPutConf))
	mux.HandleFunc("/v1/mha/manager/status", withAuth(handleMHAManagerStatus))
	mux.HandleFunc("/v1/mha/manager/start", withAuth(handleMHAManagerCtrl))
	mux.HandleFunc("/v1/mha/manager/stop", withAuth(handleMHAManagerCtrl))
	mux.HandleFunc("/v1/mha/manager/restart", withAuth(handleMHAManagerCtrl))
	mux.HandleFunc("/v1/mha/package/install", withAuth(handleMHAPackageInstall))
	mux.HandleFunc("/v1/mha/package/jobs/", withAuth(handlePackageJob))
	mux.HandleFunc("/v1/pgaf/status", withAuth(handlePGAFStatus))
	mux.HandleFunc("/v1/pgaf/switchover", withAuth(handlePGAFSwitchover))
	mux.HandleFunc("/v1/pgaf/failover", withAuth(handlePGAFFailover))
	mux.HandleFunc("/v1/pgaf/package/install", withAuth(handleMHAPackageInstall))
	mux.HandleFunc("/v1/pgaf/package/jobs/", withAuth(handlePackageJob))
	mux.HandleFunc("/v1/pgaf/service/", withAuth(handlePGAFService))
	mux.HandleFunc("/v1/repmgr/status", withAuth(handleRepmgrStatus))
	mux.HandleFunc("/v1/repmgr/switchover", withAuth(handleRepmgrSwitchover))
	mux.HandleFunc("/v1/ha/hook/run", withAuth(handleHAHookRun))
	mux.HandleFunc("/v1/proxy/conf", withAuth(handleProxyPutConf))
	mux.HandleFunc("/v1/proxy/validate", withAuth(handleProxyValidate))
	mux.HandleFunc("/v1/proxy/service/", withAuth(handleProxyService))
	mux.HandleFunc("/v1/proxy/lvs/ipvs", withAuth(handleProxyLVSIpvs))
	mux.HandleFunc("/v1/proxy/lvs/status", withAuth(handleProxyLVSStatus))
	mux.HandleFunc("/v1/proxy/vip/check", withAuth(handleProxyVIPCheck))
	mux.HandleFunc("/v1/proxy/deploy", withAuth(handleProxyDeploy))
	mux.HandleFunc("/v1/proxy/deploy/jobs/", withAuth(handleProxyDeployJob))
	mux.HandleFunc("/v1/patroni/package/install", withAuth(handleMHAPackageInstall))
	mux.HandleFunc("/v1/patroni/package/jobs/", withAuth(handlePackageJob))
	mux.HandleFunc("/v1/backup/logical", withAuth(handleLogical))
	mux.HandleFunc("/v1/backup/physical", withAuth(handlePhysical))
	mux.HandleFunc("/v1/backup/redis-rdb", withAuth(handleRedisRDB))
	mux.HandleFunc("/v1/backup/jobs/", withAuth(handleJob))
	log.Printf("platform-agent listen=%s workdir=%s conf-root=%s dry-run=%v", *listen, *workdir, *confRoot, *dryRun)
	serveAgent("platform-agent", *listen, mux, 0) // 0：允许 RDB 长流
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
				w.Header().Set("WWW-Authenticate", `Basic realm="backup-agent"`)
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

func handleLogical(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Engine    string   `json:"engine"`
		Host      string   `json:"host"`
		Port      int      `json:"port"`
		Username  string   `json:"username"`
		Password  string   `json:"password"`
		Databases []string `json:"databases"`
		Parallel  int      `json:"parallel"`
		Format    string   `json:"format"`
		Dest      struct {
			Kind      string `json:"kind"`
			LocalPath string `json:"local_path"`
			Prefix    string `json:"prefix"`
		} `json:"dest"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	if strings.TrimSpace(req.Host) == "" || req.Port <= 0 || strings.TrimSpace(req.Username) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "host/port/username required"})
		return
	}
	engine := strings.ToLower(strings.TrimSpace(req.Engine))
	dbs := req.Databases
	if len(dbs) == 0 {
		if engine == "postgres" {
			dbs = []string{"postgres"}
		} else {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "databases required"})
			return
		}
	}
	dir := *workdir
	if strings.TrimSpace(req.Dest.LocalPath) != "" {
		dir = req.Dest.LocalPath
	}
	_ = os.MkdirAll(dir, 0o750)
	jobID := newID()
	putJob(jobID, &jobState{ID: jobID, Status: "running"})
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "job_id": jobID, "status": "running"})

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
		defer cancel()
		stamp := time.Now().UTC().Format("20060102T150405Z")
		base := fmt.Sprintf("%s_%s_%s", sanitize(dbs[0]), jobID[:8], stamp)
		var outPath string
		var err error
		var logOut string
		if *dryRun {
			outPath = filepath.Join(dir, base+".stub")
			err = os.WriteFile(outPath, []byte("dry-run backup stub\n"), 0o640)
			logOut = "dry-run ok"
		} else {
			switch engine {
			case "postgres", "postgresql", "pg":
				outPath = filepath.Join(dir, base+".dump")
				logOut, err = runPGDump(ctx, req.Host, req.Port, req.Username, req.Password, dbs[0], outPath)
			case "mysql", "tidb", "oceanbase":
				outPath = filepath.Join(dir, base+".sql")
				logOut, err = runMySQLDump(ctx, req.Host, req.Port, req.Username, req.Password, dbs, outPath)
			default:
				err = fmt.Errorf("unsupported engine %s", req.Engine)
			}
		}
		if err != nil {
			updateJob(jobID, func(st *jobState) {
				st.Status = "failed"
				st.Message = err.Error() + "; " + logOut
			})
			return
		}
		fi, err := os.Stat(outPath)
		if err != nil || fi.Size() <= 0 {
			msg := "empty dump"
			if err != nil {
				msg = err.Error()
			}
			updateJob(jobID, func(st *jobState) {
				st.Status = "failed"
				st.Message = msg
			})
			return
		}
		sum, _ := fileSHA256(outPath)
		key := outPath
		if pfx := strings.Trim(req.Dest.Prefix, "/"); pfx != "" {
			key = pfx + "/" + filepath.Base(outPath)
		}
		putJob(jobID, &jobState{
			ID: jobID, Status: "success", Message: trim(logOut), LocalPath: outPath,
			ObjectKey: key, SizeBytes: fi.Size(), Checksum: "sha256:" + sum,
		})
	}()
}

func handleJob(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/v1/backup/jobs/")
	id = strings.Trim(id, "/")
	st := getJob(id)
	if st == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "message": "job not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "job": st, "job_id": st.ID, "status": st.Status, "message": st.Message,
		"local_path": st.LocalPath, "object_key": st.ObjectKey, "size_bytes": st.SizeBytes,
		"checksum": st.Checksum, "set_label": st.SetLabel, "extra_json": st.ExtraJSON,
	})
}

func runPGDump(ctx context.Context, host string, port int, user, pass, db, out string) (string, error) {
	bin, err := exec.LookPath("pg_dump")
	if err != nil {
		return "", fmt.Errorf("pg_dump not found")
	}
	cmd := exec.CommandContext(ctx, bin, "-h", host, "-p", fmt.Sprintf("%d", port), "-U", user, "-Fc", "-f", out, db)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+pass)
	b, err := cmd.CombinedOutput()
	return string(b), err
}

func runMySQLDump(ctx context.Context, host string, port int, user, pass string, dbs []string, out string) (string, error) {
	if bin, err := exec.LookPath("mysqldump"); err == nil {
		args := []string{"-h", host, "-P", fmt.Sprintf("%d", port), "-u", user, "--single-transaction", "--routines", "--triggers"}
		args = append(args, dbs...)
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Env = append(os.Environ(), "MYSQL_PWD="+pass)
		f, err := os.Create(out)
		if err != nil {
			return "", err
		}
		defer f.Close()
		cmd.Stdout = f
		var errBuf strings.Builder
		cmd.Stderr = &errBuf
		err = cmd.Run()
		return errBuf.String(), err
	}
	return "", fmt.Errorf("mysqldump not found")
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "db"
	}
	return b.String()
}

func trim(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 2000 {
		return s[:2000] + "…"
	}
	return s
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
