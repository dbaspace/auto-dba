package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

func (d *DB) ensureHATables() error {
	_, err := d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_ha_controllers (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL DEFAULT '',
		engine TEXT NOT NULL DEFAULT '',
		kind TEXT NOT NULL DEFAULT '',
		env_id TEXT NOT NULL DEFAULT '',
		base_url TEXT NOT NULL DEFAULT '',
		username TEXT NOT NULL DEFAULT '',
		password_enc TEXT NOT NULL DEFAULT '',
		remark TEXT NOT NULL DEFAULT '',
		created_by TEXT NOT NULL DEFAULT '',
		updated_by TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT ''
	)`)
	if err != nil {
		return err
	}
	// 关联配置中心平台 Agent（可选）
	ddl := `ALTER TABLE tbl_ha_controllers ADD COLUMN IF NOT EXISTS agent_id TEXT NOT NULL DEFAULT ''`
	if _, err := d.db.Exec(ddl); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate") && !strings.Contains(strings.ToLower(err.Error()), "already exists") {
			_, err2 := d.db.Exec(`ALTER TABLE tbl_ha_controllers ADD COLUMN agent_id TEXT NOT NULL DEFAULT ''`)
			if err2 != nil && !strings.Contains(strings.ToLower(err2.Error()), "duplicate") && !strings.Contains(strings.ToLower(err2.Error()), "already exists") {
				return err2
			}
		}
	}
	_, err = d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_ha_clusters (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL DEFAULT '',
		engine TEXT NOT NULL DEFAULT '',
		env_id TEXT NOT NULL DEFAULT '',
		mode TEXT NOT NULL DEFAULT 'manual',
		controller_id TEXT NOT NULL DEFAULT '',
		maint_status TEXT NOT NULL DEFAULT 'normal',
		vip TEXT NOT NULL DEFAULT '',
		remark TEXT NOT NULL DEFAULT '',
		created_by TEXT NOT NULL DEFAULT '',
		updated_by TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT ''
	)`)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_ha_clusters_engine ON tbl_ha_clusters(engine)`)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_ha_members (
		id TEXT PRIMARY KEY,
		cluster_id TEXT NOT NULL,
		instance_id TEXT NOT NULL,
		role TEXT NOT NULL DEFAULT 'unknown',
		priority INTEGER NOT NULL DEFAULT 0,
		enabled INTEGER NOT NULL DEFAULT 1,
		created_at TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT '',
		UNIQUE(cluster_id, instance_id)
	)`)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_ha_members_cluster ON tbl_ha_members(cluster_id)`)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_ha_tasks (
		id TEXT PRIMARY KEY,
		cluster_id TEXT NOT NULL,
		issue_id TEXT NOT NULL DEFAULT '',
		action TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT '',
		target_instance_id TEXT NOT NULL DEFAULT '',
		precheck_json TEXT NOT NULL DEFAULT '',
		steps_json TEXT NOT NULL DEFAULT '',
		result_json TEXT NOT NULL DEFAULT '',
		operator TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT ''
	)`)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_ha_tasks_cluster ON tbl_ha_tasks(cluster_id, created_at DESC)`)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_ha_tasks_issue ON tbl_ha_tasks(issue_id)`)
	if err != nil {
		return err
	}
	_, _ = d.db.Exec(`ALTER TABLE tbl_ha_clusters ADD COLUMN IF NOT EXISTS standby_controller_ids TEXT NOT NULL DEFAULT '[]'`)
	return nil
}

func encodeStandbyControllerIDs(ids []string) string {
	clean := make([]string, 0, len(ids))
	seen := map[string]struct{}{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		clean = append(clean, id)
	}
	if len(clean) == 0 {
		return "[]"
	}
	b, err := json.Marshal(clean)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func decodeStandbyControllerIDs(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return nil
	}
	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return nil
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			out = append(out, id)
		}
	}
	return out
}

