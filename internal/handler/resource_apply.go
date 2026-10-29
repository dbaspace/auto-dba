package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

const (
	resourceApplyKind = "resource_apply"

	raTypeDatabase        = "database"
	raTypeDomain          = "domain"
	raTypeHost            = "host"
	raTypeSrCluster       = "sr_cluster"
	raTypeDbDeployCluster = "db_deploy_cluster"

	raActionACL      = "acl"
	raActionGrant    = "grant"
	raActionAssign   = "assign"
	raActionApprover = "approver"

	raIntentVisibility = "visibility"
	raIntentApprover   = "approver"
)

// ResourceApplyHandler 平台资源申请（库/域名/主机/集群）。
type ResourceApplyHandler struct {
	Store  *store.DB
	Secret string
}

// ResourceApplyItem 申请项（写入 Issue.SQLContent）。
type ResourceApplyItem struct {
	ResourceType    string   `json:"resource_type"`
	ResourceID      string   `json:"resource_id"`
	DisplayName     string   `json:"display_name"`
	OwnerTenantID   string   `json:"owner_tenant_id"`
	OwnerTenantName string   `json:"owner_tenant_name,omitempty"`
	Action          string   `json:"action"` // acl | grant | assign | approver（服务端重算）
	GrantType       string   `json:"grant_type,omitempty"`
	Note            string   `json:"note,omitempty"`
	TargetUsers     []string `json:"target_users,omitempty"` // 库审核人：写入 approvers 的目标用户
}

// ResourceApplyPayload 工单 payload。
type ResourceApplyPayload struct {
	Kind   string              `json:"kind"`
	Intent string              `json:"intent,omitempty"` // visibility | approver
	Reason string              `json:"reason,omitempty"`
	Items  []ResourceApplyItem `json:"items"`
}

type resourceApplySubmitReq struct {
	Intent      string                    `json:"intent"`
	Items       []resourceApplySubmitItem `json:"items"`
	TargetUsers []string                  `json:"target_users"`
	AssignedTo  string                    `json:"assigned_to"`
	Reason      string                    `json:"reason"`
	Description string                    `json:"description"`
}

type resourceApplySubmitItem struct {
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
}

type resourceApplyCandidate struct {
	ResourceType    string `json:"resource_type"`
	ResourceID      string `json:"resource_id"`
	DisplayName     string `json:"display_name"`
	OwnerTenantID   string `json:"owner_tenant_id"`
	OwnerTenantName string `json:"owner_tenant_name"`
	EnvCode         string `json:"env_code,omitempty"`
	EnvName         string `json:"env_name,omitempty"`
	SuggestedAction string `json:"suggested_action"`
	Note            string `json:"note,omitempty"`
}

type resourceApplyEnvLabel struct {
	Code string
	Name string
}

func resourceApplyEnvMaps(db *store.DB) (byID map[string]store.Environment, byCode map[string]store.Environment) {
	byID = map[string]store.Environment{}
	byCode = map[string]store.Environment{}
	if db == nil {
		return byID, byCode
	}
	list, err := db.ListEnvironments()
	if err != nil {
		return byID, byCode
	}
	for _, e := range list {
		if id := strings.TrimSpace(e.ID); id != "" {
			byID[id] = e
		}
		if code := strings.TrimSpace(e.Code); code != "" {
			byCode[strings.ToLower(code)] = e
		}
	}
	return byID, byCode
}

func resourceApplyEnvFromCode(raw string, byID, byCode map[string]store.Environment) resourceApplyEnvLabel {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return resourceApplyEnvLabel{}
	}
	if e, ok := byID[raw]; ok {
		code := strings.TrimSpace(e.Code)
		if code == "" {
			code = raw
		}
		name := strings.TrimSpace(e.Name)
		if name == "" {
			name = code
		}
		return resourceApplyEnvLabel{Code: code, Name: name}
	}
	if e, ok := byCode[strings.ToLower(raw)]; ok {
		code := strings.TrimSpace(e.Code)
		if code == "" {
			code = raw
		}
		name := strings.TrimSpace(e.Name)
		if name == "" {
			name = code
		}
		return resourceApplyEnvLabel{Code: code, Name: name}
	}
	return resourceApplyEnvLabel{Code: raw, Name: raw}
}

