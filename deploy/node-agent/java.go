package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// JDK 自动安装落盘目录：/home/jdk-<完整版本>。
const jdkInstallRoot = "/home"

// preferredJDKHome 约定目录：存放满足 StarRocks 最低要求的最新 JDK，切换 update-alternatives 时优先选它。
const preferredJDKHome = "/home/jdk"

// systemJavaBin systemd 启动时 start_fe.sh / start_be.sh 未拿到 JAVA_HOME 会由它推断。
const systemJavaBin = "/usr/bin/java"

var (
	javaVersionQuotedRe = regexp.MustCompile(`version "([^"]+)"`)
	javaVersionBareRe   = regexp.MustCompile(`(?i)(?:openjdk|java)\s+(\d+(?:[.\d_+-]*\w)?)`)
	confJavaHomeRe      = regexp.MustCompile(`^(?:export\s+)?JAVA_HOME\s*=\s*(.*)$`)
	unsafeDirCharRe     = regexp.MustCompile(`[^0-9A-Za-z._+-]`)
)

type javaCandidate struct {
	Home      string `json:"home"`
	Path      string `json:"path"`
	Major     int    `json:"major"`
	Version   string `json:"version"`
	Preferred bool   `json:"preferred,omitempty"`
}

// parseJavaVersion 解析 `java -version` 输出：1.8.0_392 → 8；11.0.24 → 11；21 → 21。
func parseJavaVersion(out string) (int, string) {
	ver := ""
	if m := javaVersionQuotedRe.FindStringSubmatch(out); m != nil {
		ver = strings.TrimSpace(m[1])
	} else if m := javaVersionBareRe.FindStringSubmatch(out); m != nil {
		ver = strings.TrimSpace(m[1])
	}
	if ver == "" {
		return 0, ""
	}
	parts := strings.FieldsFunc(ver, func(r rune) bool { return r == '.' || r == '_' || r == '-' || r == '+' })
	if len(parts) == 0 {
		return 0, ver
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, ver
	}
	if major == 1 && len(parts) > 1 {
		if m2, err := strconv.Atoi(parts[1]); err == nil {
			major = m2
		}
	}
	return major, ver
}

// jdkInRange max<=0 表示不限上限。
func jdkInRange(major, min, max int) bool {
	if major <= 0 {
		return false
	}
	if min > 0 && major < min {
		return false
	}
	if max > 0 && major > max {
		return false
	}
	return true
}

func jdkRangeText(min, max int) string {
	switch {
	case min > 0 && max > 0:
		return fmt.Sprintf("%d–%d", min, max)
	case min > 0:
		return fmt.Sprintf("%d+", min)
	case max > 0:
		return fmt.Sprintf("≤%d", max)
	}
	return "不限"
}

// readConfJavaHome 读取 fe.conf / be.conf 中未注释的 JAVA_HOME（StarRocks 启动脚本会导出 conf 中的变量）。
func readConfJavaHome(confPath string) string {
	f, err := os.Open(confPath)
	if err != nil {
		return ""
	}
	defer f.Close()
	home := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if m := confJavaHomeRe.FindStringSubmatch(line); m != nil {
			home = strings.Trim(strings.TrimSpace(m[1]), `"'`)
		}
	}
	return home
}

func javaVersionOf(javaBin string) (int, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, javaBin, "-version").CombinedOutput()
	if err != nil {
		return 0, "", fmt.Errorf("%s -version: %v %s", javaBin, err, strings.TrimSpace(string(out)))
	}
	major, ver := parseJavaVersion(string(out))
	if major == 0 {
		return 0, ver, fmt.Errorf("无法解析 %s 版本: %s", javaBin, strings.TrimSpace(string(out)))
	}
	return major, ver, nil
}

// effectiveJava StarRocks 实际使用的 java：conf 中 JAVA_HOME 优先，否则 /usr/bin/java（PATH）。
func effectiveJava(role, softwarePath string) (javaBin, source, confFile string) {
	role = strings.ToLower(strings.TrimSpace(role))
	if sp := strings.TrimSpace(softwarePath); sp != "" && (role == "fe" || role == "be") {
		confFile = filepath.Join(sp, "conf", role+".conf")
		if home := readConfJavaHome(confFile); home != "" {
			return filepath.Join(home, "bin", "java"), "conf", confFile
		}
	}
	if _, err := os.Stat("/usr/bin/java"); err == nil {
		return "/usr/bin/java", "path", confFile
	}
	if p, err := exec.LookPath("java"); err == nil {
		return p, "path", confFile
	}
	return "", "path", confFile
}

// scanJavaCandidates 本机已有、满足范围的 JDK：/home/jdk 优先，其次 /home/jdk-*、/usr/lib/jvm/*。
func scanJavaCandidates(min, max int) []javaCandidate {
	return scanJavaCandidatesIn(preferredJDKHome, []string{filepath.Join(jdkInstallRoot, "jdk-*"), "/usr/lib/jvm/*"}, min, max)
}

