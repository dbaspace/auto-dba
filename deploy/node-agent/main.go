package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Node Agent：StarRocks 节点本机执行面（预检 / 盘 / 文件 / systemd / 白名单 exec）。

var (
	listen         = flag.String("listen", ":8091", "listen address")
	user           = flag.String("user", "", "basic auth username")
	pass           = flag.String("pass", "", "basic auth password")
	insecureNoAuth = flag.Bool("insecure-no-auth", false, "allow empty -user (dev only)")
	dryRun         = flag.Bool("dry-run", false, "skip destructive actions (mkfs/systemd/exec)")
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
	requireAuthFlags("node-agent")
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", withAuth(handleHealth))
	mux.HandleFunc("/v1/sr/precheck", withAuth(handlePrecheck))
	mux.HandleFunc("/v1/sr/disk/init", withAuth(handleDiskInit))
	mux.HandleFunc("/v1/sr/fs/ensure", withAuth(handleEnsureFS))
	mux.HandleFunc("/v1/sr/file/put", withAuth(handleFilePut))
	mux.HandleFunc("/v1/sr/file/get", withAuth(handleFileGet))
	mux.HandleFunc("/v1/sr/artifact/install", withAuth(handleArtifactInstall))
	mux.HandleFunc("/v1/sr/systemd/install", withAuth(handleSystemdInstall))
	mux.HandleFunc("/v1/sr/systemd/ctrl", withAuth(handleSystemdCtrl))
	mux.HandleFunc("/v1/sr/exec", withAuth(handleExec))
	mux.HandleFunc("/v1/sr/diag", withAuth(handleDiag))
	mux.HandleFunc("/v1/sr/artifact/restore", withAuth(handleArtifactRestore))
	mux.HandleFunc("/v1/sr/java/check", withAuth(handleJavaCheck))
	mux.HandleFunc("/v1/sr/java/install", withAuth(handleJavaInstall))
	mux.HandleFunc("/v1/sr/jobs/", withAuth(handleJob))
	log.Printf("node-agent listen=%s dry-run=%v", *listen, *dryRun)
	serveAgent("node-agent", *listen, mux, 30*time.Minute)
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
				w.Header().Set("WWW-Authenticate", `Basic realm="node-agent"`)
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
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func handlePrecheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Role      string `json:"role"`
		HTTPPort  int    `json:"http_port"`
		QueryPort int    `json:"query_port"`
		EditPort  int    `json:"edit_port"`
		HBPort    int    `json:"heartbeat_port"`
		DataRoot  string `json:"data_root"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	role := strings.ToLower(strings.TrimSpace(req.Role))
	items := []map[string]any{}
	okAll := true
	add := func(name string, ok bool, msg string) {
		items = append(items, map[string]any{"name": name, "ok": ok, "message": msg})
		if !ok {
			okAll = false
		}
	}
	warn := func(name, msg string) {
		items = append(items, map[string]any{"name": name, "ok": true, "level": "warn", "message": msg})
	}
	// Java
	if out, err := exec.Command("java", "-version").CombinedOutput(); err != nil {
		add("java", false, strings.TrimSpace(string(out))+" "+err.Error())
	} else {
		add("java", true, "present")
	}
	checkPort := func(name string, port int) {
		if port <= 0 {
			add(name, true, "skip")
			return
		}
		ln, err := net.Listen("tcp", ":"+strconv.Itoa(port))
		if err != nil {
			add(name, false, fmt.Sprintf("port %d in use: %v", port, err))
			return
		}
		_ = ln.Close()
		add(name, true, fmt.Sprintf("port %d free", port))
	}
	if role == "be" {
		checkPort("heartbeat_port", req.HBPort)
		dataRoot := strings.TrimSpace(req.DataRoot)
		if dataRoot == "" {
			dataRoot = "/data"
		}
		res, devs, err := evalDataRoot(dataRoot)
		if err != nil {
			add("data_root", false, err.Error())
		} else {
			add("data_root", res.OK, res.Message)
			if res.OK && res.FreeDisk != nil {
				warn("data_disk_format", fmt.Sprintf("将格式化 /dev/%s 为 xfs 并挂载到 %s，盘上数据会被清空", res.FreeDisk.KName, filepath.Clean(dataRoot)))
			}
			if res.OK && res.Rule == 3 && dirNonEmpty(dataRoot) {
				warn("data_root_nonempty", fmt.Sprintf("数据目录 %s 非空（lost+found 以外有内容），请确认不是其他服务的数据", dataRoot))
			}
			warn("extra_disk", diskSummary(devs))
		}
	} else {
		checkPort("http_port", req.HTTPPort)
		checkPort("query_port", req.QueryPort)
		checkPort("edit_port", req.EditPort)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      okAll,
		"items":   items,
		"message": precheckSummary(okAll, items),
	})
}

func precheckSummary(okAll bool, items []map[string]any) string {
	if okAll {
		return "ok"
	}
	var fails []string
	for _, it := range items {
		ok, _ := it["ok"].(bool)
		if ok {
			continue
		}
		if lv, _ := it["level"].(string); lv == "warn" {
			continue
		}
		name, _ := it["name"].(string)
		msg, _ := it["message"].(string)
		if strings.TrimSpace(msg) == "" {
			fails = append(fails, name)
		} else {
			fails = append(fails, name+": "+msg)
		}
	}
	if len(fails) == 0 {
		return "precheck failed"
	}
	return strings.Join(fails, "; ")
}

func handleDiskInit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Device       string `json:"device"`
		MountPoint   string `json:"mount_point"`
		FSType       string `json:"fstype"`
		PersistFstab bool   `json:"persist_fstab"`
		DryRun       bool   `json:"dry_run"`
		Auto         bool   `json:"auto"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	if req.MountPoint == "" {
		req.MountPoint = "/data"
	}
	if req.FSType == "" {
		req.FSType = "xfs"
	}
	if req.Auto {
		id := newJobID()
		putJob(id, &jobState{ID: id, Status: "running"})
		go func() {
			msg, err := autoDiskInit(req.MountPoint, req.PersistFstab, *dryRun || req.DryRun)
			if err != nil {
				updateJob(id, func(j *jobState) { j.Status, j.Message = "failed", err.Error() })
			} else {
				updateJob(id, func(j *jobState) { j.Status, j.Message = "success", msg })
			}
		}()
		writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "job_id": id, "status": "running"})
		return
	}
	// 拒绝明显系统盘
	dev := strings.TrimSpace(req.Device)
	if dev != "" {
		for _, bad := range []string{"/dev/sda", "/dev/vda", "/dev/nvme0n1", "/dev/xvda"} {
			if dev == bad || strings.HasPrefix(dev, bad+"p") || strings.HasPrefix(dev, bad+"1") && len(dev) <= len(bad)+1 {
				// allow partitions of data disks only — block root-like whole disks without number is too broad;
				// block exact system disk names when used as whole-disk mkfs target
			}
		}
		if dev == "/dev/sda" || dev == "/dev/vda" || dev == "/dev/xvda" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "拒绝系统盘: " + dev})
			return
		}
	}
	id := newJobID()
	putJob(id, &jobState{ID: id, Status: "running"})
	go func() {
		msg := "dry-run ok"
		errMsg := ""
		if !*dryRun && !req.DryRun {
			if err := os.MkdirAll(req.MountPoint, 0o755); err != nil {
				errMsg = err.Error()
			} else if dev != "" {
				if out, err := exec.Command("mkfs."+req.FSType, "-f", dev).CombinedOutput(); err != nil {
					errMsg = string(out) + err.Error()
				} else if out, err := exec.Command("mount", dev, req.MountPoint).CombinedOutput(); err != nil {
					errMsg = string(out) + err.Error()
				} else {
					msg = "mounted " + dev + " -> " + req.MountPoint
					if req.PersistFstab {
						line := fmt.Sprintf("%s %s %s defaults 0 0\n", dev, req.MountPoint, req.FSType)
						f, err := os.OpenFile("/etc/fstab", os.O_APPEND|os.O_WRONLY, 0o644)
						if err == nil {
							_, _ = f.WriteString(line)
							_ = f.Close()
						}
					}
				}
			} else {
				msg = "no device; ensured mountpoint " + req.MountPoint
			}
		}
		if errMsg != "" {
			updateJob(id, func(j *jobState) { j.Status, j.Message = "failed", errMsg })
		} else {
			updateJob(id, func(j *jobState) { j.Status, j.Message = "success", msg })
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "job_id": id, "status": "running"})
}