func resourceApplyEnvFromInstance(inst *store.Instance, byID, byCode map[string]store.Environment) resourceApplyEnvLabel {
	if inst == nil {
		return resourceApplyEnvLabel{}
	}
	if lab := resourceApplyEnvFromCode(inst.EnvID, byID, byCode); lab.Code != "" {
		return lab
	}
	return resourceApplyEnvFromCode(inst.IDC, byID, byCode)
}

func classifyResourceAction(ownerTenantID, applicantTenantID string) string {
	owner := strings.TrimSpace(ownerTenantID)
	if owner == "" {
		owner = store.DefaultTenantID
	}
	applicant := strings.TrimSpace(applicantTenantID)
	if owner == applicant {
		return raActionACL
	}
	if owner == store.DefaultTenantID {
		return raActionAssign
	}
	return raActionGrant
}

func normalizeResourceApplyIntent(intent string) string {
	switch strings.ToLower(strings.TrimSpace(intent)) {
	case raIntentApprover:
		return raIntentApprover
	default:
		return raIntentVisibility
	}
}

func grantResourceType(resourceType string) string {
	switch strings.TrimSpace(resourceType) {
	case raTypeHost:
		return "instance"
	default:
		return strings.TrimSpace(resourceType)
	}
}

func (h *ResourceApplyHandler) resolveResource(resourceType, resourceID string) (ownerTID, display, note string, err error) {
	resourceType = strings.TrimSpace(resourceType)
	resourceID = strings.TrimSpace(resourceID)
	if resourceType == "" || resourceID == "" {
		return "", "", "", fmt.Errorf("resource_type 与 resource_id 必填")
	}
	switch resourceType {
	case raTypeDatabase:
		db, e := h.Store.GetDatabase(resourceID)
		if e != nil {
			return "", "", "", e
		}
		if db == nil {
			return "", "", "", fmt.Errorf("库不存在")
		}
		inst, e := h.Store.GetInstance(db.InstanceID)
		if e != nil {
			return "", "", "", e
		}
		if inst == nil || !store.IsLogicalDatabaseEngine(inst.DBType) {
			return "", "", "", fmt.Errorf("所选资源不是可登记的逻辑库（父实例引擎不支持）")
		}
		note = ""
		if strings.EqualFold(db.Visibility, "private") {
			note = "私有库：同租将写入 access_users"
		} else {
			note = "全局库：同租成员已可见"
		}
		return strings.TrimSpace(db.TenantID), db.DBName, note, nil
	case raTypeDomain:
		ad, e := h.Store.GetAccessDomain(resourceID)
		if e != nil {
			return "", "", "", e
		}
		if ad == nil {
			return "", "", "", fmt.Errorf("域名不存在")
		}
		name := ad.Domain
		if ad.DBType != "" {
			name = ad.Domain + " (" + ad.DBType + ")"
		}
		return strings.TrimSpace(ad.TenantID), name, "域名无行级 ACL，同租仅记账", nil
	case raTypeHost:
		inst, e := h.Store.GetInstance(resourceID)
		if e != nil {
			return "", "", "", e
		}
		if inst == nil {
			return "", "", "", fmt.Errorf("主机不存在")
		}
		if !store.IsHostInstanceType(inst.DBType) {
			return "", "", "", fmt.Errorf("所选实例不是主机类型")
		}
		name := inst.Name
		if name == "" {
			name = inst.DomainName
		}
		if strings.EqualFold(inst.Visibility, "private") {
			note = "私有主机：同租将写入 access_users"
		} else {
			note = "全局主机：同租成员已可见"
		}
		return strings.TrimSpace(inst.TenantID), name, note, nil
	case raTypeSrCluster:
		cl, e := h.Store.GetSrCluster(resourceID)
		if e != nil {
			return "", "", "", e
		}
		if cl == nil {
			return "", "", "", fmt.Errorf("StarRocks 集群不存在")
		}
		return strings.TrimSpace(cl.TenantID), cl.Name, "集群无行级 ACL，同租仅记账", nil
	case raTypeDbDeployCluster:
		cl, e := h.Store.GetDbDeployCluster(resourceID)
		if e != nil {
			return "", "", "", e
		}
		if cl == nil {
			return "", "", "", fmt.Errorf("部署集群不存在")
		}
		return strings.TrimSpace(cl.TenantID), cl.Name, "集群无行级 ACL，同租仅记账", nil
	default:
		return "", "", "", fmt.Errorf("不支持的 resource_type: %s", resourceType)
	}
}

