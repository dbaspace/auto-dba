package dbdeploy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"auto-dba/internal/backup"
	"auto-dba/internal/ha"
	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

// ValidateDeployHAOptions 创建时/部署前 HA 门禁（可不带节点做弱校验）。
func ValidateDeployHAOptions(cl *store.DbDeployCluster) error {
	if cl == nil || !cl.HaEnabled {
		return nil
	}
	if cl.DeployKind == store.DbDeployKindCloud || cl.Topology == store.DbDeployTopoCloudEndpoint {
		return fmt.Errorf("云托管禁止启用平台 HA 接入")
	}
	eng := strings.ToLower(strings.TrimSpace(cl.Engine))
	switch eng {
	case store.DbDeployEngineMySQL, store.DbDeployEnginePostgres:
		// ok
	default:
		return fmt.Errorf("部署内 HA 接入仅支持 MySQL / PostgreSQL")
	}
	mode := strings.ToLower(strings.TrimSpace(cl.HaMode))
	if mode == "" {
		return fmt.Errorf("启用 HA 时须指定 ha_mode")
	}
	if !store.ValidHAMode(eng, mode) {
		return fmt.Errorf("HA 模式 %s 与引擎 %s 不匹配", mode, eng)
	}
	if cl.Topology != store.DbDeployTopoMasterReplica {
		return fmt.Errorf("部署内 HA 接入要求拓扑为 master_replica")
	}
	if store.HAAutoSwitchoverMode(mode) && strings.TrimSpace(cl.ControllerID) == "" {
		return fmt.Errorf("模式 %s 必须绑定兼容的 HA 控制器", mode)
	}
	return nil
}

// ValidateDeployHA 完整校验：选项 + 控制器兼容 + 节点 1 主 ≥1 从。
func ValidateDeployHA(st *store.DB, cl *store.DbDeployCluster, nodes []store.DbDeployNode) error {
	if err := ValidateDeployHAOptions(cl); err != nil {
		return err
	}
	if cl == nil || !cl.HaEnabled {
		return nil
	}
	eng := strings.ToLower(strings.TrimSpace(cl.Engine))
	mode := strings.ToLower(strings.TrimSpace(cl.HaMode))
	primaries, replicas := countHARoles(nodes)
	if primaries != 1 || replicas < 1 {
		return fmt.Errorf("HA 接入须恰好 1 primary + ≥1 replica，当前 primary=%d replica=%d", primaries, replicas)
	}
	cid := strings.TrimSpace(cl.ControllerID)
	if cid == "" {
		return nil
	}
	if st == nil {
		return fmt.Errorf("store 未初始化")
	}
	ctrl, err := st.GetHAController(cid)
	if err != nil {
		return err
	}
	if ctrl == nil {
		return fmt.Errorf("控制器不存在: %s", cid)
	}
	if !strings.EqualFold(ctrl.Engine, eng) {
		return fmt.Errorf("控制器引擎 %s 与集群 %s 不一致", ctrl.Engine, eng)
	}
	if !store.CompatibleHAController(eng, mode, ctrl.Kind) {
		return fmt.Errorf("控制器类型 %s 与模式 %s 不匹配", ctrl.Kind, mode)
	}
	return nil
}

func countHARoles(nodes []store.DbDeployNode) (primaries, replicas int) {
	for _, n := range nodes {
		switch strings.ToLower(n.Role) {
		case "primary", "single":
			primaries++
		case "replica":
			replicas++
		}
	}
	return
}

// DeployHAMember 从部署节点登记后的 HA 成员草案。
type DeployHAMember struct {
	InstanceID string
	Role       string
	Host       string
	Port       int
	Name       string
}