func scanJavaCandidatesIn(preferred string, globs []string, min, max int) []javaCandidate {
	var dirs []string
	if preferred != "" {
		dirs = append(dirs, preferred)
	}
	for _, pat := range globs {
		m, _ := filepath.Glob(pat)
		dirs = append(dirs, m...)
	}
	seen := map[string]bool{}
	var out []javaCandidate
	for _, d := range dirs {
		bin := filepath.Join(d, "bin", "java")
		st, err := os.Stat(bin)
		if err != nil || st.IsDir() {
			continue
		}
		real, err := filepath.EvalSymlinks(bin)
		if err != nil {
			real = bin
		}
		if seen[real] {
			continue
		}
		seen[real] = true
		major, ver, err := javaVersionOf(bin)
		if err != nil || !jdkInRange(major, min, max) {
			continue
		}
		out = append(out, javaCandidate{Home: d, Path: bin, Major: major, Version: ver, Preferred: preferred != "" && d == preferred})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Preferred != out[j].Preferred {
			return out[i].Preferred
		}
		if out[i].Major != out[j].Major {
			return out[i].Major > out[j].Major
		}
		return out[i].Version > out[j].Version
	})
	return out
}

func handleJavaCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Role         string `json:"role"`
		SoftwarePath string `json:"software_path"`
		JDKMin       int    `json:"jdk_min"`
		JDKMax       int    `json:"jdk_max"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	bin, source, confFile := effectiveJava(req.Role, req.SoftwarePath)
	resp := map[string]any{"path": bin, "source": source, "conf_file": confFile, "jdk_min": req.JDKMin, "jdk_max": req.JDKMax}
	want := jdkRangeText(req.JDKMin, req.JDKMax)
	ok := false
	msg := ""
	if bin == "" {
		msg = "未找到 java（/usr/bin/java 不存在）"
	} else if major, ver, err := javaVersionOf(bin); err != nil {
		msg = err.Error()
	} else {
		resp["major"], resp["version"] = major, ver
		ok = jdkInRange(major, req.JDKMin, req.JDKMax)
		if ok {
			msg = fmt.Sprintf("JDK %s（%s）满足要求 %s", ver, bin, want)
		} else {
			msg = fmt.Sprintf("JDK %s（%s）不满足要求 %s", ver, bin, want)
		}
	}
	if source == "conf" {
		msg += fmt.Sprintf("；JAVA_HOME 由 %s 指定", confFile)
	}
	for k, v := range systemJavaInfo(systemJavaBin) {
		resp[k] = v
	}
	resp["ok"], resp["message"] = ok, msg
	resp["candidates"] = scanJavaCandidates(req.JDKMin, req.JDKMax)
	writeJSON(w, http.StatusOK, resp)
}

// systemJavaInfo 系统默认 java（update-alternatives 指向）：conf 里配了 JAVA_HOME 也要单独校验，
// 因为 systemd 不加载登录环境，部分版本的启动脚本会直接由它推断 JAVA_HOME。
func systemJavaInfo(bin string) map[string]any {
	out := map[string]any{"system_checked": true, "system_path": ""}
	if _, err := os.Stat(bin); err != nil {
		out["system_message"] = bin + " 不存在"
		return out
	}
	real, err := filepath.EvalSymlinks(bin)
	if err != nil {
		real = bin
	}
	out["system_path"] = real
	major, ver, err := javaVersionOf(bin)
	if err != nil {
		out["system_message"] = err.Error()
		return out
	}
	out["system_major"], out["system_version"] = major, ver
	return out
}

type javaInstallReq struct {
	DownloadURL string `json:"download_url"`
	SHA256      string `json:"sha256"`
	JavaHome    string `json:"java_home"`
	Priority    int    `json:"priority"`
	DryRun      bool   `json:"dry_run"`
}

func handleJavaInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req javaInstallReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	if strings.TrimSpace(req.JavaHome) == "" && strings.TrimSpace(req.DownloadURL) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "download_url 或 java_home 必填"})
		return
	}
	id := newJobID()
	putJob(id, &jobState{ID: id, Status: "running"})
	go func() {
		msg, err := runJavaInstall(req, req.DryRun || *dryRun)
		if err != nil {
			updateJob(id, func(j *jobState) { j.Status, j.Message = "failed", err.Error() })
		} else {
			updateJob(id, func(j *jobState) { j.Status, j.Message = "success", msg })
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "job_id": id, "status": "running"})
}

func runJavaInstall(req javaInstallReq, dry bool) (string, error) {
	prio := req.Priority
	if prio <= 0 {
		prio = 1200
	}
	home := strings.TrimSpace(req.JavaHome)
	if dry {
		src := home
		if src == "" {
			src = req.DownloadURL
		}
		return fmt.Sprintf("dry-run: 安装 JDK（%s）并 update-alternatives --set java，priority=%d", src, prio), nil
	}
	var logs []string
	if home == "" {
		h, msg, err := downloadAndExtractJDK(req.DownloadURL, req.SHA256)
		if err != nil {
			return "", err
		}
		home = h
		logs = append(logs, msg)
	}
	major, ver, err := javaVersionOf(filepath.Join(home, "bin", "java"))
	if err != nil {
		return "", err
	}
	altLog, err := switchAlternatives(home, prio)
	if err != nil {
		return "", err
	}
	logs = append(logs, altLog)
	gotMajor, gotVer, err := javaVersionOf("/usr/bin/java")
	if err != nil {
		return "", fmt.Errorf("切换后复验失败: %w", err)
	}
	if gotMajor != major {
		return "", fmt.Errorf("切换后 /usr/bin/java 为 JDK %s，期望 %s", gotVer, ver)
	}
	logs = append(logs, fmt.Sprintf("/usr/bin/java → JDK %s（%s）", gotVer, home))
	return strings.Join(logs, "；"), nil
}

func downloadAndExtractJDK(downloadURL, wantSHA string) (home, msg string, err error) {
	tmpRoot, err := os.MkdirTemp("", "sr-jdk-*")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(tmpRoot)
	archive := filepath.Join(tmpRoot, "jdk.tar.gz")
	client := &http.Client{Timeout: time.Hour}
	resp, err := client.Get(downloadURL)
	if err != nil {
		return "", "", fmt.Errorf("下载 JDK: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("下载 JDK HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(archive)
	if err != nil {
		return "", "", err
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), resp.Body); err != nil {
		_ = f.Close()
		return "", "", fmt.Errorf("下载 JDK: %w", err)
	}
	_ = f.Close()
	sum := hex.EncodeToString(h.Sum(nil))
	if want := strings.TrimSpace(wantSHA); want != "" && !strings.EqualFold(want, sum) {
		return "", "", fmt.Errorf("JDK sha256 不匹配 got=%s want=%s", sum, want)
	}
	extractDir := filepath.Join(tmpRoot, "extract")
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return "", "", err
	}
	if out, err := exec.Command("tar", "--strip-components=1", "-xzf", archive, "-C", extractDir).CombinedOutput(); err != nil {
		return "", "", fmt.Errorf("解压 JDK: %v %s", err, strings.TrimSpace(string(out)))
	}
	_, ver, err := javaVersionOf(filepath.Join(extractDir, "bin", "java"))
	if err != nil {
		return "", "", fmt.Errorf("解压后的 JDK 不可用: %w", err)
	}
	dest := filepath.Join(jdkInstallRoot, "jdk-"+unsafeDirCharRe.ReplaceAllString(ver, "_"))
	if st, err := os.Stat(filepath.Join(dest, "bin", "java")); err == nil && !st.IsDir() {
		return dest, fmt.Sprintf("%s 已存在，直接复用", dest), nil
	}
	_ = os.RemoveAll(dest)
	if err := os.Rename(extractDir, dest); err != nil {
		if err := os.MkdirAll(dest, 0o755); err != nil {
			return "", "", err
		}
		if out, err := exec.Command("cp", "-a", extractDir+"/.", dest+"/").CombinedOutput(); err != nil {
			return "", "", fmt.Errorf("拷贝 JDK 到 %s: %v %s", dest, err, strings.TrimSpace(string(out)))
		}
	}
	return dest, fmt.Sprintf("JDK %s 已解压到 %s（sha256=%s）", ver, dest, sum[:12]), nil
}

func alternativesBin() (string, error) {
	for _, name := range []string{"update-alternatives", "alternatives"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("未找到 update-alternatives / alternatives")
}

// switchAlternatives 等价于 update-alternatives --install + --config（非交互用 --set）。
func switchAlternatives(home string, prio int) (string, error) {
	alt, err := alternativesBin()
	if err != nil {
		return "", err
	}
	var done []string
	for _, name := range []string{"java", "javac"} {
		bin := filepath.Join(home, "bin", name)
		if _, err := os.Stat(bin); err != nil {
			if name == "java" {
				return "", fmt.Errorf("%s 不存在", bin)
			}
			continue
		}
		link := "/usr/bin/" + name
		if out, err := exec.Command(alt, "--install", link, name, bin, strconv.Itoa(prio)).CombinedOutput(); err != nil {
			return "", fmt.Errorf("%s --install %s: %v %s", filepath.Base(alt), name, err, strings.TrimSpace(string(out)))
		}
		if out, err := exec.Command(alt, "--set", name, bin).CombinedOutput(); err != nil {
			return "", fmt.Errorf("%s --set %s: %v %s", filepath.Base(alt), name, err, strings.TrimSpace(string(out)))
		}
		done = append(done, name)
	}
	return fmt.Sprintf("%s --install/--set %s → %s（priority %d）", filepath.Base(alt), strings.Join(done, "/"), home, prio), nil
}
