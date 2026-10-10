package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	confFileMaxBytes = 1 << 20
	confBackupKeep   = 10
)

// confReadablePath file/get 只允许读 StarRocks 主配置，避免变成任意文件读取口子。
func confReadablePath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" || !filepath.IsAbs(p) {
		return "", errors.New("path 必须为绝对路径")
	}
	clean := filepath.Clean(p)
	if clean != p && clean+"/" != p {
		return "", errors.New("path 不允许包含 .. 或多余分隔符")
	}
	slash := filepath.ToSlash(clean)
	if !strings.HasSuffix(slash, "/conf/fe.conf") && !strings.HasSuffix(slash, "/conf/be.conf") {
		return "", errors.New("仅允许读取 */conf/fe.conf 或 */conf/be.conf")
	}
	return clean, nil
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// readExistingConf 读取覆盖前的旧文件；不存在返回 nil, nil。
func readExistingConf(path string) ([]byte, error) {
	st, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		return nil, errors.New(path + " 是目录")
	}
	if st.Size() > confFileMaxBytes {
		return nil, errors.New(path + " 超过 1MB，拒绝读取")
	}
	return os.ReadFile(path)
}

// backupConfFile 复制为 <path>.bak.<时间戳>，并只保留最近 confBackupKeep 份。
func backupConfFile(path string, content []byte, mode os.FileMode, now time.Time) (string, error) {
	bak := path + ".bak." + now.Format("20060102150405.000")
	if err := os.WriteFile(bak, content, mode); err != nil {
		return "", err
	}
	pruneConfBackups(path, confBackupKeep)
	return bak, nil
}

func pruneConfBackups(path string, keep int) {
	matches, err := filepath.Glob(path + ".bak.*")
	if err != nil || len(matches) <= keep {
		return
	}
	// 时间戳后缀定长，字典序即时间序
	sort.Strings(matches)
	for _, m := range matches[:len(matches)-keep] {
		_ = os.Remove(m)
	}
}

// handleFileGet POST /v1/sr/file/get {path} — 只读远端 fe.conf / be.conf，供平台做下发前对比。
func handleFileGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	path, err := confReadablePath(req.Path)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	st, err := os.Stat(path)
	if os.IsNotExist(err) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "exists": false, "content": ""})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	b, err := readExistingConf(path)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "exists": true, "content": string(b), "sha256": sha256Hex(b),
		"mtime": st.ModTime().UTC().Format(time.RFC3339),
	})
}
