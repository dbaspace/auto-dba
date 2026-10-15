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
	SuggestedAction string `json:"suggested_action"`
	Note            string `json:"note,omitempty"`
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
	seen := map[string]struct{}{}
	out := make([]string, 0, len(admins)+4)
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, name)
	}
	for _, a := range admins {
		add(a)
	}
	for _, n := range store.ParseApproverWhitelist(whitelistRaw) {
		add(n)
	}
	return out
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

func (h *ResourceApplyHandler) listPlatformAdminUsernames() ([]string, error) {
	users, err := h.Store.ListUsers()
	if err != nil {
		return nil, err
	}
	var out []string
	for i := range users {
		u := &users[i]
		ok, err := h.Store.UserIsPlatformAdmin(u.ID)
		if err != nil {
			return nil, err
		}
		if ok && strings.TrimSpace(u.Username) != "" {
			out = append(out, u.Username)
		}
	}
	return out, nil
}

func (h *ResourceApplyHandler) resolveAssignee(applicantTID string, items []ResourceApplyItem, preferred string) (string, error) {
	preferred = strings.TrimSpace(preferred)
	if resourceApplyNeedsPlatformAdmin(items) {
		admins, err := h.listPlatformAdminUsernames()
		if err != nil {
			return "", err
		}
		if preferred != "" {
			for _, a := range admins {
				if strings.EqualFold(a, preferred) {
					return a, nil
				}
			}
			return "", fmt.Errorf("含默认租户划拨项时，审批人须为平台管理员")
		}
		if len(admins) == 0 {
			return "", fmt.Errorf("未找到平台管理员，无法审批划拨类申请")
		}
		return admins[0], nil
	}
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
	if owner := resourceApplyDominantOwnerTenant(items); owner != "" && owner != applicantTID {
		if preferred != "" {
			if err := requireTenantMemberUsernames(h.Store, owner, []string{preferred}); err != nil {
				return "", fmt.Errorf("跨租授权审批人须为资源归属租户成员: %w", err)
			}
			return preferred, nil
		}
		members, err := listTenantMemberUsernames(h.Store, owner)
		if err != nil {
			return "", err
		}
		if len(members) == 0 {
			return "", fmt.Errorf("资源归属租户无可用审批人")
		}
		return members[0], nil
	}
	return resolveTenantAssignee(h.Store, applicantTID, preferred)
}

// Candidates GET /resource-apply/candidates?type=&q=&intent=
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
	names := tenantNameMap(h.Store)
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
	appendCand := func(rt, id, display, owner, note, action string) {
		if owner == "" {
			owner = store.DefaultTenantID
		}
		if action == "" {
			action = classifyResourceAction(owner, applicantTID)
		}
		out = append(out, resourceApplyCandidate{
			ResourceType:    rt,
			ResourceID:      id,
			DisplayName:     display,
			OwnerTenantID:   owner,
			OwnerTenantName: names[owner],
			SuggestedAction: action,
			Note:            note,
		})
	}
	collect := func(want string) error {
		switch want {
		case raTypeDatabase:
			list, err := h.Store.ListDatabases("", q)
			if err != nil {
				return err
			}
			for i := range list {
				db := &list[i]
				if !match(db.DBName, db.ID) {
					continue
				}
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
					appendCand(raTypeDatabase, db.ID, db.DBName, owner, note, raActionApprover)
					continue
				}
				note := "全局库"
				if strings.EqualFold(db.Visibility, "private") {
					note = "私有库"
				}
				appendCand(raTypeDatabase, db.ID, db.DBName, db.TenantID, note, "")
			}
		case raTypeDomain:
			list, err := h.Store.ListAccessDomains("", "", q)
			if err != nil {
				return err
			}
			for i := range list {
				ad := &list[i]
				disp := ad.Domain
				if ad.DBType != "" {
					disp = ad.Domain + " (" + ad.DBType + ")"
				}
				if !match(ad.Domain, ad.DBType, ad.ID) {
					continue
				}
				appendCand(raTypeDomain, ad.ID, disp, ad.TenantID, "", "")
			}
		case raTypeHost:
			list, err := h.Store.ListInstances("", q)
			if err != nil {
				return err
			}
			for i := range list {
				inst := &list[i]
				if !store.IsHostInstanceType(inst.DBType) {
					continue
				}
				name := inst.Name
				if name == "" {
					name = inst.DomainName
				}
				if !match(name, inst.DomainName, inst.Host, inst.ID) {
					continue
				}
				appendCand(raTypeHost, inst.ID, name, inst.TenantID, "", "")
			}
		case raTypeSrCluster:
			list, err := h.Store.ListSrClusters("")
			if err != nil {
				return err
			}
			for i := range list {
				cl := &list[i]
				if !match(cl.Name, cl.ID) {
					continue
				}
				appendCand(raTypeSrCluster, cl.ID, cl.Name, cl.TenantID, "", "")
			}
		case raTypeDbDeployCluster:
			list, err := h.Store.ListDbDeployClusters("")
			if err != nil {
				return err
			}
			for i := range list {
				cl := &list[i]
				if !match(cl.Name, cl.Engine, cl.ID) {
					continue
				}
				disp := cl.Name
				if cl.Engine != "" {
					disp = cl.Name + " (" + cl.Engine + ")"
				}
				appendCand(raTypeDbDeployCluster, cl.ID, disp, cl.TenantID, "", "")
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
	if hasAssign {
		names, err = h.listPlatformAdminUsernames()
	} else if intent == raIntentApprover {
		names, err = h.listApproverIntentAssignees()
	} else if ownerTID != "" && ownerTID != applicantTID {
		names, err = listTenantMemberUsernames(h.Store, ownerTID)
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
