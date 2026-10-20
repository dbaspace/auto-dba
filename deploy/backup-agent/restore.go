package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func handleLogicalRestore(w http.ResponseWriter, r *http.Request) {
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
		Database  string   `json:"database"`
		Databases []string `json:"databases"`
		LocalPath string   `json:"local_path"`
		ObjectKey string   `json:"object_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	if strings.TrimSpace(req.Host) == "" || req.Port <= 0 || strings.TrimSpace(req.Username) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "host/port/username required"})
		return
	}
	dumpPath := strings.TrimSpace(req.LocalPath)
	if dumpPath == "" {
		dumpPath = strings.TrimSpace(req.ObjectKey)
	}
	if dumpPath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "local_path or object_key required"})
		return
	}
	// object_key 可能是相对前缀路径；若非绝对路径则拼 workdir
	if !filepath.IsAbs(dumpPath) {
		dumpPath = filepath.Join(*workdir, dumpPath)
	}
	targetDB := strings.TrimSpace(req.Database)
	if targetDB == "" && len(req.Databases) > 0 {
		targetDB = strings.TrimSpace(req.Databases[0])
	}
	engine := strings.ToLower(strings.TrimSpace(req.Engine))
	jobID := newID()
	putJob(jobID, &jobState{ID: jobID, Status: "running"})
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "job_id": jobID, "status": "running"})

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
		defer cancel()
		var logOut string
		var err error
		if *dryRun {
			logOut = "dry-run restore ok"
		} else {
			switch engine {
			case "postgres", "postgresql", "pg":
				if targetDB == "" {
					err = fmt.Errorf("postgres restore requires database")
				} else {
					logOut, err = runPGRestore(ctx, req.Host, req.Port, req.Username, req.Password, targetDB, dumpPath)
				}
			case "mysql", "tidb", "oceanbase":
				if targetDB == "" {
					err = fmt.Errorf("mysql restore requires database")
				} else {
					logOut, err = runMySQLRestore(ctx, req.Host, req.Port, req.Username, req.Password, targetDB, dumpPath)
				}
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
		putJob(jobID, &jobState{
			ID: jobID, Status: "success", Message: trim(logOut), LocalPath: dumpPath,
		})
	}()
}

func runPGRestore(ctx context.Context, host string, port int, user, pass, db, dumpPath string) (string, error) {
	if _, err := os.Stat(dumpPath); err != nil {
		return "", fmt.Errorf("dump not found: %w", err)
	}
	bin, err := exec.LookPath("pg_restore")
	if err != nil {
		return "", fmt.Errorf("pg_restore not found")
	}
	cmd := exec.CommandContext(ctx, bin,
		"-h", host, "-p", fmt.Sprintf("%d", port), "-U", user, "-d", db,
		"--no-owner", "--no-acl", "-Fc", dumpPath,
	)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+pass)
	b, err := cmd.CombinedOutput()
	out := string(b)
	// pg_restore 对部分对象冲突常返回非 0；若输出含 ERROR 才算失败
	if err != nil && strings.Contains(strings.ToUpper(out), "ERROR") {
		return out, fmt.Errorf("pg_restore failed: %w", err)
	}
	return out, nil
}

func handlePhysicalRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Engine      string   `json:"engine"`
		LocalPath   string   `json:"local_path"`
		ObjectKey   string   `json:"object_key"`
		DataDir     string   `json:"data_dir"`
		ConfirmStop bool     `json:"confirm_stop"`
		ChainDirs   []string `json:"chain_dirs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	if !req.ConfirmStop {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "confirm_stop required (target must be stopped)"})
		return
	}
	dataDir := strings.TrimSpace(req.DataDir)
	if dataDir == "" || !filepath.IsAbs(dataDir) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "absolute data_dir required"})
		return
	}
	chain := absChainDirs(req.ChainDirs)
	src := strings.TrimSpace(req.LocalPath)
	if src == "" {
		src = strings.TrimSpace(req.ObjectKey)
	}
	if len(chain) == 0 {
		if src == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "local_path or object_key or chain_dirs required"})
			return
		}
		if !filepath.IsAbs(src) {
			src = filepath.Join(*workdir, src)
		}
		chain = []string{src}
	}
	engine := strings.ToLower(strings.TrimSpace(req.Engine))
	jobID := newID()
	putJob(jobID, &jobState{ID: jobID, Status: "running"})
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "job_id": jobID, "status": "running"})

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
		defer cancel()
		var logOut string
		var err error
		if *dryRun {
			logOut = fmt.Sprintf("dry-run physical restore ok chain=%d", len(chain))
		} else {
			switch engine {
			case "postgres", "postgresql", "pg":
				if len(chain) > 1 {
					err = fmt.Errorf("postgres physical incr chain not supported; use PITR")
				} else {
					logOut, err = runPGPhysicalRestore(ctx, chain[0], dataDir)
				}
			case "mysql", "tidb", "oceanbase":
				if len(chain) > 1 {
					logOut, err = runMySQLPhysicalChainRestore(ctx, chain, dataDir)
				} else {
					logOut, err = runMySQLPhysicalRestore(ctx, chain[0], dataDir)
				}
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
		putJob(jobID, &jobState{ID: jobID, Status: "success", Message: trim(logOut), LocalPath: dataDir})
	}()
}

