package ha

import (
	"context"
	"fmt"
	"strings"
	"time"

	"auto-dba/internal/store"
)

type MemberLive struct {
	InstanceID   string   `json:"instance_id"`
	InstanceName string   `json:"instance_name"`
	Host         string   `json:"host"`
	Port         int      `json:"port"`
	Role         string   `json:"role"`
	Registered   string   `json:"registered_role"`
	Healthy      bool     `json:"healthy"`
	LagSeconds   *float64 `json:"lag_seconds,omitempty"`
	Detail       string   `json:"detail,omitempty"`
	Error        string   `json:"error,omitempty"`
}

type Topology struct {
	ClusterID   string       `json:"cluster_id"`
	Name        string       `json:"name"`
	Engine      string       `json:"engine"`
	Mode        string       `json:"mode"`
	MaintStatus string       `json:"maint_status"`
	VIP         string       `json:"vip,omitempty"`
	ProbedAt    string       `json:"probed_at"`
	Members     []MemberLive `json:"members"`
	Warnings    []string     `json:"warnings,omitempty"`
}

type PasswordFn func(inst *store.Instance) (string, error)

type PrecheckResult struct {
	OK      bool           `json:"ok"`
	Checks  []PrecheckItem `json:"checks"`
	Summary string         `json:"summary"`
}

type PrecheckItem struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

func ProbeTopology(ctx context.Context, cluster *store.HACluster, passwordFn PasswordFn) (*Topology, error) {
	if cluster == nil {
		return nil, fmt.Errorf("集群不存在")
	}
	out := &Topology{
		ClusterID:   cluster.ID,
		Name:        cluster.Name,
		Engine:      cluster.Engine,
		Mode:        cluster.Mode,
		MaintStatus: cluster.MaintStatus,
		VIP:         cluster.VIP,
		ProbedAt:    time.Now().UTC().Format(time.RFC3339),
		Members:     []MemberLive{},
	}
	if len(cluster.Members) == 0 {
		out.Warnings = append(out.Warnings, "集群尚未登记成员实例")
		return out, nil
	}
	switch strings.ToLower(cluster.Engine) {
	case "mysql":
		return probeMySQL(ctx, cluster, passwordFn, out)
	case "postgres":
		return probePostgres(ctx, cluster, passwordFn, out)
	case "redis":
		return probeRedis(ctx, cluster, passwordFn, out)
	default:
		return nil, fmt.Errorf("不支持的引擎: %s", cluster.Engine)
	}
}