func (h *ResourceApplyHandler) buildItems(applicantTID, intent string, raw []resourceApplySubmitItem, targetUsers []string) ([]ResourceApplyItem, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("请至少选择一项资源")
	}
	intent = normalizeResourceApplyIntent(intent)
	targets := uniqueTrimmedNames(targetUsers)
	names := tenantNameMap(h.Store)
	seen := map[string]struct{}{}
	out := make([]ResourceApplyItem, 0, len(raw))
	var ownerTID string
	for _, r := range raw {
		rt := strings.TrimSpace(r.ResourceType)
		rid := strings.TrimSpace(r.ResourceID)
		key := rt + ":" + rid
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		owner, display, note, err := h.resolveResource(rt, rid)
		if err != nil {
			return nil, err
		}
		if owner == "" {
			owner = store.DefaultTenantID
		}
		action := classifyResourceAction(owner, applicantTID)
		var itemTargets []string
		if intent == raIntentApprover {
			if rt != raTypeDatabase {
				return nil, fmt.Errorf("库审核人申请仅支持已登记库")
			}
			if owner != strings.TrimSpace(applicantTID) {
				return nil, fmt.Errorf("仅可申请本租户下已登记库的审核人：%s", display)
			}
			if ownerTID == "" {
				ownerTID = owner
			} else if ownerTID != owner {
				return nil, fmt.Errorf("库审核人申请所选库须归属同一租户")
			}
			if len(targets) == 0 {
				return nil, fmt.Errorf("请至少选择一名库审核人")
			}
			action = raActionApprover
			note = "审批通过后将指定用户加入该库审核人名单"
			itemTargets = append([]string{}, targets...)
		}
		item := ResourceApplyItem{
			ResourceType:    rt,
			ResourceID:      rid,
			DisplayName:     display,
			OwnerTenantID:   owner,
			OwnerTenantName: names[owner],
			Action:          action,
			GrantType:       grantResourceType(rt),
			Note:            note,
			TargetUsers:     itemTargets,
		}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("请至少选择一项资源")
	}
	if intent == raIntentApprover {
		if err := requireTenantMemberUsernames(h.Store, ownerTID, targets); err != nil {
			return nil, fmt.Errorf("库审核人须为目标库归属租户的活跃成员: %w", err)
		}
	}
	return out, nil
}

func resourceApplyNeedsPlatformAdmin(items []ResourceApplyItem) bool {
	for _, it := range items {
		if it.Action == raActionAssign {
			return true
		}
	}
	return false
}

func resourceApplyHasApproverAction(items []ResourceApplyItem) bool {
	for _, it := range items {
		if it.Action == raActionApprover {
			return true
		}
	}
	return false
}

func resourceApplyDominantOwnerTenant(items []ResourceApplyItem) string {
	for _, it := range items {
		if it.Action == raActionGrant {
			return it.OwnerTenantID
		}
	}
	return ""
}

// mergeApproverIntentAssignees 平台管理员 ∪ 超级审核白名单（去重，保序）。
func mergeApproverIntentAssignees(admins []string, whitelistRaw string) []string {
	return mergeUsernameLists(admins, store.ParseApproverWhitelist(whitelistRaw))
}

