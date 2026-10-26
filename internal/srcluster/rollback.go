package srcluster

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"auto-dba/internal/store"
)

// upgradeRollbackDirs 升级只替换了 bin/lib，回滚也只还原这两个目录。
var upgradeRollbackDirs = []string{"bin", "lib"}

// nodeStillUpgraded 升级步骤对应节点当前是否处于新版 bin/lib（未被回滚）。
func nodeStillUpgraded(s *store.SrChangeStep) bool {
	if binReplacedBefore(s.LogText) {
		return true
	}
	return s.Status == store.SrStepSuccess && !strings.Contains(s.LogText, stepMarkRolledBack)
}

// StartUpgradeRollback 将失败/取消的升级变更中某节点的 bin/lib 还原为升级前备份，并重新启动。
func (e *Engine) StartUpgradeRollback(clusterID, upgradeChangeID, nodeID, operator string) (string, error) {
	up, err := e.Store.GetSrChange(upgradeChangeID)
	if err != nil || up == nil || up.ClusterID != clusterID {
		return "", fmt.Errorf("升级变更不存在")
	}
	if up.Type != store.SrChangeUpgrade {
		return "", fmt.Errorf("仅升级变更支持回滚节点")
	}
	if up.Status != store.SrChangeFailed && up.Status != store.SrChangeCancelled {
		return "", fmt.Errorf("仅失败或已取消的升级变更可回滚节点（当前 %s）", up.Status)
	}
	var upStep *store.SrChangeStep
	for i := range up.Steps {
		if up.Steps[i].StepKey == "upgrade_node_"+nodeID {
			upStep = &up.Steps[i]
			break
		}
	}
	if upStep == nil {
		return "", fmt.Errorf("该升级变更中没有此节点的升级步骤")
	}
	if !nodeStillUpgraded(upStep) {
		return "", fmt.Errorf("该节点未替换 bin/lib 或已回滚，无需回滚")
	}
	suffix := ""
	fromVersion := ""
	var p map[string]string
	if json.Unmarshal([]byte(up.PayloadJSON), &p) == nil {
		suffix = strings.TrimSpace(p["backup_suffix"])
		// 补升级只涉及部分节点，其余节点已是目标版本：回滚后不改集群绑定版本，之后可再按实际版本补升级。
		if p["partial"] != "true" {
			fromVersion = strings.TrimSpace(p["from_release_id"])
		}
	}
	if suffix == "" {
		return "", fmt.Errorf("升级变更缺少 backup_suffix，无法定位备份")
	}
	n, err := e.Store.GetSrNode(nodeID)
	if err != nil || n == nil || n.ClusterID != clusterID {
		return "", fmt.Errorf("节点不存在")
	}
	if strings.TrimSpace(n.AgentID) == "" {
		return "", fmt.Errorf("节点未绑定 agent_id")
	}
	if strings.TrimSpace(n.SystemdUnit) == "" {
		return "", fmt.Errorf("节点未配置 systemd_unit")
	}
	if list, err := e.Store.ListSrChanges(clusterID, 20); err == nil {
		for _, other := range list {
			if other.Status == store.SrChangeRunning || other.Status == store.SrChangePending {
				return "", fmt.Errorf("集群有进行中的变更（%s），请等待结束后再回滚", other.Type)
			}
		}
	}
	payload, _ := json.Marshal(map[string]string{
		"upgrade_change_id": up.ID, "upgrade_step_id": upStep.ID, "backup_suffix": suffix,
		"node_id": n.ID, "from_release_id": fromVersion,
	})
	ch := &store.SrChange{ClusterID: clusterID, Type: store.SrChangeUpgradeRollback, Status: store.SrChangePending, PayloadJSON: string(payload), CreatedBy: operator}
	if err := e.Store.CreateSrChange(ch); err != nil {
		return "", err
	}
	verify, verifyTitle := "verify_backends", "SHOW PROC '/backends'"
	if n.Component == "fe" {
		verify, verifyTitle = "verify_frontends", "SHOW PROC '/frontends'"
	}
	steps := []store.SrChangeStep{
		{StepKey: "rollback_node_" + n.ID, Title: fmt.Sprintf("回滚 %s %s 的 bin/lib（bak.%s）", strings.ToUpper(n.Component), n.Host, suffix), NodeID: n.ID},
		{StepKey: verify, Title: verifyTitle},
		{StepKey: "finalize", Title: "回滚完成"},
	}
	for i := range steps {
		steps[i].ChangeID = ch.ID
		steps[i].Seq = i + 1
		_ = e.Store.CreateSrChangeStep(&steps[i])
	}
	go e.runChange(ch.ID)
	return ch.ID, nil
}