// EnsureMembersFromDeploy 将部署节点登记为实例并返回 HA 成员。
func EnsureMembersFromDeploy(st *store.DB, cl *store.DbDeployCluster, nodes []store.DbDeployNode) ([]DeployHAMember, error) {
	if st == nil || cl == nil {
		return nil, fmt.Errorf("参数无效")
	}
	eng := strings.ToLower(strings.TrimSpace(cl.Engine))
	if eng == "" {
		eng = store.DbDeployEngineMySQL
	}
	defaultPort := 3306
	if eng == store.DbDeployEnginePostgres {
		defaultPort = 5432
	}
	var out []DeployHAMember
	primaries, replicas := 0, 0
	for _, n := range nodes {
		role := strings.ToLower(n.Role)
		haRole := ""
		switch role {
		case "primary", "single":
			haRole = "primary"
			primaries++
		case "replica":
			haRole = "replica"
			replicas++
		default:
			continue
		}
		port := n.Port
		if port <= 0 {
			port = defaultPort
		}
		name := fmt.Sprintf("%s-%s", cl.Name, n.Host)
		inst, err := st.EnsureInstanceFromHost(eng, name, n.Host, port, cl.Environment)
		if err != nil {
			return nil, fmt.Errorf("登记实例失败 %s:%d: %w", n.Host, port, err)
		}
		out = append(out, DeployHAMember{
			InstanceID: inst.ID,
			Role:       haRole,
			Host:       inst.Host,
			Port:       inst.Port,
			Name:       inst.Name,
		})
	}
	if primaries != 1 || replicas < 1 {
		return out, fmt.Errorf("导入后须满足 1 主 ≥1 从，当前 primary=%d replica=%d", primaries, replicas)
	}
	return out, nil
}

// BindHAClusterResult 绑定结果。
type BindHAClusterResult struct {
	HAClusterID string
	Created     bool
	Skipped     bool
}

// BindHACluster 创建（或复用）HA 集群并回写部署集群链接。
func BindHACluster(st *store.DB, cl *store.DbDeployCluster, members []DeployHAMember, operator string) (*BindHAClusterResult, error) {
	if st == nil || cl == nil {
		return nil, fmt.Errorf("参数无效")
	}
	if id := strings.TrimSpace(cl.HaClusterID); id != "" {
		exist, err := st.GetHACluster(id)
		if err != nil {
			return nil, err
		}
		if exist != nil {
			return &BindHAClusterResult{HAClusterID: exist.ID, Skipped: true}, nil
		}
	}
	mode := strings.ToLower(strings.TrimSpace(cl.HaMode))
	if mode == "" {
		mode = "manual"
	}
	eng := strings.ToLower(strings.TrimSpace(cl.Engine))
	if eng == "" {
		eng = store.DbDeployEngineMySQL
	}
	ctrlID := strings.TrimSpace(cl.ControllerID)
	if store.HAAutoSwitchoverMode(mode) && ctrlID == "" {
		return nil, fmt.Errorf("模式 %s 必须绑定控制器", mode)
	}
	if ctrlID != "" {
		ctrl, err := st.GetHAController(ctrlID)
		if err != nil {
			return nil, err
		}
		if ctrl == nil {
			return nil, fmt.Errorf("控制器不存在")
		}
		if !store.CompatibleHAController(eng, mode, ctrl.Kind) {
			return nil, fmt.Errorf("控制器类型 %s 与模式 %s 不匹配", ctrl.Kind, mode)
		}
	}
	var haMembers []store.HAMember
	primaries := 0
	for _, m := range members {
		if m.Role == "primary" {
			primaries++
		}
		haMembers = append(haMembers, store.HAMember{
			InstanceID: m.InstanceID,
			Role:       m.Role,
			Priority:   0,
			Enabled:    true,
		})
	}
	if strings.EqualFold(mode, "mha") || strings.EqualFold(mode, "pg_autofailover") || strings.EqualFold(mode, "patroni") {
		if len(haMembers) < 2 || primaries != 1 {
			return nil, fmt.Errorf("%s 须恰好 1 主 + ≥1 从", mode)
		}
	} else if primaries > 1 {
		return nil, fmt.Errorf("同一集群只能有一个 primary")
	}
	cluster := &store.HACluster{
		Name:                 cl.Name,
		Engine:               eng,
		EnvID:                cl.Environment,
		Mode:                 mode,
		ControllerID:         ctrlID,
		StandbyControllerIDs: store.NormalizeStandbyControllerIDs(ctrlID, nil),
		VIP:                  strings.TrimSpace(cl.HaVIP),
		Remark:               BuildHAClusterRemark(cl),
		MaintStatus:          "normal",
		CreatedBy:            operator,
		UpdatedBy:            operator,
	}
	if err := st.CreateHACluster(cluster); err != nil {
		return nil, err
	}
	if err := st.ReplaceHAMembers(cluster.ID, haMembers); err != nil {
		_ = st.DeleteHACluster(cluster.ID)
		return nil, err
	}
	if err := st.SetDbDeployClusterHALink(cl.ID, cluster.ID, ctrlID); err != nil {
		return nil, err
	}
	cl.HaClusterID = cluster.ID
	if ctrlID != "" {
		cl.ControllerID = ctrlID
	}
	return &BindHAClusterResult{HAClusterID: cluster.ID, Created: true}, nil
}