// mergeUsernameLists 多组用户名并集，去重保序（大小写不敏感）。
func mergeUsernameLists(groups ...[]string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, g := range groups {
		for _, name := range g {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			key := strings.ToLower(name)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, name)
		}
	}
	return out
}

// isDBATenant code=dba 的运维租户（跨租授权第三源只取该租户里的用户，不含默认租户）。
func isDBATenant(_, code string) bool {
	return strings.EqualFold(strings.TrimSpace(code), "dba")
}

// mergeResourceApplyGrantAssignees 跨租授权审批人池：管理员 ∪ 白名单 ∪ dba 租户里的用户。
func mergeResourceApplyGrantAssignees(admins []string, whitelistRaw string, opsMembers []string) []string {
	return mergeUsernameLists(admins, store.ParseApproverWhitelist(whitelistRaw), opsMembers)
}

func pickAssigneeFromPool(pool []string, preferred, emptyMsg, missMsg string) (string, error) {
	preferred = strings.TrimSpace(preferred)
	if preferred != "" {
		for _, a := range pool {
			if strings.EqualFold(a, preferred) {
				return a, nil
			}
		}
		return "", fmt.Errorf("%s", missMsg)
	}
	if len(pool) == 0 {
		return "", fmt.Errorf("%s", emptyMsg)
	}
	return pool[0], nil
}

// listApproverIntentAssignees 库审核人申请的工单审批人：平台管理员 ∪ issue_global_approver_whitelist。
func (h *ResourceApplyHandler) listApproverIntentAssignees() ([]string, error) {
	admins, err := h.listPlatformAdminUsernames()
	if err != nil {
		return nil, err
	}
	raw, err := h.Store.GetGlobalArg(store.ArgIssueGlobalApproverWhitelist)
	if err != nil {
		return nil, err
	}
	return mergeApproverIntentAssignees(admins, raw), nil
}

// listDBATenantMemberUsernames dba 租户里的活跃、未停用/锁定用户。
func (h *ResourceApplyHandler) listDBATenantMemberUsernames() ([]string, error) {
	tenants, err := h.Store.ListTenants()
	if err != nil {
		return nil, err
	}
	var groups [][]string
	seenTID := map[string]struct{}{}
	for i := range tenants {
		t := &tenants[i]
		if !isDBATenant(t.ID, t.Code) {
			continue
		}
		if _, ok := seenTID[t.ID]; ok {
			continue
		}
		seenTID[t.ID] = struct{}{}
		names, err := listTenantMemberUsernames(h.Store, t.ID)
		if err != nil {
			return nil, err
		}
		active, err := h.filterActiveUsernames(names)
		if err != nil {
			return nil, err
		}
		groups = append(groups, active)
	}
	return mergeUsernameLists(groups...), nil
}

// listResourceApplyGrantAssignees 划拨/跨租可见性工单审批人：管理员 ∪ 白名单 ∪ dba 租户里的用户。
func (h *ResourceApplyHandler) listResourceApplyGrantAssignees() ([]string, error) {
	admins, err := h.listPlatformAdminUsernames()
	if err != nil {
		return nil, err
	}
	raw, err := h.Store.GetGlobalArg(store.ArgIssueGlobalApproverWhitelist)
	if err != nil {
		return nil, err
	}
	ops, err := h.listDBATenantMemberUsernames()
	if err != nil {
		return nil, err
	}
	return mergeResourceApplyGrantAssignees(admins, raw, ops), nil
}

func (h *ResourceApplyHandler) filterActiveUsernames(names []string) ([]string, error) {
	out := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		u, err := h.Store.GetUserByUsername(name)
		if err != nil {
			return nil, err
		}
		if u == nil || u.IsLoginBlocked() {
			continue
		}
		out = append(out, name)
	}
	return out, nil
}

