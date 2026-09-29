package backup

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

	"auto-dba/internal/secret"
	"auto-dba/internal/sshx"
	"auto-dba/internal/store"
)

// DeployOpts 下发选项。
type DeployOpts struct {
	HostInstanceID string
	ListenPort     int
	RemoteDir      string
	Workdir        string
	Operator       string
	EnabledGroups  []string // backup,rdb,ha
	BackupEngines  []string // postgres,mysql,tidb,oceanbase（backup 组时必填语义）
	Escalate       string   // "" | sudo_su
}

// ResolveAgentArtifact 优先 platform-agent 制品，回退 backup-agent。
func ResolveAgentArtifact(configured string) string {
	candidates := []string{
		strings.TrimSpace(configured),
		"deploy/platform-agent/bin/platform-agent",
		"deploy/backup-agent/bin/platform-agent",
		"deploy/backup-agent/bin/backup-agent",
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

// StartDeploy 创建部署任务并异步执行。
func (e *Engine) StartDeploy(opts DeployOpts) (string, error) {
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
		return "", fmt.Errorf("仅支持 host 类型实例下发平台 Agent")
	}
	artifact := ResolveAgentArtifact(e.Cfg.AgentArtifact)
	if artifact == "" {
		return "", fmt.Errorf("未配置 backup.agent_artifact，且未找到 platform-agent/backup-agent 制品")
	}
	if _, err := os.Stat(artifact); err != nil {
		return "", fmt.Errorf("平台 Agent 制品不存在: %s（请先构建 deploy/backup-agent）", artifact)
	}
	e.Cfg.AgentArtifact = artifact
	if strings.TrimSpace(e.Cfg.SSHIdentityFile) == "" {
		return "", fmt.Errorf("未配置 terminal.ssh_identity_file，无法 SSH 下发")
	}
	groups := NormalizeGroups(opts.EnabledGroups)
	if len(groups) == 0 {
		groups = DefaultEnabledGroups()
	}
	backupEngines := NormalizeBackupEngines(opts.BackupEngines)
	for _, g := range groups {
		if g == GroupBackup && len(backupEngines) == 0 {
			return "", fmt.Errorf("勾选备份控制时请至少选择一种实例类型（PostgreSQL / MySQL / TiDB / OceanBase）")
		}
	}
	escalate := sshx.NormalizeEscalate(opts.Escalate)
	port := opts.ListenPort
	if port <= 0 {
		port = e.Cfg.AgentListenPort
	}
	if port <= 0 {
		port = 8090
	}
	remoteDir := strings.TrimSpace(opts.RemoteDir)
	if remoteDir == "" {
		remoteDir = e.Cfg.AgentRemoteDir
	}
	if remoteDir == "" {
		remoteDir = "/home/sysadmin/auto-dba/platform-agent"
	}
	workdir := strings.TrimSpace(opts.Workdir)
	if workdir == "" {
		workdir = path.Join(remoteDir, "data")
	}

	dep := &store.BackupAgentDeploy{
		HostInstanceID: hostID,
		Status:         store.BackupDeployPending,
		ListenPort:     port,
		RemoteDir:      remoteDir,
		CreatedBy:      opts.Operator,
	}
	if err := e.Store.CreateBackupAgentDeploy(dep); err != nil {
		return "", err
	}
	go e.runDeploy(dep.ID, workdir, groups, backupEngines, escalate)
	return dep.ID, nil
}

// PreflightHost 下发前 SSH 预检（不同步写库）。
func (e *Engine) PreflightHost(ctx context.Context, opts DeployOpts) (*CapabilitiesReport, error) {
	if e == nil || e.Store == nil {
		return nil, fmt.Errorf("engine 未初始化")
	}
	hostID := strings.TrimSpace(opts.HostInstanceID)
	inst, err := e.Store.GetInstance(hostID)
	if err != nil || inst == nil {
		return nil, fmt.Errorf("主机实例不存在")
	}
	if !store.IsHostInstanceType(inst.DBType) {
		return nil, fmt.Errorf("仅支持 host 类型实例")
	}
	if strings.TrimSpace(e.Cfg.SSHIdentityFile) == "" {
		return nil, fmt.Errorf("未配置 terminal.ssh_identity_file")
	}
	host := strings.TrimSpace(inst.Host)
	if host == "" {
		host = strings.TrimSpace(inst.DomainName)
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
	remoteDir := strings.TrimSpace(opts.RemoteDir)
	if remoteDir == "" {
		remoteDir = e.Cfg.AgentRemoteDir
	}
	if remoteDir == "" {
		remoteDir = "/home/sysadmin/auto-dba/platform-agent"
	}
	workdir := strings.TrimSpace(opts.Workdir)
	if workdir == "" {
		workdir = path.Join(remoteDir, "data")
	}
	groups := NormalizeGroups(opts.EnabledGroups)
	if len(groups) == 0 {
		groups = DefaultEnabledGroups()
	}
	engines := NormalizeBackupEngines(opts.BackupEngines)
	for _, g := range groups {
		if g == GroupBackup && len(engines) == 0 {
			return nil, fmt.Errorf("勾选备份控制时请至少选择一种实例类型（PostgreSQL / MySQL / TiDB / OceanBase）")
		}
	}
	return RunPreflight(ctx, PreflightOpts{
		Host: host, Port: sshPort, User: sshUser, IdentityFile: e.Cfg.SSHIdentityFile,
		RemoteDir: remoteDir, Workdir: workdir, EnabledGroups: groups, BackupEngines: engines,
		Escalate: opts.Escalate, InsecureHostKey: e.Cfg.SSHInsecureHostKey,
	})
}

func (e *Engine) runDeploy(deployID, workdir string, groups, backupEngines []string, escalate string) {
	dep, err := e.Store.GetBackupAgentDeploy(deployID)
	if err != nil || dep == nil {
		return
	}
	dep.Status = store.BackupDeployRunning
	_ = e.Store.UpdateBackupAgentDeploy(dep)

	logf := func(format string, args ...any) {
		line := fmt.Sprintf(format, args...)
		if dep.LogText != "" {
			dep.LogText += "\n"
		}
		dep.LogText += time.Now().Format("15:04:05") + " " + line
		if len(dep.LogText) > 20000 {
			dep.LogText = "…" + dep.LogText[len(dep.LogText)-18000:]
		}
		_ = e.Store.UpdateBackupAgentDeploy(dep)
	}

	fail := func(msg string) {
		logf("ERROR: %s", msg)
		dep.Status = store.BackupDeployFailed
		dep.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		_ = e.Store.UpdateBackupAgentDeploy(dep)
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
	groups = NormalizeGroups(groups)
	if len(groups) == 0 {
		groups = DefaultEnabledGroups()
	}
	escalate = sshx.NormalizeEscalate(escalate)
	logf("启用能力组: %s", strings.Join(groups, ","))
	if escalate != "" {
		logf("提权模式: %s（sudo su -）", escalate)
	}

	authUser := "agent"
	authPass := randomToken(16)
	listen := mustListen(dep.ListenPort)
	binRemote := path.Join(dep.RemoteDir, "platform-agent")
	startRemote := path.Join(dep.RemoteDir, "start.sh")
	unitRemote := path.Join(dep.RemoteDir, "platform-agent.service")

	logf("连接 %s@%s:%d …", sshUser, host, sshPort)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// 预检（按勾选能力组）
	logf("预检目标机工具依赖 …")
	preRep, preErr := RunPreflight(ctx, PreflightOpts{
		Host: host, Port: sshPort, User: sshUser, IdentityFile: e.Cfg.SSHIdentityFile,
		RemoteDir: dep.RemoteDir, Workdir: workdir, EnabledGroups: groups, BackupEngines: backupEngines,
		Escalate: escalate, InsecureHostKey: e.Cfg.SSHInsecureHostKey,
	})
	if preErr != nil {
		fail("预检失败: " + preErr.Error())
		return
	}
	if miss := MissingRequired(preRep); len(miss) > 0 {
		fail("能力预检未通过\n" + FormatPreflightSummary(preRep))
		return
	}
	logf("预检通过")

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

	logf("停止旧 Agent（若存在）…")
	pidFile := path.Join(dep.RemoteDir, "agent.pid")
	outStop, errStop, _ := runRemote(killAgentRemoteScript(pidFile, dep.ListenPort))
	if s := strings.TrimSpace(outStop + errStop); s != "" {
		logf("停止输出: %s", s)
	}
	time.Sleep(500 * time.Millisecond)

	logf("创建目录 %s", dep.RemoteDir)
	if _, _, err := runRemote("mkdir -p " + shellQuote(workdir) + " " + shellQuote(dep.RemoteDir)); err != nil {
		fail("创建目录失败: " + err.Error())
		return
	}
	// 提权创建目录后 chown 给登录用户，便于 SFTP 上传
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
	// 兼容旧路径备份名
	_, _, _ = runRemote("ln -sfn " + shellQuote(binRemote) + " " + shellQuote(path.Join(dep.RemoteDir, "backup-agent")))

	startBody := renderStartScript(listen, authUser, authPass, workdir, binRemote)
	logf("写入 start.sh")
	if err := client.WriteFile(startRemote, []byte(startBody), 0o755); err != nil {
		fail(err.Error())
		return
	}
	unitBody := renderUnit(dep.RemoteDir, listen, authUser, authPass, workdir)
	_ = client.WriteFile(unitRemote, []byte(unitBody), 0o644)

	logf("启动平台 Agent …")
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

	var probeErr error
	for i := 0; i < 15; i++ {
		probeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		probeErr = ProbeAgent(probeCtx, AgentAuth{BaseURL: baseURL, Username: authUser, Password: authPass})
		cancel()
		if probeErr == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if probeErr != nil {
		fail(fmt.Sprintf("探活失败: %v（请检查防火墙/监听地址）", probeErr))
		return
	}
	logf("探活成功")

	capRep := preRep
	if remoteCap, cerr := CallCapabilities(context.Background(), AgentAuth{BaseURL: baseURL, Username: authUser, Password: authPass}, groups); cerr == nil && remoteCap != nil {
		capRep = remoteCap
		logf("已同步 Agent capabilities")
	}

	enc, err := secret.Encrypt(e.Secret, authPass)
	if err != nil {
		fail("加密 Agent 密码失败: " + err.Error())
		return
	}
	engines := ComposeAgentEngines(backupEngines, groups)
	capJSON := EncodeCapabilities(capRep)
	groupsCSV := GroupsCSV(groups)
	tag := store.MergeBackupAgentTags("", dep.HostInstanceID, escalate)
	exist, _ := e.Store.FindBackupAgentByHostInstance(dep.HostInstanceID)
	if exist != nil {
		exist.Name = fmt.Sprintf("platform-%s", inst.Name)
		exist.BaseURL = baseURL
		exist.Username = authUser
		exist.PasswordEnc = enc
		exist.Engines = engines
		exist.EnabledGroups = groupsCSV
		exist.Capabilities = capJSON
		exist.Tags = store.MergeBackupAgentTags(exist.Tags, dep.HostInstanceID, escalate)
		exist.Remark = "platform agent deployed by auto-dba"
		exist.UpdatedBy = dep.CreatedBy
		if err := e.Store.UpdateBackupAgent(exist); err != nil {
			fail("更新 Agent 记录失败: " + err.Error())
			return
		}
		dep.AgentID = exist.ID
		logf("已更新平台 Agent %s", exist.ID)
	} else {
		a := &store.BackupAgent{
			Name:           fmt.Sprintf("platform-%s", inst.Name),
			BaseURL:        baseURL,
			Username:       authUser,
			PasswordEnc:    enc,
			Engines:        engines,
			EnabledGroups:  groupsCSV,
			Capabilities:   capJSON,
			Tags:           tag,
			Remark:         "platform agent deployed by auto-dba",
			CreatedBy:      dep.CreatedBy,
			UpdatedBy:      dep.CreatedBy,
		}
		if err := e.Store.CreateBackupAgent(a); err != nil {
			fail("创建 Agent 记录失败: " + err.Error())
			return
		}
		dep.AgentID = a.ID
		logf("已登记平台 Agent %s", a.ID)
	}

	dep.Status = store.BackupDeploySuccess
	dep.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	_ = e.Store.UpdateBackupAgentDeploy(dep)
	logf("完成")
}

func randomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
