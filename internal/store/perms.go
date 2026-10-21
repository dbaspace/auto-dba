package store

import (
	"database/sql"
	"errors"
	"strings"
)

// 平台动作 URN 约定：um.action.{resource}.{create|edit|delete}
const (
	URNInstancesCreate    = "um.action.instances.create"
	URNInstancesEdit      = "um.action.instances.edit"
	URNInstancesDelete    = "um.action.instances.delete"
	URNDatabasesCreate    = "um.action.databases.create"
	URNDatabasesEdit      = "um.action.databases.edit"
	URNDatabasesDelete    = "um.action.databases.delete"
	URNAccountsCreate     = "um.action.accounts.create"
	URNAccountsEdit       = "um.action.accounts.edit"
	URNAccountsDelete     = "um.action.accounts.delete"
	URNRegionsCreate      = "um.action.regions.create"
	URNRegionsEdit        = "um.action.regions.edit"
	URNRegionsBind        = "um.action.regions.bind"
	URNRegionsDelete      = "um.action.regions.delete"
	URNEnvironmentsCreate = "um.action.environments.create"
	URNEnvironmentsEdit   = "um.action.environments.edit"
	URNEnvironmentsDelete = "um.action.environments.delete"
	URNMonitorTagsCreate  = "um.action.monitor-tags.create"
	URNMonitorTagsEdit    = "um.action.monitor-tags.edit"
	URNMonitorTagsDelete  = "um.action.monitor-tags.delete"
	URNConnectorsCreate   = "um.action.connectors.create"
	URNConnectorsEdit     = "um.action.connectors.edit"
	URNConnectorsDelete   = "um.action.connectors.delete"
	URNReviewRulesCreate  = "um.action.review-rules.create"
	URNReviewRulesEdit    = "um.action.review-rules.edit"
	URNReviewRulesDelete  = "um.action.review-rules.delete"
	URNArgsCreate         = "um.action.args.create"
	URNArgsEdit           = "um.action.args.edit"
	URNArgsDelete         = "um.action.args.delete"

	URNSidebarScheduledJobs   = "um.sidebar.config-scheduled-jobs"
	URNScheduledJobsEdit      = "um.action.scheduled-jobs.edit"
	URNScheduledJobsTrigger   = "um.action.scheduled-jobs.trigger"
	URNUsersCreate        = "um.action.users.create"
	URNUsersEdit          = "um.action.users.edit"
	URNUsersDelete        = "um.action.users.delete"
	URNRolesCreate        = "um.action.roles.create"
	URNRolesEdit          = "um.action.roles.edit"
	URNRolesDelete        = "um.action.roles.delete"
	URNMenusCreate        = "um.action.menus.create"
	URNMenusEdit          = "um.action.menus.edit"
	URNMenusDelete        = "um.action.menus.delete"
	URNLdapEdit           = "um.action.ldap.edit"

	URNSyncSlotsCollect = "um.action.sync-slots.collect"
	URNSyncSlotsDelete  = "um.action.sync-slots.delete"

	URNMigrationCreate  = "um.action.migration.create"
	URNMigrationEdit    = "um.action.migration.edit"
	URNMigrationDelete  = "um.action.migration.delete"
	URNMigrationApprove = "um.action.migration.approve"

	URNSchemaCompareCreate = "um.action.schema-compare.create"
	URNSchemaCompareDelete = "um.action.schema-compare.delete"

	URNCDCCreate = "um.action.cdc.create"
	URNCDCEdit   = "um.action.cdc.edit"
	URNCDCDelete = "um.action.cdc.delete"

	URNFlashbackCreate    = "um.action.flashback.create"
	URNFlashbackEdit      = "um.action.flashback.edit"
	URNFlashbackCloudEdit = "um.action.flashback-cloud.edit"

	URNArchiveCreate       = "um.action.archive.create"
	URNArchiveEdit         = "um.action.archive.edit"
	URNArchiveRun          = "um.action.archive.run"
	URNArchiveStop         = "um.action.archive.stop"
	URNArchiveSettingsEdit = "um.action.archive-settings.edit"

	URNCmdbEdit   = "um.action.cmdb.edit"
	URNCmdbImport = "um.action.cmdb.import"
	URNCmdbSync   = "um.action.cmdb.sync"

	URNSqlInsightCollect      = "um.action.sql-insight.collect"
	URNSqlInsightAwrGenerate  = "um.action.sql-insight.awr-generate"
	URNSqlInsightSpaceCollect = "um.action.sql-insight.space-collect"
	URNSqlInsightAnalyze      = "um.action.sql-insight.analyze"

	URNTodoCreate = "um.action.todo.create"
	URNTodoEdit   = "um.action.todo.edit"
	URNTodoFollow = "um.action.todo.follow"
	URNTodoDelete = "um.action.todo.delete"

	URNAlertClaim      = "um.action.alert.claim"
	URNAlertResolve    = "um.action.alert.resolve"
	URNAlertSilence    = "um.action.alert.silence"
	URNAlertRuleEdit   = "um.action.alert.rule.edit"
	URNAlertRuleDelete = "um.action.alert.rule.delete"

	URNCopilotChat     = "um.action.copilot.chat"
	URNRunbookExecute  = "um.action.runbook.execute"
	URNSidebarAIOpsKB        = "um.sidebar.ops-aiops-knowledge"
	URNSidebarAIOpsIncidents = "um.sidebar.ops-aiops-incidents"
	URNSidebarAIAssistant    = "um.sidebar.ops-ai-assistant"

	URNTenantsCreate      = "um.action.tenants.create"
	URNTenantsEdit        = "um.action.tenants.edit"
	URNTenantsDisable     = "um.action.tenants.disable"
	URNTenantsDelete      = "um.action.tenants.delete"
	URNTenantsMembers     = "um.action.tenants.members"
	URNTenantsGrants      = "um.action.tenants.grants"
	URNTenantsImpersonate = "um.action.tenants.impersonate"
	URNSidebarTenants     = "um.sidebar.tenants"

	URNSidebarConfigDomains = "um.sidebar.config-domains"
	URNDomainsSync          = "um.action.domains.sync"
	URNDomainsAssign        = "um.action.domains.assign"
	URNDomainsEdit          = "um.action.domains.edit"

	// K8s 管理
	URNSidebarK8sClusters   = "um.sidebar.k8s-clusters"
	URNSidebarK8sOverview   = "um.sidebar.k8s-overview"
	URNSidebarK8sNodes      = "um.sidebar.k8s-nodes"
	URNSidebarK8sWorkloads  = "um.sidebar.k8s-workloads"
	URNSidebarK8sNetwork    = "um.sidebar.k8s-network"
	URNSidebarK8sStorage    = "um.sidebar.k8s-storage"
	URNSidebarK8sResources  = "um.sidebar.k8s-resources"
	URNSidebarK8sOperators  = "um.sidebar.k8s-operators"
	URNSidebarK8sDBClusters = "um.sidebar.k8s-db-clusters"
	URNSidebarK8sApps       = "um.sidebar.k8s-apps"

	URNK8sClusterCreate = "um.action.k8s.cluster.create"
	URNK8sClusterEdit   = "um.action.k8s.cluster.edit"
	URNK8sClusterDelete = "um.action.k8s.cluster.delete"
	URNK8sClusterAssign = "um.action.k8s.cluster.assign"
	URNK8sPodLog        = "um.action.k8s.pod.log"
	URNK8sPodExec       = "um.action.k8s.pod.exec"
	URNK8sWorkloadScale    = "um.action.k8s.workload.scale"
	URNK8sWorkloadRestart  = "um.action.k8s.workload.restart"
	URNK8sWorkloadRollback = "um.action.k8s.workload.rollback"
	URNK8sWorkloadDelete   = "um.action.k8s.workload.delete"
	URNK8sYAMLApply        = "um.action.k8s.yaml.apply"
	URNK8sNodeDrain        = "um.action.k8s.node.drain"
	URNK8sSecretView       = "um.action.k8s.secret.view"
	URNK8sQuotaEdit        = "um.action.k8s.quota.edit"
	URNK8sDBCreate         = "um.action.k8s.db.create"
	URNK8sDBScale          = "um.action.k8s.db.scale"
	URNK8sDBUpgrade        = "um.action.k8s.db.upgrade"
	URNK8sDBSwitchover     = "um.action.k8s.db.switchover"
	URNK8sDBDelete         = "um.action.k8s.db.delete"
)