func (h *ResourceApplyHandler) listPlatformAdminUsernames() ([]string, error) {
	users, err := h.Store.ListUsers()
	if err != nil {
		return nil, err
	}
	var out []string
	for i := range users {
		u := &users[i]
		if u.IsLoginBlocked() || strings.TrimSpace(u.Username) == "" {
			continue
		}
		ok, err := h.Store.UserIsPlatformAdmin(u.ID)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, u.Username)
		}
	}
	return out, nil
}

func (h *ResourceApplyHandler) resolveAssignee(applicantTID string, items []ResourceApplyItem, preferred string) (string, error) {
	preferred = strings.TrimSpace(preferred)
	if resourceApplyHasApproverAction(items) {
		cands, err := h.listApproverIntentAssignees()
		if err != nil {
			return "", err
		}
		if preferred != "" {
			for _, a := range cands {
				if strings.EqualFold(a, preferred) {
					return a, nil
				}
			}
			return "", fmt.Errorf("库审核人申请的审批人须为平台管理员或超级审核白名单用户")
		}
		if len(cands) == 0 {
			return "", fmt.Errorf("未找到平台管理员或超级审核白名单用户，无法审批库审核人申请")
		}
		return cands[0], nil
	}
	// 划拨 / 跨租（含混合）：平台管理员 ∪ 白名单 ∪ dba 租户里的用户
	if resourceApplyNeedsPlatformAdmin(items) {
		cands, err := h.listResourceApplyGrantAssignees()
		if err != nil {
			return "", err
		}
		return pickAssigneeFromPool(
			cands,
			preferred,
			"未找到可用的审批人（平台管理员、超级审核白名单或 dba 租户里的用户）",
			"审批人须为平台管理员、超级审核白名单或 dba 租户里的用户",
		)
	}
	if owner := resourceApplyDominantOwnerTenant(items); owner != "" && owner != applicantTID {
		cands, err := h.listResourceApplyGrantAssignees()
		if err != nil {
			return "", err
		}
		return pickAssigneeFromPool(
			cands,
			preferred,
			"未找到可用的跨租授权审批人（平台管理员、超级审核白名单或 dba 租户里的用户）",
			"跨租授权审批人须为平台管理员、超级审核白名单或 dba 租户里的用户",
		)
	}
	return resolveTenantAssignee(h.Store, applicantTID, preferred)
}

