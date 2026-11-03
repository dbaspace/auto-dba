package store

import (
	"fmt"
	"strings"
	"time"
)

// CDCAssignResult 一棵 CDC 任务树（初始化任务 + 子任务）的划拨结果。
type CDCAssignResult struct {
	RootID string
	Moved  int
}

// cdcTaskTreeIDs 从任意任务回溯到根任务，再收集根及全部后代。
func (d *DB) cdcTaskTreeIDs(id string) (*CDCTask, []string, error) {
	t, err := d.GetCDC(id)
	if err != nil {
		return nil, nil, err
	}
	if t == nil {
		return nil, nil, fmt.Errorf("任务不存在")
	}
	seen := map[string]bool{t.ID: true}
	for strings.TrimSpace(t.ParentID) != "" {
		p, err := d.GetCDC(t.ParentID)
		if err != nil {
			return nil, nil, err
		}
		if p == nil || seen[p.ID] {
			break
		}
		seen[p.ID] = true
		t = p
	}
	ids := []string{t.ID}
	visited := map[string]bool{t.ID: true}
	for i := 0; i < len(ids); i++ {
		kids, err := d.ListCDCByParentID(ids[i], "")
		if err != nil {
			return nil, nil, err
		}
		for _, k := range kids {
			if visited[k.ID] {
				continue
			}
			visited[k.ID] = true
			ids = append(ids, k.ID)
		}
	}
	return t, ids, nil
}

// AssignCDCTaskTenant 把 CDC 任务所在整棵任务树划拨到目标租户；日志、配置版本按 task_id 关联自动跟随。
func (d *DB) AssignCDCTaskTenant(id, toTenantID string) (CDCAssignResult, error) {
	var res CDCAssignResult
	id = strings.TrimSpace(id)
	toTenantID = strings.TrimSpace(toTenantID)
	if id == "" {
		return res, fmt.Errorf("任务必填")
	}
	if err := d.requireActiveTenant(toTenantID); err != nil {
		return res, err
	}
	root, ids, err := d.cdcTaskTreeIDs(id)
	if err != nil {
		return res, err
	}
	res.RootID = root.ID
	tx, err := d.db.Begin()
	if err != nil {
		return res, err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC().Format(time.RFC3339)
	for _, tid := range ids {
		r, err := tx.Exec(d.rewrite(`UPDATE tbl_cdc_tasks SET tenant_id=?, updated_at=? WHERE id=? AND COALESCE(tenant_id,'')<>?`),
			toTenantID, now, tid, toTenantID)
		if err != nil {
			return res, err
		}
		if n, _ := r.RowsAffected(); n > 0 {
			res.Moved += int(n)
		}
	}
	if err := tx.Commit(); err != nil {
		return res, err
	}
	return res, nil
}

// CDCTaskTenantIDs 批量读取任务归属租户。
func (d *DB) CDCTaskTenantIDs(ids []string) (map[string]string, error) {
	out := map[string]string{}
	const chunk = 500
	for start := 0; start < len(ids); start += chunk {
		end := start + chunk
		if end > len(ids) {
			end = len(ids)
		}
		part := ids[start:end]
		if len(part) == 0 {
			continue
		}
		args := make([]any, 0, len(part))
		for _, id := range part {
			args = append(args, id)
		}
		q := `SELECT id, COALESCE(tenant_id,'') FROM tbl_cdc_tasks WHERE id IN (?` + strings.Repeat(",?", len(part)-1) + `)`
		rows, err := d.Query(q, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, tid string
			if err := rows.Scan(&id, &tid); err != nil {
				rows.Close()
				return nil, err
			}
			out[id] = strings.TrimSpace(tid)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// InstanceVisibleToTenant 目标租户能否看到实例：归属、实例上有本租户登记库，或租户级实例授权。
func (d *DB) InstanceVisibleToTenant(instanceID, tenantID string) (bool, error) {
	instanceID = strings.TrimSpace(instanceID)
	tenantID = strings.TrimSpace(tenantID)
	if instanceID == "" || tenantID == "" {
		return true, nil
	}
	it, err := d.GetInstance(instanceID)
	if err != nil || it == nil {
		return false, err
	}
	if strings.TrimSpace(it.TenantID) == tenantID {
		return true, nil
	}
	n, err := d.CountDatabasesOnInstanceInTenant(instanceID, tenantID)
	if err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}
	return d.HasResourceGrant(tenantID, "", "instance", instanceID)
}
