package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// ensureTenancy 建租户相关表、给业务表加 tenant_id、回填默认租户与成员。
func (d *DB) ensureTenancy() error {
	if err := d.ensureTenantTables(); err != nil {
		return err
	}
	if err := d.ensureBusinessTenantIDColumns(); err != nil {
		return err
	}
	if err := d.ensureInstanceTenantUnique(); err != nil {
		return err
	}
	if err := d.ensureTenantScopedUniques(); err != nil {
		return err
	}
	return d.bootstrapDefaultTenant()
}

func (d *DB) ensureTenantTables() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS tbl_tenants (
			id TEXT PRIMARY KEY,
			code TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'active',
			plan TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_tenant_members (
			tenant_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			role_ids TEXT NOT NULL DEFAULT '[]',
			status TEXT NOT NULL DEFAULT 'active',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (tenant_id, user_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tenant_members_user ON tbl_tenant_members(user_id)`,
		`CREATE TABLE IF NOT EXISTS tbl_tenant_grants (
			id TEXT PRIMARY KEY,
			from_tenant_id TEXT NOT NULL,
			to_tenant_id TEXT,
			to_user_id TEXT,
			resource_type TEXT NOT NULL,
			resource_id TEXT NOT NULL,
			permission TEXT NOT NULL DEFAULT 'read',
			created_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			expires_at TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tenant_grants_from ON tbl_tenant_grants(from_tenant_id)`,
		`CREATE INDEX IF NOT EXISTS idx_tenant_grants_to_tenant ON tbl_tenant_grants(to_tenant_id)`,
		`CREATE INDEX IF NOT EXISTS idx_tenant_grants_resource ON tbl_tenant_grants(resource_type, resource_id)`,
		`CREATE TABLE IF NOT EXISTS tbl_tenant_audit_logs (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			actor_user_id TEXT NOT NULL DEFAULT '',
			action TEXT NOT NULL,
			detail TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tenant_audit_tenant ON tbl_tenant_audit_logs(tenant_id, created_at DESC)`,
	}
	for _, s := range stmts {
		if _, err := d.Exec(s); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) ensureColumnIfMissing(table, col, defSQL string) error {
	var n int
	err := d.db.QueryRow(`
		SELECT COUNT(1) FROM information_schema.columns
		WHERE table_name = $1 AND column_name = $2
	`, table, col).Scan(&n)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err = d.db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, col, defSQL))
	return err
}

func (d *DB) ensureIndexIfMissing(name, ddl string) error {
	_, err := d.Exec(ddl)
	return err
}

// ensureTenantScopedUniques 把按环境/键全局唯一的租户表改为租户内唯一。
func (d *DB) ensureTenantScopedUniques() error {
	items := []struct {
		table, oldName, oldDef, index, cols string
	}{
		{"tbl_oss_settings", "tbl_oss_settings_environment_key", "UNIQUE (environment)", "uq_oss_settings_tenant_env", "tenant_id, environment"},
		{"tbl_ds_admin", "tbl_ds_admin_environment_key", "UNIQUE (environment)", "uq_ds_admin_tenant_env", "tenant_id, environment"},
		{"tbl_nacos_admin", "tbl_nacos_admin_environment_key", "UNIQUE (environment)", "uq_nacos_admin_tenant_env", "tenant_id, environment"},
		{"tbl_fe_admin", "tbl_fe_admin_environment_domain_username_key", "UNIQUE (environment, domain, username)", "uq_fe_admin_tenant_env_domain_user", "tenant_id, environment, domain, username"},
		{"tbl_cmdb_tag_defs", "tbl_cmdb_tag_defs_key_key", "UNIQUE (key)", "uq_cmdb_tag_defs_tenant_key", "tenant_id, key"},
	}
	for _, it := range items {
		var n int
		if err := d.db.QueryRow(`
			SELECT COUNT(1) FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = $1
		`, it.table).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			continue
		}
		// 索引依赖 tenant_id；表若由 skip 路径先建出，这里先补列再建索引。
		if err := d.ensureColumnIfMissing(it.table, "tenant_id", "TEXT NOT NULL DEFAULT '"+DefaultTenantID+"'"); err != nil {
			return fmt.Errorf("%s.tenant_id: %w", it.table, err)
		}
		if it.oldName != "" {
			_, _ = d.db.Exec(`ALTER TABLE ` + it.table + ` DROP CONSTRAINT IF EXISTS ` + it.oldName)
			_, _ = d.db.Exec(`DROP INDEX IF EXISTS ` + it.oldName)
		}
		if err := d.dropUniqueConstraintByDef(it.table, it.oldDef); err != nil {
			return err
		}
		if err := d.ensureIndexIfMissing(it.index,
			`CREATE UNIQUE INDEX IF NOT EXISTS `+it.index+` ON `+it.table+`(`+it.cols+`)`); err != nil {
			return err
		}
	}
	return nil
}

// dropUniqueConstraintByDef 按约束定义（如 "UNIQUE (key)"）删除表上的唯一约束，不依赖约束名。
// 保留字列（如 key）在 pg_get_constraintdef 中会变成 UNIQUE ("key")，比对前会去掉引号与空白。
func (d *DB) dropUniqueConstraintByDef(table, def string) error {
	norm := func(s string) string {
		s = strings.ToLower(strings.TrimSpace(s))
		s = strings.ReplaceAll(s, `"`, "")
		s = strings.ReplaceAll(s, " ", "")
		return s
	}
	want := norm(def)
	// 常见 PG 自动命名：table_col_key；保留字列也一并尝试。
	_, _ = d.db.Exec(`ALTER TABLE ` + table + ` DROP CONSTRAINT IF EXISTS ` + table + `_key_key`)
	rows, err := d.db.Query(`SELECT conname, pg_get_constraintdef(oid) FROM pg_constraint
		WHERE conrelid = to_regclass($1)::regclass AND contype = 'u'`, table)
	if err != nil {
		return err
	}
	var names []string
	for rows.Next() {
		var name, got string
		if err := rows.Scan(&name, &got); err != nil {
			rows.Close()
			return err
		}
		if norm(got) == want {
			names = append(names, name)
		}
	}
	rows.Close()
	for _, n := range names {
		if _, err := d.db.Exec(`ALTER TABLE ` + table + ` DROP CONSTRAINT IF EXISTS "` + n + `"`); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) ensureInstanceTenantUnique() error {
	_, _ = d.db.Exec(`ALTER TABLE tbl_instances DROP CONSTRAINT IF EXISTS tbl_instances_domain_name_port_key`)
	_, _ = d.db.Exec(`DROP INDEX IF EXISTS tbl_instances_domain_name_port_key`)
	return d.ensureIndexIfMissing("uq_instances_tenant_domain_port",
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_instances_tenant_domain_port ON tbl_instances(tenant_id, domain_name, port)`)
}

// tenantScopedTables 需行级 tenant_id 的业务表（P0/P1）。
var tenantScopedTables = []string{
	"tbl_instances",
	"tbl_databases",
	"tbl_db_accounts",
	"tbl_issues",
	"tbl_todos",
	"tbl_alert_rules",
	"tbl_alerts",
	"tbl_alert_silences",
	"tbl_cdc_tasks",
	"tbl_db_deploy_clusters",
	"tbl_db_deploy_agents",
	"tbl_db_deploy_agent_deploys",
	"tbl_sr_clusters",
	"tbl_sr_agents",
	"tbl_sr_agent_deploys",
	"tbl_terminal_sessions",
	"tbl_backup_policies",
	"tbl_backup_runs",
	"tbl_backup_agents",
	"tbl_backup_agent_deploys",
	"tbl_k8s_clusters",
	"tbl_k8s_namespace_bindings",
	"tbl_k8s_audit_logs",
	"tbl_k8s_operators",
	"tbl_k8s_db_clusters",
	"tbl_ha_controllers",
	"tbl_ha_clusters",
	"tbl_proxy_services",
	"tbl_oss_settings",
	"tbl_fe_admin",
	"tbl_ds_admin",
	"tbl_nacos_admin",
	"tbl_kafka_orders",
	"tbl_connectors",
	"tbl_migration_tasks",
	"tbl_flashback_tasks",
	"tbl_archive_tasks",
	"tbl_schema_compare_tasks",
	"tbl_archive_runs",
	"tbl_backup_storages",
	"tbl_ha_members",
	"tbl_ha_tasks",
	"tbl_db_deploy_nodes",
	"tbl_db_deploy_changes",
	"tbl_db_deploy_cluster_confs",
	"tbl_sr_nodes",
	"tbl_sr_changes",
	"tbl_sr_cluster_configs",
	"tbl_sr_node_configs",
	"tbl_sr_conf_revisions",
	"tbl_todo_logs",
	"tbl_todo_subtasks",
	"tbl_todo_images",
	"tbl_alert_events",
	"tbl_cdc_sync_logs",
	"tbl_cdc_connector_config_versions",
	"tbl_proxy_conf_versions",
	"tbl_cmdb_relations",
	"tbl_cmdb_tag_defs",
	"tbl_cmdb_tag_bindings",
	"tbl_cmdb_audit_logs",
	"tbl_cmdb_sync_jobs",
	"tbl_cmdb_sync_runs",
	"tbl_cmdb_ci_snapshots",
	"tbl_sync_slots",
	"tbl_query_audits",
	"tbl_multilang_tasks",
	"tbl_insight_jobs",
	"tbl_monitor_job",
	"tbl_monitor_target_group",
	"tbl_monitor_label_tags",
	"tbl_db_awr_reports",
	"tbl_db_awr_generate_jobs",
	"tbl_sql_insight_metrics",
	"tbl_sql_insight_rules",
	"tbl_sql_insight_analysis_runs",
	"tbl_sql_insight_findings",
	"tbl_sql_insight_observations",
	"tbl_sql_insight_metric_baselines",
	"tbl_sql_insight_cases",
	"tbl_pg_space_snapshots",
	"tbl_pg_space_collect_jobs",
	"tbl_pg_space_advice_latest",
	"tbl_pg_activity_snapshots",
}

func (d *DB) ensureBusinessTenantIDColumns() error {
	for _, table := range tenantScopedTables {
		var n int
		err := d.db.QueryRow(`
			SELECT COUNT(1) FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = $1
		`, table).Scan(&n)
		if err != nil {
			return err
		}
		if n == 0 {
			continue
		}
		if err := d.ensureColumnIfMissing(table, "tenant_id", "TEXT NOT NULL DEFAULT '"+DefaultTenantID+"'"); err != nil {
			return fmt.Errorf("%s.tenant_id: %w", table, err)
		}
		idx := "idx_" + strings.TrimPrefix(table, "tbl_") + "_tenant"
		if err := d.ensureIndexIfMissing(idx, fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s(tenant_id)`, idx, table)); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) bootstrapDefaultTenant() error {
	now := time.Now().UTC().Format(time.RFC3339)
	exist, err := d.GetTenant(DefaultTenantID)
	if err != nil {
		return err
	}
	if exist == nil {
		byCode, err := d.GetTenantByCode(DefaultTenantCode)
		if err != nil {
			return err
		}
		if byCode == nil {
			if err := d.CreateTenant(&Tenant{
				ID:          DefaultTenantID,
				Code:        DefaultTenantCode,
				Name:        "默认租户",
				Status:      TenantStatusActive,
				Plan:        "standard",
				Description: "系统升级自动创建；历史数据归属此租户",
				CreatedAt:   now,
				UpdatedAt:   now,
			}); err != nil {
				return err
			}
			log.Printf("tenancy: created default tenant %s", DefaultTenantID)
		}
	}

	// 回填业务表空/缺失 tenant_id
	for _, table := range tenantScopedTables {
		var n int
		err := d.db.QueryRow(`
			SELECT COUNT(1) FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = $1
		`, table).Scan(&n)
		if err != nil {
			return err
		}
		if n == 0 {
			continue
		}
		if _, err := d.Exec(fmt.Sprintf(
			`UPDATE %s SET tenant_id=? WHERE tenant_id IS NULL OR TRIM(tenant_id)=''`, table),
			DefaultTenantID); err != nil {
			return fmt.Errorf("backfill %s: %w", table, err)
		}
	}

	if err := d.SyncDefaultTenantMemberships(); err != nil {
		return err
	}
	if err := d.CleanupDefaultMembershipsForBusinessMembers(); err != nil {
		return err
	}
	if err := d.CleanupOrphanTenantMemberships(); err != nil {
		return err
	}
	return d.RealignDeployInstanceTenants()
}

// SyncDefaultTenantMemberships 将尚未入组的用户加入默认租户；平台管理员即使已有其它租户成员也确保在默认租户内（幂等）。
func (d *DB) SyncDefaultTenantMemberships() error {
	now := time.Now().UTC().Format(time.RFC3339)
	rows, err := d.Query(`SELECT id, role_id, role FROM tbl_users`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type urow struct {
		id, roleID, role string
	}
	var users []urow
	for rows.Next() {
		var u urow
		var rid sql.NullString
		if err := rows.Scan(&u.id, &rid, &u.role); err != nil {
			return err
		}
		u.roleID = rid.String
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, u := range users {
		isAdmin, err := d.UserIsPlatformAdmin(u.id)
		if err != nil {
			return err
		}
		if isAdmin {
			if err := d.EnsurePlatformAdminDefaultMembership(u.id); err != nil {
				return err
			}
			continue
		}
		n, err := d.CountActiveMemberships(u.id)
		if err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		roleIDs, err := d.ListUserRoleIDs(u.id)
		if err != nil {
			return err
		}
		if len(roleIDs) == 0 && strings.TrimSpace(u.roleID) != "" {
			roleIDs = []string{u.roleID}
		}
		if len(roleIDs) == 0 {
			if r, _ := d.GetRoleByCode(strings.TrimSpace(u.role)); r != nil {
				roleIDs = []string{r.ID}
			}
		}
		if len(roleIDs) == 0 {
			if r, _ := d.GetRoleByCode("viewer"); r != nil {
				roleIDs = []string{r.ID}
			}
		}
		if len(roleIDs) == 0 {
			continue
		}
		if err := d.UpsertTenantMember(&TenantMember{
			TenantID:  DefaultTenantID,
			UserID:    u.id,
			RoleIDs:   roleIDs,
			Status:    MemberStatusActive,
			CreatedAt: now,
			UpdatedAt: now,
		}); err != nil {
			return err
		}
	}
	return nil
}

// MemberRoleIDsJSON 供测试/调试。
func MemberRoleIDsJSON(ids []string) string {
	b, _ := json.Marshal(uniqueTrimmed(ids))
	return string(b)
}
