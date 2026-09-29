package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func handlePhysical(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Engine           string `json:"engine"`
		Method           string `json:"method"`
		Host             string `json:"host"`
		Port             int    `json:"port"`
		Username         string `json:"username"`
		Password         string `json:"password"`
		Parallel         int    `json:"parallel"`
		Compress         bool   `json:"compress"`
		IncrementalBased string `json:"incremental_basedir"`
		Dest             struct {
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
	method := strings.TrimSpace(req.Method)
	if method == "" {
		method = "physical_full"
	}
	engine := strings.ToLower(strings.TrimSpace(req.Engine))
	dir := *workdir
	if strings.TrimSpace(req.Dest.LocalPath) != "" {
		dir = req.Dest.LocalPath
	}
	_ = os.MkdirAll(dir, 0o750)
	jobID := newID()
	stamp := time.Now().UTC().Format("20060102T150405Z")
	label := stamp
	outDir := filepath.Join(dir, fmt.Sprintf("phys_%s_%s_%s", sanitize(engine), method, stamp))
	putJob(jobID, &jobState{ID: jobID, Status: "running"})
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "job_id": jobID, "status": "running"})

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
		defer cancel()
		var logOut string
		var err error
		if *dryRun {
			_ = os.MkdirAll(outDir, 0o750)
			stub := filepath.Join(outDir, "backup.stub")
			err = os.WriteFile(stub, []byte("dry-run physical backup\n"), 0o640)
			logOut = "dry-run physical ok"
		} else {
			switch {
			case engine == "postgres" || engine == "postgresql" || engine == "pg":
				if method != "physical_full" {
					err = fmt.Errorf("postgres agent supports physical_full (pg_basebackup) only; incr needs WAL/pgBackRest")
				} else {
					logOut, err = runPGBaseBackup(ctx, req.Host, req.Port, req.Username, req.Password, req.Compress, outDir)
				}
			case engine == "mysql" || engine == "tidb" || engine == "oceanbase":
				logOut, err = runXtraBackup(ctx, req.Host, req.Port, req.Username, req.Password, method, req.IncrementalBased, req.Parallel, outDir)
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
		size, sum, cerr := dirChecksum(outDir)
		if cerr != nil || size <= 0 {
			msg := "empty physical backup"
			if cerr != nil {
				msg = cerr.Error()
			}
			updateJob(jobID, func(st *jobState) {
				st.Status = "failed"
				st.Message = msg
			})
			return
		}
		key := outDir
		if pfx := strings.Trim(req.Dest.Prefix, "/"); pfx != "" {
			key = pfx + "/" + filepath.Base(outDir)
		}
		extra, _ := json.Marshal(map[string]string{"method": method, "engine": engine})
		putJob(jobID, &jobState{
			ID: jobID, Status: "success", Message: trim(logOut), LocalPath: outDir,
			ObjectKey: key, SizeBytes: size, Checksum: "sha256:" + sum,
			SetLabel: label, ExtraJSON: string(extra),
		})
	}()
}

func runPGBaseBackup(ctx context.Context, host string, port int, user, pass string, compress bool, outDir string) (string, error) {
	bin, err := exec.LookPath("pg_basebackup")
	if err != nil {
		return "", fmt.Errorf("pg_basebackup not found")
	}
	_ = os.RemoveAll(outDir)
	args := []string{
		"-h", host,
		"-p", fmt.Sprintf("%d", port),
		"-U", user,
		"-D", outDir,
		"-X", "stream",
		"-c", "fast",
		"-v",
	}
	if compress {
		args = append(args, "-Ft", "-z")
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+pass)
	b, err := cmd.CombinedOutput()
	return string(b), err
}

func lookXtra() (string, error) {
	for _, name := range []string{"xtrabackup", "mariabackup"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("xtrabackup/mariabackup not found")
}

func runXtraBackup(ctx context.Context, host string, port int, user, pass, method, incrBased string, parallel int, outDir string) (string, error) {
	bin, err := lookXtra()
	if err != nil {
		return "", err
	}
	_ = os.RemoveAll(outDir)
	_ = os.MkdirAll(outDir, 0o750)
	args := []string{
		"--backup",
		"--host=" + host,
		fmt.Sprintf("--port=%d", port),
		"--user=" + user,
		"--password=" + pass,
		"--target-dir=" + outDir,
	}
	if parallel > 1 {
		args = append(args, fmt.Sprintf("--parallel=%d", parallel))
	}
	if method == "physical_incr" {
		if strings.TrimSpace(incrBased) == "" {
			return "", fmt.Errorf("incremental_basedir required for physical_incr")
		}
		args = append(args, "--incremental-basedir="+incrBased)
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	b, err := cmd.CombinedOutput()
	return string(b), err
}

func dirChecksum(root string) (int64, string, error) {
	var total int64
	var h hash.Hash = sha256.New()
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		total += info.Size()
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		rel, _ := filepath.Rel(root, path)
		_, _ = h.Write([]byte(rel + "\n"))
		if _, err := io.Copy(h, f); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return 0, "", err
	}
	return total, hex.EncodeToString(h.Sum(nil)), nil
}
