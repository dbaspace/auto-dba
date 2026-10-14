package srcluster

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

// srClusterTagPrefix 资源配置实例上标记所属 SR 集群的标签前缀（sr-cluster=<集群ID>）。
const srClusterTagPrefix = "sr-cluster="

// InstanceLink 集群关联的 StarRocks 资源实例摘要（不含密码）。
type InstanceLink struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	PasswordSet bool   `json:"password_set"`
	MatchedBy   string `json:"matched_by"` // tag | host
}

func clusterTag(clusterID string) string { return srClusterTagPrefix + clusterID }

func isStarRocksInstance(it store.Instance) bool {
	return strings.EqualFold(strings.TrimSpace(it.DBType), "starrocks")
}

func splitHosts(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '，' || r == ';' || r == ' ' }) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// lookupHost 解析域名为 IP；IP 原样返回；失败返回空。
func lookupHost(host string) []string {
	h, _, _ := strings.Cut(strings.TrimSpace(host), ":")
	if h == "" {
		return nil
	}
	if net.ParseIP(h) != nil {
		return []string{h}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupHost(ctx, h)
	if err != nil {
		return nil
	}
	return addrs
}

// clusterFEHosts FE 节点主机与 fe_host（去重）。
func clusterFEHosts(cl *store.SrCluster, nodes []store.SrNode) []string {
	seen := map[string]bool{}
	var out []string
	add := func(h string) {
		h = strings.TrimSpace(h)
		if h == "" || seen[strings.ToLower(h)] {
			return
		}
		seen[strings.ToLower(h)] = true
		out = append(out, h)
	}
	if cl != nil {
		add(cl.FeHost)
	}
	for _, n := range nodes {
		if n.Component == "fe" {
			add(n.Host)
		}
	}
	return out
}

func feQueryPort(cl *store.SrCluster) int {
	if cl != nil && cl.FeQueryPort > 0 {
		return cl.FeQueryPort
	}
	return 9030
}

// pickLinkedInstance 先按标签 sr-cluster=<id>，再按实例主机（逐个解析）命中 FE 且端口一致匹配；多个命中时优先已设密码。
func sameClusterTenant(cl *store.SrCluster, it store.Instance) bool {
	want := strings.TrimSpace(cl.TenantID)
	got := strings.TrimSpace(it.TenantID)
	if want == "" || got == "" {
		return true
	}
	return want == got
}

func pickLinkedInstance(insts []store.Instance, cl *store.SrCluster, feHosts []string, resolve func(string) []string) (*store.Instance, string) {
	if cl == nil {
		return nil, ""
	}
	if cl.ID != "" {
		tag := clusterTag(cl.ID)
		for i := range insts {
			if !isStarRocksInstance(insts[i]) || !sameClusterTenant(cl, insts[i]) {
				continue
			}
			for _, t := range insts[i].Tags {
				if strings.EqualFold(strings.TrimSpace(t), tag) {
					return &insts[i], "tag"
				}
			}
		}
	}
	port := feQueryPort(cl)
	var hit *store.Instance
	for i := range insts {
		it := &insts[i]
		if !isStarRocksInstance(*it) || it.Port != port || !sameClusterTenant(cl, *it) {
			continue
		}
		if !instanceHitsHosts(it.Host, feHosts, resolve) {
			continue
		}
		if hit == nil || (strings.TrimSpace(hit.Password) == "" && strings.TrimSpace(it.Password) != "") {
			hit = it
		}
	}
	if hit != nil {
		return hit, "host"
	}
	return nil, ""
}

func instanceHitsHosts(instHost string, feHosts []string, resolve func(string) []string) bool {
	for _, addr := range splitHosts(instHost) {
		cands := []string{addr}
		if resolve != nil {
			cands = append(cands, resolve(addr)...)
		}
		for _, c := range cands {
			for _, fh := range feHosts {
				if hostsEqual(c, fh) {
					return true
				}
			}
		}
	}
	return false
}

// findLinkedInstance 集群关联的 StarRocks 资源实例（资源配置 → 实例管理）。
func (e *Engine) clusterTenantID(cl *store.SrCluster) string {
	if cl == nil {
		return ""
	}
	if tid := strings.TrimSpace(cl.TenantID); tid != "" {
		return tid
	}
	if e == nil || e.Store == nil || strings.TrimSpace(cl.ID) == "" {
		return ""
	}
	tid := e.Store.RowTenantID("tbl_sr_clusters", cl.ID)
	cl.TenantID = tid
	return tid
}

func (e *Engine) findLinkedInstance(cl *store.SrCluster, nodes []store.SrNode) (*store.Instance, string) {
	if e == nil || e.Store == nil || cl == nil {
		return nil, ""
	}
	tid := e.clusterTenantID(cl)
	if tid == "" {
		return nil, ""
	}
	insts, err := e.Store.ListInstancesInTenant(tid, "", "")
	if err != nil {
		return nil, ""
	}
	return pickLinkedInstance(insts, cl, clusterFEHosts(cl, nodes), lookupHost)
}

// LinkedInstance 供接口展示的关联实例摘要。
func (e *Engine) LinkedInstance(cl *store.SrCluster, nodes []store.SrNode) *InstanceLink {
	it, by := e.findLinkedInstance(cl, nodes)
	if it == nil {
		return nil
	}
	return &InstanceLink{ID: it.ID, Name: it.Name, Host: it.Host, Port: it.Port, PasswordSet: strings.TrimSpace(it.Password) != "", MatchedBy: by}
}

// feCredentials 连 FE 的账号密码：preferCluster 时集群密码优先；否则实例 → FeAdmin → 集群。
func (e *Engine) feCredentials(cl *store.SrCluster, nodes []store.SrNode, preferCluster bool) (user, pass string, err error) {
	user = strings.TrimSpace(cl.FeUser)
	if user == "" {
		user = "root"
	}
	if preferCluster && strings.TrimSpace(cl.FePasswordEnc) != "" {
		pass, err = secret.Decrypt(e.Secret, cl.FePasswordEnc)
		return user, pass, err
	}
	if it, _ := e.findLinkedInstance(cl, nodes); it != nil && strings.TrimSpace(it.Password) != "" {
		if p, derr := secret.Decrypt(e.Secret, it.Password); derr == nil {
			if u := strings.TrimSpace(it.Username); u != "" {
				user = u
			}
			return user, p, nil
		}
	}
	if u, p, ok := e.lookupFeAdminCred(cl, nodes); ok {
		return u, p, nil
	}
	pass, err = secret.Decrypt(e.Secret, cl.FePasswordEnc)
	return user, pass, err
}

func stripHostPort(ep string) string {
	ep = strings.TrimSpace(ep)
	if ep == "" {
		return ""
	}
	if h, _, ok := strings.Cut(ep, ":"); ok {
		// 仅当后缀像端口时截断
		allDigit := true
		for _, c := range ep[len(h)+1:] {
			if c < '0' || c > '9' {
				allDigit = false
				break
			}
		}
		if allDigit && len(ep) > len(h)+1 {
			return h
		}
	}
	return ep
}

// LookupFeAdminCredForProbe 供登记探测在表单无密码时回退 FE 管理凭据。
func (e *Engine) LookupFeAdminCredForProbe(cl *store.SrCluster, nodes []store.SrNode) (user, pass string, ok bool) {
	return e.lookupFeAdminCred(cl, nodes)
}

// lookupFeAdminCred 按 FE 节点主机匹配配置中心 FE 管理凭据。
func (e *Engine) lookupFeAdminCred(cl *store.SrCluster, nodes []store.SrNode) (user, pass string, ok bool) {
	if e.Store == nil {
		return "", "", false
	}
	list, err := e.Store.ListFeAdmins("", "")
	if err != nil || len(list) == 0 {
		return "", "", false
	}
	hosts := make([]string, 0, len(nodes)+1)
	if h := strings.TrimSpace(cl.FeHost); h != "" {
		hosts = append(hosts, h)
	}
	for _, n := range nodes {
		if n.Component == "fe" && strings.TrimSpace(n.Host) != "" {
			hosts = append(hosts, n.Host)
		}
	}
	want := map[string]struct{}{}
	for _, h := range hosts {
		h = strings.ToLower(strings.TrimSpace(h))
		if h != "" {
			want[h] = struct{}{}
		}
	}
	if len(want) == 0 {
		return "", "", false
	}
	for i := range list {
		it := &list[i]
		if strings.TrimSpace(it.PasswordEnc) == "" {
			continue
		}
		for _, ep := range strings.FieldsFunc(it.FeNode, func(r rune) bool {
			return r == ',' || r == '，' || r == ';' || r == '\n'
		}) {
			host := stripHostPort(strings.TrimSpace(ep))
			if host == "" {
				continue
			}
			host = strings.ToLower(host)
			if _, hit := want[host]; !hit {
				continue
			}
			p, derr := secret.Decrypt(e.Secret, it.PasswordEnc)
			if derr != nil || p == "" {
				continue
			}
			u := strings.TrimSpace(it.Username)
			if u == "" {
				u = "root"
			}
			return u, p, true
		}
	}
	return "", "", false
}

func buildFEDSN(user, pass, host string, port int) string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/", user, pass, host, port)
}