// ParseHAOptions 解析部署集群上的 MHA 选项 JSON（含部署别名字段映射）。
func ParseHAOptions(cl *store.DbDeployCluster) ha.MHAConfOptions {
	opts := ha.DefaultMHAConfOptions("")
	if cl != nil {
		opts = ha.DefaultMHAConfOptions(cl.Name)
		if s := strings.TrimSpace(cl.HaOptionsJSON); s != "" {
			var env deployHAOptionsJSON
			if err := json.Unmarshal([]byte(s), &env); err == nil {
				opts = env.MHAConfOptions
				if strings.TrimSpace(opts.MasterIPFailoverScript) == "" {
					opts.MasterIPFailoverScript = strings.TrimSpace(env.MHAFailoverScript)
				}
				if strings.TrimSpace(opts.MasterIPOnlineChangeScript) == "" {
					opts.MasterIPOnlineChangeScript = strings.TrimSpace(env.MHAOnlineChangeScript)
				}
			} else {
				_ = json.Unmarshal([]byte(s), &opts)
			}
			// 用集群名补默认路径等
			def := ha.DefaultMHAConfOptions(cl.Name)
			if strings.TrimSpace(opts.ConfPath) == "" {
				opts.ConfPath = def.ConfPath
			}
			if strings.TrimSpace(opts.User) == "" {
				opts.User = def.User
			}
			if strings.TrimSpace(opts.SSHUser) == "" {
				opts.SSHUser = def.SSHUser
			}
			if opts.SSHPort <= 0 {
				opts.SSHPort = def.SSHPort
			}
			if strings.TrimSpace(opts.ManagerWorkdir) == "" {
				opts.ManagerWorkdir = def.ManagerWorkdir
			}
			if strings.TrimSpace(opts.ManagerLog) == "" {
				opts.ManagerLog = def.ManagerLog
			}
			if strings.TrimSpace(opts.RemoteWorkdir) == "" {
				opts.RemoteWorkdir = def.RemoteWorkdir
			}
			if strings.TrimSpace(opts.MasterBinlogDir) == "" {
				opts.MasterBinlogDir = def.MasterBinlogDir
			}
			if opts.PingInterval <= 0 {
				opts.PingInterval = def.PingInterval
			}
			if opts.CheckReplDelay == nil {
				opts.CheckReplDelay = def.CheckReplDelay
			}
			if opts.CheckReplDelta <= 0 {
				opts.CheckReplDelta = def.CheckReplDelta
			}
		}
	}
	return opts
}

