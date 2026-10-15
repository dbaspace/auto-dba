package handler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"auto-dba/internal/store"
	"auto-dba/internal/ticketexec"
)

func appendUniqueUsername(list []string, username string) []string {
	username = strings.TrimSpace(username)
	if username == "" {
		return list
	}
	for _, u := range list {
		if strings.EqualFold(strings.TrimSpace(u), username) {
			return list
		}
	}
	return append(append([]string{}, list...), username)
}

func (h *ResourceApplyHandler) applyACL(item ResourceApplyItem, username string) (string, error) {
	switch item.ResourceType {
	case raTypeDatabase:
		db, err := h.Store.GetDatabase(item.ResourceID)
		if err != nil {
			return "", err
		}
		if db == nil {
			return "", fmt.Errorf("库不存在")
		}
		if !strings.EqualFold(db.Visibility, "private") {
			return "同租全局库，无需改 ACL", nil
		}
		before := len(db.AccessUsers)
		db.AccessUsers = appendUniqueUsername(db.AccessUsers, username)
		if len(db.AccessUsers) == before {
			return "已在 access_users 中", nil
		}
		if err := h.Store.UpdateDatabase(db); err != nil {
			return "", err
		}
		return "已写入库 access_users", nil
	case raTypeHost:
		inst, err := h.Store.GetInstance(item.ResourceID)
		if err != nil {
			return "", err
		}
		if inst == nil {
			return "", fmt.Errorf("主机不存在")
		}
		if !strings.EqualFold(inst.Visibility, "private") {
			return "同租全局主机，无需改 ACL", nil
		}
		before := len(inst.AccessUsers)
		inst.AccessUsers = appendUniqueUsername(inst.AccessUsers, username)
		if len(inst.AccessUsers) == before {
			return "已在 access_users 中", nil
		}
		if err := h.Store.UpdateInstance(inst); err != nil {
			return "", err
		}
		return "已写入主机 access_users", nil
	case raTypeDomain, raTypeSrCluster, raTypeDbDeployCluster:
		return "同租资源已可见，仅记账", nil
	default:
		return "", fmt.Errorf("ACL 不支持类型 %s", item.ResourceType)
	}
}

func (h *ResourceApplyHandler) applyGrant(item ResourceApplyItem, toTenantID, createdBy string) (string, error) {
	gt := grantResourceType(item.ResourceType)
	if gt == "" {
		return "", fmt.Errorf("无效 grant 类型")
	}
	ok, err := h.Store.HasResourceGrant(toTenantID, "", gt, item.ResourceID)
	if err != nil {
		return "", err
	}
	if ok {
		return "授权已存在，跳过", nil
	}
	g := &store.TenantGrant{
		FromTenantID: item.OwnerTenantID,
		ToTenantID:   toTenantID,
		ResourceType: gt,
		ResourceID:   item.ResourceID,
		Permission:   "read",
		CreatedBy:    createdBy,
	}
	if err := h.Store.CreateTenantGrant(g); err != nil {
		return "", err
	}
	_ = h.Store.AppendTenantAudit(item.OwnerTenantID, createdBy, "resource_apply.grant",
		fmt.Sprintf("%s %s -> %s", gt, item.ResourceID, toTenantID))
	return "已创建跨租只读授权", nil
}

func (h *ResourceApplyHandler) applyAssign(item ResourceApplyItem, toTenantID string) (string, error) {
	toTenantID = strings.TrimSpace(toTenantID)
	if toTenantID == "" {
		return "", fmt.Errorf("目标租户为空")
	}
	switch item.ResourceType {
	case raTypeDatabase:
		if err := h.Store.AssignDatabaseTenant(item.ResourceID, toTenantID); err != nil {
			return "", err
		}
		return "已划拨库到申请租户", nil
	case raTypeDomain:
		if err := h.Store.AssignAccessDomainTenant(item.ResourceID, toTenantID); err != nil {
			return "", err
		}
		return "已划拨域名到申请租户", nil
	case raTypeHost:
		if err := h.Store.AssignInstanceTenant(item.ResourceID, toTenantID); err != nil {
			return "", err
		}
		return "已划拨主机实例到申请租户", nil
	case raTypeSrCluster:
		if err := h.Store.AssignSrClusterTenant(item.ResourceID, toTenantID); err != nil {
			return "", err
		}
		return "已划拨 StarRocks 集群到申请租户", nil
	case raTypeDbDeployCluster:
		if err := h.Store.AssignDbDeployClusterTenant(item.ResourceID, toTenantID); err != nil {
			return "", err
		}
		return "已划拨部署集群到申请租户", nil
	default:
		return "", fmt.Errorf("划拨不支持类型 %s", item.ResourceType)
	}
}

