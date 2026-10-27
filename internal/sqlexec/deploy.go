package sqlexec

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"auto-dba/internal/agentport"
	"auto-dba/internal/config"
	"auto-dba/internal/secret"
	"auto-dba/internal/sshx"
	"auto-dba/internal/store"
)

const DeployKind = "sql"

// DeployOpts SQL 执行器 SSH 下发选项。
type DeployOpts struct {
	HostInstanceID  string
	ListenPort      int
	RemoteDir       string
	Escalate        string
	Operator        string
	Name            string
	EnvCodes        []string
	Secret          string
	SSHIdentityFile string
	SSHUser         string
	SSHInsecure     bool
	// ResolveIdentity 按实例 idc 选钥；Dial 时优先于 SSHIdentityFile
	ResolveIdentity func(envCode string) string
}

func resolveSSHIdentity(envCode, fallback string, resolve func(string) string) string {
	if resolve != nil {
		if p := strings.TrimSpace(resolve(envCode)); p != "" {
			return p
		}
	}
	if cfg := config.Live(); cfg != nil {
		if p := cfg.Terminal.ResolveSSHIdentity(envCode); p != "" {
			return p
		}
	}
	return strings.TrimSpace(fallback)
}

const sqlAgentRel = "deploy/sql-agent/bin/sql-agent"

// moduleRootFrom 从 dir 向上找含 go.mod 的目录。找不到时返回空，避免把文件系统根当成模块根。
func moduleRootFrom(dir string) string {
	dir = filepath.Clean(dir)
	for {
		if st, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !st.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func moduleRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	return moduleRootFrom(dir)
}

func existingFile(p string) string {
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		return p
	}
	return ""
}

func resolveArtifactIn(dirs []string) string {
	seen := map[string]struct{}{}
	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		dir = filepath.Clean(dir)
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		if p := existingFile(filepath.Join(dir, filepath.FromSlash(sqlAgentRel))); p != "" {
			return p
		}
	}
	return ""
}

// ResolveArtifact 查找安装包或源码树里已构建的 sql-agent。
func ResolveArtifact() string {
	dirs := []string{}
	if wd, err := os.Getwd(); err == nil {
		dirs = append(dirs, wd)
	}
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		dirs = append(dirs, exeDir, filepath.Dir(exeDir))
	}
	if root := moduleRoot(); root != "" {
		dirs = append(dirs, root)
	}
	return resolveArtifactIn(dirs)
}

// sourceRoot 仅在含 go.mod 且存在 sql-agent 源码时返回模块根。
func sourceRoot() (string, bool) {
	root := moduleRoot()
	if root == "" {
		return "", false
	}
	src := filepath.Join(root, "deploy", "sql-agent", "main.go")
	if st, err := os.Stat(src); err != nil || st.IsDir() {
		return "", false
	}
	return root, true
}

func buildArtifact(logf func(string, ...any)) (string, error) {
	root, ok := sourceRoot()
	if !ok {
		return "", fmt.Errorf("未找到安装包内的 %s，工作目录须为包根", sqlAgentRel)
	}
	out := filepath.Join(root, filepath.FromSlash(sqlAgentRel))
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return "", err
	}
	logf("正在编译 linux/amd64")
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w", "-o", out, ".")
	cmd.Dir = filepath.Join(root, "deploy", "sql-agent")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64")
	buf, err := cmd.CombinedOutput()
	if text := strings.TrimSpace(string(buf)); text != "" {
		logf("%s", text)
	}
	if err != nil {
		return "", fmt.Errorf("编译 sql-agent 失败: %w", err)
	}
	return out, nil
}