// deployHAOptionsJSON 部署侧 ha_options_json：MHA 配置 + VIP 钩子（钩子不进入 app.cnf）。
type deployHAOptionsJSON struct {
	ha.MHAConfOptions
	MHAFailoverScript     string            `json:"mha_failover_script,omitempty"`
	MHAOnlineChangeScript string            `json:"mha_online_change_script,omitempty"`
	VIPHookScript         string            `json:"vip_hook_script,omitempty"`
	VIPHookArgs           json.RawMessage   `json:"vip_hook_args,omitempty"`
	VIPHookEnv            map[string]string `json:"vip_hook_env,omitempty"`
	HookAgentURL          string            `json:"hook_agent_url,omitempty"`
	HookAgentUser         string            `json:"hook_agent_user,omitempty"`
	HookAgentPass         string            `json:"hook_agent_pass,omitempty"`
}

func parseVIPHookArgs(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		out := make([]string, 0, len(arr))
		for _, s := range arr {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		fields := strings.FieldsFunc(s, func(r rune) bool {
			return r == ' ' || r == ',' || r == '\t'
		})
		out := make([]string, 0, len(fields))
		for _, f := range fields {
			f = strings.TrimSpace(f)
			if f != "" {
				out = append(out, f)
			}
		}
		return out
	}
	return nil
}

// ParseDeployHAHookOptions 从部署 ha_options_json 提取 VIP 钩子（与 HA 维护页 remark 约定一致）。
func ParseDeployHAHookOptions(cl *store.DbDeployCluster) ha.HAHookOptions {
	opt := ha.HAHookOptions{}
	if cl == nil {
		return opt
	}
	s := strings.TrimSpace(cl.HaOptionsJSON)
	if s == "" {
		return opt
	}
	var env deployHAOptionsJSON
	if err := json.Unmarshal([]byte(s), &env); err != nil {
		return opt
	}
	opt.Script = strings.TrimSpace(env.VIPHookScript)
	opt.Args = parseVIPHookArgs(env.VIPHookArgs)
	opt.Env = env.VIPHookEnv
	opt.AgentURL = strings.TrimSpace(env.HookAgentURL)
	opt.Username = strings.TrimSpace(env.HookAgentUser)
	opt.Password = env.HookAgentPass
	return opt
}

// BuildHAClusterRemark 创建 HA 集群时的 Remark：有钩子则写 JSON，否则保留纯文本说明。
func BuildHAClusterRemark(cl *store.DbDeployCluster) string {
	const note = "由集群部署自动接入"
	hook := ParseDeployHAHookOptions(cl)
	if strings.TrimSpace(hook.Script) == "" && len(hook.Args) == 0 &&
		strings.TrimSpace(hook.AgentURL) == "" && len(hook.Env) == 0 {
		return note
	}
	m := map[string]any{"note": note}
	if s := strings.TrimSpace(hook.Script); s != "" {
		m["vip_hook_script"] = s
	}
	if len(hook.Args) > 0 {
		m["vip_hook_args"] = hook.Args
	}
	if len(hook.Env) > 0 {
		m["vip_hook_env"] = hook.Env
	}
	if u := strings.TrimSpace(hook.AgentURL); u != "" {
		m["hook_agent_url"] = u
	}
	if u := strings.TrimSpace(hook.Username); u != "" {
		m["hook_agent_user"] = u
	}
	if p := strings.TrimSpace(hook.Password); p != "" {
		m["hook_agent_pass"] = p
	}
	b, err := json.Marshal(m)
	if err != nil {
		return note
	}
	return string(b)
}

// CollectHAVIPWarnings MHA VIP/脚本软警告（不阻断部署）；其它模式仅登记、不强制。
func CollectHAVIPWarnings(cl *store.DbDeployCluster) []string {
	if cl == nil || !cl.HaEnabled {
		return nil
	}
	mode := strings.ToLower(strings.TrimSpace(cl.HaMode))
	if mode != "mha" {
		return nil
	}
	vip := strings.TrimSpace(cl.HaVIP)
	opts := ParseHAOptions(cl)
	hook := ParseDeployHAHookOptions(cl)
	failover := strings.TrimSpace(opts.MasterIPFailoverScript)
	hookScript := strings.TrimSpace(hook.Script)

	var warns []string
	if vip == "" && failover == "" {
		warns = append(warns, "MHA 未配置业务 VIP 与 master_ip_failover_script：切换后需人工改流量")
	} else if vip != "" && failover == "" && hookScript == "" {
		warns = append(warns, "已登记 VIP 但未配置 failover/vip_hook 脚本：切换后需人工改流量")
	} else if vip == "" && (failover != "" || hookScript != "") {
		warns = append(warns, "已配置漂移/钩子脚本但未登记业务 VIP：建议填写 ha_vip 以便钩子注入 HA_VIP")
	}
	return warns
}

