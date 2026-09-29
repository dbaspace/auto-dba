package dbdeploy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path"
	"strings"
	"time"

	"auto-dba/internal/nodecheck"
	"auto-dba/internal/secret"
	"auto-dba/internal/sshx"
	"auto-dba/internal/store"
)

// DeployOpts DB Agent SSH 下发选项。
type DeployOpts struct {
	HostInstanceID string
	ListenPort     int
	RemoteDir      string
	Escalate       string
	Operator       string
}

// ResolveDbAgentArtifact 查找已构建的 db-agent 二进制。
func ResolveDbAgentArtifact(configured string) string {
	candidates := []string{
		strings.TrimSpace(configured),
		"deploy/db-agent/bin/db-agent",
	}
	for _, p := range candidates {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return strings.TrimSpace(configured)
}

// StartAgentDeploy 创建下发任务并异步执行；成功后写入/更新 tbl_db_deploy_agents。
func (e *Engine) StartAgentDeploy(opts DeployOpts) (string, error) {
	if e == nil || e.Store == nil {
		return "", fmt.Errorf("engine 未初始化")
	}
	hostID := strings.TrimSpace(opts.HostInstanceID)
	if hostID == "" {
		return "", fmt.Errorf("host_instance_id 必填")
	}
	inst, err := e.Store.GetInstance(hostID)
	if err != nil || inst == nil {
		return "", fmt.Errorf("主机实例不存在")
	}
	if !store.IsHostInstanceType(inst.DBType) {
		return "", fmt.Errorf("仅支持 host 类型实例下发 DB Agent")
	}
	artifact := ResolveDbAgentArtifact(e.Cfg.AgentArtifact)
	if artifact == "" {
		return "", fmt.Errorf("未找到 db-agent 制品（请先 cd deploy/db-agent && ./build.sh）")
	}
	if _, err := os.Stat(artifact); err != nil {
		return "", fmt.Errorf("DB Agent 制品不存在: %s（请先构建 deploy/db-agent）", artifact)
	}
	e.Cfg.AgentArtifact = artifact
	if strings.TrimSpace(e.Cfg.SSHIdentityFile) == "" {
		return "", fmt.Errorf("未配置 terminal.ssh_identity_file，无法 SSH 下发")
	}
	port := opts.ListenPort
	if port <= 0 {
		port = e.Cfg.AgentListenPort
	}
	if port <= 0 {
		port = 8092
	}
	remoteDir := strings.TrimSpace(opts.RemoteDir)
	if remoteDir == "" {
		remoteDir = e.Cfg.AgentRemoteDir
	}
	if remoteDir == "" {
		remoteDir = "/home/sysadmin/auto-dba/db-agent"
	}
	dep := &store.DbDeployAgentDeploy{
		HostInstanceID: hostID,
		Status:         "pending",
		ListenPort:     port,
		RemoteDir:      remoteDir,
		CreatedBy:      opts.Operator,
	}
	if err := e.Store.CreateDbDeployAgentDeploy(dep); err != nil {
		return "", err
	}
	go e.runAgentDeploy(dep.ID, sshx.NormalizeEscalate(opts.Escalate))
	return dep.ID, nil
}

func (e *Engine) runAgentDeploy(deployID, escalate string) {
	dep, err := e.Store.GetDbDeployAgentDeploy(deployID)
	if err != nil || dep == nil {
		return
	}
	dep.Status = "running"
	_ = e.Store.UpdateDbDeployAgentDeploy(dep)

	logf := func(format string, args ...any) {
		line := fmt.Sprintf(format, args...)
		if dep.LogText != "" {
			dep.LogText += "\n"
		}
		dep.LogText += time.Now().Format("15:04:05") + " " + line
		if len(dep.LogText) > 20000 {
			dep.LogText = "…" + dep.LogText[len(dep.LogText)-18000:]
		}
		_ = e.Store.UpdateDbDeployAgentDeploy(dep)
	}
	fail := func(msg string) {
		logf("ERROR: %s", msg)
		dep.Status = "failed"
		dep.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		_ = e.Store.UpdateDbDeployAgentDeploy(dep)
	}

	inst, err := e.Store.GetInstance(dep.HostInstanceID)
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
		sshUser = e.Cfg.SSHUser
	}
	if sshUser == "" {
		sshUser = "sysadmin"
	}
	sshPort := inst.Port
	if sshPort <= 0 {
		sshPort = 22
	}

	authUser := "agent"
	authPass := randomToken(16)
	listen := mustListen(dep.ListenPort)
	binRemote := path.Join(dep.RemoteDir, "db-agent")
	startRemote := path.Join(dep.RemoteDir, "start.sh")
	unitRemote := path.Join(dep.RemoteDir, "db-agent.service")
	pidFile := path.Join(dep.RemoteDir, "agent.pid")

	if escalate != "" {
		logf("提权模式: %s", escalate)
	}
	logf("连接 %s@%s:%d …", sshUser, host, sshPort)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	client, err := sshx.Dial(host, sshPort, sshx.Config{
		User:            sshUser,
		IdentityFile:    e.Cfg.SSHIdentityFile,
		Timeout:         30 * time.Second,
		InsecureHostKey: e.Cfg.SSHInsecureHostKey,
	})
	if err != nil {
		fail(err.Error())
		return
	}
	defer client.Close()

	runRemote := func(cmd string) (string, string, error) {
		return client.RunEscalated(ctx, escalate, cmd)
	}

	logf("停止旧 DB Agent（若存在）…")
	outStop, errStop, _ := runRemote(killDbAgentScript(pidFile, dep.ListenPort))
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
		if _, _, err := runRemote(chownCmd); err != nil {
			logf("WARN: chown 失败（SFTP 可能无写权限）: %v", err)
		}
	}

	logf("上传制品 %s → %s", e.Cfg.AgentArtifact, binRemote)
	if err := client.Upload(e.Cfg.AgentArtifact, binRemote); err != nil {
		fail(err.Error())
		return
	}
	_, _, _ = runRemote("chmod 755 " + shellQuote(binRemote))

	startBody := renderStartScript(listen, authUser, authPass, binRemote)
	logf("写入 start.sh")
	if err := client.WriteFile(startRemote, []byte(startBody), 0o755); err != nil {
		fail(err.Error())
		return
	}
	unitBody := renderAgentUnit(dep.RemoteDir, listen, authUser, authPass)
	_ = client.WriteFile(unitRemote, []byte(unitBody), 0o644)

	logf("启动 DB Agent …")
	out, errOut, err := runRemote(shellQuote(startRemote))
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

	auth := AgentAuth{BaseURL: baseURL, Username: authUser, Password: authPass}
	var probeErr error
	for i := 0; i < 15; i++ {
		probeCtx, c2 := context.WithTimeout(context.Background(), 5*time.Second)
		probeErr = e.Agent.Probe(probeCtx, auth)
		c2()
		if probeErr == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if probeErr != nil {
		fail(fmt.Sprintf("探活失败: %v（请检查防火墙/监听；确认制品为 db-agent 且端口 %d）", probeErr, dep.ListenPort))
		return
	}
	logf("探活成功")

	enc, err := secret.Encrypt(e.Secret, authPass)
	if err != nil {
		fail("加密 Agent 密码失败: " + err.Error())
		return
	}
	exist, _ := e.Store.FindDbDeployAgentByHostInstance(dep.HostInstanceID)
	if exist != nil {
		exist.Name = fmt.Sprintf("db-agent-%s", inst.Name)
		exist.BaseURL = baseURL
		exist.Username = authUser
		exist.PasswordEnc = enc
		exist.HostInstID = dep.HostInstanceID
		exist.Remark = "db-agent deployed by auto-dba"
		exist.UpdatedBy = dep.CreatedBy
		if err := e.Store.UpsertDbDeployAgent(exist); err != nil {
			fail("更新 Agent 记录失败: " + err.Error())
			return
		}
		dep.AgentID = exist.ID
		logf("已更新 DB Agent %s", exist.ID)
	} else {
		a := &store.DbDeployAgent{
			Name:        fmt.Sprintf("db-agent-%s", inst.Name),
			BaseURL:     baseURL,
			Username:    authUser,
			PasswordEnc: enc,
			HostInstID:  dep.HostInstanceID,
			Remark:      "db-agent deployed by auto-dba",
			CreatedBy:   dep.CreatedBy,
			UpdatedBy:   dep.CreatedBy,
		}
		if err := e.Store.UpsertDbDeployAgent(a); err != nil {
			fail("创建 Agent 记录失败: " + err.Error())
			return
		}
		dep.AgentID = a.ID
		logf("已登记 DB Agent %s", a.ID)
	}
	if n, err := e.Store.RebindDbDeployNodesAgent(dep.HostInstanceID, nodecheck.AgentAddrs(inst, baseURL), dep.AgentID); err != nil {
		logf("WARN: 重新绑定节点失败: %v", err)
	} else if n > 0 {
		logf("已重新绑定 %d 个节点", n)
	}

	dep.Status = "success"
	dep.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	_ = e.Store.UpdateDbDeployAgentDeploy(dep)
	logf("完成")
}

func killDbAgentScript(pidFile string, listenPort int) string {
	return fmt.Sprintf(
		`set +e
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
  for p in $(pgrep -x db-agent 2>/dev/null); do
    [ "$p" = "$$" ] && continue
    kill "$p" 2>/dev/null
    sleep 0.5
    kill -9 "$p" 2>/dev/null
  done
elif command -v pidof >/dev/null 2>&1; then
  for p in $(pidof db-agent 2>/dev/null); do
    kill "$p" 2>/dev/null
    kill -9 "$p" 2>/dev/null
  done
fi
if [ "$PORT" -gt 0 ] 2>/dev/null && command -v fuser >/dev/null 2>&1; then
  fuser -k "${PORT}/tcp" 2>/dev/null || true
  sleep 0.5
fi
exit 0
`, shellQuote(pidFile), listenPort)
}

func randomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
