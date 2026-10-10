package store

import "strings"

// RowVisibleInTenant 判断 table.id 行是否归属 tenantID（空 tenantID 不过滤）。
func (d *DB) RowVisibleInTenant(table, id, tenantID string) (bool, error) {
	if strings.TrimSpace(tenantID) == "" {
		return true, nil
	}
	return d.rowVisibleInTenant(table, id, tenantID)
}

// tenantIDSet 返回 table 中归属 tenantID 的 id 集合；tenantID 为空返回 nil（表示不过滤）。
func (d *DB) tenantIDSet(table, tenantID string) (map[string]bool, error) {
	tid := strings.TrimSpace(tenantID)
	if tid == "" {
		return nil, nil
	}
	rows, err := d.Query(`SELECT id FROM `+table+` WHERE COALESCE(tenant_id,'')=?`, tid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

func filterByIDSet[T any](list []T, set map[string]bool, key func(*T) string) []T {
	if set == nil {
		return list
	}
	out := make([]T, 0, len(list))
	for i := range list {
		if set[key(&list[i])] {
			out = append(out, list[i])
		}
	}
	return out
}

func (d *DB) ListSrAgentsInTenant(tenantID string) ([]SrAgent, error) {
	list, err := d.ListSrAgents()
	if err != nil {
		return nil, err
	}
	set, err := d.tenantIDSet("tbl_sr_agents", tenantID)
	if err != nil {
		return nil, err
	}
	return filterByIDSet(list, set, func(a *SrAgent) string { return a.ID }), nil
}

func (d *DB) GetSrAgentInTenant(id, tenantID string) (*SrAgent, error) {
	a, err := d.GetSrAgent(id)
	return ownedInTenant(d, a, err, "tbl_sr_agents", id, tenantID)
}

func (d *DB) ListSrAgentDeploysInTenant(tenantID, hostInstanceID string, limit int) ([]SrAgentDeploy, error) {
	list, err := d.ListSrAgentDeploys(hostInstanceID, limit)
	if err != nil {
		return nil, err
	}
	set, err := d.tenantIDSet("tbl_sr_agent_deploys", tenantID)
	if err != nil {
		return nil, err
	}
	return filterByIDSet(list, set, func(a *SrAgentDeploy) string { return a.ID }), nil
}

func (d *DB) GetSrAgentDeployInTenant(id, tenantID string) (*SrAgentDeploy, error) {
	a, err := d.GetSrAgentDeploy(id)
	return ownedInTenant(d, a, err, "tbl_sr_agent_deploys", id, tenantID)
}

func (d *DB) ListSrChangesInTenant(tenantID, clusterID string, limit int) ([]SrChange, error) {
	if strings.TrimSpace(tenantID) == "" {
		return d.ListSrChanges(clusterID, limit)
	}
	set, err := d.tenantIDSet("tbl_sr_clusters", tenantID)
	if err != nil {
		return nil, err
	}
	if clusterID != "" && !set[clusterID] {
		return []SrChange{}, nil
	}
	list, err := d.ListSrChanges(clusterID, limit)
	if err != nil {
		return nil, err
	}
	return filterByIDSet(list, set, func(ch *SrChange) string { return ch.ClusterID }), nil
}

// GetSrChangeInTenant 以所属集群的租户判断可见性。
func (d *DB) GetSrChangeInTenant(id, tenantID string) (*SrChange, error) {
	ch, err := d.GetSrChange(id)
	if err != nil || ch == nil || strings.TrimSpace(tenantID) == "" {
		return ch, err
	}
	ok, err := d.rowVisibleInTenant("tbl_sr_clusters", ch.ClusterID, tenantID)
	if err != nil || !ok {
		return nil, err
	}
	return ch, nil
}

func (d *DB) ListDbDeployAgentsInTenant(tenantID string) ([]DbDeployAgent, error) {
	list, err := d.ListDbDeployAgents()
	if err != nil {
		return nil, err
	}
	set, err := d.tenantIDSet("tbl_db_deploy_agents", tenantID)
	if err != nil {
		return nil, err
	}
	return filterByIDSet(list, set, func(a *DbDeployAgent) string { return a.ID }), nil
}

func (d *DB) GetDbDeployAgentInTenant(id, tenantID string) (*DbDeployAgent, error) {
	a, err := d.GetDbDeployAgent(id)
	return ownedInTenant(d, a, err, "tbl_db_deploy_agents", id, tenantID)
}

func (d *DB) ListHAControllersInTenant(tenantID, engine, envID string) ([]HAController, error) {
	list, err := d.ListHAControllers(engine, envID)
	if err != nil {
		return nil, err
	}
	set, err := d.tenantIDSet("tbl_ha_controllers", tenantID)
	if err != nil {
		return nil, err
	}
	return filterByIDSet(list, set, func(c *HAController) string { return c.ID }), nil
}

func (d *DB) GetHAControllerInTenant(id, tenantID string) (*HAController, error) {
	c, err := d.GetHAController(id)
	return ownedInTenant(d, c, err, "tbl_ha_controllers", id, tenantID)
}

func (d *DB) ListHATasksInTenant(tenantID, clusterID string, limit int) ([]HATask, error) {
	if strings.TrimSpace(tenantID) == "" {
		return d.ListHATasks(clusterID, limit)
	}
	set, err := d.tenantIDSet("tbl_ha_clusters", tenantID)
	if err != nil {
		return nil, err
	}
	if clusterID != "" && !set[clusterID] {
		return []HATask{}, nil
	}
	list, err := d.ListHATasks(clusterID, limit)
	if err != nil {
		return nil, err
	}
	return filterByIDSet(list, set, func(t *HATask) string { return t.ClusterID }), nil
}

func (d *DB) GetSlotInTenant(id, tenantID string) (bool, error) {
	return d.RowVisibleInTenant("tbl_sync_slots", id, tenantID)
}

// ListArchiveRunsInTenant 按归档任务租户过滤执行记录。
func (d *DB) ListArchiveRunsInTenant(tenantID, taskID string, limit int) ([]ArchiveRun, error) {
	if strings.TrimSpace(tenantID) == "" {
		return d.ListArchiveRuns(taskID, limit)
	}
	set, err := d.tenantIDSet("tbl_archive_tasks", tenantID)
	if err != nil {
		return nil, err
	}
	if taskID != "" && !set[taskID] {
		return []ArchiveRun{}, nil
	}
	list, err := d.ListArchiveRuns(taskID, limit)
	if err != nil {
		return nil, err
	}
	return filterByIDSet(list, set, func(r *ArchiveRun) string { return r.TaskID }), nil
}

func (d *DB) GetCmdbTagDefInTenant(id, tenantID string) (*CmdbTagDef, error) {
	r, err := d.GetCmdbTagDef(id)
	return ownedInTenant(d, r, err, "tbl_cmdb_tag_defs", id, tenantID)
}

// TenantOfInstance 返回实例所属租户（不存在则空）。
func (d *DB) TenantOfInstance(instanceID string) string {
	return d.inheritTenantFromInstance(instanceID)
}

// inheritTenantFromParent 子行沿用父行的 tenant_id（父行无租户时保持列默认值）。
func (d *DB) inheritTenantFromParent(childTable, childID, parentTable, parentID string) error {
	tid, err := d.lookupRowTenantID(parentTable, parentID)
	if err != nil || tid == "" {
		return err
	}
	return d.AssignTenantID(childTable, childID, tid)
}