// FormatHAVIPAuditLine 审计日志：VIP 与脚本路径回显。
func FormatHAVIPAuditLine(cl *store.DbDeployCluster) string {
	if cl == nil {
		return ""
	}
	opts := ParseHAOptions(cl)
	hook := ParseDeployHAHookOptions(cl)
	return fmt.Sprintf("vip=%s failover_script=%s online_change_script=%s vip_hook=%s",
		strings.TrimSpace(cl.HaVIP),
		strings.TrimSpace(opts.MasterIPFailoverScript),
		strings.TrimSpace(opts.MasterIPOnlineChangeScript),
		strings.TrimSpace(hook.Script),
	)
}

// EnvCheckHAController 对可环境检查的控制器做探活。
func EnvCheckHAController(ctx context.Context, st *store.DB, secretKey, controllerID string) (map[string]any, error) {
	ctrl, err := st.GetHAController(strings.TrimSpace(controllerID))
	if err != nil || ctrl == nil {
		return nil, fmt.Errorf("控制器不存在")
	}
	kind := strings.ToLower(strings.TrimSpace(ctrl.Kind))
	pass := ""
	if strings.TrimSpace(ctrl.PasswordEnc) != "" {
		p, e := secret.Decrypt(secretKey, ctrl.PasswordEnc)
		if e != nil {
			return nil, fmt.Errorf("解密控制器密码失败: %w", e)
		}
		pass = p
	}
	auth := ha.ControllerAuth{
		Kind: ctrl.Kind, BaseURL: ctrl.BaseURL, Username: ctrl.Username, Password: pass,
		AppConf: strings.TrimSpace(ctrl.Remark),
	}
	switch kind {
	case "mha":
		auth.AppConf = ha.ParseMHAAppConf(ctrl.Remark)
		agentAuth := backup.AgentAuth{BaseURL: ctrl.BaseURL, Username: ctrl.Username, Password: pass}
		return backup.CallHAEnvCheck(ctx, agentAuth, kind, auth.AppConf)
	case "pg_autofailover":
		agentAuth := backup.AgentAuth{BaseURL: ctrl.BaseURL, Username: ctrl.Username, Password: pass}
		return backup.CallHAEnvCheck(ctx, agentAuth, kind, auth.AppConf)
	case "patroni":
		ok, summary, checks := ha.PatroniEnvCheck(ctx, auth)
		return map[string]any{"ok": ok, "summary": summary, "checks": checks}, nil
	case "repmgr":
		ok, summary, checks := ha.RepmgrEnvCheck(ctx, auth)
		return map[string]any{"ok": ok, "summary": summary, "checks": checks}, nil
	case "orchestrator":
		ok, summary, checks := ha.OrchestratorEnvCheck(ctx, auth, "", 0)
		return map[string]any{"ok": ok, "summary": summary, "checks": checks}, nil
	case "proxysql":
		ok, summary, checks := ha.ProxySQLEnvCheck(ctx, auth)
		return map[string]any{"ok": ok, "summary": summary, "checks": checks}, nil
	default:
		return nil, fmt.Errorf("环境检查仅支持 kind=mha / orchestrator / proxysql / pg_autofailover / patroni / repmgr")
	}
}

// EnvCheckMHA 兼容旧名。
func EnvCheckMHA(ctx context.Context, st *store.DB, secretKey, controllerID string) (map[string]any, error) {
	return EnvCheckHAController(ctx, st, secretKey, controllerID)
}