func (e *Engine) stepRollbackNode(ctx context.Context, cl *store.SrCluster, n *store.SrNode, step *store.SrChangeStep) error {
	ch, err := e.Store.GetSrChange(step.ChangeID)
	if err != nil || ch == nil {
		return fmt.Errorf("回滚变更不存在")
	}
	var p map[string]string
	_ = json.Unmarshal([]byte(ch.PayloadJSON), &p)
	suffix := strings.TrimSpace(p["backup_suffix"])
	if suffix == "" {
		return fmt.Errorf("缺少 backup_suffix")
	}
	auth, err := e.agentAuth(n.AgentID)
	if err != nil {
		return err
	}
	soft := nodeSoftwarePath(e.Store, cl, n)
	unit := n.SystemdUnit
	e.stepLog(step, "停服 %s", unit)
	if _, err := e.Agent.SystemdCtrl(ctx, auth, SystemdCtrlReq{Action: "stop", Unit: unit, ChangeID: step.ChangeID, StepID: step.ID, DryRun: e.Cfg.DryRun}); err != nil {
		return fmt.Errorf("停服失败: %w", err)
	}
	e.stepLog(step, "还原 %s/bak.%s/{%s} → %s", soft, suffix, strings.Join(upgradeRollbackDirs, ","), soft)
	resp, err := e.Agent.ArtifactRestore(ctx, auth, ArtifactRestoreReq{TargetDir: soft, BackupSuffix: suffix, Dirs: upgradeRollbackDirs, DryRun: e.Cfg.DryRun})
	if err != nil {
		return fmt.Errorf("还原失败: %w", err)
	}
	if resp != nil && strings.TrimSpace(resp.Message) != "" {
		e.stepLog(step, "%s", strings.TrimSpace(resp.Message))
	}
	e.stepLog(step, "启动 %s", unit)
	if _, err := e.startUnitWithDiag(ctx, auth, cl, n, unit, "start", step); err != nil {
		return fmt.Errorf("回滚后启动失败: %w", err)
	}
	n.Status = store.SrNodeReady
	_ = e.Store.UpdateSrNode(n)
	e.stepLog(step, "已启动 %s（旧版本）", unit)
	e.markUpgradeStepRolledBack(p["upgrade_change_id"], p["upgrade_step_id"], step.ChangeID, cl, p["from_release_id"])
	return nil
}

// markUpgradeStepRolledBack 在原升级步骤追加 [rolled_back]，并置回 pending：之后重试升级会重新替换该节点。
// 所有节点都已回滚时，集群版本恢复为升级前版本。
func (e *Engine) markUpgradeStepRolledBack(upgradeChangeID, upgradeStepID, rollbackChangeID string, cl *store.SrCluster, fromReleaseID string) {
	up, err := e.Store.GetSrChange(upgradeChangeID)
	if err != nil || up == nil {
		return
	}
	var rolledSeq int
	for i := range up.Steps {
		s := &up.Steps[i]
		if s.ID != upgradeStepID {
			continue
		}
		rolledSeq = s.Seq
		s.LogText = appendStepLine(s.LogText, time.Now(), stepMarkRolledBack+" 已回滚到升级前 bin/lib（回滚变更 "+shortID(rollbackChangeID)+"）")
		s.Status = store.SrStepPending
		_ = e.Store.UpdateSrChangeStep(s)
	}
	anyUpgraded := false
	for i := range up.Steps {
		s := &up.Steps[i]
		if s.ID == upgradeStepID {
			continue
		}
		if strings.HasPrefix(s.StepKey, "upgrade_node_") && nodeStillUpgraded(s) {
			anyUpgraded = true
		}
		if rolledSeq > 0 && s.Seq > rolledSeq && s.Status == store.SrStepSuccess &&
			(strings.HasPrefix(s.StepKey, "verify_") || s.StepKey == "finalize") {
			s.Status = store.SrStepPending
			_ = e.Store.UpdateSrChangeStep(s)
		}
	}
	if anyUpgraded || strings.TrimSpace(fromReleaseID) == "" || cl == nil {
		return
	}
	if rel, err := e.Store.GetSrRelease(fromReleaseID); err == nil && rel != nil {
		cl.ReleaseID = rel.ID
		cl.Version = rel.Version
		_ = e.Store.UpdateSrCluster(cl)
	}
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
