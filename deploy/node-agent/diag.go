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
	"regexp"
	"strconv"
	"strings"
	"time"
)

var unitNameRe = regexp.MustCompile(`^[A-Za-z0-9@._-]+$`)

// maxDiagLogLines 日志文件尾部行数上限；控制面诊断总长有限，避免挤掉前面的 status / journalctl / unit 段。
const maxDiagLogLines = 40

// diagLogNames 只读这些日志文件，避免 diag 变成任意文件读取。
var diagLogNames = map[string][]string{
	"fe": {"fe.out", "fe.warn.log"},
	"be": {"be.out", "be.WARNING"},
}

// handleDiag 启动失败诊断：systemctl status + journalctl + 软件目录 log 下白名单日志末尾。
func handleDiag(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Unit         string `json:"unit"`
		Role         string `json:"role"`
		SoftwarePath string `json:"software_path"`
		Lines        int    `json:"lines"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	lines := req.Lines
	if lines <= 0 {
		lines = 60
	}
	if lines > 300 {
		lines = 300
	}
	var b strings.Builder
	if unit := strings.TrimSpace(req.Unit); unit != "" {
		if !unitNameRe.MatchString(unit) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "invalid unit"})
			return
		}
		if !strings.HasSuffix(unit, ".service") {
			unit += ".service"
		}
		for _, c := range diagUnitCommands(unit, lines) {
			writeDiagSection(&b, strings.Join(c, " "), runDiagCmd(c[0], c[1:]...))
		}
	}
	files, err := diagLogFiles(req.Role, req.SoftwarePath)
	if err != nil {
		writeDiagSection(&b, "日志文件", err.Error())
	}
	for _, f := range files {
		writeDiagSection(&b, "tail "+f, tailFile(f, min(lines, maxDiagLogLines)))
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": strings.TrimSpace(b.String())})
}

// diagUnitCommands unit 相关诊断命令；systemctl cat 用于核对 Type / PIDFile / Environment / ExecStart。
func diagUnitCommands(unit string, lines int) [][]string {
	return [][]string{
		{"systemctl", "status", unit, "-l", "--no-pager"},
		{"journalctl", "-u", unit, "-n", strconv.Itoa(lines), "--no-pager"},
		{"systemctl", "cat", unit, "--no-pager"},
	}
}

func writeDiagSection(b *strings.Builder, title, body string) {
	body = strings.TrimSpace(body)
	if body == "" {
		body = "（无输出）"
	}
	fmt.Fprintf(b, "==== %s ====\n%s\n\n", title, body)
}

func runDiagCmd(name string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	s := string(out)
	// systemctl status 对 failed 单元返回非 0，输出本身才是要看的内容
	if err != nil && strings.TrimSpace(s) == "" {
		return err.Error()
	}
	return s
}

// diagLogFiles 软件目录须为绝对路径且不含 ..；只返回白名单文件名。
func diagLogFiles(role, softwarePath string) ([]string, error) {
	names := diagLogNames[strings.ToLower(strings.TrimSpace(role))]
	if len(names) == 0 {
		return nil, nil
	}
	sp := strings.TrimSpace(softwarePath)
	if sp == "" {
		return nil, nil
	}
	if !filepath.IsAbs(sp) || strings.Contains(sp, "..") {
		return nil, fmt.Errorf("software_path 非法: %s", sp)
	}
	sp = filepath.Clean(sp)
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, filepath.Join(sp, "log", n))
	}
	return out, nil
}

// tailFile 读文件末尾 n 行（最多读 64KB）。
func tailFile(path string, n int) string {
	f, err := os.Open(path)
	if err != nil {
		return err.Error()
	}
	defer f.Close()
	const maxRead = 64 << 10
	st, err := f.Stat()
	if err != nil {
		return err.Error()
	}
	off := st.Size() - maxRead
	if off < 0 {
		off = 0
	}
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return err.Error()
	}
	raw, err := io.ReadAll(f)
	if err != nil {
		return err.Error()
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if off > 0 && len(lines) > 1 {
		lines = lines[1:] // 首行可能被截断
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// handleArtifactRestore 升级回滚：把 target/bak.<suffix>/ 下的目录还原，当前目录移到 bak.<suffix>.failed-<时间>/。
func handleArtifactRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		TargetDir    string   `json:"target_dir"`
		BackupSuffix string   `json:"backup_suffix"`
		Dirs         []string `json:"dirs"`
		DryRun       bool     `json:"dry_run"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	if *dryRun || req.DryRun {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": fmt.Sprintf("dry-run restore %s from bak.%s %v", req.TargetDir, req.BackupSuffix, req.Dirs)})
		return
	}
	msg, errMsg := restoreSubdirsInPlace(req.TargetDir, req.BackupSuffix, req.Dirs)
	if errMsg != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": errMsg})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": msg})
}

func restoreSubdirsInPlace(targetDir, backupSuffix string, dirs []string) (string, string) {
	targetDir = strings.TrimSpace(targetDir)
	if !filepath.IsAbs(targetDir) || strings.Contains(targetDir, "..") {
		return "", "target_dir 非法"
	}
	suffix := strings.TrimSpace(backupSuffix)
	if suffix == "" || strings.ContainsAny(suffix, "/\\") || strings.Contains(suffix, "..") {
		return "", "invalid backup_suffix"
	}
	if len(dirs) == 0 {
		dirs = []string{"bin", "lib"}
	}
	bakRoot := filepath.Join(targetDir, "bak."+suffix)
	for _, name := range dirs {
		name = strings.TrimSpace(name)
		if name == "" || strings.ContainsAny(name, "/\\") || name == "." || name == ".." {
			return "", "invalid dir: " + name
		}
		if st, err := os.Stat(filepath.Join(bakRoot, name)); err != nil || !st.IsDir() {
			return "", fmt.Sprintf("备份不存在：%s（可能已回滚过，或该节点未执行过替换）", filepath.Join(bakRoot, name))
		}
	}
	failedRoot := filepath.Join(targetDir, "bak."+suffix+".failed-"+time.Now().Format("20060102-150405"))
	var notes []string
	for _, name := range dirs {
		name = strings.TrimSpace(name)
		cur := filepath.Join(targetDir, name)
		if _, err := os.Stat(cur); err == nil {
			if err := os.MkdirAll(failedRoot, 0o755); err != nil {
				return strings.Join(notes, "; "), "mkdir: " + err.Error()
			}
			if err := os.Rename(cur, filepath.Join(failedRoot, name)); err != nil {
				return strings.Join(notes, "; "), "move current " + name + ": " + err.Error()
			}
			notes = append(notes, fmt.Sprintf("%s -> %s", cur, filepath.Join(failedRoot, name)))
		}
		if err := os.Rename(filepath.Join(bakRoot, name), cur); err != nil {
			return strings.Join(notes, "; "), "restore " + name + ": " + err.Error()
		}
		notes = append(notes, "restored "+name+" from "+bakRoot)
	}
	return strings.Join(notes, "; "), ""
}