func controllerAuth(st *store.DB, secretKey, controllerID string) (ha.ControllerAuth, *store.HAController, error) {
	ctrl, err := st.GetHAController(strings.TrimSpace(controllerID))
	if err != nil || ctrl == nil {
		return ha.ControllerAuth{}, nil, fmt.Errorf("控制器不存在")
	}
	pass := ""
	if strings.TrimSpace(ctrl.PasswordEnc) != "" {
		p, e := secret.Decrypt(secretKey, ctrl.PasswordEnc)
		if e != nil {
			return ha.ControllerAuth{}, nil, fmt.Errorf("解密控制器密码失败: %w", e)
		}
		pass = p
	}
	appConf := strings.TrimSpace(ctrl.Remark)
	if strings.EqualFold(ctrl.Kind, "mha") {
		appConf = ha.ParseMHAAppConf(ctrl.Remark)
	}
	return ha.ControllerAuth{
		Kind: ctrl.Kind, BaseURL: ctrl.BaseURL, Username: ctrl.Username, Password: pass,
		AppConf: appConf,
	}, ctrl, nil
}

// BindMHAConf 生成并下发 MHA app.cnf。
func BindMHAConf(ctx context.Context, st *store.DB, secretKey string, cl *store.DbDeployCluster, operator string) (string, error) {
	if cl == nil || strings.TrimSpace(cl.HaClusterID) == "" {
		return "", fmt.Errorf("尚未创建 HA 集群")
	}
	cluster, err := st.GetHACluster(cl.HaClusterID)
	if err != nil || cluster == nil {
		return "", fmt.Errorf("HA 集群不存在")
	}
	opts := ParseHAOptions(cl)
	content, err := ha.RenderMHAAppConf(cluster, opts)
	if err != nil {
		return "", err
	}
	path := opts.ConfPath
	if path == "" {
		path = ha.DefaultMHAConfOptions(cluster.Name).ConfPath
	}
	optJSON, _ := json.Marshal(opts)
	it := &store.HAMHAConf{
		ClusterID:   cluster.ID,
		ConfPath:    path,
		Content:     content,
		OptionsJSON: string(optJSON),
		UpdatedBy:   operator,
	}
	if err := st.UpsertHAMHAConf(it); err != nil {
		return "", err
	}
	auth, ctrl, err := controllerAuth(st, secretKey, cluster.ControllerID)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(ctrl.Kind, "mha") {
		return "", fmt.Errorf("控制器非 MHA")
	}
	auth.AppConf = path
	if err := ha.MHAAgentPutConf(ctx, auth, path, content); err != nil {
		return "", fmt.Errorf("下发 app.cnf 失败: %w", err)
	}
	ctrl.Remark = path
	ctrl.UpdatedBy = operator
	_ = st.UpdateHAController(ctrl)
	return path, nil
}

// VerifyMHARepl 调用 Agent check_repl。
func VerifyMHARepl(ctx context.Context, st *store.DB, secretKey string, cl *store.DbDeployCluster) (string, error) {
	if cl == nil || strings.TrimSpace(cl.HaClusterID) == "" {
		return "", fmt.Errorf("尚未创建 HA 集群")
	}
	cluster, err := st.GetHACluster(cl.HaClusterID)
	if err != nil || cluster == nil {
		return "", fmt.Errorf("HA 集群不存在")
	}
	auth, _, err := controllerAuth(st, secretKey, cluster.ControllerID)
	if err != nil {
		return "", err
	}
	ok, detail := ha.MHAAgentReplOK(ctx, auth)
	if !ok {
		return detail, fmt.Errorf("MHA check_repl 未通过: %s", detail)
	}
	return detail, nil
}

