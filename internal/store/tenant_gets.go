package store

import "strings"

func (d *DB) GetAlertRuleInTenant(id, tenantID string) (*AlertRule, error) {
	r, err := d.GetAlertRule(id)
	return ownedInTenant(d, r, err, "tbl_alert_rules", id, tenantID)
}

func (d *DB) GetAlertInTenant(id, tenantID string) (*Alert, error) {
	a, err := d.GetAlert(id)
	return ownedInTenant(d, a, err, "tbl_alerts", id, tenantID)
}

func (d *DB) GetAlertSilenceInTenant(id, tenantID string) (*AlertSilence, error) {
	s, err := d.GetAlertSilence(id)
	return ownedInTenant(d, s, err, "tbl_alert_silences", id, tenantID)
}

func (d *DB) GetSrClusterInTenant(id, tenantID string) (*SrCluster, error) {
	c, err := d.GetSrCluster(id)
	return ownedInTenant(d, c, err, "tbl_sr_clusters", id, tenantID)
}

func (d *DB) GetDbDeployClusterInTenant(id, tenantID string) (*DbDeployCluster, error) {
	c, err := d.GetDbDeployCluster(id)
	return ownedInTenant(d, c, err, "tbl_db_deploy_clusters", id, tenantID)
}

func (d *DB) GetCDCInTenant(id, tenantID string) (*CDCTask, error) {
	t, err := d.GetCDC(id)
	return ownedInTenant(d, t, err, "tbl_cdc_tasks", id, tenantID)
}

func (d *DB) GetMigrationInTenant(id, tenantID string) (*MigrationTask, error) {
	t, err := d.GetMigration(id)
	return ownedInTenant(d, t, err, "tbl_migration_tasks", id, tenantID)
}

func (d *DB) GetFlashbackInTenant(id, tenantID string) (*FlashbackTask, error) {
	t, err := d.GetFlashback(id)
	return ownedInTenant(d, t, err, "tbl_flashback_tasks", id, tenantID)
}

func (d *DB) GetArchiveInTenant(id, tenantID string) (*ArchiveTask, error) {
	t, err := d.GetArchive(id)
	return ownedInTenant(d, t, err, "tbl_archive_tasks", id, tenantID)
}

func (d *DB) GetHAClusterInTenant(id, tenantID string) (*HACluster, error) {
	t, err := d.GetHACluster(id)
	return ownedInTenant(d, t, err, "tbl_ha_clusters", id, tenantID)
}

func (d *DB) GetProxyServiceInTenant(id, tenantID string) (*ProxyService, error) {
	t, err := d.GetProxyService(id)
	return ownedInTenant(d, t, err, "tbl_proxy_services", id, tenantID)
}

func (d *DB) GetBackupRunInTenant(id, tenantID string) (*BackupRun, error) {
	r, err := d.GetBackupRun(id)
	return ownedInTenant(d, r, err, "tbl_backup_runs", id, tenantID)
}

func (d *DB) GetBackupAgentDeployInTenant(id, tenantID string) (*BackupAgentDeploy, error) {
	it, err := d.GetBackupAgentDeploy(id)
	return ownedInTenant(d, it, err, "tbl_backup_agent_deploys", id, tenantID)
}

func (d *DB) GetDbDeployChangeInTenant(id, tenantID string) (*DbDeployChange, error) {
	ch, err := d.GetDbDeployChange(id)
	if err != nil || ch == nil {
		return ch, err
	}
	ok, err := d.rowVisibleInTenant("tbl_db_deploy_changes", id, tenantID)
	if err != nil || !ok {
		// 兼容未回填 tenant_id 的变更行：按所属集群租户判定
		if err == nil && !ok && strings.TrimSpace(ch.ClusterID) != "" {
			cl, cerr := d.GetDbDeployClusterInTenant(ch.ClusterID, tenantID)
			if cerr != nil {
				return nil, cerr
			}
			if cl != nil {
				return ch, nil
			}
		}
		return nil, err
	}
	return ch, nil
}

func (d *DB) GetDbDeployAgentDeployInTenant(id, tenantID string) (*DbDeployAgentDeploy, error) {
	it, err := d.GetDbDeployAgentDeploy(id)
	return ownedInTenant(d, it, err, "tbl_db_deploy_agent_deploys", id, tenantID)
}