// StartDeploy 创建下发任务。环境冲突在调用前由 handler 拒绝。
func StartDeploy(db *store.DB, opts DeployOpts) (string, error) {
	if db == nil {
		return "", fmt.Errorf("store 未初始化")
	}
	hostID := strings.TrimSpace(opts.HostInstanceID)
	if hostID == "" {
		return "", fmt.Errorf("host_instance_id 必填")
	}
	inst, err := db.GetInstance(hostID)
	if err != nil || inst == nil {
		return "", fmt.Errorf("主机实例不存在")
	}
	if !store.IsHostInstanceType(inst.DBType) {
		return "", fmt.Errorf("仅支持 host 类型实例下发 SQL 执行器")
	}
	if resolveSSHIdentity(inst.IDC, opts.SSHIdentityFile, opts.ResolveIdentity) == "" {
		return "", fmt.Errorf("未配置 terminal.ssh_identity_file / ssh_key_profiles，无法 SSH 下发")
	}
	port := opts.ListenPort
	if port <= 0 {
		port = 8093
	}
	if err := agentport.ValidateListenPort(port); err != nil {
		return "", err
	}
	remoteDir := strings.TrimSpace(opts.RemoteDir)
	if remoteDir == "" {
		sshName := strings.TrimSpace(inst.Username)
		if sshName == "" {
			sshName = strings.TrimSpace(opts.SSHUser)
		}
		if sshName == "" {
			sshName = "sysadmin"
		}
		remoteDir = "/home/" + sshName + "/auto-dba/sql-agent"
	}
	recent, err := db.ListSQLExecutorDeploys(hostID, 5)
	if err == nil {
		now := time.Now()
		for _, d := range recent {
			if store.AgentDeployActive(d.Status, d.CreatedAt, now) {
				return "", fmt.Errorf("主机 %s 已有进行中的 SQL 执行器下发任务，请等待完成", inst.Name)
			}
		}
	}
	payload, _ := json.Marshal(map[string]any{
		"env_codes": opts.EnvCodes,
		"name":      strings.TrimSpace(opts.Name),
		"escalate":  sshx.NormalizeEscalate(opts.Escalate),
	})
	dep := &store.BackupAgentDeploy{
		HostInstanceID: hostID,
		Status:         "pending",
		ListenPort:     port,
		RemoteDir:      remoteDir,
		CreatedBy:      opts.Operator,
	}
	if err := db.CreateBackupAgentDeploy(dep); err != nil {
		return "", err
	}
	if err := db.SetBackupAgentDeployKind(dep.ID, DeployKind, string(payload)); err != nil {
		return "", err
	}
	go runDeploy(db, dep.ID, opts)
	return dep.ID, nil
}