// ProbeHATopology 探测 HA 拓扑（部署接入验收）。
func ProbeHATopology(ctx context.Context, st *store.DB, secretKey string, cl *store.DbDeployCluster) (string, error) {
	if cl == nil || strings.TrimSpace(cl.HaClusterID) == "" {
		return "", fmt.Errorf("尚未创建 HA 集群")
	}
	cluster, err := st.GetHACluster(cl.HaClusterID)
	if err != nil || cluster == nil {
		return "", fmt.Errorf("HA 集群不存在")
	}
	topo, err := ha.ProbeTopology(ctx, cluster, func(inst *store.Instance) (string, error) {
		if inst == nil || strings.TrimSpace(inst.Password) == "" {
			return "", nil
		}
		return secret.Decrypt(secretKey, inst.Password)
	})
	if err != nil {
		return "", err
	}
	extra := map[string]any{}
	mode := strings.ToLower(strings.TrimSpace(cluster.Mode))
	if strings.TrimSpace(cluster.ControllerID) != "" {
		auth, ctrl, aerr := controllerAuth(st, secretKey, cluster.ControllerID)
		if aerr == nil && ctrl != nil {
			switch mode {
			case "patroni":
				if clust, err := ha.PatroniGetCluster(ctx, auth); err == nil {
					extra["patroni_members"] = len(clust.Members)
				} else {
					topo.Warnings = append(topo.Warnings, "Patroni: "+err.Error())
				}
			case "pg_autofailover":
				if stMap, err := ha.PGAFAgentShowState(ctx, auth); err == nil {
					extra["pgaf_state"] = "ok"
					_ = stMap
				} else {
					topo.Warnings = append(topo.Warnings, "pg_autofailover: "+err.Error())
				}
			case "repmgr":
				if ok, summary, _ := ha.RepmgrEnvCheck(ctx, auth); !ok {
					topo.Warnings = append(topo.Warnings, "repmgr: "+summary)
				} else {
					extra["repmgr"] = "ok"
				}
			}
		}
	}
	b, _ := json.Marshal(map[string]any{
		"members":   len(topo.Members),
		"warnings":  topo.Warnings,
		"probed_at": topo.ProbedAt,
		"extra":     extra,
	})
	if len(topo.Members) == 0 {
		return string(b), fmt.Errorf("拓扑探测无成员，请确认实例连通")
	}
	return string(b), nil
}

// appendHASteps 按模式追加 HA 步骤。
func appendHASteps(add func(key, title, nodeID string), mode string) {
	add("ha_precheck", "HA 规范预检", "")
	add("ha_register", "登记实例成员", "")
	add("ha_create_cluster", "创建 HA 集群并绑定", "")
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "mha":
		add("ha_mha_push", "生成并下发 MHA conf", "")
		add("ha_mha_verify", "MHA 复制检查", "")
	case "proxysql_repl":
		add("ha_proxysql_env", "ProxySQL 环境检查", "")
		add("ha_orch_probe", "拓扑探测", "")
	case "orchestrator":
		add("ha_orch_env", "Orchestrator 环境检查", "")
		add("ha_orch_probe", "Orchestrator/拓扑探测", "")
	case "pg_autofailover":
		add("ha_pgaf_env", "pg_auto_failover 环境检查", "")
		add("ha_pgaf_probe", "拓扑探测", "")
	case "patroni":
		add("ha_patroni_env", "Patroni 环境检查", "")
		add("ha_patroni_probe", "Patroni/拓扑探测", "")
	case "repmgr":
		add("ha_repmgr_env", "repmgr 环境检查", "")
		add("ha_repmgr_probe", "拓扑探测", "")
	}
}

func buildHaInitSteps(cl *store.DbDeployCluster) []store.DbDeployChangeStep {
	var steps []store.DbDeployChangeStep
	seq := 0
	add := func(key, title, nodeID string) {
		seq++
		steps = append(steps, store.DbDeployChangeStep{Seq: seq, StepKey: key, Title: title, NodeID: nodeID})
	}
	appendHASteps(add, cl.HaMode)
	add("finalize", "完成", "")
	return steps
}

// haStepFailedAfterInstall 判断失败步骤是否属于 HA（装机已完成）。
func haStepFailedAfterInstall(stepKey string) bool {
	return strings.HasPrefix(stepKey, "ha_")
}

func haInitTimeout() time.Duration { return 90 * time.Second }
