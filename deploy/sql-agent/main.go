package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"auto-dba/internal/sqlexec"
)

var (
	listen = flag.String("listen", ":8093", "监听地址")
	user   = flag.String("user", "", "Basic Auth 用户名")
	pass   = flag.String("pass", "", "Basic Auth 密码")
)

func main() {
	flag.Parse()
	if strings.TrimSpace(*user) == "" || *pass == "" {
		log.Fatal("sql-agent: -user 和 -pass 必填")
	}
	runner := sqlexec.NewRunner()
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", withAuth(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}))
	mux.HandleFunc("/v1/sql/jobs", withAuth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req sqlexec.JobRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "请求无法解析", http.StatusBadRequest)
			return
		}
		if len(req.Targets) == 0 {
			http.Error(w, "没有要执行的库", http.StatusBadRequest)
			return
		}
		id := uuid.NewString()
		writeJSON(w, http.StatusOK, runner.Submit(id, req))
	}))
	mux.HandleFunc("/v1/sql/jobs/", withAuth(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/v1/sql/jobs/")
		id := path
		stop := false
		if strings.HasSuffix(path, "/stop") {
			stop = true
			id = strings.TrimSuffix(path, "/stop")
			id = strings.TrimSuffix(id, "/")
		}
		id = strings.Trim(id, "/")
		if id == "" || strings.Contains(id, "/") {
			writeMissingJob(w, id)
			return
		}
		if stop {
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			view, ok := runner.Stop(id)
			if !ok {
				writeMissingJob(w, id)
				return
			}
			writeJSON(w, http.StatusOK, view)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		after, _ := strconv.Atoi(r.URL.Query().Get("after"))
		view, ok := runner.Get(id, after)
		if !ok {
			writeMissingJob(w, id)
			return
		}
		writeJSON(w, http.StatusOK, view)
	}))
	log.Printf("sql-agent listen=%s", *listen)
	srv := &http.Server{
		Addr:              *listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	log.Fatal(srv.ListenAndServe())
}

func withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != *user || p != *pass {
			w.Header().Set("WWW-Authenticate", `Basic realm="sql-agent"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func writeMissingJob(w http.ResponseWriter, id string) {
	writeJSON(w, http.StatusNotFound, map[string]any{"error": "job_not_found", "id": id})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