func runDeploy(db *store.DB, deployID string, opts DeployOpts) {
	dep, err := db.GetBackupAgentDeploy(deployID)
	if err != nil || dep == nil {
		return
	}
	dep.Status = "running"
	_ = db.UpdateBackupAgentDeploy(dep)
	logf := func(format string, args ...any) {
		line := fmt.Sprintf(format, args...)
		if dep.LogText != "" {
			dep.LogText += "\n"
		}
		dep.LogText += time.Now().Format("15:04:05") + " " + line
		if len(dep.LogText) > 20000 {
			dep.LogText = "…" + dep.LogText[len(dep.LogText)-18000:]
		}
		_ = db.UpdateBackupAgentDeploy(dep)
	}
	fail := func(msg string) {
		logf("ERROR: %s", msg)
		dep.Status = "failed"
		dep.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		_ = db.UpdateBackupAgentDeploy(dep)
	}

	inst, err := db.GetInstance(dep.HostInstanceID)
	if err != nil || inst == nil {
		fail("主机实例不存在")
		return
	}
	host := strings.TrimSpace(inst.Host)
	if host == "" {
		host = strings.TrimSpace(inst.DomainName)
	}
	if host == "" {
		fail("主机缺少 Host/Domain")
		return
	}
	sshUser := strings.TrimSpace(inst.Username)
	if sshUser == "" {
		sshUser = opts.SSHUser
	}
	if sshUser == "" {
		sshUser = "sysadmin"
	}
	sshPort := inst.Port
	if sshPort <= 0 {
		sshPort = 22
	}
	escalate := sshx.NormalizeEscalate(opts.Escalate)
	artifact := ResolveArtifact()
	if artifact == "" {
		built, berr := buildArtifact(logf)
		if berr != nil {
			wd, _ := os.Getwd()
			fail(fmt.Sprintf("%s（当前工作目录 %s）", berr.Error(), wd))
			return
		}
		artifact = built
	}
	logf("使用 sql-agent：%s", artifact)
	authUser := "sqlagent"
	authPass := randomToken(16)
	listen := mustListen(dep.ListenPort)
	binRemote := path.Join(dep.RemoteDir, "sql-agent")
	startRemote := path.Join(dep.RemoteDir, "start.sh")
	unitRemote := path.Join(dep.RemoteDir, "sql-agent.service")
	pidFile := path.Join(dep.RemoteDir, "agent.pid")

	logf("仅安装/重启 SQL 执行器（目录 %s、端口 %d）；不启停数据库或平台 Agent", dep.RemoteDir, dep.ListenPort)
	if escalate != "" {
		logf("提权模式: %s", escalate)
	}
	identity := resolveSSHIdentity(inst.IDC, opts.SSHIdentityFile, opts.ResolveIdentity)
	if identity == "" {
		fail("未配置 terminal.ssh_identity_file / ssh_key_profiles，无法 SSH 下发")
		return
	}
	logf("连接 %s@%s:%d …", sshUser, host, sshPort)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	client, err := sshx.Dial(host, sshPort, sshx.Config{
		User:            sshUser,
		IdentityFile:    identity,
		Timeout:         30 * time.Second,
		InsecureHostKey: opts.SSHInsecure,
	})
	if err != nil {
		fail(err.Error())
		return
	}
	defer client.Close()
	runRemote := func(cmd string) (string, string, error) {
		return client.RunEscalated(ctx, escalate, cmd)
	}

	logf("停止旧 SQL 执行器（若存在）…")
	outStop, errStop, _ := runRemote(killSQLAgentScript(pidFile, dep.ListenPort))
	if s := strings.TrimSpace(outStop + errStop); s != "" {
		logf("停止输出: %s", s)
	}
	time.Sleep(500 * time.Millisecond)

	logf("创建目录 %s", dep.RemoteDir)
	if _, _, err := runRemote("mkdir -p " + shellQuote(dep.RemoteDir)); err != nil {
		fail("创建目录失败: " + err.Error())
		return
	}
	if escalate == sshx.EscalateSudoSu {
		chownCmd := "chown -R " + shellQuote(sshUser+":"+sshUser) + " " + shellQuote(dep.RemoteDir)
		out, errOut, err := runRemote(chownCmd)
		if err != nil {
			fail("目录属主仍是 root，上传不会继续: " + strings.TrimSpace(out+" "+errOut) + " " + err.Error())
			return
		}
		logf("已将 %s 属主改为 %s", dep.RemoteDir, sshUser)
	}
	logf("上传制品 %s → %s", artifact, binRemote)
	if err := client.Upload(artifact, binRemote); err != nil {
		fail(err.Error())
		return
	}
	_, _, _ = runRemote("chmod 755 " + shellQuote(binRemote))

	if err := client.WriteFile(startRemote, []byte(renderStartScript(listen, authUser, authPass, binRemote)), 0o755); err != nil {
		fail(err.Error())
		return
	}
	unitBody := renderUnit(dep.RemoteDir, listen, authUser, authPass)
	if err := client.WriteFile(unitRemote, []byte(unitBody), 0o644); err != nil {
		fail(err.Error())
		return
	}

	logf("启动 SQL 执行器 …")
	var out, errOut string
	if escalate != "" {
		install := fmt.Sprintf("cp %s /etc/systemd/system/sql-agent.service && systemctl daemon-reload && systemctl enable --now sql-agent", shellQuote(unitRemote))
		out, errOut, err = runRemote(install)
	} else {
		out, errOut, err = runRemote(shellQuote(startRemote))
	}
	if err != nil {
		fail(fmt.Sprintf("启动失败: %v; out=%s err=%s", err, out, errOut))
		return
	}
	logf("启动输出: %s", strings.TrimSpace(out+errOut))

	reachHost := strings.TrimSpace(inst.DomainName)
	if reachHost == "" {
		reachHost = host
	}
	baseURL := fmt.Sprintf("http://%s", net.JoinHostPort(reachHost, fmt.Sprintf("%d", dep.ListenPort)))
	dep.BaseURL = baseURL
	logf("探活 %s/healthz …", baseURL)
	var probeErr error
	for i := 0; i < 15; i++ {
		probeCtx, c2 := context.WithTimeout(context.Background(), 5*time.Second)
		probeErr = Probe(probeCtx, baseURL, authUser, authPass)
		c2()
		if probeErr == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if probeErr != nil {
		fail("探活失败: " + probeErr.Error())
		return
	}
	logf("探活成功")

	enc, err := secret.Encrypt(opts.Secret, authPass)
	if err != nil {
		fail("加密执行器密码失败: " + err.Error())
		return
	}
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = "sql-agent-" + inst.Name
	}
	codes := opts.EnvCodes
	if fresh, err := db.GetBackupAgentDeploy(deployID); err == nil && fresh != nil && fresh.Payload != "" {
		var body struct {
			EnvCodes []string `json:"env_codes"`
			Name     string   `json:"name"`
		}
		if json.Unmarshal([]byte(fresh.Payload), &body) == nil {
			if len(body.EnvCodes) > 0 {
				codes = body.EnvCodes
			}
			if strings.TrimSpace(body.Name) != "" && strings.TrimSpace(opts.Name) == "" {
				name = strings.TrimSpace(body.Name)
			}
		}
	}
	row, _ := db.FindSQLExecutorByName(name)
	if row == nil {
		row = &store.SQLExecutor{Name: name, Enabled: true}
	}
	row.Name = name
	row.BaseURL = baseURL
	row.Username = authUser
	row.PasswordEnc = enc
	row.EnvCodes = codes
	row.Enabled = true
	if err := db.SaveSQLExecutor(row); err != nil {
		fail("登记执行器失败: " + err.Error())
		return
	}
	dep.AgentID = row.ID
	logf("已登记执行器 %s", row.ID)
	dep.Status = "success"
	dep.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	_ = db.UpdateBackupAgentDeploy(dep)
	logf("完成")
}

