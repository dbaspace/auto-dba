package backup

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"auto-dba/internal/secret"
	"auto-dba/internal/sshx"
	"auto-dba/internal/store"
)

const (
	AgentStatusRunning = "running"
	AgentStatusStopped = "stopped"
	AgentStatusUnknown = "unknown"
)

// AgentRuntimeStatus 运行态探测结果。
type AgentRuntimeStatus struct {
	ID      string `json:"id"`
	Status  string `json:"status"` // running|stopped|unknown
	Message string `json:"message,omitempty"`
	BaseURL string `json:"base_url,omitempty"`
}

// ProbeAgentStatus 通过 healthz 判断是否在跑。
func (e *Engine) ProbeAgentStatus(ctx context.Context, a *store.BackupAgent) AgentRuntimeStatus {
	out := AgentRuntimeStatus{ID: a.ID, Status: AgentStatusUnknown, BaseURL: a.BaseURL}
	if a == nil || strings.TrimSpace(a.BaseURL) == "" {
		out.Message = "无 Base URL"
		return out
	}
	pass := ""
	if a.PasswordEnc != "" && e != nil && e.Secret != "" {
		pass, _ = secret.Decrypt(e.Secret, a.PasswordEnc)
	}
	if err := ProbeAgent(ctx, AgentAuth{BaseURL: a.BaseURL, Username: a.Username, Password: pass}); err != nil {
		out.Status = AgentStatusStopped
		out.Message = err.Error()
		return out
	}
	out.Status = AgentStatusRunning
	out.Message = "healthz ok"
	return out
}

// StopRemoteAgent 经 SSH 停止平台下发的远端 Agent。
// requireHost=false 时无 host 绑定则跳过（删除登记场景）；true 时返回错误。
func (e *Engine) StopRemoteAgent(a *store.BackupAgent) error {
	return e.StopRemoteAgentOpt(a, false)
}

// StopRemoteAgentOpt 停止远端；requireHost 为 true 时必须是平台下发。
func (e *Engine) StopRemoteAgentOpt(a *store.BackupAgent, requireHost bool) error {
	if e == nil || e.Store == nil {
		return fmt.Errorf("engine 未初始化")
	}
	if a == nil {
		return fmt.Errorf("Agent 为空")
	}
	hostID := agentHostID(a)
	if hostID == "" {
		if requireHost {
			return fmt.Errorf("该 Agent 非平台下发，无法远程启停（请在目标机手动操作）")
		}
		return nil
	}
	client, hostMeta, err := e.dialAgentHost(hostID)
	if err != nil {
		return err
	}
	defer client.Close()

	remoteDir, listenPort := e.resolveRemoteTarget(a, hostID)
	pidFile := path.Join(remoteDir, "agent.pid")
	escalate := sshx.NormalizeEscalate(store.ParseBackupAgentEscalateTag(a.Tags))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// 禁止 pkill -f 全路径：SSH 会话 cmdline 含脚本正文，会误杀自身（exit 143 / SIGTERM）。
	script := killAgentRemoteScript(pidFile, listenPort)
	out, errOut, runErr := client.RunEscalated(ctx, escalate, script)
	_ = hostMeta
	combined := strings.TrimSpace(out + "\n" + errOut)

	// 以 healthz 为准：即便 SSH 会话被误杀，Agent 也可能已停
	probeCtx, cancelProbe := context.WithTimeout(context.Background(), 5*time.Second)
	st := e.ProbeAgentStatus(probeCtx, a)
	cancelProbe()
	if st.Status != AgentStatusRunning {
		return nil
	}
	msg := fmt.Sprintf("远端 Agent 仍在运行（端口 %d，目录 %s）", listenPort, remoteDir)
	if runErr != nil {
		msg += "; " + runErr.Error()
	}
	if combined != "" {
		msg += "; " + combined
	}
	return fmt.Errorf("%s", msg)
}

// StartRemoteAgent 经 SSH 执行远端 start.sh 启动 Agent（需已下发过）。
func (e *Engine) StartRemoteAgent(a *store.BackupAgent) error {
	if e == nil || e.Store == nil {
		return fmt.Errorf("engine 未初始化")
	}
	if a == nil {
		return fmt.Errorf("Agent 为空")
	}
	hostID := agentHostID(a)
	if hostID == "" {
		return fmt.Errorf("该 Agent 非平台下发，无法远程启动")
	}
	client, _, err := e.dialAgentHost(hostID)
	if err != nil {
		return err
	}
	defer client.Close()

	remoteDir, _ := e.resolveRemoteTarget(a, hostID)
	startRemote := path.Join(remoteDir, "start.sh")
	pidFile := path.Join(remoteDir, "agent.pid")
	escalate := sshx.NormalizeEscalate(store.ParseBackupAgentEscalateTag(a.Tags))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// 已在跑则直接返回
	probeCtx, cancelProbe := context.WithTimeout(context.Background(), 5*time.Second)
	st := e.ProbeAgentStatus(probeCtx, a)
	cancelProbe()
	if st.Status == AgentStatusRunning {
		return nil
	}

	script := fmt.Sprintf(
		`set -e
START=%s
PIDF=%s
if [ ! -x "$START" ] && [ ! -f "$START" ]; then
  echo "missing start.sh at $START" >&2
  exit 2
fi
if [ -f "$PIDF" ] && kill -0 "$(cat "$PIDF")" 2>/dev/null; then
  echo "already running pid=$(cat "$PIDF")"
  exit 0
fi
chmod +x "$START" 2>/dev/null || true
"$START"
`,
		shellQuote(startRemote),
		shellQuote(pidFile),
	)
	out, errOut, err := client.RunEscalated(ctx, escalate, script)
	if err != nil {
		return fmt.Errorf("启动失败: %v; out=%s err=%s（若缺少 start.sh 请先「重新下发」）", err, strings.TrimSpace(out), strings.TrimSpace(errOut))
	}

	var last error
	for i := 0; i < 10; i++ {
		pctx, c := context.WithTimeout(context.Background(), 3*time.Second)
		last = nil
		st = e.ProbeAgentStatus(pctx, a)
		c()
		if st.Status == AgentStatusRunning {
			return nil
		}
		last = fmt.Errorf("%s", st.Message)
		time.Sleep(time.Second)
	}
	if last != nil {
		return fmt.Errorf("已执行 start.sh 但探活未成功: %v", last)
	}
	return fmt.Errorf("已执行 start.sh 但探活未成功")
}