// Candidates GET /resource-apply/candidates?type=&q=&intent=&env=
func (h *ResourceApplyHandler) Candidates(c *gin.Context) {
	applicantTID := middleware.TenantID(c)
	if applicantTID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请先选择租户"})
		return
	}
	intent := normalizeResourceApplyIntent(c.Query("intent"))
	typ := strings.TrimSpace(c.Query("type"))
	if intent == raIntentApprover {
		typ = raTypeDatabase
	}
	q := strings.TrimSpace(c.Query("q"))
	qLower := strings.ToLower(q)
	filterEnv := strings.ToLower(strings.TrimSpace(c.Query("env")))
	names := tenantNameMap(h.Store)
	envByID, envByCode := resourceApplyEnvMaps(h.Store)
	instByID := map[string]*store.Instance{}
	ensureInstances := func() error {
		if len(instByID) > 0 {
			return nil
		}
		list, err := h.Store.ListInstances("", "")
		if err != nil {
			return err
		}
		for i := range list {
			inst := &list[i]
			instByID[inst.ID] = inst
		}
		return nil
	}
	var out []resourceApplyCandidate
	match := func(parts ...string) bool {
		if qLower == "" {
			return true
		}
		for _, p := range parts {
			if strings.Contains(strings.ToLower(p), qLower) {
				return true
			}
		}
		return false
	}
	appendCand := func(rt, id, display, owner, note, action string, env resourceApplyEnvLabel) {
		if owner == "" {
			owner = store.DefaultTenantID
		}
		if action == "" {
			action = classifyResourceAction(owner, applicantTID)
		}
		if filterEnv != "" {
			code := strings.ToLower(strings.TrimSpace(env.Code))
			name := strings.ToLower(strings.TrimSpace(env.Name))
			if code != filterEnv && name != filterEnv {
				return
			}
		}
		if !match(display, id, owner, names[owner], env.Code, env.Name, note) {
			return
		}
		out = append(out, resourceApplyCandidate{
			ResourceType:    rt,
			ResourceID:      id,
			DisplayName:     display,
			OwnerTenantID:   owner,
			OwnerTenantName: names[owner],
			EnvCode:         env.Code,
			EnvName:         env.Name,
			SuggestedAction: action,
			Note:            note,
		})
	}
	collect := func(want string) error {
		switch want {
		case raTypeDatabase:
			if err := ensureInstances(); err != nil {
				return err
			}
			list, err := h.Store.ListDatabases("", "")
			if err != nil {
				return err
			}
			for i := range list {
				db := &list[i]
				inst := instByID[db.InstanceID]
				if inst == nil || !store.IsLogicalDatabaseEngine(inst.DBType) {
					continue
				}
				env := resourceApplyEnvFromInstance(inst, envByID, envByCode)
				owner := strings.TrimSpace(db.TenantID)
				if owner == "" {
					owner = store.DefaultTenantID
				}
				if intent == raIntentApprover {
					if owner != applicantTID {
						continue
					}
					note := "加入审核人名单"
					if strings.EqualFold(db.Visibility, "private") {
						note = "加入审核人；私有库将同时开通可见"
					}
					appendCand(raTypeDatabase, db.ID, db.DBName, owner, note, raActionApprover, env)
					continue
				}
				note := "全局库"
				if strings.EqualFold(db.Visibility, "private") {
					note = "私有库"
				}
				appendCand(raTypeDatabase, db.ID, db.DBName, db.TenantID, note, "", env)
			}
		case raTypeDomain:
			list, err := h.Store.ListAccessDomains("", "", "")
			if err != nil {
				return err
			}
			for i := range list {
				ad := &list[i]
				disp := ad.Domain
				if ad.DBType != "" {
					disp = ad.Domain + " (" + ad.DBType + ")"
				}
				env := resourceApplyEnvFromCode(ad.EnvCode, envByID, envByCode)
				appendCand(raTypeDomain, ad.ID, disp, ad.TenantID, "", "", env)
			}
		case raTypeHost:
			if err := ensureInstances(); err != nil {
				return err
			}
			for _, inst := range instByID {
				if !store.IsHostInstanceType(inst.DBType) {
					continue
				}
				name := inst.Name
				if name == "" {
					name = inst.DomainName
				}
				env := resourceApplyEnvFromInstance(inst, envByID, envByCode)
				appendCand(raTypeHost, inst.ID, name, inst.TenantID, "", "", env)
			}
		case raTypeSrCluster:
			list, err := h.Store.ListSrClusters("")
			if err != nil {
				return err
			}
			for i := range list {
				cl := &list[i]
				env := resourceApplyEnvFromCode(cl.Environment, envByID, envByCode)
				appendCand(raTypeSrCluster, cl.ID, cl.Name, cl.TenantID, "", "", env)
			}
		case raTypeDbDeployCluster:
			list, err := h.Store.ListDbDeployClusters("")
			if err != nil {
				return err
			}
			for i := range list {
				cl := &list[i]
				disp := cl.Name
				if cl.Engine != "" {
					disp = cl.Name + " (" + cl.Engine + ")"
				}
				env := resourceApplyEnvFromCode(cl.Environment, envByID, envByCode)
				appendCand(raTypeDbDeployCluster, cl.ID, disp, cl.TenantID, "", "", env)
			}
		default:
			return fmt.Errorf("type 须为 database|domain|host|sr_cluster|db_deploy_cluster")
		}
		return nil
	}
	types := []string{typ}
	if typ == "" && intent != raIntentApprover {
		types = []string{raTypeDatabase, raTypeDomain, raTypeHost, raTypeSrCluster, raTypeDbDeployCluster}
	}
	for _, t := range types {
		if err := collect(t); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
	}
	if out == nil {
		out = []resourceApplyCandidate{}
	}
	const maxAll = 200
	if len(out) > maxAll {
		out = out[:maxAll]
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

// TargetUsers GET /resource-apply/target-users — 库审核人候选（当前租户活跃成员）。
func (h *ResourceApplyHandler) TargetUsers(c *gin.Context) {
	applicantTID := middleware.TenantID(c)
	if applicantTID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请先选择租户"})
		return
	}
	names, err := listTenantMemberUsernames(h.Store, applicantTID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if names == nil {
		names = []string{}
	}
	opts := make([]gin.H, 0, len(names))
	for _, n := range names {
		opts = append(opts, gin.H{"value": n, "label": n})
	}
	c.JSON(http.StatusOK, gin.H{"data": opts})
}

// Approvers GET /resource-apply/approvers?intent=&has_assign=1&owner_tenant_id=&resource_ids=
func (h *ResourceApplyHandler) Approvers(c *gin.Context) {
	applicantTID := middleware.TenantID(c)
	if applicantTID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请先选择租户"})
		return
	}
	intent := normalizeResourceApplyIntent(c.Query("intent"))
	hasAssign := c.Query("has_assign") == "1" || c.Query("has_assign") == "true"
	ownerTID := strings.TrimSpace(c.Query("owner_tenant_id"))
	var names []string
	var err error
	if intent == raIntentApprover {
		names, err = h.listApproverIntentAssignees()
	} else if hasAssign || (ownerTID != "" && ownerTID != applicantTID) {
		// 划拨 / 跨租：管理员 ∪ 白名单 ∪ dba 租户里的用户
		names, err = h.listResourceApplyGrantAssignees()
	} else {
		names, err = listTenantMemberUsernames(h.Store, applicantTID)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if names == nil {
		names = []string{}
	}
	opts := make([]gin.H, 0, len(names))
	for _, n := range names {
		opts = append(opts, gin.H{"value": n, "label": n})
	}
	c.JSON(http.StatusOK, gin.H{"data": opts})
}

// Submit POST /resource-apply
func (h *ResourceApplyHandler) Submit(c *gin.Context) {
	var req resourceApplySubmitReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	applicantTID := middleware.TenantID(c)
	if applicantTID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请先选择租户"})
		return
	}
	username := middleware.Username(c)
	intent := normalizeResourceApplyIntent(req.Intent)
	items, err := h.buildItems(applicantTID, intent, req.Items, req.TargetUsers)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	assignee, err := h.resolveAssignee(applicantTID, items, req.AssignedTo)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = strings.TrimSpace(req.Description)
	}
	payload := ResourceApplyPayload{Kind: resourceApplyKind, Intent: intent, Reason: reason, Items: items}
	raw, err := json.Marshal(payload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	desc := strings.TrimSpace(req.Description)
	if desc == "" {
		desc = reason
	}
	if desc == "" {
		desc = fmt.Sprintf("平台资源申请 %d 项", len(items))
	}
	issue := &store.Issue{
		IssueType:   store.IssueTypeResourceApply,
		Type:        store.IssueTypeResourceApply,
		SQLContent:  string(raw),
		Description: desc,
		Status:      store.StatusPending,
		Submitter:   username,
		AssignedTo:  assignee,
		TenantID:    applicantTID,
	}
	if err := h.Store.CreateIssue(issue); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	NotifyIssueCreatedAsync(h.Store, h.Secret, issue)
	c.JSON(http.StatusOK, gin.H{"data": issue})
}

func parseResourceApplyPayload(sqlContent string) (*ResourceApplyPayload, error) {
	raw := strings.TrimSpace(sqlContent)
	if raw == "" {
		return nil, fmt.Errorf("工单内容为空")
	}
	var p ResourceApplyPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, fmt.Errorf("解析资源申请内容失败: %w", err)
	}
	if p.Kind != "" && p.Kind != resourceApplyKind {
		return nil, fmt.Errorf("不是平台资源申请工单")
	}
	if len(p.Items) == 0 {
		return nil, fmt.Errorf("申请项为空")
	}
	return &p, nil
}