func (d *DB) ListHAClusters(engine, envID, keyword string) ([]HACluster, error) {
	q := `SELECT id, name, engine, env_id, mode, controller_id, COALESCE(standby_controller_ids,'[]'), maint_status, vip, remark, created_by, updated_by, created_at, updated_at
		FROM tbl_ha_clusters WHERE 1=1`
	var args []any
	if e := strings.TrimSpace(engine); e != "" {
		q += ` AND engine=?`
		args = append(args, strings.ToLower(e))
	}
	if env := strings.TrimSpace(envID); env != "" {
		q += ` AND env_id=?`
		args = append(args, env)
	}
	if kw := strings.TrimSpace(keyword); kw != "" {
		q += ` AND (name ILIKE ? OR vip ILIKE ? OR remark ILIKE ?)`
		like := "%" + kw + "%"
		args = append(args, like, like, like)
	}
	q += ` ORDER BY updated_at DESC`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HACluster
	for rows.Next() {
		var c HACluster
		var standbyRaw string
		if err := rows.Scan(&c.ID, &c.Name, &c.Engine, &c.EnvID, &c.Mode, &c.ControllerID, &standbyRaw, &c.MaintStatus, &c.VIP, &c.Remark, &c.CreatedBy, &c.UpdatedBy, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		c.StandbyControllerIDs = decodeStandbyControllerIDs(standbyRaw)
		out = append(out, c)
	}
	if out == nil {
		out = []HACluster{}
	}
	return out, rows.Err()
}

func (d *DB) GetHACluster(id string) (*HACluster, error) {
	c := &HACluster{}
	var standbyRaw string
	err := d.QueryRow(`SELECT id, name, engine, env_id, mode, controller_id, COALESCE(standby_controller_ids,'[]'), maint_status, vip, remark, created_by, updated_by, created_at, updated_at
		FROM tbl_ha_clusters WHERE id=?`, id).Scan(
		&c.ID, &c.Name, &c.Engine, &c.EnvID, &c.Mode, &c.ControllerID, &standbyRaw, &c.MaintStatus, &c.VIP, &c.Remark, &c.CreatedBy, &c.UpdatedBy, &c.CreatedAt, &c.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.StandbyControllerIDs = decodeStandbyControllerIDs(standbyRaw)
	members, err := d.ListHAMembers(id)
	if err != nil {
		return nil, err
	}
	c.Members = members
	return c, nil
}

func (d *DB) CreateHACluster(c *HACluster) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	if c.Mode == "" {
		c.Mode = "manual"
	}
	if c.MaintStatus == "" {
		c.MaintStatus = "normal"
	}
	c.Engine = strings.ToLower(strings.TrimSpace(c.Engine))
	c.CreatedAt = now
	c.UpdatedAt = now
	standbyJSON := encodeStandbyControllerIDs(c.StandbyControllerIDs)
	_, err := d.Exec(`INSERT INTO tbl_ha_clusters(id, name, engine, env_id, mode, controller_id, standby_controller_ids, maint_status, vip, remark, created_by, updated_by, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.Name, c.Engine, c.EnvID, c.Mode, c.ControllerID, standbyJSON, c.MaintStatus, c.VIP, c.Remark, c.CreatedBy, c.UpdatedBy, c.CreatedAt, c.UpdatedAt)
	return err
}

func (d *DB) UpdateHACluster(c *HACluster) error {
	c.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	c.Engine = strings.ToLower(strings.TrimSpace(c.Engine))
	standbyJSON := encodeStandbyControllerIDs(c.StandbyControllerIDs)
	_, err := d.Exec(`UPDATE tbl_ha_clusters SET name=?, engine=?, env_id=?, mode=?, controller_id=?, standby_controller_ids=?, maint_status=?, vip=?, remark=?, updated_by=?, updated_at=? WHERE id=?`,
		c.Name, c.Engine, c.EnvID, c.Mode, c.ControllerID, standbyJSON, c.MaintStatus, c.VIP, c.Remark, c.UpdatedBy, c.UpdatedAt, c.ID)
	return err
}

func (d *DB) SetHAClusterMaintStatus(id, status, updatedBy string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`UPDATE tbl_ha_clusters SET maint_status=?, updated_by=?, updated_at=? WHERE id=?`, status, updatedBy, now, id)
	return err
}

func (d *DB) DeleteHACluster(id string) error {
	if _, err := d.Exec(`DELETE FROM tbl_ha_members WHERE cluster_id=?`, id); err != nil {
		return err
	}
	_, err := d.Exec(`DELETE FROM tbl_ha_clusters WHERE id=?`, id)
	return err
}

func (d *DB) ListHAMembers(clusterID string) ([]HAMember, error) {
	rows, err := d.Query(`SELECT m.id, m.cluster_id, m.instance_id, m.role, m.priority, m.enabled, m.created_at, m.updated_at,
		COALESCE(i.name,''), COALESCE(i.host,''), COALESCE(i.port,0), COALESCE(i.db_type,'')
		FROM tbl_ha_members m
		LEFT JOIN tbl_instances i ON i.id = m.instance_id
		WHERE m.cluster_id=?
		ORDER BY CASE m.role WHEN 'primary' THEN 0 WHEN 'replica' THEN 1 ELSE 2 END, m.priority DESC, m.created_at`, clusterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HAMember
	for rows.Next() {
		var m HAMember
		var en int
		if err := rows.Scan(&m.ID, &m.ClusterID, &m.InstanceID, &m.Role, &m.Priority, &en, &m.CreatedAt, &m.UpdatedAt,
			&m.InstanceName, &m.Host, &m.Port, &m.DBType); err != nil {
			return nil, err
		}
		m.Enabled = en != 0
		out = append(out, m)
	}
	if out == nil {
		out = []HAMember{}
	}
	return out, rows.Err()
}

func (d *DB) ReplaceHAMembers(clusterID string, members []HAMember) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := d.Exec(`DELETE FROM tbl_ha_members WHERE cluster_id=?`, clusterID); err != nil {
		return err
	}
	for i := range members {
		m := &members[i]
		if m.ID == "" {
			m.ID = uuid.NewString()
		}
		m.ClusterID = clusterID
		if m.Role == "" {
			m.Role = "unknown"
		}
		m.CreatedAt = now
		m.UpdatedAt = now
		en := 0
		if m.Enabled {
			en = 1
		}
		if _, err := d.Exec(`INSERT INTO tbl_ha_members(id, cluster_id, instance_id, role, priority, enabled, created_at, updated_at)
			VALUES(?,?,?,?,?,?,?,?)`, m.ID, m.ClusterID, m.InstanceID, m.Role, m.Priority, en, m.CreatedAt, m.UpdatedAt); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) UpdateHAMemberRoles(clusterID, newPrimaryID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := d.Exec(`UPDATE tbl_ha_members SET role='replica', updated_at=? WHERE cluster_id=? AND role='primary'`, now, clusterID); err != nil {
		return err
	}
	_, err := d.Exec(`UPDATE tbl_ha_members SET role='primary', updated_at=? WHERE cluster_id=? AND instance_id=?`, now, clusterID, newPrimaryID)
	return err
}

func (d *DB) CreateHATask(t *HATask) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	if t.Status == "" {
		t.Status = StatusPending
	}
	t.CreatedAt = now
	t.UpdatedAt = now
	_, err := d.Exec(`INSERT INTO tbl_ha_tasks(id, cluster_id, issue_id, action, status, target_instance_id, precheck_json, steps_json, result_json, operator, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.ClusterID, t.IssueID, t.Action, t.Status, t.TargetInstanceID, t.PrecheckJSON, t.StepsJSON, t.ResultJSON, t.Operator, t.CreatedAt, t.UpdatedAt)
	return err
}

func (d *DB) GetHATask(id string) (*HATask, error) {
	t := &HATask{}
	err := d.QueryRow(`SELECT id, cluster_id, issue_id, action, status, target_instance_id, precheck_json, steps_json, result_json, operator, created_at, updated_at
		FROM tbl_ha_tasks WHERE id=?`, id).Scan(
		&t.ID, &t.ClusterID, &t.IssueID, &t.Action, &t.Status, &t.TargetInstanceID, &t.PrecheckJSON, &t.StepsJSON, &t.ResultJSON, &t.Operator, &t.CreatedAt, &t.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return t, err
}

func (d *DB) GetHATaskByIssue(issueID string) (*HATask, error) {
	t := &HATask{}
	err := d.QueryRow(`SELECT id, cluster_id, issue_id, action, status, target_instance_id, precheck_json, steps_json, result_json, operator, created_at, updated_at
		FROM tbl_ha_tasks WHERE issue_id=? ORDER BY created_at DESC LIMIT 1`, issueID).Scan(
		&t.ID, &t.ClusterID, &t.IssueID, &t.Action, &t.Status, &t.TargetInstanceID, &t.PrecheckJSON, &t.StepsJSON, &t.ResultJSON, &t.Operator, &t.CreatedAt, &t.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return t, err
}

func (d *DB) UpdateHATask(t *HATask) error {
	t.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`UPDATE tbl_ha_tasks SET status=?, precheck_json=?, steps_json=?, result_json=?, updated_at=? WHERE id=?`,
		t.Status, t.PrecheckJSON, t.StepsJSON, t.ResultJSON, t.UpdatedAt, t.ID)
	return err
}

func (d *DB) ListHATasks(clusterID string, limit int) ([]HATask, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT t.id, t.cluster_id, t.issue_id, t.action, t.status, t.target_instance_id, t.precheck_json, t.steps_json, t.result_json, t.operator, t.created_at, t.updated_at,
		COALESCE(c.name,''), COALESCE(c.engine,'')
		FROM tbl_ha_tasks t LEFT JOIN tbl_ha_clusters c ON c.id=t.cluster_id WHERE 1=1`
	var args []any
	if cid := strings.TrimSpace(clusterID); cid != "" {
		q += ` AND t.cluster_id=?`
		args = append(args, cid)
	}
	q += ` ORDER BY t.created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HATask
	for rows.Next() {
		var t HATask
		if err := rows.Scan(&t.ID, &t.ClusterID, &t.IssueID, &t.Action, &t.Status, &t.TargetInstanceID, &t.PrecheckJSON, &t.StepsJSON, &t.ResultJSON, &t.Operator, &t.CreatedAt, &t.UpdatedAt, &t.ClusterName, &t.Engine); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if out == nil {
		out = []HATask{}
	}
	return out, rows.Err()
}

func (d *DB) ListHAControllers(engine, envID string) ([]HAController, error) {
	q := `SELECT id, name, engine, kind, env_id, base_url, username, password_enc, COALESCE(agent_id,''), remark, created_by, updated_by, created_at, updated_at
		FROM tbl_ha_controllers WHERE 1=1`
	var args []any
	if e := strings.TrimSpace(engine); e != "" {
		q += ` AND engine=?`
		args = append(args, strings.ToLower(e))
	}
	if env := strings.TrimSpace(envID); env != "" {
		q += ` AND env_id=?`
		args = append(args, env)
	}
	q += ` ORDER BY updated_at DESC`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HAController
	for rows.Next() {
		var c HAController
		if err := rows.Scan(&c.ID, &c.Name, &c.Engine, &c.Kind, &c.EnvID, &c.BaseURL, &c.Username, &c.PasswordEnc, &c.AgentID, &c.Remark, &c.CreatedBy, &c.UpdatedBy, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		c.PasswordSet = strings.TrimSpace(c.PasswordEnc) != ""
		c.PasswordEnc = ""
		out = append(out, c)
	}
	if out == nil {
		out = []HAController{}
	}
	return out, rows.Err()
}

func (d *DB) GetHAController(id string) (*HAController, error) {
	c := &HAController{}
	err := d.QueryRow(`SELECT id, name, engine, kind, env_id, base_url, username, password_enc, COALESCE(agent_id,''), remark, created_by, updated_by, created_at, updated_at
		FROM tbl_ha_controllers WHERE id=?`, id).Scan(
		&c.ID, &c.Name, &c.Engine, &c.Kind, &c.EnvID, &c.BaseURL, &c.Username, &c.PasswordEnc, &c.AgentID, &c.Remark, &c.CreatedBy, &c.UpdatedBy, &c.CreatedAt, &c.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.PasswordSet = strings.TrimSpace(c.PasswordEnc) != ""
	return c, nil
}

func (d *DB) CreateHAController(c *HAController) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	c.Engine = strings.ToLower(strings.TrimSpace(c.Engine))
	c.Kind = strings.ToLower(strings.TrimSpace(c.Kind))
	c.CreatedAt = now
	c.UpdatedAt = now
	_, err := d.Exec(`INSERT INTO tbl_ha_controllers(id, name, engine, kind, env_id, base_url, username, password_enc, agent_id, remark, created_by, updated_by, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.Name, c.Engine, c.Kind, c.EnvID, c.BaseURL, c.Username, c.PasswordEnc, c.AgentID, c.Remark, c.CreatedBy, c.UpdatedBy, c.CreatedAt, c.UpdatedAt)
	return err
}

func (d *DB) UpdateHAController(c *HAController) error {
	c.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	c.Engine = strings.ToLower(strings.TrimSpace(c.Engine))
	c.Kind = strings.ToLower(strings.TrimSpace(c.Kind))
	_, err := d.Exec(`UPDATE tbl_ha_controllers SET name=?, engine=?, kind=?, env_id=?, base_url=?, username=?, password_enc=?, agent_id=?, remark=?, updated_by=?, updated_at=? WHERE id=?`,
		c.Name, c.Engine, c.Kind, c.EnvID, c.BaseURL, c.Username, c.PasswordEnc, c.AgentID, c.Remark, c.UpdatedBy, c.UpdatedAt, c.ID)
	return err
}

func (d *DB) DeleteHAController(id string) error {
	_, err := d.Exec(`DELETE FROM tbl_ha_controllers WHERE id=?`, id)
	return err
}

// CountHAControllerRefs 统计控制器被 HA 集群引用次数（主控 / 备控）。
func (d *DB) CountHAControllerRefs() (map[string][2]int, error) {
	rows, err := d.Query(`SELECT controller_id, COALESCE(standby_controller_ids,'[]') FROM tbl_ha_clusters`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][2]int{}
	for rows.Next() {
		var primary, standbyRaw string
		if err := rows.Scan(&primary, &standbyRaw); err != nil {
			return nil, err
		}
		primary = strings.TrimSpace(primary)
		if primary != "" {
			v := out[primary]
			v[0]++
			out[primary] = v
		}
		for _, sid := range decodeStandbyControllerIDs(standbyRaw) {
			if sid == primary {
				continue
			}
			v := out[sid]
			v[1]++
			out[sid] = v
		}
	}
	return out, rows.Err()
}

// NormalizeStandbyControllerIDs 去掉空值与主控自身。
func NormalizeStandbyControllerIDs(primary string, standbys []string) []string {
	primary = strings.TrimSpace(primary)
	seen := map[string]struct{}{}
	var out []string
	for _, id := range standbys {
		id = strings.TrimSpace(id)
		if id == "" || id == primary {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

type HAModeOption struct {
	Code  string `json:"code"`
	Label string `json:"label"`
	Hint  string `json:"hint,omitempty"`
}

type HAKindOption struct {
	Code   string `json:"code"`
	Label  string `json:"label"`
	Engine string `json:"engine"`
}

func HAModesForEngine(engine string) []HAModeOption {
	switch strings.ToLower(strings.TrimSpace(engine)) {
	case "mysql":
		return []HAModeOption{
			{Code: "manual", Label: "人工/未托管", Hint: "仅登记拓扑，切换按手册执行"},
			{Code: "async_repl", Label: "异步复制（人工切换）", Hint: "主从复制，无托管工具"},
			{Code: "orchestrator", Label: "复制 + Orchestrator", Hint: "graceful-master-takeover"},
			{Code: "mha", Label: "复制 + MHA", Hint: "Master High Availability"},
			{Code: "proxysql_repl", Label: "ProxySQL + 复制", Hint: "流量入口由 ProxySQL 转发"},
			{Code: "mgr", Label: "MGR / InnoDB Cluster", Hint: "组复制共识"},
			{Code: "pxc", Label: "PXC / Galera", Hint: "同步多主"},
			{Code: "shared_storage", Label: "DRBD + Pacemaker", Hint: "共享存储"},
			{Code: "k8s_operator", Label: "K8s MySQL Operator", Hint: "云原生 Operator"},
		}
	case "postgres":
		return []HAModeOption{
			{Code: "manual", Label: "人工/未托管", Hint: "仅登记拓扑"},
			{Code: "async_repl", Label: "流复制（人工切换）", Hint: "原生 streaming replication"},
			{Code: "pg_autofailover", Label: "pg_auto_failover", Hint: "Monitor + Keeper，pg_autoctl 编排切换"},
			{Code: "patroni", Label: "Patroni", Hint: "DCS 选主 + REST switchover/failover"},
			{Code: "repmgr", Label: "repmgr", Hint: "流复制 + repmgr；计划切换经 Agent 白名单"},
			{Code: "k8s_operator", Label: "K8s PG Operator", Hint: "CloudNativePG 等；清单登记"},
		}
	case "redis":
		return []HAModeOption{
			{Code: "manual", Label: "人工/未托管", Hint: "仅登记拓扑"},
			{Code: "sentinel", Label: "Redis Sentinel", Hint: "SENTINEL FAILOVER"},
			{Code: "cluster", Label: "Redis Cluster", Hint: "分片集群，不按单主从切换"},
		}
	default:
		return nil
	}
}

func DefaultHAMode(engine string) string {
	modes := HAModesForEngine(engine)
	if len(modes) == 0 {
		return "manual"
	}
	return modes[0].Code
}

func ValidHAMode(engine, mode string) bool {
	m := strings.ToLower(strings.TrimSpace(mode))
	if m == "" {
		return false
	}
	for _, opt := range HAModesForEngine(engine) {
		if opt.Code == m {
			return true
		}
	}
	return false
}

func NormalizeHAMode(engine, mode string) string {
	m := strings.ToLower(strings.TrimSpace(mode))
	if ValidHAMode(engine, m) {
		return m
	}
	return DefaultHAMode(engine)
}

func HAControllerKinds() []HAKindOption {
	return []HAKindOption{
		{Code: "orchestrator", Label: "Orchestrator", Engine: "mysql"},
		{Code: "mha", Label: "MHA", Engine: "mysql"},
		{Code: "proxysql", Label: "ProxySQL", Engine: "mysql"},
		{Code: "pg_autofailover", Label: "pg_auto_failover", Engine: "postgres"},
		{Code: "patroni", Label: "Patroni", Engine: "postgres"},
		{Code: "repmgr", Label: "repmgr", Engine: "postgres"},
		{Code: "sentinel", Label: "Sentinel", Engine: "redis"},
	}
}

func ValidHAControllerKind(engine, kind string) bool {
	e := strings.ToLower(strings.TrimSpace(engine))
	k := strings.ToLower(strings.TrimSpace(kind))
	for _, opt := range HAControllerKinds() {
		if opt.Engine == e && opt.Code == k {
			return true
		}
	}
	return false
}

func CompatibleHAController(engine, mode, kind string) bool {
	e := strings.ToLower(strings.TrimSpace(engine))
	m := strings.ToLower(strings.TrimSpace(mode))
	k := strings.ToLower(strings.TrimSpace(kind))
	if !ValidHAMode(e, m) || !ValidHAControllerKind(e, k) {
		return false
	}
	switch m {
	case "orchestrator":
		return k == "orchestrator"
	case "mha":
		return k == "mha"
	case "proxysql_repl":
		return k == "proxysql"
	case "pg_autofailover":
		return k == "pg_autofailover"
	case "patroni":
		return k == "patroni"
	case "repmgr":
		return k == "repmgr"
	case "sentinel":
		return k == "sentinel"
	default:
		return false
	}
}

func HAAutoSwitchoverMode(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "orchestrator", "mha", "proxysql_repl", "pg_autofailover", "patroni", "repmgr", "sentinel":
		return true
	default:
		return false
	}
}

func ValidHAEngine(e string) bool {
	switch strings.ToLower(strings.TrimSpace(e)) {
	case "mysql", "postgres", "redis":
		return true
	default:
		return false
	}
}

func ValidHAAction(a string) bool {
	switch strings.TrimSpace(a) {
	case "switchover", "enter_maint", "exit_maint", "failover_dead":
		return true
	default:
		return false
	}
}

func HAActionLabel(a string) string {
	switch a {
	case "switchover":
		return "计划内切换"
	case "enter_maint":
		return "进入维护"
	case "exit_maint":
		return "退出维护"
	case "failover_dead":
		return "主宕机强制切换"
	default:
		return a
	}
}

func NormalizeHAEngine(dbType string) string {
	t := strings.ToLower(strings.TrimSpace(dbType))
	switch t {
	case "mysql", "mariadb", "tidb", "oceanbase":
		return "mysql"
	case "postgres", "postgresql", "pg", "cockroach", "cockroachdb":
		return "postgres"
	case "redis":
		return "redis"
	default:
		return t
	}
}

func AssertHAEngineMatch(clusterEngine, instanceDBType string) error {
	want := strings.ToLower(strings.TrimSpace(clusterEngine))
	got := NormalizeHAEngine(instanceDBType)
	if want != got {
		return fmt.Errorf("实例类型 %s 与集群引擎 %s 不匹配", instanceDBType, clusterEngine)
	}
	return nil
}