func Precheck(ctx context.Context, cluster *store.HACluster, action, targetInstanceID string, passwordFn PasswordFn) *PrecheckResult {
	res := &PrecheckResult{OK: true, Checks: []PrecheckItem{}}
	add := func(name string, ok bool, msg string) {
		res.Checks = append(res.Checks, PrecheckItem{Name: name, OK: ok, Message: msg})
		if !ok {
			res.OK = false
		}
	}
	if cluster == nil {
		add("集群", false, "集群不存在")
		res.Summary = "预检失败"
		return res
	}
	if !store.ValidHAAction(action) {
		add("操作类型", false, "不支持的操作: "+action)
		res.Summary = "预检失败"
		return res
	}
	add("操作类型", true, store.HAActionLabel(action))

	if len(cluster.Members) == 0 {
		add("成员", false, "无成员实例")
	} else {
		add("成员", true, fmt.Sprintf("%d 个成员", len(cluster.Members)))
	}

	switch action {
	case "enter_maint":
		if cluster.MaintStatus == "maintaining" {
			add("维护状态", false, "已处于维护中")
		} else {
			add("维护状态", true, "当前正常，可进入维护")
		}
	case "exit_maint":
		if cluster.MaintStatus != "maintaining" {
			add("维护状态", false, "当前不在维护中")
		} else {
			add("维护状态", true, "可退出维护")
		}
	case "switchover":
		if cluster.MaintStatus == "maintaining" {
			add("维护状态", true, "维护中，适合计划切换")
		} else {
			add("维护状态", true, "建议先进入维护窗口（非强制）")
		}
		if strings.TrimSpace(targetInstanceID) == "" {
			add("目标节点", false, "计划切换需指定新主实例")
		} else {
			found := false
			for _, m := range cluster.Members {
				if m.InstanceID == targetInstanceID && m.Enabled {
					found = true
					if m.Role == "primary" {
						add("目标节点", false, "目标已是当前登记主节点")
					} else {
						add("目标节点", true, "目标实例已登记: "+m.InstanceName)
					}
					break
				}
			}
			if !found {
				add("目标节点", false, "目标实例不在集群成员中")
			}
		}
	case "failover_dead":
		mode := strings.ToLower(strings.TrimSpace(cluster.Mode))
		switch mode {
		case "mha":
			add("模式", true, "MHA dead-master")
		case "orchestrator":
			add("模式", true, "Orchestrator force-master-failover")
		case "pg_autofailover":
			add("模式", true, "pg_auto_failover 强制切换")
		case "patroni":
			add("模式", true, "Patroni failover")
		case "mgr", "pxc":
			add("模式", false, "MGR/PXC 不在本平台触发强制切主（组内共识/Galera 自愈），请走现网手册")
		default:
			add("模式", false, "强制切换仅支持 MHA / Orchestrator / pg_autofailover / Patroni")
		}
		if strings.TrimSpace(targetInstanceID) != "" {
			found := false
			for _, m := range cluster.Members {
				if m.InstanceID == targetInstanceID && m.Enabled {
					found = true
					if m.Role == "primary" {
						add("目标节点", false, "目标不能是当前登记主节点")
					} else {
						add("目标节点", true, "指定新主: "+m.InstanceName)
					}
					break
				}
			}
			if !found {
				add("目标节点", false, "目标实例不在集群成员中")
			}
		} else {
			add("目标节点", true, "未指定新主，由控制器自动选择")
		}
	}

	topo, err := ProbeTopology(ctx, cluster, passwordFn)
	if err != nil {
		add("拓扑探测", false, err.Error())
	} else {
		unhealthy := 0
		primaryUnreachable := false
		var maxLag *float64
		for _, m := range topo.Members {
			if !m.Healthy {
				unhealthy++
				if m.Role == "primary" || m.Registered == "primary" {
					primaryUnreachable = true
				}
			}
			if m.LagSeconds != nil {
				if maxLag == nil || *m.LagSeconds > *maxLag {
					v := *m.LagSeconds
					maxLag = &v
				}
			}
		}
		if action == "failover_dead" {
			if primaryUnreachable {
				add("主节点可达性", true, "当前主探测异常（符合强制切换前提）")
			} else {
				add("主节点可达性", false, "当前主仍可达，请使用计划内切换；强制切换可能造成双主")
			}
			if unhealthy > 1 {
				add("成员健康", true, fmt.Sprintf("%d 个成员探测异常（含主）", unhealthy))
			} else if unhealthy == 1 && primaryUnreachable {
				add("成员健康", true, "仅主节点异常")
			} else {
				add("成员健康", true, "探测完成")
			}
		} else if unhealthy > 0 {
			add("成员健康", false, fmt.Sprintf("%d 个成员探测异常", unhealthy))
		} else {
			add("成员健康", true, "探测正常")
		}
		if action == "switchover" && maxLag != nil && *maxLag > 30 {
			add("复制延迟", false, fmt.Sprintf("最大延迟 %.1fs，超过 30s 阈值", *maxLag))
		} else if action == "failover_dead" {
			add("复制延迟", true, "强制切换不校验延迟（可能丢事务）")
		} else if maxLag != nil {
			add("复制延迟", true, fmt.Sprintf("最大延迟 %.1fs", *maxLag))
		} else {
			add("复制延迟", true, "未获取到延迟（可继续，请人工确认）")
		}
		enrichMGRPXCPrecheck(cluster, action, targetInstanceID, topo, add)
	}

	if res.OK {
		res.Summary = "预检通过"
	} else {
		res.Summary = "预检未通过，请处理后再提交"
	}
	return res
}

