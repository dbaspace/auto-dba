package cmdb

import (
	"strings"
)

func (s *Service) annotateAppTags(list []CI) {
	for i := range list {
		var apps []string
		for _, t := range list[i].Tags {
			tl := strings.ToLower(strings.TrimSpace(t))
			if strings.HasPrefix(tl, "app=") || strings.HasPrefix(tl, "biz=") {
				apps = append(apps, t)
			}
		}
		list[i].AppTags = apps
	}
}

func hasApprovers(attrs map[string]any) bool {
	if attrs == nil {
		return false
	}
	switch v := attrs["approvers"].(type) {
	case []string:
		return len(v) > 0
	case []any:
		return len(v) > 0
	default:
		return false
	}
}

// annotateHealth 为 instance/database 附加计算型质量徽章。
func (s *Service) annotateHealth(list []CI, tenantID string) {
	if len(list) == 0 {
		return
	}
	dbCountByInst := map[string]int{}
	dbs, _ := s.Store.ListDatabasesInTenant(tenantID, "", "")
	for _, db := range dbs {
		dbCountByInst[db.InstanceID]++
	}
	for i := range list {
		var badges []string
		switch list[i].Type {
		case TypeInstance:
			if dbCountByInst[list[i].ID] == 0 {
				badges = append(badges, "无库登记")
			}
		case TypeDatabase:
			if !hasApprovers(list[i].Attrs) {
				badges = append(badges, "无审批人")
			}
			if tid, _ := list[i].Attrs["tenant_id"].(string); strings.TrimSpace(tid) == "" {
				badges = append(badges, "无租户")
			}
		}
		list[i].HealthBadges = badges
	}
}

// HealthOverview 全局质量概览（发现页用）。
type HealthOverview struct {
	MonitorUnmatched   int `json:"monitor_unmatched"`
	InstanceNoDB       int `json:"instance_no_db"`
	DatabaseNoApprover int `json:"database_no_approver"`
}

func (s *Service) HealthOverview(tenantID string) (*HealthOverview, error) {
	ov := &HealthOverview{}
	cands, err := s.DiscoverFromMonitorInTenant(tenantID)
	if err != nil {
		return nil, err
	}
	for _, c := range cands {
		if c.MatchedID == "" {
			ov.MonitorUnmatched++
		}
	}
	insts, err := s.Store.ListInstancesInTenant(tenantID, "", "")
	if err != nil {
		return nil, err
	}
	dbs, err := s.Store.ListDatabasesInTenant(tenantID, "", "")
	if err != nil {
		return nil, err
	}
	dbByInst := map[string]int{}
	for _, db := range dbs {
		dbByInst[db.InstanceID]++
		if len(db.Approvers) == 0 {
			ov.DatabaseNoApprover++
		}
	}
	for _, it := range insts {
		if dbByInst[it.ID] == 0 {
			ov.InstanceNoDB++
		}
	}
	return ov, nil
}
