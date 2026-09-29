package srcluster

import (
	"context"
	"fmt"
	"strings"
	"time"

	"auto-dba/internal/store"
)

// UpgradeCheckItem 升级前门禁单项。
type UpgradeCheckItem struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Level   string `json:"level,omitempty"` // warn：仅提醒，不阻断
	Message string `json:"message,omitempty"`
}

// UpgradePrecheckResult 升级前环境检查结果。
type UpgradePrecheckResult struct {
	OK      bool               `json:"ok"`
	Items   []UpgradeCheckItem `json:"items"`
	Summary string             `json:"summary,omitempty"`
}

func (r *UpgradePrecheckResult) add(name string, ok bool, msg string) {
	r.Items = append(r.Items, UpgradeCheckItem{Name: name, OK: ok, Message: msg})
	if !ok {
		r.OK = false
	}
}

func (r *UpgradePrecheckResult) warn(name, msg string) {
	r.Items = append(r.Items, UpgradeCheckItem{Name: name, OK: true, Level: "warn", Message: msg})
}

// addJDKResults 升级预检的逐节点 JDK 项：满足 ok；升级时可安装 warn；无法满足 fail。
func (r *UpgradePrecheckResult) addJDKResults(results []jdkNodeResult) {
	for _, jr := range results {
		name := "jdk_" + jr.Node.ID
		switch {
		case jr.Err != nil:
			r.add(name, false, fmt.Sprintf("%s JDK 检查失败: %v", nodeLabel(&jr.Node), jr.Err))
		case jr.Decision.Status == jdkInstall:
			r.warn(name, jr.Decision.Message)
		default:
			r.add(name, jr.Decision.Status == jdkOK, jr.Decision.Message)
		}
	}
}