func enrichMGRPXCPrecheck(cluster *store.HACluster, action, targetID string, topo *Topology, add func(string, bool, string)) {
	if cluster == nil || topo == nil {
		return
	}
	mode := strings.ToLower(strings.TrimSpace(cluster.Mode))
	if mode != "mgr" && mode != "pxc" {
		return
	}
	if action != "switchover" && action != "enter_maint" && action != "exit_maint" {
		return
	}
	add("平台边界", true, "MGR/PXC 不在平台内触发组选举或 wsrep 危险命令；仅加固观测与角色登记门禁")
	nonOnline := 0
	primaries := 0
	for _, m := range topo.Members {
		d := strings.ToLower(m.Detail)
		if mode == "mgr" {
			if strings.Contains(m.Detail, "MGR") && !strings.Contains(m.Detail, "ONLINE") {
				nonOnline++
			}
			if m.Role == "primary" {
				primaries++
			}
			if targetID != "" && m.InstanceID == targetID {
				if !m.Healthy || !strings.Contains(m.Detail, "ONLINE") {
					add("目标 MGR 状态", false, "目标须为 ONLINE 组成员")
				} else {
					add("目标节点健康", true, m.InstanceName)
				}
			}
		}
		if mode == "pxc" {
			if strings.Contains(d, "wsrep_ready=") && !strings.Contains(d, "wsrep_ready=on") {
				nonOnline++
			}
			if strings.Contains(d, "donor") {
				add("Donor", false, m.InstanceName+" 处于 Donor/Desync，禁止作为切换目标")
			}
			if m.Role == "primary" {
				primaries++
			}
			if targetID != "" && m.InstanceID == targetID {
				if !m.Healthy || strings.Contains(d, "donor") || (strings.Contains(d, "wsrep_ready=") && !strings.Contains(d, "wsrep_ready=on")) {
					add("目标 PXC 状态", false, "目标须 wsrep_ready=ON 且非 Donor")
				} else {
					add("目标节点健康", true, m.InstanceName)
				}
			}
		}
	}
	if mode == "mgr" {
		if nonOnline > 0 {
			add("MGR ONLINE", false, fmt.Sprintf("%d 个成员非 ONLINE，禁止登记角色变更", nonOnline))
		} else {
			add("MGR ONLINE", true, "探测成员均为 ONLINE 或未读到异常")
		}
		if action == "switchover" && primaries != 1 {
			add("MGR PRIMARY", false, fmt.Sprintf("当前探测 PRIMARY 数=%d（期望 1），请先用现网工具稳定组状态", primaries))
		} else if primaries == 1 {
			add("MGR PRIMARY", true, "存在唯一 PRIMARY")
		}
	}
	if mode == "pxc" {
		if nonOnline > 0 {
			add("PXC ready", false, fmt.Sprintf("%d 个成员 wsrep_ready 异常", nonOnline))
		} else {
			add("PXC ready", true, "wsrep_ready 检查完成")
		}
	}
}

func ManualSwitchoverChecklist(engine string) []string {
	return SwitchoverChecklist(engine, store.DefaultHAMode(engine))
}