func IsActionURN(urn string) bool {
	return strings.HasPrefix(strings.TrimSpace(urn), "um.action.")
}

func IsActionMenu(m Menu) bool {
	if IsActionURN(m.URN) {
		return true
	}
	return m.Path == "" && strings.HasPrefix(m.ID, "m-act-")
}

// RoleHasURN 判断角色是否拥有指定 URN（启用中的菜单）。
func (d *DB) RoleHasURN(roleID, urn string) (bool, error) {
	urn = strings.TrimSpace(urn)
	if roleID == "" || urn == "" {
		return false, nil
	}
	var n int
	err := d.QueryRow(
		`SELECT COUNT(1) FROM tbl_role_menus rm
		 JOIN tbl_menus m ON m.id = rm.menu_id
		 WHERE rm.role_id=? AND m.urn=? AND m.enabled=1`,
		roleID, urn,
	).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// ListRoleURNs 返回角色已授权的全部 URN（扁平列表，供前端鉴权）。
func (d *DB) ListRoleURNs(roleID string) ([]string, error) {
	if roleID == "" {
		return []string{}, nil
	}
	rows, err := d.Query(
		`SELECT m.urn FROM tbl_role_menus rm
		 JOIN tbl_menus m ON m.id = rm.menu_id
		 WHERE rm.role_id=? AND m.enabled=1 AND COALESCE(m.urn, '') <> ''
		 ORDER BY m.urn`,
		roleID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	var out []string
	for rows.Next() {
		var urn sql.NullString
		if err := rows.Scan(&urn); err != nil {
			return nil, err
		}
		u := strings.TrimSpace(urn.String)
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, u)
	}
	if out == nil {
		out = []string{}
	}
	return out, rows.Err()
}

// GetMenuByURN 按 URN 查找菜单（取第一条）。
func (d *DB) GetMenuByURN(urn string) (*Menu, error) {
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return nil, nil
	}
	row := d.QueryRow(`SELECT `+menuCols+` FROM tbl_menus WHERE urn=? LIMIT 1`, urn)
	m, err := scanMenu(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return m, err
}

// ExpandMenuIDsWithParents 勾选动作节点时自动补上祖先页面，保证「查看」前提成立。
func (d *DB) ExpandMenuIDsWithParents(menuIDs []string) ([]string, error) {
	all, err := d.ListMenus()
	if err != nil {
		return nil, err
	}
	byID := map[string]Menu{}
	for _, m := range all {
		byID[m.ID] = m
	}
	set := map[string]bool{}
	var out []string
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" || set[id] {
			return
		}
		set[id] = true
		out = append(out, id)
	}
	for _, id := range menuIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		add(id)
		pid := ""
		if m, ok := byID[id]; ok {
			pid = m.ParentID
		}
		for pid != "" {
			add(pid)
			p, ok := byID[pid]
			if !ok {
				break
			}
			pid = p.ParentID
		}
	}
	return out, nil
}

// PermissionLabel 用于 403 提示的中文能力名。
func (d *DB) PermissionLabel(urn string) string {
	if d != nil && d.db != nil {
		m, err := d.GetMenuByURN(urn)
		if err == nil && m != nil {
			if strings.TrimSpace(m.Title) != "" {
				return m.Title
			}
			if strings.TrimSpace(m.PermissionDesc) != "" {
				return m.PermissionDesc
			}
		}
	}
	urn = strings.TrimSpace(urn)
	switch {
	case strings.HasSuffix(urn, ".create"):
		return "新建"
	case strings.HasSuffix(urn, ".edit"):
		return "编辑"
	case strings.HasSuffix(urn, ".delete"):
		return "删除"
	case strings.HasSuffix(urn, ".collect"):
		return "采集"
	case strings.HasSuffix(urn, ".run"):
		return "执行"
	case strings.HasSuffix(urn, ".stop"):
		return "停止"
	case strings.HasSuffix(urn, ".approve"):
		return "审批"
	case strings.HasSuffix(urn, ".bind"):
		return "绑定"
	case strings.HasPrefix(urn, "um.sidebar."):
		return "查看"
	default:
		return urn
	}
}
