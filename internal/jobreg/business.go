package jobreg

import (
	"strings"

	"auto-dba/internal/store"
)

// ListBusinessSchedules 聚合业务侧调度实体（只读深链）。
func ListBusinessSchedules(db *store.DB, tenantID string) ([]BusinessSchedule, error) {
	if db == nil {
		return nil, nil
	}
	var out []BusinessSchedule

	pols, err := db.ListBackupPoliciesInTenant(tenantID, "")
	if err == nil {
		for _, p := range pols {
			out = append(out, BusinessSchedule{
				Kind: "backup_policy", ID: p.ID, Name: p.Name,
				ScheduleExpr: p.ScheduleExpr, Enabled: p.Enabled,
				LastRunAt: db.LastBackupRunAt(p.ID),
				Link:      "/ops/backup/policies",
				TenantID:  p.TenantID,
			})
		}
	}

	arcs, err := db.ListArchives()
	if err == nil {
		for i := range arcs {
			t := &arcs[i]
			if tenantID != "" && t.TenantID != "" && t.TenantID != tenantID {
				continue
			}
			if store.ArchiveRunMode(t) != "scheduled" && strings.TrimSpace(t.ScheduleExpr) == "" {
				continue
			}
			expr := strings.TrimSpace(t.ScheduleExpr)
			if expr == "" {
				expr = store.ParseArchiveExtra(t).ScheduleExpr
			}
			out = append(out, BusinessSchedule{
				Kind: "archive_task", ID: t.ID, Name: t.Name,
				ScheduleExpr: expr, Enabled: t.Status != store.StatusPaused,
				Status: t.Status, LastRunAt: db.LastArchiveRunAt(t.ID),
				Link: "/ops/archive/tasks", TenantID: t.TenantID,
			})
		}
	}

	jobs, err := db.ListCmdbSyncJobs()
	if err == nil {
		for _, j := range jobs {
			out = append(out, BusinessSchedule{
				Kind: "cmdb_sync", ID: j.ID, Name: j.Name,
				ScheduleExpr: j.Cron, Enabled: j.Enabled,
				Link: "/cmdb/sync", Extra: j.Adapter,
			})
		}
	}

	migs, err := db.ListMigrationsInTenant(tenantID)
	if err == nil {
		for _, m := range migs {
			if strings.TrimSpace(m.ScheduledAt) == "" && m.Status != store.StatusScheduled {
				continue
			}
			name := m.Name
			if name == "" {
				name = m.ID
			}
			out = append(out, BusinessSchedule{
				Kind: "migration", ID: m.ID, Name: name,
				ScheduleExpr: m.ScheduledAt, Enabled: m.Status == store.StatusScheduled,
				Status: m.Status, Link: "/ops/migration/" + m.ID,
			})
		}
	}

	return out, nil
}