func absChainDirs(dirs []string) []string {
	var out []string
	for _, d := range dirs {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		if !filepath.IsAbs(d) {
			d = filepath.Join(*workdir, d)
		}
		out = append(out, d)
	}
	return out
}

func runPGPhysicalRestore(ctx context.Context, backupDir, dataDir string) (string, error) {
	fi, err := os.Stat(backupDir)
	if err != nil {
		return "", fmt.Errorf("backup path: %w", err)
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("postgres physical restore expects backup directory (pg_basebackup -D), got file")
	}
	if err := os.MkdirAll(filepath.Dir(dataDir), 0o750); err != nil {
		return "", err
	}
	// 目标目录须为空或可重建
	if entries, _ := os.ReadDir(dataDir); len(entries) > 0 {
		return "", fmt.Errorf("data_dir 非空，请先停库并清空: %s", dataDir)
	}
	_ = os.RemoveAll(dataDir)
	cmd := exec.CommandContext(ctx, "cp", "-a", backupDir, dataDir)
	b, err := cmd.CombinedOutput()
	out := string(b)
	if err != nil {
		return out, fmt.Errorf("copy backup to data_dir failed: %w", err)
	}
	return out + "\ncopied to " + dataDir + "; start postgres manually", nil
}

func lookXtrabackup() (string, error) {
	bin, err := exec.LookPath("xtrabackup")
	if err != nil {
		bin, err = exec.LookPath("mariabackup")
		if err != nil {
			return "", fmt.Errorf("xtrabackup/mariabackup not found")
		}
	}
	return bin, nil
}

func runMySQLPhysicalRestore(ctx context.Context, backupDir, dataDir string) (string, error) {
	fi, err := os.Stat(backupDir)
	if err != nil {
		return "", fmt.Errorf("backup path: %w", err)
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("mysql physical restore expects xtrabackup directory")
	}
	bin, err := lookXtrabackup()
	if err != nil {
		return "", err
	}
	var logs strings.Builder
	prepare := exec.CommandContext(ctx, bin, "--prepare", "--target-dir="+backupDir)
	b, err := prepare.CombinedOutput()
	logs.Write(b)
	if err != nil {
		return logs.String(), fmt.Errorf("xtrabackup --prepare failed: %w", err)
	}
	if entries, _ := os.ReadDir(dataDir); len(entries) > 0 {
		return logs.String(), fmt.Errorf("data_dir 非空，请先停库并清空: %s", dataDir)
	}
	copyBack := exec.CommandContext(ctx, bin, "--copy-back", "--target-dir="+backupDir, "--datadir="+dataDir)
	b2, err := copyBack.CombinedOutput()
	logs.Write(b2)
	if err != nil {
		return logs.String(), fmt.Errorf("xtrabackup --copy-back failed: %w", err)
	}
	logs.WriteString("\ncopy-back done; start mysqld manually")
	return logs.String(), nil
}