func SwitchoverChecklist(engine, mode string) []string {
	e := strings.ToLower(strings.TrimSpace(engine))
	m := strings.ToLower(strings.TrimSpace(mode))
	switch {
	case e == "mysql" && m == "orchestrator":
		return []string{
			"确认从库复制线程正常、延迟可接受",
			"应用切只读或停写",
			"通过 Orchestrator graceful-master-takeover 提升目标（已登记控制器时自动执行）",
			"确认 VIP 或 ProxySQL 已指向新主",
			"原主降为从库并重建复制",
		}
	case e == "mysql" && m == "mha":
		return []string{
			"确认 MHA Manager Agent 可达，且 app.cnf 与平台成员 Host/Port 一致",
			"确认 masterha_check_repl 通过、复制延迟可接受",
			"应用切只读或停写（计划切换窗口）",
			"经 Agent 调用 masterha_master_switch（主存活在线切换；平台不接管 masterha_manager 自动 failover）",
			"确认 VIP / 脚本已指向新主；原主按 MHA 流程降为从库",
		}
	case e == "mysql" && m == "proxysql_repl":
		return []string{
			"确认复制延迟可接受",
			"应用停写或 ProxySQL 只读",
			"确认目标节点已可写或已按复制拓扑提升（平台将更新 ProxySQL writer HG）",
			"经 ProxySQL Admin 切换 writer hostgroup（已登记控制器时自动执行）",
			"原主改 read_only 并接回复制",
		}
	case e == "mysql" && m == "mgr":
		return []string{
			"确认 replication_group_members 均为 ONLINE",
			"目标节点 MEMBER_ROLE 允许成为 PRIMARY",
			"勿对组外节点执行 promote",
			"按现网流程触发组内主选举（不在本平台对组发危险命令）",
			"核对应用连接串仍指向当前 PRIMARY",
		}
	case e == "mysql" && m == "pxc":
		return []string{
			"确认 wsrep_ready=ON、cluster_status=Primary",
			"目标非 Donor / 非 desync",
			"按 Galera 手册摘写并切换流量",
			"勿对未加入组的节点 promote",
		}
	case e == "mysql" && m == "shared_storage":
		return []string{
			"按现网 Pacemaker 手册执行计划切换",
			"平台仅更新登记角色，不直接操作集群",
			"切换后核对 VIP / Service 端点",
		}
	case e == "mysql" && m == "k8s_operator":
		return []string{
			"确认 Percona/Operator CR 状态正常",
			"经平台「容器管理 → 数据库服务」发起切换（按 Operator 语义）",
			"切换后核对 Service 端点与实例登记",
		}
	case e == "postgres" && m == "pg_autofailover":
		return []string{
			"确认 pg_autoctl show state 各节点 Current/Assigned 稳定",
			"应用切只读或停写",
			"经平台 Agent 执行 pg_autoctl perform switchover（或指定节点 promotion）",
			"核对新 primary 可写、secondary 跟随",
			"更新连接串 / VIP / PgBouncer",
		}
	case e == "postgres" && m == "patroni":
		return []string{
			"确认 pg_stat_replication 延迟可接受",
			"应用切只读或停写",
			"调用 Patroni REST /switchover（已登记控制器时自动执行）",
			"更新连接串 / VIP",
			"原主以 replica 重新加入",
		}
	case e == "postgres" && m == "repmgr":
		return []string{
			"确认 repmgr cluster show 拓扑正常",
			"应用切只读或停写",
			"经平台 Agent 执行 repmgr standby switchover（已登记控制器时自动执行）",
			"更新连接串 / VIP",
		}
	case e == "postgres" && m == "k8s_operator":
		return []string{
			"确认 CloudNativePG Cluster 状态 Ready",
			"经平台「容器管理 → 数据库服务」调用 Operator Switchover（或 patch targetPrimary）",
			"切换后核对 Service / 连接串与实例登记",
		}
	case e == "redis" && m == "sentinel":
		return []string{
			"确认 INFO replication 与 Sentinel 监控正常",
			"调用 SENTINEL FAILOVER（已登记控制器时自动执行）",
			"验证新主可写、从库跟随",
			"更新客户端/代理指向",
		}
	case e == "redis" && m == "cluster":
		return []string{
			"Redis Cluster 以槽位为单位，不按单主从切换",
			"按 CLUSTER FAILOVER / 运维手册处理故障节点",
			"平台不自动执行 CLUSTER FAILOVER",
		}
	case e == "mysql":
		return []string{
			"确认目标从库复制线程正常且延迟可接受",
			"应用切只读或停写",
			"人工提升目标为新主",
			"切换 VIP / 连接串指向新主",
			"原主降为从库并重建复制",
		}
	case e == "postgres":
		return []string{
			"确认 pg_stat_replication 延迟可接受",
			"应用切只读或停写",
			"按现网流程 promote",
			"更新连接串 / VIP",
			"原主以新角色重新加入",
		}
	case e == "redis":
		return []string{
			"确认 INFO replication 正常",
			"按现网流程切换主从",
			"验证新主可写",
		}
	default:
		return []string{"按现网 HA 手册执行计划内切换并回写结果"}
	}
}
