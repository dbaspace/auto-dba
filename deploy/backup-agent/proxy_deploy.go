package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

var (
	proxyDeployJobsMu sync.Mutex
	proxyDeployJobs   = map[string]*proxyDeployJob{}
)

type proxyDeployJob struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	Message   string    `json:"message,omitempty"`
	Log       string    `json:"log,omitempty"`
	Kind      string    `json:"kind,omitempty"`
	UpdatedAt time.Time `json:"-"`
}

func putProxyDeployJob(id string, job *proxyDeployJob) {
	if job != nil {
		job.UpdatedAt = time.Now()
	}
	proxyDeployJobsMu.Lock()
	defer proxyDeployJobsMu.Unlock()
	proxyDeployJobs[id] = job
	now := time.Now()
	for jid, j := range proxyDeployJobs {
		if j == nil {
			delete(proxyDeployJobs, jid)
			continue
		}
		if strings.EqualFold(j.Status, "running") {
			continue
		}
		if j.UpdatedAt.IsZero() || now.Sub(j.UpdatedAt) > 2*time.Hour {
			delete(proxyDeployJobs, jid)
		}
	}
	for len(proxyDeployJobs) > 128 {
		var oldestID string
		var oldest time.Time
		for jid, j := range proxyDeployJobs {
			if j == nil || strings.EqualFold(j.Status, "running") {
				continue
			}
			if oldestID == "" || j.UpdatedAt.Before(oldest) {
				oldestID, oldest = jid, j.UpdatedAt
			}
		}
		if oldestID == "" {
			break
		}
		delete(proxyDeployJobs, oldestID)
	}
}

// handleProxyVIPCheck 检查 VIP 是否已被本机或网段占用（不可用作新 VIP）。
func handleProxyVIPCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	vip := strings.TrimSpace(r.URL.Query().Get("vip"))
	if r.Method == http.MethodPost {
		var req struct {
			VIP string `json:"vip"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if strings.TrimSpace(req.VIP) != "" {
			vip = strings.TrimSpace(req.VIP)
		}
	}
	if i := strings.IndexByte(vip, '/'); i >= 0 {
		vip = vip[:i]
	}
	vip = strings.TrimSpace(vip)
	if vip == "" || net.ParseIP(vip) == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "VIP 非法"})
		return
	}
	if *dryRun {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "available": true, "message": "dry-run", "vip": vip,
		})
		return
	}
	local, localDetail := vipOnLocalInterfaces(vip)
	if local {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "available": false, "vip": vip,
			"reason": "local", "message": "VIP 已在本机网卡上: " + localDetail,
		})
		return
	}
	if occupied, how := vipOccupiedOnNetwork(vip); occupied {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "available": false, "vip": vip,
			"reason": "network", "message": "VIP 已被网络占用: " + how,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "available": true, "vip": vip, "message": "VIP 可用",
	})
}

func vipOnLocalInterfaces(vip string) (bool, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ip", "-o", "addr", "show").CombinedOutput()
	if err != nil {
		// 退回解析本机全部地址
		ifaces, ierr := net.Interfaces()
		if ierr != nil {
			return false, ""
		}
		for _, iface := range ifaces {
			addrs, _ := iface.Addrs()
			for _, a := range addrs {
				ipNet, ok := a.(*net.IPNet)
				if !ok || ipNet.IP == nil {
					continue
				}
				if ipNet.IP.String() == vip || (ipNet.IP.To4() != nil && ipNet.IP.To4().String() == vip) {
					return true, iface.Name
				}
			}
		}
		return false, ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, " "+vip+"/") || strings.Contains(line, " "+vip+" ") {
			return true, strings.TrimSpace(line)
		}
	}
	return false, ""
}

func vipOccupiedOnNetwork(vip string) (bool, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	// arping -D：重复地址探测（无应答则可用）；需 root，失败则降级 ping
	if lookPath("arping") {
		out, err := exec.CommandContext(ctx, "arping", "-D", "-c", "2", "-w", "3", vip).CombinedOutput()
		text := strings.TrimSpace(string(out))
		// 退出码 1 常表示检测到重复；部分实现 0=无冲突
		if err != nil && (strings.Contains(text, "Received") || strings.Contains(strings.ToLower(text), "reply") ||
			strings.Contains(text, "1 response") || strings.Contains(text, "from")) {
			return true, "arping: " + firstLine(text)
		}
		if err == nil && (strings.Contains(text, "Received 1") || strings.Contains(text, "Received 2")) {
			return true, "arping: " + firstLine(text)
		}
	}
	// ping：若有应答说明地址已被使用（未部署 VIP 前不应通）
	pingBin := "ping"
	args := []string{"-c", "1", "-W", "1", vip}
	if !lookPath("ping") {
		return false, ""
	}
	out, err := exec.CommandContext(ctx, pingBin, args...).CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err == nil && (strings.Contains(text, "1 received") || strings.Contains(text, "1 packets received") ||
		strings.Contains(text, " bytes from ")) {
		return true, "ping 可达: " + firstLine(text)
	}
	return false, ""
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	if len(s) > 160 {
		return s[:160]
	}
	return s
}

// handleProxyDeploy 按 kind 安装代理包 + keepalived，并启用对应 systemd。
func handleProxyDeploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Kind        string `json:"kind"` // haproxy | nginx | lvs
		VIP         string `json:"vip"`
		EnableStart bool   `json:"enable_start"`
		SkipInstall bool   `json:"skip_install"`
		SkipVIPCheck bool  `json:"skip_vip_check"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	pkgs, units, bins, err := proxyDeploySpec(kind)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	vip := strings.TrimSpace(req.VIP)
	if i := strings.IndexByte(vip, '/'); i >= 0 {
		vip = vip[:i]
	}
	if vip == "" || net.ParseIP(vip) == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "VIP 必填且须为合法 IP（三种代理均部署 Keepalived）"})
		return
	}
	jobID := fmt.Sprintf("%d", time.Now().UnixNano())
	if len(jobID) > 16 {
		jobID = jobID[len(jobID)-16:]
	}
	job := &proxyDeployJob{ID: jobID, Status: "running", Message: "deploying", Kind: kind}
	putProxyDeployJob(jobID, job)
	go runProxyDeploy(job, kind, vip, pkgs, units, bins, req.EnableStart, req.SkipInstall, req.SkipVIPCheck)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "job_id": jobID})
}

func handleProxyDeployJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/v1/proxy/deploy/jobs/")
	id = strings.Trim(id, "/")
	proxyDeployJobsMu.Lock()
	job := proxyDeployJobs[id]
	proxyDeployJobsMu.Unlock()
	if job == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "message": "job not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "job": job})
}

// proxyDeploySpec 三种代理均安装 keepalived；LVS 另装 ipvsadm。
func proxyDeploySpec(kind string) (pkgs []string, units []string, bins []string, err error) {
	switch kind {
	case "haproxy":
		return []string{"haproxy", "keepalived"}, []string{"haproxy", "keepalived"}, []string{"haproxy", "keepalived"}, nil
	case "nginx":
		return []string{"nginx", "keepalived"}, []string{"nginx", "keepalived"}, []string{"nginx", "keepalived"}, nil
	case "lvs":
		return []string{"keepalived", "ipvsadm"}, []string{"keepalived"}, []string{"keepalived", "ipvsadm"}, nil
	default:
		return nil, nil, nil, fmt.Errorf("kind 须为 haproxy|nginx|lvs")
	}
}

func runProxyDeploy(job *proxyDeployJob, kind, vip string, pkgs, units, bins []string, enableStart, skipInstall, skipVIPCheck bool) {
	appendLog := func(s string) {
		proxyDeployJobsMu.Lock()
		job.Log += s + "\n"
		job.UpdatedAt = time.Now()
		proxyDeployJobsMu.Unlock()
	}
	fail := func(msg string) {
		proxyDeployJobsMu.Lock()
		job.Status = "failed"
		job.Message = msg
		job.UpdatedAt = time.Now()
		proxyDeployJobsMu.Unlock()
	}
	ok := func(msg string) {
		proxyDeployJobsMu.Lock()
		job.Status = "success"
		job.Message = msg
		job.UpdatedAt = time.Now()
		proxyDeployJobsMu.Unlock()
	}

	if *dryRun {
		appendLog(fmt.Sprintf("dry-run deploy kind=%s vip=%s pkgs=%v units=%v", kind, vip, pkgs, units))
		ok("dry-run ok")
		return
	}

	if vip == "" {
		fail("VIP 必填")
		return
	}
	if !skipVIPCheck {
		appendLog("check VIP " + vip)
		if local, detail := vipOnLocalInterfaces(vip); local {
			fail("VIP 已在本机存在，不可用: " + detail)
			return
		}
		if occupied, how := vipOccupiedOnNetwork(vip); occupied {
			fail("VIP 已被网络占用，不可用: " + how)
			return
		}
		appendLog("VIP check ok")
	} else {
		appendLog("skip_vip_check=true")
	}

	if kind == "lvs" {
		appendLog("modprobe ip_vs (best-effort)")
		_ = exec.Command("modprobe", "ip_vs").Run()
		for _, m := range []string{"ip_vs_rr", "ip_vs_wrr", "ip_vs_lc", "ip_vs_wlc"} {
			_ = exec.Command("modprobe", m).Run()
		}
	}

	if !skipInstall {
		mgr, err := detectPkgManager()
		if err != nil {
			fail(err.Error())
			return
		}
		appendLog("pkg manager: " + mgr)
		seen := map[string]bool{}
		for _, pkg := range pkgs {
			if seen[pkg] {
				continue
			}
			seen[pkg] = true
			if !safePkgName(pkg) {
				fail("非法包名: " + pkg)
				return
			}
			appendLog("install " + pkg)
			if err := installOSPackage(mgr, pkg); err != nil {
				fail(err.Error())
				return
			}
		}
	} else {
		appendLog("skip_install=true")
	}

	for _, b := range bins {
		if lookPath(b) {
			appendLog("found binary: " + b)
		} else {
			fail("安装后未找到命令: " + b)
			return
		}
	}

	for _, unit := range units {
		unit = strings.TrimSpace(unit)
		if unit == "" {
			continue
		}
		if enableStart {
			appendLog("systemctl enable --now " + unit)
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			out, err := exec.CommandContext(ctx, "systemctl", "enable", "--now", unit).CombinedOutput()
			cancel()
			if len(out) > 0 {
				appendLog(strings.TrimSpace(string(out)))
			}
			if err != nil {
				fail(fmt.Sprintf("systemctl enable --now %s: %v", unit, err))
				return
			}
		} else {
			appendLog("systemctl enable " + unit)
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			out, err := exec.CommandContext(ctx, "systemctl", "enable", unit).CombinedOutput()
			cancel()
			if len(out) > 0 {
				appendLog(strings.TrimSpace(string(out)))
			}
			if err != nil {
				appendLog("enable warn " + unit + ": " + err.Error())
			}
		}
	}
	ok("deploy ok")
}

func safePkgName(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' || r == '+' {
			continue
		}
		return false
	}
	return true
}

func detectPkgManager() (string, error) {
	if lookPath("dnf") {
		return "dnf", nil
	}
	if lookPath("yum") {
		return "yum", nil
	}
	if lookPath("apt-get") {
		return "apt-get", nil
	}
	return "", fmt.Errorf("未找到 dnf/yum/apt-get，无法装包")
}

func installOSPackage(mgr, pkg string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	var cmd *exec.Cmd
	switch mgr {
	case "dnf":
		cmd = exec.CommandContext(ctx, "dnf", "install", "-y", pkg)
	case "yum":
		cmd = exec.CommandContext(ctx, "yum", "install", "-y", pkg)
	case "apt-get":
		_ = exec.CommandContext(ctx, "apt-get", "update", "-y").Run()
		cmd = exec.CommandContext(ctx, "apt-get", "install", "-y", pkg)
	default:
		return fmt.Errorf("不支持的包管理器: %s", mgr)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("安装 %s 失败: %v (%s)", pkg, err, strings.TrimSpace(string(out)))
	}
	return nil
}