type agentHostMeta struct {
	Host    string
	SSHUser string
	SSHPort int
}

func agentHostID(a *store.BackupAgent) string {
	if a == nil {
		return ""
	}
	if id := strings.TrimSpace(a.HostInstance); id != "" {
		return id
	}
	return store.ParseBackupAgentHostTag(a.Tags)
}

// killAgentRemoteScript 安全停止远端 Agent（pid 文件 + 精确进程名；勿用 pkill -f 全路径）。
func killAgentRemoteScript(pidFile string, listenPort int) string {
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
  for p in $(pgrep -x backup-agent 2>/dev/null); do
    [ "$p" = "$$" ] && continue
    kill "$p" 2>/dev/null
    sleep 0.5
    kill -9 "$p" 2>/dev/null
  done
elif command -v pidof >/dev/null 2>&1; then
  for p in $(pidof backup-agent 2>/dev/null); do
    kill "$p" 2>/dev/null
    kill -9 "$p" 2>/dev/null
  done
fi
if [ "$PORT" -gt 0 ] 2>/dev/null && command -v fuser >/dev/null 2>&1; then
  fuser -k "${PORT}/tcp" 2>/dev/null || true
  sleep 0.5
fi
if command -v ss >/dev/null 2>&1; then
  ss -lnt "sport = :${PORT}" 2>/dev/null | grep -q ":${PORT}" && echo STILL_LISTEN || echo STOPPED
elif command -v netstat >/dev/null 2>&1; then
  netstat -lnt 2>/dev/null | grep -q ":${PORT} " && echo STILL_LISTEN || echo STOPPED
else
  echo STOPPED
fi
exit 0
`,
		shellQuote(pidFile),
		listenPort,
	)
}

func (e *Engine) dialAgentHost(hostID string) (*sshx.Client, agentHostMeta, error) {
	var meta agentHostMeta
	if strings.TrimSpace(e.Cfg.SSHIdentityFile) == "" {
		return nil, meta, fmt.Errorf("未配置 terminal.ssh_identity_file，无法 SSH")
	}
	inst, err := e.Store.GetInstance(hostID)
	if err != nil || inst == nil {
		return nil, meta, fmt.Errorf("绑定主机实例不存在: %s", hostID)
	}
	host := strings.TrimSpace(inst.Host)
	if host == "" {
		host = strings.TrimSpace(inst.DomainName)
	}
	if host == "" {
		return nil, meta, fmt.Errorf("主机缺少 Host/Domain")
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
	meta = agentHostMeta{Host: host, SSHUser: sshUser, SSHPort: sshPort}
	client, err := sshx.Dial(host, sshPort, sshx.Config{
		User:            sshUser,
		IdentityFile:    e.Cfg.SSHIdentityFile,
		Timeout:         30 * time.Second,
		InsecureHostKey: e.Cfg.SSHInsecureHostKey,
	})
	if err != nil {
		return nil, meta, fmt.Errorf("SSH 连接失败: %w", err)
	}
	return client, meta, nil
}

func (e *Engine) resolveRemoteTarget(a *store.BackupAgent, hostID string) (remoteDir string, listenPort int) {
	remoteDir = strings.TrimSpace(e.Cfg.AgentRemoteDir)
	if remoteDir == "" {
		remoteDir = "/home/sysadmin/auto-dba/backup-agent"
	}
	listenPort = e.Cfg.AgentListenPort
	if listenPort <= 0 {
		listenPort = 8090
	}
	if dep, _ := e.Store.LatestSuccessDeployForAgent(a.ID, hostID); dep != nil {
		if d := strings.TrimSpace(dep.RemoteDir); d != "" {
			remoteDir = d
		}
		if dep.ListenPort > 0 {
			listenPort = dep.ListenPort
		}
	}
	if p := portFromBaseURL(a.BaseURL); p > 0 {
		listenPort = p
	}
	return remoteDir, listenPort
}

func portFromBaseURL(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		if h, p, err := net.SplitHostPort(strings.TrimPrefix(raw, "http://")); err == nil && h != "" {
			n, _ := strconv.Atoi(p)
			return n
		}
		return 0
	}
	if u.Port() != "" {
		n, _ := strconv.Atoi(u.Port())
		return n
	}
	return 0
}