func killSQLAgentScript(pidFile string, listenPort int) string {
	return fmt.Sprintf(
		`set +e
if command -v systemctl >/dev/null 2>&1; then
  if systemctl list-unit-files sql-agent.service >/dev/null 2>&1 || [ -f /etc/systemd/system/sql-agent.service ]; then
    systemctl disable --now sql-agent 2>/dev/null || systemctl stop sql-agent 2>/dev/null
  fi
fi
PIDF=%s
PORT=%d
if [ -f "$PIDF" ]; then
  OPID=$(cat "$PIDF" 2>/dev/null)
  if [ -n "$OPID" ]; then
    kill "$OPID" 2>/dev/null
    sleep 1
    kill -9 "$OPID" 2>/dev/null
  fi
  rm -f "$PIDF"
fi
if command -v pgrep >/dev/null 2>&1; then
  for p in $(pgrep -x sql-agent 2>/dev/null); do
    [ "$p" = "$$" ] && continue
    kill "$p" 2>/dev/null
    sleep 0.5
    kill -9 "$p" 2>/dev/null
  done
fi
%s
exit 0
`, shellQuote(pidFile), listenPort, agentport.FuserKillSnippet(listenPort))
}

// DeployPayloadEscalate 从下发记录 payload 取出提权方式。
func DeployPayloadEscalate(payload string) string {
	var body struct {
		Escalate string `json:"escalate"`
	}
	_ = json.Unmarshal([]byte(payload), &body)
	return sshx.NormalizeEscalate(body.Escalate)
}

// StopOpts 停止远端 sql-agent。
type StopOpts struct {
	HostInstanceID  string
	RemoteDir       string
	ListenPort      int
	Escalate        string
	SSHIdentityFile string
	SSHUser         string
	SSHInsecure     bool
	ResolveIdentity func(envCode string) string
}