// runMySQLPhysicalChainRestore chain[0]=base, chain[1:]=incremental dirs（xtrabackup 增量拼接）。
func runMySQLPhysicalChainRestore(ctx context.Context, chain []string, dataDir string) (string, error) {
	if len(chain) < 2 {
		return runMySQLPhysicalRestore(ctx, chain[0], dataDir)
	}
	bin, err := lookXtrabackup()
	if err != nil {
		return "", err
	}
	base := chain[0]
	for i, d := range chain {
		fi, err := os.Stat(d)
		if err != nil {
			return "", fmt.Errorf("chain[%d] %s: %w", i, d, err)
		}
		if !fi.IsDir() {
			return "", fmt.Errorf("chain[%d] must be directory: %s", i, d)
		}
	}
	var logs strings.Builder
	// base: apply-log-only
	cmd := exec.CommandContext(ctx, bin, "--prepare", "--apply-log-only", "--target-dir="+base)
	b, err := cmd.CombinedOutput()
	logs.Write(b)
	if err != nil {
		return logs.String(), fmt.Errorf("prepare base --apply-log-only failed: %w", err)
	}
	for i := 1; i < len(chain); i++ {
		inc := chain[i]
		args := []string{"--prepare", "--target-dir=" + base, "--incremental-dir=" + inc}
		if i < len(chain)-1 {
			args = []string{"--prepare", "--apply-log-only", "--target-dir=" + base, "--incremental-dir=" + inc}
		}
		cmd = exec.CommandContext(ctx, bin, args...)
		b, err = cmd.CombinedOutput()
		logs.Write(b)
		if err != nil {
			return logs.String(), fmt.Errorf("prepare incr[%d] failed: %w", i, err)
		}
	}
	if entries, _ := os.ReadDir(dataDir); len(entries) > 0 {
		return logs.String(), fmt.Errorf("data_dir 非空，请先停库并清空: %s", dataDir)
	}
	copyBack := exec.CommandContext(ctx, bin, "--copy-back", "--target-dir="+base, "--datadir="+dataDir)
	b2, err := copyBack.CombinedOutput()
	logs.Write(b2)
	if err != nil {
		return logs.String(), fmt.Errorf("xtrabackup --copy-back failed: %w", err)
	}
	logs.WriteString(fmt.Sprintf("\nchain restore done segments=%d; start mysqld manually", len(chain)))
	return logs.String(), nil
}