// ProbeAndAlignFERoles 登记已有集群前校验 FE 连接（表单密码优先，否则用匹配到的资源实例），
// 并按实际角色对齐 nodes 中 FE 的 fe_role；返回登记与现场不一致的说明。
func (e *Engine) ProbeAndAlignFERoles(ctx context.Context, cl *store.SrCluster, nodes []store.SrNode) ([]string, error) {
	if e.Cfg.DryRun {
		return nil, nil
	}
	rows, err := e.probeFrontends(ctx, cl, nodes)
	if err != nil {
		return nil, err
	}
	return alignRegisteredFERoles(nodes, rows), nil
}

// FERoleInfo 登记表单探测用的 FE 实时角色。
type FERoleInfo struct {
	Host string `json:"host"`
	Role string `json:"role"` // LEADER | FOLLOWER | OBSERVER
}

// ProbeFERoles 用给定 FE 查询口与密码执行 SHOW PROC '/frontends'，返回各节点实时角色（不写库）。
func (e *Engine) ProbeFERoles(ctx context.Context, feHost, user, pass string, queryPort int) ([]FERoleInfo, error) {
	feHost = strings.TrimSpace(feHost)
	if feHost == "" {
		return nil, fmt.Errorf("fe_host 为空")
	}
	if queryPort <= 0 {
		queryPort = 9030
	}
	user = strings.TrimSpace(user)
	if user == "" {
		user = "root"
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := queryProcRowsDSN(cctx, buildFEDSN(user, pass, feHost, queryPort), "/frontends", false)
	if err != nil {
		return nil, err
	}
	out := make([]FERoleInfo, 0, len(rows))
	for _, r := range rows {
		role := strings.ToUpper(strings.TrimSpace(r.Role))
		if role == "" {
			role = "FOLLOWER"
		}
		out = append(out, FERoleInfo{Host: r.Host, Role: role})
	}
	return out, nil
}

// probeFrontends 用给定节点解析凭据后执行 SHOW PROC '/frontends'（只读）。
func (e *Engine) probeFrontends(ctx context.Context, cl *store.SrCluster, nodes []store.SrNode) ([]procVersionRow, error) {
	host := strings.TrimSpace(cl.FeHost)
	if host == "" {
		return nil, fmt.Errorf("fe_host 为空")
	}
	user, pass, err := e.feCredentials(cl, nodes, true)
	if err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return queryProcRowsDSN(cctx, buildFEDSN(user, pass, host, feQueryPort(cl)), "/frontends", false)
}

// alignRegisteredFERoles 按 SHOW PROC 实际角色对齐登记的 fe_role（OBSERVER→observer，LEADER/FOLLOWER→follower），返回差异说明。
func alignRegisteredFERoles(nodes []store.SrNode, rows []procVersionRow) []string {
	var diffs []string
	matched := make([]bool, len(rows))
	for i := range nodes {
		n := &nodes[i]
		if n.Component != "fe" {
			continue
		}
		found := false
		for j, r := range rows {
			if !hostsEqual(n.Host, r.Host) {
				continue
			}
			found, matched[j] = true, true
			if strings.EqualFold(r.Role, "OBSERVER") {
				n.FeRole = "observer"
			} else {
				n.FeRole = "follower"
			}
			break
		}
		if !found {
			diffs = append(diffs, fmt.Sprintf("FE %s 未出现在 SHOW PROC '/frontends' 中", n.Host))
		}
	}
	for j, r := range rows {
		if !matched[j] {
			diffs = append(diffs, fmt.Sprintf("SHOW PROC '/frontends' 中的 FE %s（%s）未登记", r.Host, r.Role))
		}
	}
	return diffs
}

// envForCluster 集群 environment 对应的环境（按 id / code / name 匹配）及其所属第一个区域。
func (e *Engine) envForCluster(cl *store.SrCluster) (*store.Environment, string) {
	want := strings.TrimSpace(cl.Environment)
	if want == "" {
		return nil, ""
	}
	envs, err := e.Store.ListEnvironments()
	if err != nil {
		return nil, ""
	}
	var env *store.Environment
	for i := range envs {
		if envs[i].ID == want || strings.EqualFold(envs[i].Code, want) || strings.EqualFold(envs[i].Name, want) {
			env = &envs[i]
			break
		}
	}
	if env == nil {
		return nil, ""
	}
	regions, _ := e.Store.ListRegions()
	for _, r := range regions {
		list, _ := e.Store.ListRegionEnvs(r.ID)
		for _, x := range list {
			if x.ID == env.ID {
				return env, r.ID
			}
		}
	}
	return env, ""
}

func envCode(env *store.Environment, cl *store.SrCluster) string {
	if env != nil && strings.TrimSpace(env.Code) != "" {
		return strings.TrimSpace(env.Code)
	}
	return strings.TrimSpace(cl.Environment)
}

// EnsureClusterInstance 在资源配置中登记集群对应的 StarRocks 实例：已有匹配实例只补标签，否则新建。
func (e *Engine) EnsureClusterInstance(cl *store.SrCluster, nodes []store.SrNode, who string) (*store.Instance, bool, error) {
	if e == nil || e.Store == nil || cl == nil || cl.ID == "" {
		return nil, false, fmt.Errorf("集群无效")
	}
	if it, _ := e.findLinkedInstance(cl, nodes); it != nil {
		tag := clusterTag(cl.ID)
		for _, t := range it.Tags {
			if strings.EqualFold(strings.TrimSpace(t), tag) {
				return it, false, nil
			}
		}
		it.Tags = append(it.Tags, tag)
		if err := e.Store.UpdateInstance(it); err != nil {
			return nil, false, err
		}
		return it, false, nil
	}
	hosts := clusterFEHosts(cl, nodes)
	if len(hosts) == 0 {
		return nil, false, fmt.Errorf("集群没有 FE 主机，无法登记实例")
	}
	env, regionID := e.envForCluster(cl)
	code := envCode(env, cl)
	user := strings.TrimSpace(cl.FeUser)
	if user == "" {
		user = "root"
	}
	if strings.TrimSpace(who) == "" {
		who = "admin"
	}
	tags := []string{clusterTag(cl.ID)}
	if code != "" {
		tags = append(tags, "idc="+code)
	}
	it := &store.Instance{
		Name:        cl.Name,
		DBType:      "starrocks",
		Host:        strings.Join(hosts, ","),
		Port:        feQueryPort(cl),
		Username:    user,
		Password:    cl.FePasswordEnc,
		IDC:         code,
		RegionID:    regionID,
		Domains:     []string{hosts[0]},
		DomainName:  hosts[0],
		Tags:        tags,
		Approvers:   []string{who},
		Visibility:  "global",
		Description: "由 StarRocks 集群部署自动登记（集群 " + cl.Name + "）",
	}
	if env != nil {
		it.EnvID = env.ID
	}
	tid := e.clusterTenantID(cl)
	if tid == "" {
		return nil, false, fmt.Errorf("集群未划分租户，无法登记实例")
	}
	it.TenantID = tid
	if err := e.Store.CreateInstance(it); err != nil {
		return nil, false, err
	}
	return it, true, nil
}

// RegistrySyncResult 同步到配置中心（资源实例 + 监控目标注册）的结果。
type RegistrySyncResult struct {
	Instance        *InstanceLink `json:"instance,omitempty"`
	InstanceCreated bool          `json:"instance_created"`
	Monitor         *MonitorLink  `json:"monitor_job,omitempty"`
	MonitorCreated  bool          `json:"monitor_created"`
	Warnings        []string      `json:"warnings,omitempty"`
}

// Summary 一句话描述，写入变更状态消息。
func (r *RegistrySyncResult) Summary() string {
	var parts []string
	if r.Instance != nil {
		verb := "已关联"
		if r.InstanceCreated {
			verb = "已登记"
		}
		parts = append(parts, fmt.Sprintf("%s资源实例 %s", verb, r.Instance.Name))
	}
	if r.Monitor != nil {
		verb := "已同步"
		if r.MonitorCreated {
			verb = "已注册"
		}
		parts = append(parts, fmt.Sprintf("%s监控任务 %s（FE %d / BE %d）", verb, r.Monitor.Name, r.Monitor.FECount, r.Monitor.BECount))
	}
	parts = append(parts, r.Warnings...)
	return strings.Join(parts, "；")
}

// SyncRegistry 同步集群到配置中心：withInstance 时登记/关联资源实例，随后同步监控目标注册；removed 为本次下线的节点。
func (e *Engine) SyncRegistry(cl *store.SrCluster, removed []store.SrNode, who string, withInstance bool) *RegistrySyncResult {
	res := &RegistrySyncResult{}
	nodes, _ := e.Store.ListSrNodes(cl.ID)
	if withInstance {
		if _, created, err := e.EnsureClusterInstance(cl, nodes, who); err != nil {
			res.Warnings = append(res.Warnings, "登记资源实例失败: "+err.Error())
		} else {
			res.InstanceCreated = created
		}
	}
	res.Instance = e.LinkedInstance(cl, nodes)
	mon, created, warns, err := e.SyncClusterMonitor(cl, nodes, removed)
	if err != nil {
		res.Warnings = append(res.Warnings, "同步监控目标失败: "+err.Error())
	}
	res.Monitor, res.MonitorCreated = mon, created
	res.Warnings = append(res.Warnings, warns...)
	return res
}

// syncRegistryAfterChange 变更成功后同步配置中心：初始化/重装登记实例并同步监控；扩容同步监控；下线移除监控目标。
func (e *Engine) syncRegistryAfterChange(ch *store.SrChange, cl *store.SrCluster) string {
	if e == nil || e.Store == nil || ch == nil || cl == nil || e.Cfg.DryRun {
		return ""
	}
	var res *RegistrySyncResult
	switch ch.Type {
	case store.SrChangeClusterInit, store.SrChangeClusterReinit:
		res = e.SyncRegistry(cl, nil, ch.CreatedBy, true)
	case store.SrChangeAddFE, store.SrChangeAddBE, store.SrChangeRecommissionBE:
		res = e.SyncRegistry(cl, nil, ch.CreatedBy, false)
	case store.SrChangeRemoveFE, store.SrChangeRemoveBE, store.SrChangeDecommissionBE:
		var removed []store.SrNode
		seen := map[string]bool{}
		for _, st := range ch.Steps {
			if st.NodeID == "" || seen[st.NodeID] {
				continue
			}
			seen[st.NodeID] = true
			if n, err := e.Store.GetSrNode(st.NodeID); err == nil && n != nil {
				removed = append(removed, *n)
			}
		}
		res = e.SyncRegistry(cl, removed, ch.CreatedBy, false)
		if ch.Type != store.SrChangeRemoveFE {
			msg := res.Summary()
			if gone, err := e.reconcileRemovedBackends(context.Background(), cl, ch.ID); err == nil && len(gone) > 0 {
				var hosts []string
				for _, n := range gone {
					hosts = append(hosts, n.Host)
				}
				tail := "缩容完成：BE " + strings.Join(hosts, "、") + " 已不在 SHOW PROC '/backends'，已移除节点登记"
				if msg != "" {
					msg += "；"
				}
				msg += tail
			}
			return msg
		}
	default:
		return ""
	}
	return res.Summary()
}