func (h *ResourceApplyHandler) applyApprover(item ResourceApplyItem, applicantTID, submitter string) (string, error) {
	if item.ResourceType != raTypeDatabase {
		return "", fmt.Errorf("库审核人申请仅支持已登记库")
	}
	submitter = strings.TrimSpace(submitter)
	if submitter == "" {
		return "", fmt.Errorf("提交人为空")
	}
	if err := requireTenantMemberUsernames(h.Store, applicantTID, []string{submitter}); err != nil {
		return "", fmt.Errorf("申请人须为本租户成员: %w", err)
	}
	targets := uniqueTrimmedNames(item.TargetUsers)
	if len(targets) == 0 {
		return "", fmt.Errorf("库审核人为空")
	}
	db, err := h.Store.GetDatabase(item.ResourceID)
	if err != nil {
		return "", err
	}
	if db == nil {
		return "", fmt.Errorf("库不存在")
	}
	owner := strings.TrimSpace(db.TenantID)
	if owner == "" {
		owner = store.DefaultTenantID
	}
	if owner != strings.TrimSpace(applicantTID) {
		return "", fmt.Errorf("库已不属于申请租户，无法加入审核人")
	}
	if err := requireTenantMemberUsernames(h.Store, owner, targets); err != nil {
		return "", fmt.Errorf("库审核人须为库归属租户成员: %w", err)
	}
	added, already := 0, 0
	aclAdded := 0
	for _, u := range targets {
		beforeAppr := len(db.Approvers)
		db.Approvers = appendUniqueUsername(db.Approvers, u)
		if len(db.Approvers) == beforeAppr {
			already++
		} else {
			added++
		}
		if strings.EqualFold(db.Visibility, "private") {
			beforeACL := len(db.AccessUsers)
			db.AccessUsers = appendUniqueUsername(db.AccessUsers, u)
			if len(db.AccessUsers) > beforeACL {
				aclAdded++
			}
		}
	}
	if err := h.Store.UpdateDatabase(db); err != nil {
		return "", err
	}
	msgs := make([]string, 0, 3)
	if added > 0 {
		msgs = append(msgs, fmt.Sprintf("已写入库审核人 %d 人", added))
	}
	if already > 0 {
		msgs = append(msgs, fmt.Sprintf("已在名单中 %d 人", already))
	}
	if aclAdded > 0 {
		msgs = append(msgs, fmt.Sprintf("私有库已同步开通可见 %d 人", aclAdded))
	}
	if len(msgs) == 0 {
		return "无需变更", nil
	}
	return strings.Join(msgs, "；"), nil
}

func (h *ResourceApplyHandler) applyItem(item ResourceApplyItem, applicantTID, username string) (string, error) {
	desired := strings.TrimSpace(item.Action)
	// 执行前按当前归属重算可见性类 action；审核人申请保留 approver
	owner, _, _, err := h.resolveResource(item.ResourceType, item.ResourceID)
	if err != nil {
		return "", err
	}
	if owner == "" {
		owner = store.DefaultTenantID
	}
	item.OwnerTenantID = owner
	if desired == raActionApprover {
		item.Action = raActionApprover
	} else {
		item.Action = classifyResourceAction(owner, applicantTID)
	}
	item.GrantType = grantResourceType(item.ResourceType)

	switch item.Action {
	case raActionACL:
		return h.applyACL(item, username)
	case raActionGrant:
		return h.applyGrant(item, applicantTID, username)
	case raActionAssign:
		return h.applyAssign(item, applicantTID)
	case raActionApprover:
		return h.applyApprover(item, applicantTID, username)
	default:
		return "", fmt.Errorf("未知 action: %s", item.Action)
	}
}

// ExecuteResourceApplyTicket 审批通过后由 TicketHandler 调用。
func (h *ResourceApplyHandler) ExecuteResourceApplyTicket(ctx context.Context, it *store.Issue) (*ticketexec.Result, error) {
	_ = ctx
	if it == nil {
		return nil, fmt.Errorf("工单不存在")
	}
	started := timeNowUTC()
	if payload, err := parseResourceApplyPayload(it.SQLContent); err != nil {
		return ticketexec.FailBeforeExec(err.Error(), ticketexec.Record{
			Host: "-", DB: "-", SQL: "(resource_apply)", Status: "failed",
		}), nil
	} else {
		applicantTID := strings.TrimSpace(it.TenantID)
		if applicantTID == "" {
			applicantTID = strings.TrimSpace(h.Store.RowTenantID("tbl_issues", it.ID))
		}
		if applicantTID == "" {
			applicantTID = store.DefaultTenantID
		}
		username := strings.TrimSpace(it.Submitter)
		records := make([]ticketexec.Record, 0, len(payload.Items))
		okCount, failCount := 0, 0
		for i, item := range payload.Items {
			// InstanceID 仅填真实实例 ID；勿把 resource_id（库/域名等）误写入，否则详情页会 GET /instances/{库ID} 报「实例不存在」
			instID := ""
			if item.ResourceType == raTypeDatabase {
				if db, _ := h.Store.GetDatabase(item.ResourceID); db != nil {
					instID = strings.TrimSpace(db.InstanceID)
				}
			} else if item.ResourceType == raTypeHost {
				instID = strings.TrimSpace(item.ResourceID)
			}
			rec := ticketexec.Record{
				InstanceID: instID,
				Host:       item.ResourceType,
				DB:         item.DisplayName,
				SQL:        fmt.Sprintf("%s -> %s", item.ResourceID, item.Action),
				Order:      i + 1,
			}
			msg, err := h.applyItem(item, applicantTID, username)
			if err != nil {
				failCount++
				rec.Status = "failed"
				rec.ErrorMessage = err.Error()
			} else {
				okCount++
				rec.Status = "success"
				rec.ErrorMessage = msg
			}
			records = append(records, rec)
		}
		res := &ticketexec.Result{
			Success:      failCount == 0 || okCount > 0,
			Message:      fmt.Sprintf("资源申请执行完成：成功 %d，失败 %d", okCount, failCount),
			Summary:      fmt.Sprintf("成功 %d，失败 %d", okCount, failCount),
			StartedAt:    started,
			EndedAt:      timeNowUTC(),
			SuccessCount: okCount,
			FailCount:    failCount,
			Records:      records,
			Engine:       "resource_apply",
		}
		if failCount > 0 && okCount == 0 {
			res.Success = false
		}
		if failCount > 0 && okCount > 0 {
			res.Message = fmt.Sprintf("资源申请部分成功：成功 %d，失败 %d", okCount, failCount)
		}
		return res, nil
	}
}

func timeNowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}
