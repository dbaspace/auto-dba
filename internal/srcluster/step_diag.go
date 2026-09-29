package srcluster

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"auto-dba/internal/store"
)

const (
	// stepMarkReplaced 升级步骤已替换 bin/lib；据此判断节点能否「回滚该节点」。
	stepMarkReplaced = "[replaced]"
	// stepMarkRolledBack 节点已人工回滚到旧版 bin/lib；之后重试升级须重新替换。
	stepMarkRolledBack = "[rolled_back]"
	// legacyStartFailed 旧版本写入的「替换后启动失败」文案，兼容已有失败记录。
	legacyStartFailed = "升级后启服失败"

	maxDiagLen    = 10 << 10
	maxDiagHead   = 6 << 10
	maxStepLogLen = 64 << 10
)

// appendStepLine 追加一行带时间的日志（纯函数，便于测试）。
func appendStepLine(logText string, at time.Time, line string) string {
	line = at.Format("15:04:05") + " " + line
	if strings.TrimSpace(logText) == "" {
		return line
	}
	out := logText + "\n" + line
	if len(out) > maxStepLogLen {
		out = "…（前文已截断）\n" + out[len(out)-maxStepLogLen:]
	}
	return out
}

// stepLog 追加阶段日志并立即落库，前端轮询变更时能看到进度。
func (e *Engine) stepLog(step *store.SrChangeStep, format string, args ...any) {
	step.LogText = appendStepLine(step.LogText, time.Now(), fmt.Sprintf(format, args...))
	if e != nil && e.Store != nil {
		_ = e.Store.UpdateSrChangeStep(step)
	}
}

// binReplacedBefore 该节点此前已替换过 bin/lib 且之后没有回滚（可回滚）。
func binReplacedBefore(prevLog string) bool {
	replaced := strings.LastIndex(prevLog, stepMarkReplaced)
	if i := strings.LastIndex(prevLog, legacyStartFailed); i > replaced {
		replaced = i
	}
	if replaced < 0 {
		return false
	}
	return replaced > strings.LastIndex(prevLog, stepMarkRolledBack)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

// truncateDiag 保留头部（systemctl status / journalctl / systemctl cat）与尾部（日志末尾），按行截断。
func truncateDiag(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxDiagLen {
		return s
	}
	head := s[:maxDiagHead]
	if i := strings.LastIndexByte(head, '\n'); i > 0 {
		head = head[:i]
	}
	tail := s[len(s)-(maxDiagLen-maxDiagHead):]
	if i := strings.IndexByte(tail, '\n'); i >= 0 && i < len(tail)-1 {
		tail = tail[i+1:]
	}
	return head + "\n…（中间已截断）…\n" + tail
}

// collectStartDiag 启动失败后采集 systemctl status / journalctl / 日志末尾；旧 Agent 退回 systemctl status。
func (e *Engine) collectStartDiag(ctx context.Context, auth AgentAuth, cl *store.SrCluster, n *store.SrNode, unit string) string {
	base := ctx
	if base.Err() != nil {
		base = context.Background()
	}
	dctx, cancel := context.WithTimeout(base, 30*time.Second)
	defer cancel()
	resp, code, err := e.Agent.Diag(dctx, auth, DiagReq{Unit: unit, Role: n.Component, SoftwarePath: nodeSoftwarePath(cl, n), Lines: 60})
	if err == nil && resp != nil && strings.TrimSpace(resp.Message) != "" {
		return truncateDiag("诊断信息：\n" + resp.Message)
	}
	if code == http.StatusNotFound {
		st, err2 := e.Agent.SystemdCtrl(dctx, auth, SystemdCtrlReq{Action: "status", Unit: unit})
		if err2 == nil && st != nil && strings.TrimSpace(st.Message) != "" {
			return truncateDiag(fmt.Sprintf("诊断信息（systemctl status；重新下发 Agent 可获取 journalctl 与 %s.out）：\n%s", n.Component, st.Message))
		}
		if err2 != nil {
			err = err2
		}
	}
	if err != nil {
		return "诊断信息采集失败: " + err.Error()
	}
	return ""
}

// unitMissingDaemon 解析 systemctl cat 输出：Type=forking 却以不带 --daemon 的 start_fe.sh / start_be.sh 启动时返回 true。
// 这种组合下脚本会前台 exec java，systemd 等不到 fork 退出，启动一直卡在 activating 直到超时被杀。
func unitMissingDaemon(unitText string) (execStart string, missing bool) {
	typ := "simple"
	for _, raw := range strings.Split(unitText, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "Type":
			typ = strings.ToLower(strings.TrimSpace(v))
		case "ExecStart":
			execStart = strings.TrimSpace(v)
		}
	}
	if typ != "forking" || execStart == "" {
		return execStart, false
	}
	fields := strings.Fields(strings.TrimLeft(execStart, "-@:+!"))
	if len(fields) == 0 {
		return execStart, false
	}
	switch filepath.Base(fields[0]) {
	case "start_fe.sh", "start_be.sh":
	default:
		return execStart, false
	}
	for _, a := range fields[1:] {
		if a == "--daemon" {
			return execStart, false
		}
	}
	return execStart, true
}

// checkUnitDaemon 停服前检查 unit；读取失败（旧 Agent 不支持 cat 等）只记日志不拦截。
func (e *Engine) checkUnitDaemon(ctx context.Context, auth AgentAuth, unit string, step *store.SrChangeStep) error {
	resp, err := e.Agent.SystemdCtrl(ctx, auth, SystemdCtrlReq{Action: "cat", Unit: unit, ChangeID: step.ChangeID, StepID: step.ID, DryRun: e.Cfg.DryRun})
	if err != nil {
		e.stepLog(step, "跳过 unit 检查（读取 %s 失败，重新下发 Agent 可启用）: %s", unit, firstLine(err.Error()))
		return nil
	}
	if resp == nil {
		return nil
	}
	if execStart, missing := unitMissingDaemon(resp.Message); missing {
		return fmt.Errorf("%s 为 Type=forking 但 ExecStart=%s 未带 --daemon，启动会卡在 activating 直到超时；请改为 ExecStart=%s --daemon 并执行 systemctl daemon-reload 后重试（未停服、未替换）", unit, execStart, execStart)
	}
	return nil
}

func (e *Engine) clusterOf(n *store.SrNode) *store.SrCluster {
	if cl, err := e.Store.GetSrCluster(n.ClusterID); err == nil && cl != nil {
		return cl
	}
	return &store.SrCluster{ID: n.ClusterID}
}

// startUnitWithDiag reset-failed（忽略错误，旧 Agent 不支持）→ start；失败时附诊断信息。
func (e *Engine) startUnitWithDiag(ctx context.Context, auth AgentAuth, cl *store.SrCluster, n *store.SrNode, unit, action string, step *store.SrChangeStep) (*JobResp, error) {
	if action == "" {
		action = "start"
	}
	_, _ = e.Agent.SystemdCtrl(ctx, auth, SystemdCtrlReq{Action: "reset-failed", Unit: unit, ChangeID: step.ChangeID, StepID: step.ID, DryRun: e.Cfg.DryRun})
	resp, err := e.Agent.SystemdCtrl(ctx, auth, SystemdCtrlReq{Action: action, Unit: unit, ChangeID: step.ChangeID, StepID: step.ID, DryRun: e.Cfg.DryRun})
	if err == nil {
		return resp, nil
	}
	if diag := e.collectStartDiag(ctx, auth, cl, n, unit); diag != "" {
		return resp, fmt.Errorf("%w\n%s", err, diag)
	}
	return resp, err
}