func runMySQLRestore(ctx context.Context, host string, port int, user, pass, db, dumpPath string) (string, error) {
	if _, err := os.Stat(dumpPath); err != nil {
		return "", fmt.Errorf("dump not found: %w", err)
	}
	bin, err := exec.LookPath("mysql")
	if err != nil {
		return "", fmt.Errorf("mysql client not found")
	}
	f, err := os.Open(dumpPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	cmd := exec.CommandContext(ctx, bin,
		"-h", host, "-P", fmt.Sprintf("%d", port), "-u", user, db,
	)
	cmd.Env = append(os.Environ(), "MYSQL_PWD="+pass)
	cmd.Stdin = f
	var errBuf strings.Builder
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return errBuf.String(), err
}

func handlePITRRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Engine         string `json:"engine"`
		LocalPath      string `json:"local_path"`
		ObjectKey      string `json:"object_key"`
		DataDir        string `json:"data_dir"`
		ConfirmStop    bool   `json:"confirm_stop"`
		RecoverTo      string `json:"recover_to"`
		WalOrBinlogDir string `json:"wal_or_binlog_dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	if !req.ConfirmStop || strings.TrimSpace(req.DataDir) == "" || strings.TrimSpace(req.RecoverTo) == "" || strings.TrimSpace(req.WalOrBinlogDir) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "confirm_stop, data_dir, recover_to, wal_or_binlog_dir required"})
		return
	}
	src := strings.TrimSpace(req.LocalPath)
	if src == "" {
		src = strings.TrimSpace(req.ObjectKey)
	}
	if src == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "local_path or object_key required"})
		return
	}
	if !filepath.IsAbs(src) {
		src = filepath.Join(*workdir, src)
	}
	engine := strings.ToLower(strings.TrimSpace(req.Engine))
	jobID := newID()
	putJob(jobID, &jobState{ID: jobID, Status: "running"})
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "job_id": jobID, "status": "running"})

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
		defer cancel()
		var logOut string
		var err error
		if *dryRun {
			logOut = "dry-run pitr ok recover_to=" + req.RecoverTo
		} else {
			switch engine {
			case "postgres", "postgresql", "pg":
				logOut, err = runPGPITR(ctx, src, req.DataDir, req.WalOrBinlogDir, req.RecoverTo)
			case "mysql", "tidb", "oceanbase":
				logOut, err = runMySQLPITR(ctx, src, req.DataDir, req.WalOrBinlogDir, req.RecoverTo)
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
		putJob(jobID, &jobState{ID: jobID, Status: "success", Message: trim(logOut), LocalPath: req.DataDir})
	}()
}

func runPGPITR(ctx context.Context, backupDir, dataDir, walDir, recoverTo string) (string, error) {
	out, err := runPGPhysicalRestore(ctx, backupDir, dataDir)
	if err != nil {
		return out, err
	}
	if _, err := os.Stat(walDir); err != nil {
		return out, fmt.Errorf("wal dir: %w", err)
	}
	// recovery.signal + postgresql.auto.conf 片段
	autoConf := filepath.Join(dataDir, "postgresql.auto.conf")
	frag := fmt.Sprintf("\n# auto-dba pitr\nrestore_command = 'cp %s/%%f %%p'\nrecovery_target_time = '%s'\nrecovery_target_action = 'promote'\n",
		strings.TrimRight(walDir, "/"), recoverTo)
	f, err := os.OpenFile(autoConf, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
	if err != nil {
		return out, err
	}
	_, _ = f.WriteString(frag)
	_ = f.Close()
	_ = os.WriteFile(filepath.Join(dataDir, "recovery.signal"), []byte(""), 0o640)
	return out + "\nPITR configured recover_to=" + recoverTo + "; start postgres to apply WAL then promote", nil
}

func runMySQLPITR(ctx context.Context, backupDir, dataDir, binlogDir, recoverTo string) (string, error) {
	out, err := runMySQLPhysicalRestore(ctx, backupDir, dataDir)
	if err != nil {
		return out, err
	}
	if _, err := os.Stat(binlogDir); err != nil {
		return out, fmt.Errorf("binlog dir: %w", err)
	}
	bin, err := exec.LookPath("mysqlbinlog")
	if err != nil {
		return out, fmt.Errorf("mysqlbinlog not found; physical restore done, apply binlogs manually to %s", recoverTo)
	}
	entries, err := os.ReadDir(binlogDir)
	if err != nil {
		return out, err
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		files = append(files, filepath.Join(binlogDir, name))
	}
	if len(files) == 0 {
		return out, fmt.Errorf("binlog dir empty")
	}
	// 写出可人工/脚本执行的 SQL 文件；不停写库（实例可能未起）
	sqlOut := filepath.Join(dataDir, "auto-dba-pitr-binlog.sql")
	args := append([]string{"--stop-datetime=" + recoverTo}, files...)
	cmd := exec.CommandContext(ctx, bin, args...)
	f, err := os.Create(sqlOut)
	if err != nil {
		return out, err
	}
	defer f.Close()
	cmd.Stdout = f
	var errBuf strings.Builder
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return out + errBuf.String(), fmt.Errorf("mysqlbinlog failed: %w", err)
	}
	return out + "\nmysqlbinlog written to " + sqlOut + "; start mysqld then: mysql < " + sqlOut, nil
}