// PrecheckUpgrade 升级前门禁：OSS、FE/BE 制品、节点 Agent、无进行中变更等。
// 版本以目标 release_id 为准，不再校验环境版本白名单。
func (e *Engine) PrecheckUpgrade(ctx context.Context, clusterID, targetReleaseID string) *UpgradePrecheckResult {
	res := &UpgradePrecheckResult{OK: true}
	targetReleaseID = strings.TrimSpace(targetReleaseID)
	if targetReleaseID == "" {
		res.add("target_release", false, "目标版本 release_id 为空")
		res.Summary = "升级前检查未通过"
		return res
	}
	if e == nil || e.Store == nil {
		res.add("engine", false, "engine 未初始化")
		res.Summary = "升级前检查未通过"
		return res
	}

	cl, err := e.Store.GetSrCluster(clusterID)
	if err != nil || cl == nil {
		res.add("cluster", false, "集群不存在")
		res.Summary = "升级前检查未通过"
		return res
	}
	res.add("cluster", true, fmt.Sprintf("%s（status=%s）", cl.Name, cl.Status))

	env := strings.TrimSpace(cl.Environment)
	if env == "" {
		res.add("environment", true, "未填写（可选标签，不影响版本选择）")
	} else {
		res.add("environment", true, env)
	}

	if cl.Status == store.SrClusterIniting {
		res.add("cluster_status", false, "集群正在初始化中，请完成或取消后再升级")
	} else if cl.Status != store.SrClusterReady && cl.Status != store.SrClusterFailed {
		res.add("cluster_status", false, fmt.Sprintf("集群状态为 %s，仅 ready/failed 允许升级", cl.Status))
	} else {
		res.add("cluster_status", true, cl.Status)
	}

	if err := e.rejectIfActiveChange(clusterID); err != nil {
		res.add("active_change", false, err.Error())
	} else {
		res.add("active_change", true, "无进行中的变更单")
	}

	rel, err := e.Store.GetSrRelease(targetReleaseID)
	if err != nil || rel == nil {
		res.add("release", false, fmt.Sprintf("目标版本不存在（release_id=%s）", targetReleaseID))
	} else {
		if !rel.Enabled {
			res.add("release", false, fmt.Sprintf("目标版本 %s 已禁用", rel.Version))
		} else {
			res.add("release", true, fmt.Sprintf("%s（id=%s）", rel.Version, rel.ID))
		}
		if strings.TrimSpace(cl.ReleaseID) == rel.ID {
			ok, msg := e.sameReleaseUpgradeCheck(ctx, cl, rel)
			res.add("version_change", ok, msg)
		} else {
			res.add("version_change", true, fmt.Sprintf("%s → %s", strings.TrimSpace(cl.Version), rel.Version))
		}
	}

	// OSS 配置与连通性
	if _, err := e.loadOSSPlain(); err != nil {
		res.add("oss_config", false, "OSS 未配置或无效: "+err.Error())
	} else {
		res.add("oss_config", true, "已加载配置中心 OSS")
		cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		ok, msg := e.TestOSS(cctx)
		cancel()
		res.add("oss_reachability", ok, msg)
	}

	// FE/BE 制品完整性 + 可签名
	probeCl := *cl
	probeCl.ReleaseID = targetReleaseID
	if rel != nil {
		probeCl.Version = rel.Version
	}
	for _, role := range []string{"fe", "be"} {
		name := "artifact_" + role
		if !e.releaseHasInstallableRole(targetReleaseID, role) {
			art, _ := e.Store.GetSrReleaseArtifact(targetReleaseID, role)
			ver := probeCl.Version
			if rel != nil {
				ver = rel.Version
			}
			res.add(name, false, artifactGapError(ver, targetReleaseID, role, art).Error())
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		urlStr, _, _, filename, err := e.ResolveInstallSpec(cctx, &probeCl, role)
		cancel()
		if err != nil {
			res.add(name, false, err.Error())
		} else {
			res.add(name, true, fmt.Sprintf("%s 可签名下载（%s）", role, filenameOrKey(filename, urlStr)))
		}
	}

	nodes, err := e.Store.ListSrNodes(clusterID)
	if err != nil {
		res.add("nodes", false, err.Error())
	} else if len(nodes) == 0 {
		res.add("nodes", false, "无 FE/BE 节点")
	} else {
		feN, beN := 0, 0
		missingAgent := 0
		for _, n := range nodes {
			if n.Component == "fe" {
				feN++
			}
			if n.Component == "be" {
				beN++
			}
			if strings.TrimSpace(n.AgentID) == "" {
				missingAgent++
				res.add("node_agent_"+n.ID, false, fmt.Sprintf("%s %s 未绑定 agent_id", n.Component, n.Host))
			}
		}
		res.add("nodes", missingAgent == 0, fmt.Sprintf("FE=%d BE=%d", feN, beN))
		if feN == 0 {
			res.add("topology_fe", false, "缺少 FE 节点")
		} else {
			res.add("topology_fe", true, fmt.Sprintf("%d 个 FE", feN))
		}
		if beN == 0 {
			res.add("topology_be", false, "缺少 BE 节点")
		} else {
			res.add("topology_be", true, fmt.Sprintf("%d 个 BE", beN))
		}
	}

	// Agent 探活
	if err == nil && len(nodes) > 0 {
		seen := map[string]bool{}
		allOK := true
		for _, n := range nodes {
			aid := strings.TrimSpace(n.AgentID)
			if aid == "" || seen[aid] {
				continue
			}
			seen[aid] = true
			auth, aerr := e.agentAuth(aid)
			if aerr != nil {
				res.add("agent_"+aid, false, aerr.Error())
				allOK = false
				continue
			}
			cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
			perr := e.Agent.Probe(cctx, auth)
			cancel()
			if perr != nil {
				res.add("agent_"+aid, false, fmt.Sprintf("%s healthz: %v", auth.BaseURL, perr))
				allOK = false
			} else {
				res.add("agent_"+aid, true, auth.BaseURL+" ok")
			}
		}
		if allOK && len(seen) > 0 {
			res.add("agents_health", true, fmt.Sprintf("%d 个 Agent 探活通过", len(seen)))
		} else if len(seen) == 0 {
			res.add("agents_health", false, "无可用 Agent")
		}
		if allOK && len(seen) > 0 && rel != nil {
			withAgent := make([]store.SrNode, 0, len(nodes))
			for _, n := range nodes {
				if strings.TrimSpace(n.AgentID) != "" {
					withAgent = append(withAgent, n)
				}
			}
			res.addJDKResults(e.evalClusterJDK(ctx, cl, withAgent, rel))
		}
	}

	if !res.OK {
		var fails []string
		for _, it := range res.Items {
			if !it.OK {
				fails = append(fails, it.Name+": "+it.Message)
			}
		}
		res.Summary = "升级前检查未通过（" + strings.Join(fails, "；") + "）"
	} else {
		res.Summary = "升级前检查通过"
		warns := 0
		for _, it := range res.Items {
			if it.Level == "warn" {
				warns++
			}
		}
		if warns > 0 {
			res.Summary += fmt.Sprintf("（%d 项提醒，升级时将自动处理 JDK）", warns)
		}
	}
	return res
}

// sameReleaseUpgradeCheck 目标版本与集群绑定版本相同时，按节点实际版本判断是否需要补升级。
func (e *Engine) sameReleaseUpgradeCheck(ctx context.Context, cl *store.SrCluster, rel *store.SrRelease) (bool, string) {
	nodes, err := e.Store.ListSrNodes(cl.ID)
	if err != nil || len(nodes) == 0 {
		return false, "目标版本与当前集群绑定版本相同，且无法列出节点，无需升级"
	}
	actual, err := e.nodeActualVersions(ctx, cl, nodes)
	if err != nil {
		return false, fmt.Sprintf("目标版本与当前集群绑定版本相同；无法读取节点实际版本：%v", err)
	}
	bad, desc := mismatchedNodes(nodes, actual, rel.Version)
	if len(bad) == 0 {
		return false, fmt.Sprintf("目标版本与当前集群绑定版本相同，且所有节点实际版本均为 %s，无需升级", rel.Version)
	}
	return true, fmt.Sprintf("集群已绑定 %s，但以下节点实际版本不符：%s；本次只升级这些节点", rel.Version, strings.Join(desc, "；"))
}

func filenameOrKey(filename, urlStr string) string {
	if strings.TrimSpace(filename) != "" {
		return filename
	}
	if len(urlStr) > 48 {
		return urlStr[:48] + "…"
	}
	return urlStr
}