// StopRemote 停掉目标机上的 sql-agent（systemd 禁用并杀进程）。
func StopRemote(db *store.DB, opts StopOpts) error {
	if db == nil {
		return fmt.Errorf("数据库未初始化")
	}
	hostID := strings.TrimSpace(opts.HostInstanceID)
	if hostID == "" {
		return fmt.Errorf("缺少主机")
	}
	inst, err := db.GetInstance(hostID)
	if err != nil || inst == nil {
		return fmt.Errorf("主机实例不存在")
	}
	identity := resolveSSHIdentity(inst.IDC, opts.SSHIdentityFile, opts.ResolveIdentity)
	if identity == "" {
		return fmt.Errorf("未配置 terminal.ssh_identity_file / ssh_key_profiles，无法 SSH 停止")
	}
	remoteDir := strings.TrimSpace(opts.RemoteDir)
	if remoteDir == "" {
		sshName := strings.TrimSpace(inst.Username)
		if sshName == "" {
			sshName = strings.TrimSpace(opts.SSHUser)
		}
		if sshName == "" {
			sshName = "sysadmin"
		}
		remoteDir = "/home/" + sshName + "/auto-dba/sql-agent"
	}
	port := opts.ListenPort
	if port <= 0 {
		port = 8093
	}
	host := strings.TrimSpace(inst.Host)
	if host == "" {
		host = strings.TrimSpace(inst.DomainName)
	}
	if host == "" {
		return fmt.Errorf("主机缺少 Host/Domain")
	}
	sshUser := strings.TrimSpace(inst.Username)
	if sshUser == "" {
		sshUser = opts.SSHUser
	}
	if sshUser == "" {
		sshUser = "sysadmin"
	}
	sshPort := inst.Port
	if sshPort <= 0 {
		sshPort = 22
	}
	escalate := sshx.NormalizeEscalate(opts.Escalate)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client, err := sshx.Dial(host, sshPort, sshx.Config{
		User:            sshUser,
		IdentityFile:    identity,
		Timeout:         30 * time.Second,
		InsecureHostKey: opts.SSHInsecure,
	})
	if err != nil {
		return err
	}
	defer client.Close()
	pidFile := path.Join(remoteDir, "agent.pid")
	out, errOut, err := client.RunEscalated(ctx, escalate, killSQLAgentScript(pidFile, port))
	if err != nil {
		return fmt.Errorf("停止失败: %v; out=%s err=%s", err, strings.TrimSpace(out), strings.TrimSpace(errOut))
	}
	return nil
}

// DetectSystemdEscalate 目标机已有 sql-agent 单元时返回提权模式，供旧下发记录重发。
func DetectSystemdEscalate(db *store.DB, opts StopOpts) string {
	if db == nil || strings.TrimSpace(opts.HostInstanceID) == "" {
		return ""
	}
	inst, err := db.GetInstance(strings.TrimSpace(opts.HostInstanceID))
	if err != nil || inst == nil {
		return ""
	}
	identity := resolveSSHIdentity(inst.IDC, opts.SSHIdentityFile, opts.ResolveIdentity)
	if identity == "" {
		return ""
	}
	host := strings.TrimSpace(inst.Host)
	if host == "" {
		host = strings.TrimSpace(inst.DomainName)
	}
	if host == "" {
		return ""
	}
	sshUser := strings.TrimSpace(inst.Username)
	if sshUser == "" {
		sshUser = opts.SSHUser
	}
	if sshUser == "" {
		sshUser = "sysadmin"
	}
	sshPort := inst.Port
	if sshPort <= 0 {
		sshPort = 22
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := sshx.Dial(host, sshPort, sshx.Config{
		User:            sshUser,
		IdentityFile:    identity,
		Timeout:         20 * time.Second,
		InsecureHostKey: opts.SSHInsecure,
	})
	if err != nil {
		return ""
	}
	defer client.Close()
	out, _, err := client.RunEscalated(ctx, sshx.EscalateSudoSu, `test -f /etc/systemd/system/sql-agent.service && echo yes`)
	if err == nil && strings.Contains(out, "yes") {
		return sshx.EscalateSudoSu
	}
	return ""
}

func randomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
