package srcluster

import (
	"context"
	"fmt"
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

// AgentRuntimeStatus Node Agent 运行态。
type AgentRuntimeStatus struct {
	ID      string `json:"id"`
	Status  string `json:"status"` // running|stopped|unknown
	Message string `json:"message,omitempty"`
	BaseURL string `json:"base_url,omitempty"`
}

// ProbeAgentStatus 通过 healthz 判断是否在跑。
func (e *Engine) ProbeAgentStatus(ctx context.Context, a *store.SrAgent) AgentRuntimeStatus {
	out := AgentRuntimeStatus{ID: a.ID, Status: AgentStatusUnknown, BaseURL: a.BaseURL}
	if a == nil || strings.TrimSpace(a.BaseURL) == "" {
		out.Message = "无 Base URL"
		return out
	}
	pass := ""
	if a.PasswordEnc != "" && e != nil && e.Secret != "" {
		pass, _ = secret.Decrypt(e.Secret, a.PasswordEnc)
	}
	if e.Agent == nil {
		e.Agent = NewAgentClient()
	}
	if err := e.Agent.Probe(ctx, AgentAuth{BaseURL: a.BaseURL, Username: a.Username, Password: pass}); err != nil {
		out.Status = AgentStatusStopped
		out.Message = err.Error()
		return out
	}
	out.Status = AgentStatusRunning
	out.Message = "healthz ok"
	return out
}

func (e *Engine) resolveNodeRemoteTarget(a *store.SrAgent) (remoteDir string, listenPort int) {
	remoteDir = e.Cfg.AgentRemoteDir
	if remoteDir == "" {
		remoteDir = "/home/sysadmin/auto-dba/node-agent"
	}
	listenPort = e.Cfg.AgentListenPort
	if listenPort <= 0 {
		listenPort = 8091
	}
	if a != nil && strings.TrimSpace(a.HostInstID) != "" {
		if dep, _ := e.Store.ListSrAgentDeploys(a.HostInstID, 20); len(dep) > 0 {
			for _, d := range dep {
				if d.Status == store.SrDeploySuccess {
					if d.RemoteDir != "" {
						remoteDir = d.RemoteDir
					}
					if d.ListenPort > 0 {
						listenPort = d.ListenPort
					}
					break
				}
			}
		}
	}
	if a != nil && strings.TrimSpace(a.BaseURL) != "" {
		if u, err := url.Parse(a.BaseURL); err == nil && u.Port() != "" {
			if p, err := strconv.Atoi(u.Port()); err == nil && p > 0 {
				listenPort = p
			}
		}
	}
	return remoteDir, listenPort
}

func (e *Engine) dialNodeHost(hostInstanceID string) (*sshx.Client, *store.Instance, error) {
	if strings.TrimSpace(e.Cfg.SSHIdentityFile) == "" {
		return nil, nil, fmt.Errorf("未配置 terminal.ssh_identity_file")
	}
	inst, err := e.Store.GetInstance(hostInstanceID)
	if err != nil || inst == nil {
		return nil, nil, fmt.Errorf("主机实例不存在")
	}
	host := strings.TrimSpace(inst.Host)
	if host == "" {
		host = strings.TrimSpace(inst.DomainName)
	}
	if host == "" {
		return nil, nil, fmt.Errorf("主机缺少 Host/Domain")
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
	client, err := sshx.Dial(host, sshPort, sshx.Config{
		User:            sshUser,
		IdentityFile:    e.Cfg.SSHIdentityFile,
		Timeout:         30 * time.Second,
		InsecureHostKey: e.Cfg.SSHInsecureHostKey,
	})
	if err != nil {
		return nil, nil, err
	}
	return client, inst, nil
}

func (e *Engine) agentEscalate(a *store.SrAgent) string {
	if a == nil {
		return ""
	}
	return sshx.NormalizeEscalate(store.ParseSrAgentEscalate(a.Remark))
}

func (e *Engine) persistAgentEscalate(a *store.SrAgent, escalate string) {
	if e == nil || e.Store == nil || a == nil {
		return
	}
	esc := sshx.NormalizeEscalate(escalate)
	if esc == "" || store.ParseSrAgentEscalate(a.Remark) == esc {
		return
	}
	a.Remark = store.FormatSrAgentRemark(esc, a.Remark)
	_ = e.Store.UpdateSrAgent(a)
}

// StopRemoteAgent 经 SSH 停止已下发的 Node Agent。
func (e *Engine) StopRemoteAgent(a *store.SrAgent) error {
	if e == nil || e.Store == nil || a == nil {
		return fmt.Errorf("engine/agent 未初始化")
	}
	hostID := strings.TrimSpace(a.HostInstID)
	if hostID == "" {
		return fmt.Errorf("该 Agent 非 SSH 下发，无法远程停止（请在目标机手动操作）")
	}
	client, _, err := e.dialNodeHost(hostID)
	if err != nil {
		return err
	}
	defer client.Close()

	remoteDir, listenPort := e.resolveNodeRemoteTarget(a)
	pidFile := path.Join(remoteDir, "agent.pid")
	script := killNodeAgentScript(pidFile, listenPort)
	escalate := e.agentEscalate(a)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	tryStop := func(esc string) (string, error) {
		out, errOut, runErr := client.RunEscalated(ctx, esc, script)
		combined := strings.TrimSpace(out + "\n" + errOut)
		probeCtx, cancelProbe := context.WithTimeout(context.Background(), 5*time.Second)
		st := e.ProbeAgentStatus(probeCtx, a)
		cancelProbe()
		if st.Status != AgentStatusRunning {
			e.persistAgentEscalate(a, esc)
			return combined, nil
		}
		msg := fmt.Sprintf("远端 Node Agent 仍在运行（端口 %d，目录 %s）", listenPort, remoteDir)
		if runErr != nil {
			msg += "; " + runErr.Error()
		}
		if combined != "" {
			msg += "; " + combined
		}
		return combined, fmt.Errorf("%s", msg)
	}

	_, err = tryStop(escalate)
	if err == nil {
		return nil
	}
	// 历史下发用了 sudo_su 但未写入 remark：无提权 kill 无效，再试一次提权。
	if escalate != sshx.EscalateSudoSu {
		if _, err2 := tryStop(sshx.EscalateSudoSu); err2 == nil {
			return nil
		} else {
			return err2
		}
	}
	return err
}

// StartRemoteAgent 经 SSH 执行远端 start.sh。
func (e *Engine) StartRemoteAgent(a *store.SrAgent) error {
	if e == nil || e.Store == nil || a == nil {
		return fmt.Errorf("engine/agent 未初始化")
	}
	hostID := strings.TrimSpace(a.HostInstID)
	if hostID == "" {
		return fmt.Errorf("该 Agent 非 SSH 下发，无法远程启动（请手工登记后在目标机启动，或先 SSH 下发）")
	}
	client, _, err := e.dialNodeHost(hostID)
	if err != nil {
		return err
	}
	defer client.Close()

	remoteDir, _ := e.resolveNodeRemoteTarget(a)
	startRemote := path.Join(remoteDir, "start.sh")
	pidFile := path.Join(remoteDir, "agent.pid")
	escalate := e.agentEscalate(a)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

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

	tryStart := func(esc string) error {
		out, errOut, err := client.RunEscalated(ctx, esc, script)
		if err != nil {
			return fmt.Errorf("启动失败: %v; out=%s err=%s（若缺少 start.sh 请先「SSH 下发」）", err, strings.TrimSpace(out), strings.TrimSpace(errOut))
		}
		var last error
		for i := 0; i < 10; i++ {
			pctx, c := context.WithTimeout(context.Background(), 3*time.Second)
			st = e.ProbeAgentStatus(pctx, a)
			c()
			if st.Status == AgentStatusRunning {
				e.persistAgentEscalate(a, esc)
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

	if err := tryStart(escalate); err == nil {
		return nil
	} else if escalate != sshx.EscalateSudoSu {
		// 目录属 root、无提权无法执行 start.sh：回退 sudo_su
		if err2 := tryStart(sshx.EscalateSudoSu); err2 == nil {
			return nil
		} else {
			return err2
		}
	} else {
		return err
	}
}
