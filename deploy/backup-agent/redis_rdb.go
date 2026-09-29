package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// handleRedisRDB：在公共主机用 redis-cli --rdb 拉取 RDB，流式回传平台。
func handleRedisRDB(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Password string `json:"password"`
		TimeoutS int    `json:"timeout_seconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	host := strings.TrimSpace(req.Host)
	if host == "" || req.Port <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "host/port required"})
		return
	}
	timeout := time.Duration(req.TimeoutS) * time.Second
	if timeout <= 0 {
		timeout = time.Hour
	}
	if timeout > 6*time.Hour {
		timeout = 6 * time.Hour
	}

	dir := filepath.Join(*workdir, "redis-rdb")
	_ = os.MkdirAll(dir, 0o750)
	jobID := newID()
	outPath := filepath.Join(dir, fmt.Sprintf("dump_%s.rdb", jobID[:12]))
	defer func() { _ = os.Remove(outPath) }()

	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	if *dryRun {
		// 最小合法 RDB 头（REDIS0009 + EOF），便于联调
		stub := []byte("REDIS0009\xff")
		if err := os.WriteFile(outPath, stub, 0o640); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": err.Error()})
			return
		}
	} else {
		if err := runRedisCLIDump(ctx, host, req.Port, req.Username, req.Password, outPath); err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": err.Error()})
			return
		}
	}

	fi, err := os.Stat(outPath)
	if err != nil || fi.Size() <= 0 {
		msg := "empty rdb"
		if err != nil {
			msg = err.Error()
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": msg})
		return
	}
	if err := validateRDBMagic(outPath); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	sum, _ := fileSHA256(outPath)

	f, err := os.Open(outPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="dump.rdb"`)
	w.Header().Set("X-Job-Id", jobID)
	w.Header().Set("X-Size-Bytes", strconv.FormatInt(fi.Size(), 10))
	if sum != "" {
		w.Header().Set("X-Checksum", "sha256:"+sum)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, f)
}

func runRedisCLIDump(ctx context.Context, host string, port int, username, password, outPath string) error {
	bin, err := exec.LookPath("redis-cli")
	if err != nil {
		return fmt.Errorf("redis-cli not found: 请在 Agent 主机安装 redis-cli")
	}
	args := []string{"-h", host, "-p", strconv.Itoa(port)}
	if u := strings.TrimSpace(username); u != "" && !strings.EqualFold(u, "default") {
		args = append(args, "--user", u)
	}
	args = append(args, "--rdb", outPath)
	cmd := exec.CommandContext(ctx, bin, args...)
	env := append([]string{}, os.Environ()...)
	if strings.TrimSpace(password) != "" {
		env = append(env, "REDISCLI_AUTH="+password)
	}
	cmd.Env = env
	var errBuf strings.Builder
	cmd.Stdout = &errBuf
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errBuf.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("redis-cli --rdb 失败: %s", trim(msg))
	}
	return nil
}

func validateRDBMagic(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var hdr [9]byte
	n, err := io.ReadFull(f, hdr[:])
	if err != nil && err != io.ErrUnexpectedEOF {
		return fmt.Errorf("读取 RDB 头失败: %w", err)
	}
	if n < 5 {
		return fmt.Errorf("文件过小，不是有效 RDB")
	}
	s := string(hdr[:])
	if strings.HasPrefix(s, "REDIS") || strings.HasPrefix(s, "VALKEY") {
		return nil
	}
	return fmt.Errorf("非法 RDB magic（期望 REDIS/VALKEY）")
}