func handleEnsureFS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Paths        []string `json:"paths"`
		BackupSuffix string   `json:"backup_suffix"` // 非空：把 path 下现有子项移入 path/bak.<suffix>/
		DryRun       bool     `json:"dry_run"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	suffix := strings.TrimSpace(req.BackupSuffix)
	if suffix != "" && strings.ContainsAny(suffix, "/\\") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "invalid backup_suffix"})
		return
	}
	var notes []string
	for _, p := range req.Paths {
		p = filepath.Clean(strings.TrimSpace(p))
		if p == "" || p == "/" || p == "." {
			continue
		}
		if suffix != "" {
			note, err := backupInPlace(p, suffix, *dryRun || req.DryRun)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": err.Error()})
				return
			}
			if note != "" {
				notes = append(notes, note)
			}
		}
		if *dryRun || req.DryRun {
			notes = append(notes, "dry-run ensure "+p)
			continue
		}
		if err := os.MkdirAll(p, 0o755); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": err.Error()})
			return
		}
	}
	msg := strings.Join(notes, "; ")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": msg})
}

// backupInPlace 在当前目录内备份：子项移入 path/bak.<suffix>/，不旁路新建路径。
func backupInPlace(p, suffix string, dry bool) (string, error) {
	if _, err := os.Stat(p); err != nil {
		if os.IsNotExist(err) {
			return p + ": skip(missing)", nil
		}
		return "", err
	}
	bakName := "bak." + suffix
	bak := filepath.Join(p, bakName)
	if dry {
		return "dry-run backup " + p + "/* -> " + bak, nil
	}
	entries, err := os.ReadDir(p)
	if err != nil {
		return "", err
	}
	var toMove []os.DirEntry
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "bak.") {
			continue
		}
		toMove = append(toMove, e)
	}
	if len(toMove) == 0 {
		return p + ": skip(empty)", nil
	}
	if err := os.MkdirAll(bak, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", bak, err)
	}
	moved := 0
	for _, e := range toMove {
		src := filepath.Join(p, e.Name())
		dst := filepath.Join(bak, e.Name())
		if err := os.Rename(src, dst); err != nil {
			return "", fmt.Errorf("move %s: %w", src, err)
		}
		moved++
	}
	return fmt.Sprintf("%s/* -> %s (%d entries)", p, bak, moved), nil
}

func handleFilePut(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Mode    string `json:"mode"`
		// Backup 为 true 时，覆盖前把旧文件备份为 <path>.bak.<时间戳> 并回传旧内容，供平台记录「下发前」版本。
		Backup bool `json:"backup"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "path required"})
		return
	}
	tmp := req.Path + ".new"
	if err := os.MkdirAll(filepath.Dir(req.Path), 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	mode := os.FileMode(0o644)
	if req.Mode != "" {
		if v, err := strconv.ParseUint(req.Mode, 8, 32); err == nil {
			mode = os.FileMode(v)
		}
	}
	resp := map[string]any{"ok": true}
	if req.Backup {
		prev, err := readExistingConf(req.Path)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "读取旧文件失败: " + err.Error()})
			return
		}
		if prev != nil {
			bak, err := backupConfFile(req.Path, prev, mode, time.Now())
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "备份旧文件失败: " + err.Error()})
				return
			}
			resp["previous_exists"] = true
			resp["previous_content"] = string(prev)
			resp["previous_sha256"] = sha256Hex(prev)
			resp["backup_path"] = bak
			resp["message"] = "backup " + bak
		}
	}
	if err := os.WriteFile(tmp, []byte(req.Content), mode); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	if err := os.Rename(tmp, req.Path); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func handleArtifactInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Role            string   `json:"role"`
		TargetDir       string   `json:"target_dir"`
		Version         string   `json:"version"`
		DownloadURL     string   `json:"download_url"`
		SHA256          string   `json:"sha256"`
		Format          string   `json:"format"`
		Filename        string   `json:"filename"`
		StripComponents int      `json:"strip_components"`
		ReplaceDirs     []string `json:"replace_dirs"`   // 非空：仅替换这些子目录（升级用 bin/lib）
		BackupSuffix    string   `json:"backup_suffix"`  // 原地备份到 target/bak.<suffix>/
		DryRun          bool     `json:"dry_run"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	if strings.TrimSpace(req.TargetDir) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "target_dir required"})
		return
	}
	id := newJobID()
	putJob(id, &jobState{ID: id, Status: "running"})
	go func() {
		msg, errMsg := runArtifactInstall(req.Role, req.TargetDir, req.DownloadURL, req.SHA256, req.Format, req.StripComponents, req.ReplaceDirs, req.BackupSuffix, req.DryRun || *dryRun)
		if errMsg != "" {
			updateJob(id, func(j *jobState) { j.Status, j.Message = "failed", errMsg })
		} else {
			updateJob(id, func(j *jobState) { j.Status, j.Message = "success", msg })
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "job_id": id, "status": "running"})
}

func runArtifactInstall(role, targetDir, downloadURL, wantSHA, format string, strip int, replaceDirs []string, backupSuffix string, dry bool) (string, string) {
	if dry || strings.HasPrefix(downloadURL, "dry-run://") {
		_ = os.MkdirAll(filepath.Join(targetDir, "bin"), 0o755)
		stub := "#!/bin/bash\necho stub-start; exit 0\n"
		_ = os.WriteFile(filepath.Join(targetDir, "bin", "start_fe.sh"), []byte(stub), 0o755)
		_ = os.WriteFile(filepath.Join(targetDir, "bin", "start_be.sh"), []byte(stub), 0o755)
		if len(replaceDirs) > 0 {
			return fmt.Sprintf("dry-run upgrade role=%s dir=%s replace=%v", role, targetDir, replaceDirs), ""
		}
		return fmt.Sprintf("dry-run install role=%s dir=%s", role, targetDir), ""
	}
	if strings.TrimSpace(downloadURL) == "" {
		return "", "download_url required for real install"
	}
	if strings.TrimSpace(wantSHA) == "" {
		return "", "sha256 required"
	}
	tmpRoot, err := os.MkdirTemp("", "sr-artifact-*")
	if err != nil {
		return "", err.Error()
	}
	defer os.RemoveAll(tmpRoot)
	archive := filepath.Join(tmpRoot, "pkg.tar.gz")
	if format == "" {
		format = "tar.gz"
	}
	client := &http.Client{Timeout: 2 * time.Hour}
	resp, err := client.Get(downloadURL)
	if err != nil {
		return "", "download: " + err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Sprintf("download HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(archive)
	if err != nil {
		return "", err.Error()
	}
	h := sha256.New()
	w := io.MultiWriter(f, h)
	if _, err := io.Copy(w, resp.Body); err != nil {
		_ = f.Close()
		return "", "download copy: " + err.Error()
	}
	_ = f.Close()
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, strings.TrimSpace(wantSHA)) {
		return "", fmt.Sprintf("sha256 mismatch got=%s want=%s", got, wantSHA)
	}
	extractDir := filepath.Join(tmpRoot, "extract")
	_ = os.MkdirAll(extractDir, 0o755)
	cmd := exec.Command("tar", "-xzf", archive, "-C", extractDir)
	if strip > 0 {
		cmd = exec.Command("tar", "--strip-components="+strconv.Itoa(strip), "-xzf", archive, "-C", extractDir)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", string(out) + err.Error()
	}

	// 升级：仅替换指定子目录（默认场景 bin/lib），在软件目录内备份
	if len(replaceDirs) > 0 {
		msg, errMsg := replaceSubdirsInPlace(targetDir, extractDir, replaceDirs, backupSuffix)
		if errMsg != "" {
			return "", errMsg
		}
		return fmt.Sprintf("%s sha256=%s", msg, got[:12]), ""
	}

	// 全量装包：原子替换整目录
	newDir := targetDir + ".new"
	_ = os.RemoveAll(newDir)
	if err := os.Rename(extractDir, newDir); err != nil {
		_ = os.MkdirAll(newDir, 0o755)
		cp := exec.Command("bash", "-c", fmt.Sprintf("cp -a %q/. %q/", extractDir, newDir))
		if out, err := cp.CombinedOutput(); err != nil {
			return "", string(out) + err.Error()
		}
	}
	bak := targetDir + ".bak"
	_ = os.RemoveAll(bak)
	if _, err := os.Stat(targetDir); err == nil {
		_ = os.Rename(targetDir, bak)
	}
	if err := os.Rename(newDir, targetDir); err != nil {
		_ = os.Rename(bak, targetDir)
		return "", "swap target: " + err.Error()
	}
	if entries, err := os.ReadDir(bak); err == nil {
		for _, e := range entries {
			if !strings.HasPrefix(e.Name(), "bak.") {
				continue
			}
			src := filepath.Join(bak, e.Name())
			dst := filepath.Join(targetDir, e.Name())
			_ = os.RemoveAll(dst)
			_ = os.Rename(src, dst)
		}
	}
	_ = os.RemoveAll(bak)
	return fmt.Sprintf("installed role=%s dir=%s sha256=%s", role, targetDir, got[:12]), ""
}

// replaceSubdirsInPlace 在 targetDir 内备份并替换指定子目录；包内查找同名目录（支持一层嵌套）。
func replaceSubdirsInPlace(targetDir, extractDir string, dirs []string, backupSuffix string) (string, string) {
	suffix := strings.TrimSpace(backupSuffix)
	if suffix == "" {
		suffix = time.Now().Format("20060102-150405")
	}
	if strings.ContainsAny(suffix, "/\\") {
		return "", "invalid backup_suffix"
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", err.Error()
	}
	bakRoot := filepath.Join(targetDir, "bak."+suffix)
	var notes []string
	for _, name := range dirs {
		name = strings.TrimSpace(name)
		if name == "" || strings.ContainsAny(name, "/\\") || name == "." || name == ".." {
			return "", "invalid replace dir: " + name
		}
		src := findNamedDir(extractDir, name)
		if src == "" {
			return "", fmt.Sprintf("package missing %s under %s", name, extractDir)
		}
		dst := filepath.Join(targetDir, name)
		if st, err := os.Stat(dst); err == nil && st.IsDir() {
			bakDir := bakRoot
			if _, err := os.Stat(filepath.Join(bakRoot, name)); err == nil {
				// 同一次升级重试：保留首次备份（旧版本），当前目录另存，避免覆盖后无法回滚
				bakDir = filepath.Join(targetDir, "bak."+suffix+".retry-"+time.Now().Format("20060102-150405"))
			}
			if err := os.MkdirAll(bakDir, 0o755); err != nil {
				return "", "mkdir bak: " + err.Error()
			}
			bakDst := filepath.Join(bakDir, name)
			_ = os.RemoveAll(bakDst)
			if err := os.Rename(dst, bakDst); err != nil {
				_ = os.MkdirAll(bakDst, 0o755)
				cp := exec.Command("bash", "-c", fmt.Sprintf("cp -a %q/. %q/ && rm -rf %q", dst, bakDst, dst))
				if out, err := cp.CombinedOutput(); err != nil {
					return "", "backup " + name + ": " + string(out) + err.Error()
				}
			}
			notes = append(notes, fmt.Sprintf("backup %s -> %s", dst, bakDst))
		}
		_ = os.RemoveAll(dst)
		if err := os.Rename(src, dst); err != nil {
			_ = os.MkdirAll(dst, 0o755)
			cp := exec.Command("bash", "-c", fmt.Sprintf("cp -a %q/. %q/", src, dst))
			if out, err := cp.CombinedOutput(); err != nil {
				return "", "replace " + name + ": " + string(out) + err.Error()
			}
		}
		notes = append(notes, "replaced "+name)
	}
	return strings.Join(notes, "; "), ""
}

// findNamedDir 在 root 或其一层子目录中查找名为 name 的目录。
func findNamedDir(root, name string) string {
	direct := filepath.Join(root, name)
	if st, err := os.Stat(direct); err == nil && st.IsDir() {
		return direct
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		cand := filepath.Join(root, e.Name(), name)
		if st, err := os.Stat(cand); err == nil && st.IsDir() {
			return cand
		}
	}
	return ""
}

func handleSystemdInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Unit    string `json:"unit"`
		Content string `json:"content"`
		DryRun  bool   `json:"dry_run"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	name := strings.TrimSuffix(strings.TrimSpace(req.Unit), ".service")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "unit required"})
		return
	}
	path := "/etc/systemd/system/" + name + ".service"
	if *dryRun || req.DryRun {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "dry-run write " + path})
		return
	}
	if err := os.WriteFile(path+".new", []byte(req.Content), 0o644); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	if err := os.Rename(path+".new", path); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	_ = exec.Command("systemctl", "daemon-reload").Run()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func handleSystemdCtrl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Action string `json:"action"`
		Unit   string `json:"unit"`
		DryRun bool   `json:"dry_run"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	unit := strings.TrimSpace(req.Unit)
	if unit == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "unit required"})
		return
	}
	if !strings.HasSuffix(unit, ".service") {
		unit += ".service"
	}
	act := strings.ToLower(strings.TrimSpace(req.Action))
	switch act {
	case "start", "stop", "restart", "enable", "status", "reset-failed", "cat":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "invalid action"})
		return
	}
	if *dryRun || req.DryRun {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "dry-run systemctl " + act + " " + unit})
		return
	}
	out, err := exec.Command("systemctl", act, unit).CombinedOutput()
	if err != nil && act != "status" {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": string(out) + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": strings.TrimSpace(string(out))})
}

func handleExec(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
		DryRun  bool     `json:"dry_run"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	cmdPath := strings.TrimSpace(req.Command)
	base := filepath.Base(cmdPath)
	if base != "start_fe.sh" && base != "start_be.sh" && base != "stop_fe.sh" && base != "stop_be.sh" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "command not in whitelist"})
		return
	}
	// 仅允许 --helper / --daemon 等参数
	for _, a := range req.Args {
		if strings.ContainsAny(a, ";|&`$") {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "unsafe arg"})
			return
		}
	}
	if *dryRun || req.DryRun {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "dry-run " + cmdPath + " " + strings.Join(req.Args, " ")})
		return
	}
	out, err := exec.Command(cmdPath, req.Args...).CombinedOutput()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": string(out) + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": strings.TrimSpace(string(out))})
}

func handleJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/v1/sr/jobs/")
	j := getJob(id)
	if j == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "message": "job not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": j.Status == "success", "job_id": j.ID, "status": j.Status, "message": j.Message})
}

func newJobID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b) + strconv.FormatInt(time.Now().Unix(), 16)
}
