package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Backup 子系统状态常量。
const (
	BackupMethodLogicalFull  = "logical_full"
	BackupMethodPhysicalFull = "physical_full"
	BackupMethodPhysicalIncr = "physical_incr"

	BackupRunPending = "pending"
	BackupRunRunning = "running"
	BackupRunSuccess = "success"
	BackupRunFailed  = "failed"

	BackupTriggerManual = "manual"
	BackupTriggerCron   = "cron"

	BackupStorageLocal = "local"
	BackupStorageS3    = "s3"
)

// IsPhysicalBackupMethod 是否物理备份方式。
func IsPhysicalBackupMethod(m string) bool {
	switch strings.TrimSpace(m) {
	case BackupMethodPhysicalFull, BackupMethodPhysicalIncr:
		return true
	default:
		return false
	}
}

// ValidBackupMethod 校验备份方式。
func ValidBackupMethod(m string) bool {
	switch strings.TrimSpace(m) {
	case BackupMethodLogicalFull, BackupMethodPhysicalFull, BackupMethodPhysicalIncr, "":
		return true
	default:
		return false
	}
}

// BackupStorage 存储后端。
type BackupStorage struct {
	ID           string `json:"id"`
	TenantID     string `json:"tenant_id,omitempty"`
	Name         string `json:"name"`
	Kind         string `json:"kind"` // local | s3
	Endpoint     string `json:"endpoint,omitempty"`
	Bucket       string `json:"bucket,omitempty"`
	Prefix       string `json:"prefix,omitempty"`
	AccessKey    string `json:"access_key,omitempty"`
	SecretKeyEnc string `json:"-"`
	SecretKeySet bool   `json:"secret_key_set"`
	LocalPath    string `json:"local_path,omitempty"`
	Remark       string `json:"remark,omitempty"`
	CreatedBy    string `json:"created_by,omitempty"`
	UpdatedBy    string `json:"updated_by,omitempty"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

// BackupAgent 平台 Agent（备份 / RDB / HA 环境检查）。
type BackupAgent struct {
	ID            string `json:"id"`
	TenantID      string `json:"tenant_id,omitempty"`
	Name          string `json:"name"`
	BaseURL       string `json:"base_url"`
	Username      string `json:"username,omitempty"`
	PasswordEnc   string `json:"-"`
	PasswordSet   bool   `json:"password_set"`
	Engines       string `json:"engines,omitempty"`        // 兼容旧字段
	EnabledGroups string `json:"enabled_groups,omitempty"` // backup,rdb,ha
	Capabilities  string `json:"capabilities,omitempty"`   // JSON CapabilitiesReport
	Tags          string `json:"tags,omitempty"`
	Remark        string `json:"remark,omitempty"`
	HostInstance  string `json:"host_instance_id,omitempty"`
	CreatedBy     string `json:"created_by,omitempty"`
	UpdatedBy     string `json:"updated_by,omitempty"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

// BackupAgentDeploy 一次 Agent 下发任务。
type BackupAgentDeploy struct {
	ID             string `json:"id"`
	HostInstanceID string `json:"host_instance_id"`
	AgentID        string `json:"agent_id,omitempty"`
	Status         string `json:"status"`
	ListenPort     int    `json:"listen_port"`
	RemoteDir      string `json:"remote_dir,omitempty"`
	BaseURL        string `json:"base_url,omitempty"`
	LogText        string `json:"log_text,omitempty"`
	CreatedBy      string `json:"created_by,omitempty"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	FinishedAt     string `json:"finished_at,omitempty"`
	Kind           string `json:"kind,omitempty"`
	Payload        string `json:"payload,omitempty"`
}

const (
	BackupDeployPending = "pending"
	BackupDeployRunning = "running"
	BackupDeploySuccess = "success"
	BackupDeployFailed  = "failed"
)

// BackupPolicy 备份策略。
type BackupPolicy struct {
	ID            string   `json:"id"`
	TenantID      string   `json:"tenant_id,omitempty"`
	Name          string   `json:"name"`
	InstanceID    string   `json:"instance_id"`
	InstanceName  string   `json:"instance_name,omitempty"`
	Engine        string   `json:"engine"`
	Method        string   `json:"method"`
	DatabasesJSON string   `json:"-"`
	Databases     []string `json:"databases"`
	ScheduleExpr  string   `json:"schedule_expr"`
	RetentionDays int      `json:"retention_days"`
	Parallel      int      `json:"parallel"`
	Compress      bool     `json:"compress"`
	StorageID     string   `json:"storage_id"`
	AgentID       string   `json:"agent_id,omitempty"`
	Enabled       bool     `json:"enabled"`
	CreatedBy     string   `json:"created_by,omitempty"`
	UpdatedBy     string   `json:"updated_by,omitempty"`
	CreatedAt     string   `json:"created_at"`
	UpdatedAt     string   `json:"updated_at"`
}

// BackupRun 一次备份执行。
type BackupRun struct {
	ID           string `json:"id"`
	TenantID     string `json:"tenant_id,omitempty"`
	PolicyID     string `json:"policy_id"`
	PolicyName   string `json:"policy_name,omitempty"`
	InstanceID   string `json:"instance_id"`
	Engine       string `json:"engine"`
	Method       string `json:"method,omitempty"`
	Status       string `json:"status"`
	Trigger      string `json:"trigger"`
	ParentRunID  string `json:"parent_run_id,omitempty"`
	SetLabel     string `json:"set_label,omitempty"`
	ObjectKey    string `json:"object_key,omitempty"`
	LocalPath    string `json:"local_path,omitempty"`
	SizeBytes    int64  `json:"size_bytes"`
	Checksum     string `json:"checksum,omitempty"`
	LogSummary   string `json:"log_summary,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
	ExtraJSON    string `json:"extra_json,omitempty"` // LSN/binlog 等
	StartedAt    string `json:"started_at,omitempty"`
	FinishedAt   string `json:"finished_at,omitempty"`
	CreatedBy    string `json:"created_by,omitempty"`
	CreatedAt    string `json:"created_at"`
}

func (d *DB) ensureBackupTables() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS tbl_backup_storages (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL DEFAULT '',
			kind TEXT NOT NULL DEFAULT 'local',
			endpoint TEXT NOT NULL DEFAULT '',
			bucket TEXT NOT NULL DEFAULT '',
			prefix TEXT NOT NULL DEFAULT '',
			access_key TEXT NOT NULL DEFAULT '',
			secret_key_enc TEXT NOT NULL DEFAULT '',
			local_path TEXT NOT NULL DEFAULT '',
			remark TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL DEFAULT '',
			updated_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_backup_agents (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL DEFAULT '',
			base_url TEXT NOT NULL DEFAULT '',
			username TEXT NOT NULL DEFAULT '',
			password_enc TEXT NOT NULL DEFAULT '',
			engines TEXT NOT NULL DEFAULT '',
			enabled_groups TEXT NOT NULL DEFAULT '',
			capabilities TEXT NOT NULL DEFAULT '',
			tags TEXT NOT NULL DEFAULT '',
			remark TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL DEFAULT '',
			updated_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_backup_policies (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL DEFAULT '',
			instance_id TEXT NOT NULL DEFAULT '',
			engine TEXT NOT NULL DEFAULT '',
			method TEXT NOT NULL DEFAULT 'logical_full',
			databases_json TEXT NOT NULL DEFAULT '[]',
			schedule_expr TEXT NOT NULL DEFAULT '',
			retention_days INTEGER NOT NULL DEFAULT 7,
			parallel INTEGER NOT NULL DEFAULT 1,
			compress INTEGER NOT NULL DEFAULT 0,
			storage_id TEXT NOT NULL DEFAULT '',
			agent_id TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			created_by TEXT NOT NULL DEFAULT '',
			updated_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_backup_runs (
			id TEXT PRIMARY KEY,
			policy_id TEXT NOT NULL DEFAULT '',
			instance_id TEXT NOT NULL DEFAULT '',
			engine TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT '',
			trigger_src TEXT NOT NULL DEFAULT '',
			object_key TEXT NOT NULL DEFAULT '',
			local_path TEXT NOT NULL DEFAULT '',
			size_bytes BIGINT NOT NULL DEFAULT 0,
			checksum TEXT NOT NULL DEFAULT '',
			log_summary TEXT NOT NULL DEFAULT '',
			error_message TEXT NOT NULL DEFAULT '',
			started_at TEXT NOT NULL DEFAULT '',
			finished_at TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_backup_policies_instance ON tbl_backup_policies(instance_id)`,
		`CREATE INDEX IF NOT EXISTS idx_backup_runs_policy ON tbl_backup_runs(policy_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_backup_runs_status ON tbl_backup_runs(status, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS tbl_backup_agent_deploys (
			id TEXT PRIMARY KEY,
			host_instance_id TEXT NOT NULL DEFAULT '',
			agent_id TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT '',
			listen_port INTEGER NOT NULL DEFAULT 8090,
			remote_dir TEXT NOT NULL DEFAULT '',
			base_url TEXT NOT NULL DEFAULT '',
			log_text TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT '',
			finished_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_backup_agent_deploys_host ON tbl_backup_agent_deploys(host_instance_id, created_at DESC)`,
	}
	for _, s := range stmts {
		if _, err := d.db.Exec(s); err != nil {
			return err
		}
	}
	if err := d.ensureBackupRunExtraColumns(); err != nil {
		return err
	}
	return d.ensureSQLExecutorDeployColumns()
}

func (d *DB) ensureBackupRunExtraColumns() error {
	cols := []struct{ name, ddl string }{
		{"method", "ALTER TABLE tbl_backup_runs ADD COLUMN IF NOT EXISTS method TEXT NOT NULL DEFAULT ''"},
		{"parent_run_id", "ALTER TABLE tbl_backup_runs ADD COLUMN IF NOT EXISTS parent_run_id TEXT NOT NULL DEFAULT ''"},
		{"set_label", "ALTER TABLE tbl_backup_runs ADD COLUMN IF NOT EXISTS set_label TEXT NOT NULL DEFAULT ''"},
		{"extra_json", "ALTER TABLE tbl_backup_runs ADD COLUMN IF NOT EXISTS extra_json TEXT NOT NULL DEFAULT ''"},
	}
	for _, c := range cols {
		if _, err := d.db.Exec(c.ddl); err != nil {
			if !strings.Contains(strings.ToLower(err.Error()), "duplicate") && !strings.Contains(strings.ToLower(err.Error()), "already exists") {
				_, err2 := d.db.Exec("ALTER TABLE tbl_backup_runs ADD COLUMN " + c.name + " TEXT NOT NULL DEFAULT ''")
				if err2 != nil && !strings.Contains(strings.ToLower(err2.Error()), "duplicate") && !strings.Contains(strings.ToLower(err2.Error()), "already exists") {
					return err
				}
			}
		}
	}
	agentCols := []string{"enabled_groups", "capabilities"}
	for _, name := range agentCols {
		ddl := "ALTER TABLE tbl_backup_agents ADD COLUMN IF NOT EXISTS " + name + " TEXT NOT NULL DEFAULT ''"
		if _, err := d.db.Exec(ddl); err != nil {
			if !strings.Contains(strings.ToLower(err.Error()), "duplicate") && !strings.Contains(strings.ToLower(err.Error()), "already exists") {
				_, err2 := d.db.Exec("ALTER TABLE tbl_backup_agents ADD COLUMN " + name + " TEXT NOT NULL DEFAULT ''")
				if err2 != nil && !strings.Contains(strings.ToLower(err2.Error()), "duplicate") && !strings.Contains(strings.ToLower(err2.Error()), "already exists") {
					return err
				}
			}
		}
	}
	return nil
}

func encodeBackupDBs(dbs []string) string {
	if dbs == nil {
		dbs = []string{}
	}
	raw, _ := json.Marshal(dbs)
	return string(raw)
}

func decodeBackupDBs(raw string) []string {
	var out []string
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out == nil {
		return []string{}
	}
	return out
}

func scanBackupPolicy(p *BackupPolicy) error {
	p.Databases = decodeBackupDBs(p.DatabasesJSON)
	return nil
}

// --- Storage ---

func (d *DB) ListBackupStorages() ([]BackupStorage, error) {
	return d.ListBackupStoragesInTenant("")
}

func (d *DB) ListBackupStoragesInTenant(tenantID string) ([]BackupStorage, error) {
	q := `SELECT id, name, kind, endpoint, bucket, prefix, access_key, secret_key_enc, local_path, remark,
		created_by, updated_by, created_at, updated_at FROM tbl_backup_storages WHERE 1=1`
	args := []any{}
	q, args = appendTenantFilter(q, args, tenantID)
	q += ` ORDER BY updated_at DESC`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BackupStorage
	for rows.Next() {
		var s BackupStorage
		if err := rows.Scan(&s.ID, &s.Name, &s.Kind, &s.Endpoint, &s.Bucket, &s.Prefix, &s.AccessKey, &s.SecretKeyEnc,
			&s.LocalPath, &s.Remark, &s.CreatedBy, &s.UpdatedBy, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		s.SecretKeySet = s.SecretKeyEnc != ""
		s.SecretKeyEnc = ""
		out = append(out, s)
	}
	if out == nil {
		out = []BackupStorage{}
	}
	return out, rows.Err()
}

func (d *DB) GetBackupStorage(id string) (*BackupStorage, error) {
	s := &BackupStorage{}
	err := d.QueryRow(`SELECT id, name, kind, endpoint, bucket, prefix, access_key, secret_key_enc, local_path, remark,
		created_by, updated_by, created_at, updated_at FROM tbl_backup_storages WHERE id=?`, id).Scan(
		&s.ID, &s.Name, &s.Kind, &s.Endpoint, &s.Bucket, &s.Prefix, &s.AccessKey, &s.SecretKeyEnc,
		&s.LocalPath, &s.Remark, &s.CreatedBy, &s.UpdatedBy, &s.CreatedAt, &s.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.SecretKeySet = s.SecretKeyEnc != ""
	return s, nil
}

func (d *DB) GetBackupStorageInTenant(id, tenantID string) (*BackupStorage, error) {
	s, err := d.GetBackupStorage(id)
	return ownedInTenant(d, s, err, "tbl_backup_storages", id, tenantID)
}

func (d *DB) CreateBackupStorage(s *BackupStorage) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if s.ID == "" {
		s.ID = uuid.NewString()
	}
	if s.Kind == "" {
		s.Kind = BackupStorageLocal
	}
	s.CreatedAt, s.UpdatedAt = now, now
	_, err := d.Exec(`INSERT INTO tbl_backup_storages(id, name, kind, endpoint, bucket, prefix, access_key, secret_key_enc, local_path, remark, created_by, updated_by, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		s.ID, s.Name, s.Kind, s.Endpoint, s.Bucket, s.Prefix, s.AccessKey, s.SecretKeyEnc, s.LocalPath, s.Remark,
		s.CreatedBy, s.UpdatedBy, s.CreatedAt, s.UpdatedAt)
	if err != nil {
		return err
	}
	return d.AssignTenantID("tbl_backup_storages", s.ID, s.TenantID)
}

func (d *DB) UpdateBackupStorage(s *BackupStorage) error {
	s.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`UPDATE tbl_backup_storages SET name=?, kind=?, endpoint=?, bucket=?, prefix=?, access_key=?, secret_key_enc=?, local_path=?, remark=?, updated_by=?, updated_at=? WHERE id=?`,
		s.Name, s.Kind, s.Endpoint, s.Bucket, s.Prefix, s.AccessKey, s.SecretKeyEnc, s.LocalPath, s.Remark, s.UpdatedBy, s.UpdatedAt, s.ID)
	return err
}

func (d *DB) DeleteBackupStorage(id string) error {
	n, err := d.Count(`SELECT COUNT(1) FROM tbl_backup_policies WHERE storage_id=?`, id)
	if err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("仍有策略引用该存储，无法删除")
	}
	_, err = d.Exec(`DELETE FROM tbl_backup_storages WHERE id=?`, id)
	return err
}

// --- Agent ---

func (d *DB) ListBackupAgents() ([]BackupAgent, error) {
	return d.ListBackupAgentsInTenant("")
}

func (d *DB) ListBackupAgentsInTenant(tenantID string) ([]BackupAgent, error) {
	q := `SELECT id, name, base_url, username, password_enc, engines, COALESCE(enabled_groups,''), COALESCE(capabilities,''), tags, remark, created_by, updated_by, created_at, updated_at
		FROM tbl_backup_agents WHERE 1=1`
	args := []any{}
	q, args = appendTenantFilter(q, args, tenantID)
	q += ` ORDER BY updated_at DESC`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BackupAgent
	for rows.Next() {
		var a BackupAgent
		if err := rows.Scan(&a.ID, &a.Name, &a.BaseURL, &a.Username, &a.PasswordEnc, &a.Engines, &a.EnabledGroups, &a.Capabilities, &a.Tags, &a.Remark,
			&a.CreatedBy, &a.UpdatedBy, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		a.PasswordSet = a.PasswordEnc != ""
		a.PasswordEnc = ""
		a.HostInstance = ParseBackupAgentHostTag(a.Tags)
		out = append(out, a)
	}
	if out == nil {
		out = []BackupAgent{}
	}
	return out, rows.Err()
}

func (d *DB) GetBackupAgentInTenant(id, tenantID string) (*BackupAgent, error) {
	a, err := d.GetBackupAgent(id)
	return ownedInTenant(d, a, err, "tbl_backup_agents", id, tenantID)
}

func (d *DB) GetBackupAgent(id string) (*BackupAgent, error) {
	a := &BackupAgent{}
	err := d.QueryRow(`SELECT id, name, base_url, username, password_enc, engines, COALESCE(enabled_groups,''), COALESCE(capabilities,''), tags, remark, created_by, updated_by, created_at, updated_at
		FROM tbl_backup_agents WHERE id=?`, id).Scan(
		&a.ID, &a.Name, &a.BaseURL, &a.Username, &a.PasswordEnc, &a.Engines, &a.EnabledGroups, &a.Capabilities, &a.Tags, &a.Remark,
		&a.CreatedBy, &a.UpdatedBy, &a.CreatedAt, &a.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	decorateBackupAgent(a)
	return a, nil
}

func (d *DB) CreateBackupAgent(a *BackupAgent) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	a.CreatedAt, a.UpdatedAt = now, now
	_, err := d.Exec(`INSERT INTO tbl_backup_agents(id, name, base_url, username, password_enc, engines, enabled_groups, capabilities, tags, remark, created_by, updated_by, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.Name, a.BaseURL, a.Username, a.PasswordEnc, a.Engines, a.EnabledGroups, a.Capabilities, a.Tags, a.Remark,
		a.CreatedBy, a.UpdatedBy, a.CreatedAt, a.UpdatedAt)
	if err != nil {
		return err
	}
	return d.AssignTenantID("tbl_backup_agents", a.ID, a.TenantID)
}

func (d *DB) UpdateBackupAgent(a *BackupAgent) error {
	a.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`UPDATE tbl_backup_agents SET name=?, base_url=?, username=?, password_enc=?, engines=?, enabled_groups=?, capabilities=?, tags=?, remark=?, updated_by=?, updated_at=? WHERE id=?`,
		a.Name, a.BaseURL, a.Username, a.PasswordEnc, a.Engines, a.EnabledGroups, a.Capabilities, a.Tags, a.Remark, a.UpdatedBy, a.UpdatedAt, a.ID)
	return err
}

func (d *DB) DeleteBackupAgent(id string) error {
	n, err := d.Count(`SELECT COUNT(1) FROM tbl_backup_policies WHERE agent_id=?`, id)
	if err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("仍有策略引用该 Agent，无法删除")
	}
	_, err = d.Exec(`DELETE FROM tbl_backup_agents WHERE id=?`, id)
	return err
}

// --- Policy ---

func (d *DB) ListBackupPolicies(instanceID string) ([]BackupPolicy, error) {
	return d.ListBackupPoliciesInTenant("", instanceID)
}

func (d *DB) ListBackupPoliciesInTenant(tenantID, instanceID string) ([]BackupPolicy, error) {
	q := `SELECT p.id, p.name, p.instance_id, COALESCE(i.name,''), p.engine, p.method, p.databases_json, p.schedule_expr,
		p.retention_days, p.parallel, p.compress, p.storage_id, p.agent_id, p.enabled, p.created_by, p.updated_by, p.created_at, p.updated_at
		FROM tbl_backup_policies p LEFT JOIN tbl_instances i ON i.id=p.instance_id WHERE 1=1`
	var args []any
	if tid := strings.TrimSpace(tenantID); tid != "" {
		q += ` AND COALESCE(p.tenant_id,'')=?`
		args = append(args, tid)
	}
	if id := strings.TrimSpace(instanceID); id != "" {
		q += ` AND p.instance_id=?`
		args = append(args, id)
	}
	q += ` ORDER BY p.updated_at DESC`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BackupPolicy
	for rows.Next() {
		var p BackupPolicy
		var compress, enabled int
		if err := rows.Scan(&p.ID, &p.Name, &p.InstanceID, &p.InstanceName, &p.Engine, &p.Method, &p.DatabasesJSON, &p.ScheduleExpr,
			&p.RetentionDays, &p.Parallel, &compress, &p.StorageID, &p.AgentID, &enabled, &p.CreatedBy, &p.UpdatedBy, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.Compress = compress != 0
		p.Enabled = enabled != 0
		_ = scanBackupPolicy(&p)
		out = append(out, p)
	}
	if out == nil {
		out = []BackupPolicy{}
	}
	return out, rows.Err()
}

func (d *DB) GetBackupPolicyInTenant(id, tenantID string) (*BackupPolicy, error) {
	p, err := d.GetBackupPolicy(id)
	return ownedInTenant(d, p, err, "tbl_backup_policies", id, tenantID)
}

func (d *DB) GetBackupPolicy(id string) (*BackupPolicy, error) {
	p := &BackupPolicy{}
	var compress, enabled int
	err := d.QueryRow(`SELECT p.id, p.name, p.instance_id, COALESCE(i.name,''), p.engine, p.method, p.databases_json, p.schedule_expr,
		p.retention_days, p.parallel, p.compress, p.storage_id, p.agent_id, p.enabled, p.created_by, p.updated_by, p.created_at, p.updated_at
		FROM tbl_backup_policies p LEFT JOIN tbl_instances i ON i.id=p.instance_id WHERE p.id=?`, id).Scan(
		&p.ID, &p.Name, &p.InstanceID, &p.InstanceName, &p.Engine, &p.Method, &p.DatabasesJSON, &p.ScheduleExpr,
		&p.RetentionDays, &p.Parallel, &compress, &p.StorageID, &p.AgentID, &enabled, &p.CreatedBy, &p.UpdatedBy, &p.CreatedAt, &p.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.Compress = compress != 0
	p.Enabled = enabled != 0
	_ = scanBackupPolicy(p)
	return p, nil
}

func (d *DB) CreateBackupPolicy(p *BackupPolicy) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	if p.Method == "" {
		p.Method = BackupMethodLogicalFull
	}
	if p.RetentionDays <= 0 {
		p.RetentionDays = 7
	}
	if p.Parallel <= 0 {
		p.Parallel = 1
	}
	p.DatabasesJSON = encodeBackupDBs(p.Databases)
	p.CreatedAt, p.UpdatedAt = now, now
	compress, enabled := 0, 0
	if p.Compress {
		compress = 1
	}
	if p.Enabled {
		enabled = 1
	}
	_, err := d.Exec(`INSERT INTO tbl_backup_policies(id, name, instance_id, engine, method, databases_json, schedule_expr, retention_days, parallel, compress, storage_id, agent_id, enabled, created_by, updated_by, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.Name, p.InstanceID, p.Engine, p.Method, p.DatabasesJSON, p.ScheduleExpr, p.RetentionDays, p.Parallel, compress,
		p.StorageID, p.AgentID, enabled, p.CreatedBy, p.UpdatedBy, p.CreatedAt, p.UpdatedAt)
	if err != nil {
		return err
	}
	if p.TenantID == "" {
		p.TenantID = d.inheritTenantFromInstance(p.InstanceID)
	}
	return d.AssignTenantID("tbl_backup_policies", p.ID, p.TenantID)
}

func (d *DB) UpdateBackupPolicy(p *BackupPolicy) error {
	p.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	p.DatabasesJSON = encodeBackupDBs(p.Databases)
	if p.RetentionDays <= 0 {
		p.RetentionDays = 7
	}
	if p.Parallel <= 0 {
		p.Parallel = 1
	}
	compress, enabled := 0, 0
	if p.Compress {
		compress = 1
	}
	if p.Enabled {
		enabled = 1
	}
	_, err := d.Exec(`UPDATE tbl_backup_policies SET name=?, instance_id=?, engine=?, method=?, databases_json=?, schedule_expr=?, retention_days=?, parallel=?, compress=?, storage_id=?, agent_id=?, enabled=?, updated_by=?, updated_at=? WHERE id=?`,
		p.Name, p.InstanceID, p.Engine, p.Method, p.DatabasesJSON, p.ScheduleExpr, p.RetentionDays, p.Parallel, compress,
		p.StorageID, p.AgentID, enabled, p.UpdatedBy, p.UpdatedAt, p.ID)
	return err
}

func (d *DB) DeleteBackupPolicy(id string) error {
	_, err := d.Exec(`DELETE FROM tbl_backup_policies WHERE id=?`, id)
	return err
}

func (d *DB) SetBackupPolicyEnabled(id string, enabled bool, updatedBy string) error {
	flag := 0
	if enabled {
		flag = 1
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`UPDATE tbl_backup_policies SET enabled=?, updated_by=?, updated_at=? WHERE id=?`, flag, updatedBy, now, id)
	return err
}

func (d *DB) ListEnabledBackupPolicies() ([]BackupPolicy, error) {
	rows, err := d.Query(`SELECT p.id, p.name, p.instance_id, COALESCE(i.name,''), p.engine, p.method, p.databases_json, p.schedule_expr,
		p.retention_days, p.parallel, p.compress, p.storage_id, p.agent_id, p.enabled, p.created_by, p.updated_by, p.created_at, p.updated_at
		FROM tbl_backup_policies p LEFT JOIN tbl_instances i ON i.id=p.instance_id WHERE p.enabled=1 ORDER BY p.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BackupPolicy
	for rows.Next() {
		var p BackupPolicy
		var compress, enabled int
		if err := rows.Scan(&p.ID, &p.Name, &p.InstanceID, &p.InstanceName, &p.Engine, &p.Method, &p.DatabasesJSON, &p.ScheduleExpr,
			&p.RetentionDays, &p.Parallel, &compress, &p.StorageID, &p.AgentID, &enabled, &p.CreatedBy, &p.UpdatedBy, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.Compress = compress != 0
		p.Enabled = enabled != 0
		_ = scanBackupPolicy(&p)
		out = append(out, p)
	}
	if out == nil {
		out = []BackupPolicy{}
	}
	return out, rows.Err()
}

// --- Run ---

func (d *DB) CreateBackupRun(r *BackupRun) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	if r.Status == "" {
		r.Status = BackupRunPending
	}
	r.CreatedAt = now
	if r.TenantID == "" {
		r.TenantID = d.inheritTenantFromInstance(r.InstanceID)
	}
	_, err := d.Exec(`INSERT INTO tbl_backup_runs(id, policy_id, instance_id, engine, method, status, trigger_src, parent_run_id, set_label, object_key, local_path, size_bytes, checksum, log_summary, error_message, extra_json, started_at, finished_at, created_by, created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.PolicyID, r.InstanceID, r.Engine, r.Method, r.Status, r.Trigger, r.ParentRunID, r.SetLabel, r.ObjectKey, r.LocalPath, r.SizeBytes, r.Checksum,
		r.LogSummary, r.ErrorMessage, r.ExtraJSON, r.StartedAt, r.FinishedAt, r.CreatedBy, r.CreatedAt)
	if err != nil {
		return err
	}
	if strings.TrimSpace(r.TenantID) != "" {
		return d.AssignTenantID("tbl_backup_runs", r.ID, r.TenantID)
	}
	return nil
}

func (d *DB) UpdateBackupRun(r *BackupRun) error {
	_, err := d.Exec(`UPDATE tbl_backup_runs SET status=?, method=?, parent_run_id=?, set_label=?, object_key=?, local_path=?, size_bytes=?, checksum=?, log_summary=?, error_message=?, extra_json=?, started_at=?, finished_at=? WHERE id=?`,
		r.Status, r.Method, r.ParentRunID, r.SetLabel, r.ObjectKey, r.LocalPath, r.SizeBytes, r.Checksum, r.LogSummary, r.ErrorMessage, r.ExtraJSON, r.StartedAt, r.FinishedAt, r.ID)
	return err
}

func (d *DB) GetBackupRun(id string) (*BackupRun, error) {
	r := &BackupRun{}
	err := d.QueryRow(`SELECT r.id, r.policy_id, COALESCE(p.name,''), r.instance_id, r.engine, COALESCE(r.method,''), r.status, r.trigger_src,
		COALESCE(r.parent_run_id,''), COALESCE(r.set_label,''), r.object_key, r.local_path,
		r.size_bytes, r.checksum, r.log_summary, r.error_message, COALESCE(r.extra_json,''), r.started_at, r.finished_at, r.created_by, r.created_at
		FROM tbl_backup_runs r LEFT JOIN tbl_backup_policies p ON p.id=r.policy_id WHERE r.id=?`, id).Scan(
		&r.ID, &r.PolicyID, &r.PolicyName, &r.InstanceID, &r.Engine, &r.Method, &r.Status, &r.Trigger,
		&r.ParentRunID, &r.SetLabel, &r.ObjectKey, &r.LocalPath,
		&r.SizeBytes, &r.Checksum, &r.LogSummary, &r.ErrorMessage, &r.ExtraJSON, &r.StartedAt, &r.FinishedAt, &r.CreatedBy, &r.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (d *DB) ListBackupRuns(policyID, instanceID, status string, limit int) ([]BackupRun, error) {
	return d.ListBackupRunsInTenant("", policyID, instanceID, status, limit)
}

func (d *DB) ListBackupRunsInTenant(tenantID, policyID, instanceID, status string, limit int) ([]BackupRun, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT r.id, r.policy_id, COALESCE(p.name,''), r.instance_id, r.engine, COALESCE(r.method,''), r.status, r.trigger_src,
		COALESCE(r.parent_run_id,''), COALESCE(r.set_label,''), r.object_key, r.local_path,
		r.size_bytes, r.checksum, r.log_summary, r.error_message, COALESCE(r.extra_json,''), r.started_at, r.finished_at, r.created_by, r.created_at
		FROM tbl_backup_runs r LEFT JOIN tbl_backup_policies p ON p.id=r.policy_id WHERE 1=1`
	var args []any
	if tid := strings.TrimSpace(tenantID); tid != "" {
		q += ` AND COALESCE(r.tenant_id,'')=?`
		args = append(args, tid)
	}
	if id := strings.TrimSpace(policyID); id != "" {
		q += ` AND r.policy_id=?`
		args = append(args, id)
	}
	if id := strings.TrimSpace(instanceID); id != "" {
		q += ` AND r.instance_id=?`
		args = append(args, id)
	}
	if st := strings.TrimSpace(status); st != "" {
		q += ` AND r.status=?`
		args = append(args, st)
	}
	q += ` ORDER BY r.created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BackupRun
	for rows.Next() {
		var r BackupRun
		if err := rows.Scan(&r.ID, &r.PolicyID, &r.PolicyName, &r.InstanceID, &r.Engine, &r.Method, &r.Status, &r.Trigger,
			&r.ParentRunID, &r.SetLabel, &r.ObjectKey, &r.LocalPath,
			&r.SizeBytes, &r.Checksum, &r.LogSummary, &r.ErrorMessage, &r.ExtraJSON, &r.StartedAt, &r.FinishedAt, &r.CreatedBy, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if out == nil {
		out = []BackupRun{}
	}
	return out, rows.Err()
}

// LastSuccessfulPhysicalBase 返回策略最近一次成功的物理全量或增量 Run（作增量基）。
func (d *DB) LastSuccessfulPhysicalBase(policyID string) (*BackupRun, error) {
	r := &BackupRun{}
	err := d.QueryRow(`SELECT r.id, r.policy_id, COALESCE(p.name,''), r.instance_id, r.engine, COALESCE(r.method,''), r.status, r.trigger_src,
		COALESCE(r.parent_run_id,''), COALESCE(r.set_label,''), r.object_key, r.local_path,
		r.size_bytes, r.checksum, r.log_summary, r.error_message, COALESCE(r.extra_json,''), r.started_at, r.finished_at, r.created_by, r.created_at
		FROM tbl_backup_runs r LEFT JOIN tbl_backup_policies p ON p.id=r.policy_id
		WHERE r.policy_id=? AND r.status=? AND r.method IN (?,?)
		ORDER BY r.created_at DESC LIMIT 1`,
		policyID, BackupRunSuccess, BackupMethodPhysicalFull, BackupMethodPhysicalIncr,
	).Scan(
		&r.ID, &r.PolicyID, &r.PolicyName, &r.InstanceID, &r.Engine, &r.Method, &r.Status, &r.Trigger,
		&r.ParentRunID, &r.SetLabel, &r.ObjectKey, &r.LocalPath,
		&r.SizeBytes, &r.Checksum, &r.LogSummary, &r.ErrorMessage, &r.ExtraJSON, &r.StartedAt, &r.FinishedAt, &r.CreatedBy, &r.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (d *DB) LastBackupRunAt(policyID string) string {
	var at string
	_ = d.QueryRow(`SELECT COALESCE(started_at, created_at) FROM tbl_backup_runs WHERE policy_id=? AND status IN (?,?) ORDER BY created_at DESC LIMIT 1`,
		policyID, BackupRunSuccess, BackupRunFailed).Scan(&at)
	return at
}

func (d *DB) HasRunningBackupForInstance(instanceID string) (bool, error) {
	n, err := d.Count(`SELECT COUNT(1) FROM tbl_backup_runs WHERE instance_id=? AND status=?`, instanceID, BackupRunRunning)
	return n > 0, err
}

func (d *DB) Count(query string, args ...any) (int64, error) {
	var n int64
	err := d.QueryRow(query, args...).Scan(&n)
	return n, err
}

// BackupAgentHostTag 标记平台下发的 host 实例。
func BackupAgentHostTag(hostInstanceID string) string {
	return "host:" + strings.TrimSpace(hostInstanceID)
}

// BackupAgentEscalateTag 标记下发时的提权模式。
func BackupAgentEscalateTag(mode string) string {
	m := strings.ToLower(strings.TrimSpace(mode))
	if m == "" || m == "none" {
		return ""
	}
	return "escalate:" + m
}

// ParseBackupAgentHostTag 从 tags 解析 host 实例 ID。
func ParseBackupAgentHostTag(tags string) string {
	for _, p := range strings.Split(tags, ",") {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "host:") {
			return strings.TrimSpace(strings.TrimPrefix(p, "host:"))
		}
	}
	return ""
}

// ParseBackupAgentEscalateTag 从 tags 解析提权模式。
func ParseBackupAgentEscalateTag(tags string) string {
	for _, p := range strings.Split(tags, ",") {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "escalate:") {
			return strings.TrimSpace(strings.TrimPrefix(p, "escalate:"))
		}
	}
	return ""
}

// MergeBackupAgentTags 合并 host / escalate 等标记（保留其它未知 tag）。
func MergeBackupAgentTags(existing, hostInstanceID, escalate string) string {
	keep := []string{}
	for _, p := range strings.Split(existing, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, "host:") || strings.HasPrefix(p, "escalate:") {
			continue
		}
		keep = append(keep, p)
	}
	if hid := strings.TrimSpace(hostInstanceID); hid != "" {
		keep = append(keep, BackupAgentHostTag(hid))
	}
	if et := BackupAgentEscalateTag(escalate); et != "" {
		keep = append(keep, et)
	}
	return strings.Join(keep, ",")
}

func decorateBackupAgent(a *BackupAgent) {
	if a == nil {
		return
	}
	a.HostInstance = ParseBackupAgentHostTag(a.Tags)
	a.PasswordSet = a.PasswordEnc != ""
}

// FindBackupAgentByHostInstance 按 host 实例查找已下发 Agent。
func (d *DB) FindBackupAgentByHostInstance(hostInstanceID string) (*BackupAgent, error) {
	tag := BackupAgentHostTag(hostInstanceID)
	rows, err := d.Query(`SELECT id, name, base_url, username, password_enc, engines, COALESCE(enabled_groups,''), COALESCE(capabilities,''), tags, remark, created_by, updated_by, created_at, updated_at
		FROM tbl_backup_agents WHERE tags LIKE ? ORDER BY updated_at DESC LIMIT 1`, "%"+tag+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	a := &BackupAgent{}
	if err := rows.Scan(&a.ID, &a.Name, &a.BaseURL, &a.Username, &a.PasswordEnc, &a.Engines, &a.EnabledGroups, &a.Capabilities, &a.Tags, &a.Remark,
		&a.CreatedBy, &a.UpdatedBy, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, err
	}
	decorateBackupAgent(a)
	return a, nil
}

// --- Agent Deploy ---

func (d *DB) CreateBackupAgentDeploy(it *BackupAgentDeploy) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if it.ID == "" {
		it.ID = uuid.NewString()
	}
	if it.Status == "" {
		it.Status = BackupDeployPending
	}
	if it.ListenPort <= 0 {
		it.ListenPort = 8090
	}
	it.CreatedAt, it.UpdatedAt = now, now
	_, err := d.Exec(`INSERT INTO tbl_backup_agent_deploys(id, host_instance_id, agent_id, status, listen_port, remote_dir, base_url, log_text, created_by, created_at, updated_at, finished_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		it.ID, it.HostInstanceID, it.AgentID, it.Status, it.ListenPort, it.RemoteDir, it.BaseURL, it.LogText,
		it.CreatedBy, it.CreatedAt, it.UpdatedAt, it.FinishedAt)
	if err != nil {
		return err
	}
	tid := d.inheritTenantFromInstance(it.HostInstanceID)
	if tid != "" {
		return d.AssignTenantID("tbl_backup_agent_deploys", it.ID, tid)
	}
	return nil
}

func (d *DB) UpdateBackupAgentDeploy(it *BackupAgentDeploy) error {
	it.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`UPDATE tbl_backup_agent_deploys SET agent_id=?, status=?, listen_port=?, remote_dir=?, base_url=?, log_text=?, updated_at=?, finished_at=? WHERE id=?`,
		it.AgentID, it.Status, it.ListenPort, it.RemoteDir, it.BaseURL, it.LogText, it.UpdatedAt, it.FinishedAt, it.ID)
	return err
}

func (d *DB) GetBackupAgentDeploy(id string) (*BackupAgentDeploy, error) {
	it := &BackupAgentDeploy{}
	err := d.QueryRow(`SELECT id, host_instance_id, agent_id, status, listen_port, remote_dir, base_url, log_text, created_by, created_at, updated_at, finished_at, COALESCE(kind,'platform'), COALESCE(payload,'')
		FROM tbl_backup_agent_deploys WHERE id=?`, id).Scan(
		&it.ID, &it.HostInstanceID, &it.AgentID, &it.Status, &it.ListenPort, &it.RemoteDir, &it.BaseURL, &it.LogText,
		&it.CreatedBy, &it.CreatedAt, &it.UpdatedAt, &it.FinishedAt, &it.Kind, &it.Payload,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return it, nil
}

func (d *DB) ListBackupAgentDeploys(hostInstanceID string, limit int) ([]BackupAgentDeploy, error) {
	return d.ListBackupAgentDeploysInTenant("", hostInstanceID, limit)
}

func (d *DB) ListBackupAgentDeploysInTenant(tenantID, hostInstanceID string, limit int) ([]BackupAgentDeploy, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT id, host_instance_id, agent_id, status, listen_port, remote_dir, base_url, log_text, created_by, created_at, updated_at, finished_at, COALESCE(kind,'platform')
		FROM tbl_backup_agent_deploys WHERE 1=1`
	var args []any
	q, args = appendTenantFilter(q, args, tenantID)
	if id := strings.TrimSpace(hostInstanceID); id != "" {
		q += ` AND host_instance_id=?`
		args = append(args, id)
	}
	q += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BackupAgentDeploy
	for rows.Next() {
		var it BackupAgentDeploy
		if err := rows.Scan(&it.ID, &it.HostInstanceID, &it.AgentID, &it.Status, &it.ListenPort, &it.RemoteDir, &it.BaseURL, &it.LogText,
			&it.CreatedBy, &it.CreatedAt, &it.UpdatedAt, &it.FinishedAt, &it.Kind); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	if out == nil {
		out = []BackupAgentDeploy{}
	}
	return out, rows.Err()
}

// LatestSuccessDeployForAgent 最近一次成功下发（优先按 agent_id，再按 host）。
func (d *DB) LatestSuccessDeployForAgent(agentID, hostInstanceID string) (*BackupAgentDeploy, error) {
	agentID = strings.TrimSpace(agentID)
	hostInstanceID = strings.TrimSpace(hostInstanceID)
	scanOne := func(q string, args ...any) (*BackupAgentDeploy, error) {
		it := &BackupAgentDeploy{}
		err := d.QueryRow(q, args...).Scan(
			&it.ID, &it.HostInstanceID, &it.AgentID, &it.Status, &it.ListenPort, &it.RemoteDir, &it.BaseURL, &it.LogText,
			&it.CreatedBy, &it.CreatedAt, &it.UpdatedAt, &it.FinishedAt,
		)
		if err == sql.ErrNoRows {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return it, nil
	}
	const cols = `id, host_instance_id, agent_id, status, listen_port, remote_dir, base_url, log_text, created_by, created_at, updated_at, finished_at`
	if agentID != "" {
		it, err := scanOne(`SELECT `+cols+` FROM tbl_backup_agent_deploys WHERE agent_id=? AND status=? AND COALESCE(kind,'platform')<>'sql' ORDER BY created_at DESC LIMIT 1`,
			agentID, BackupDeploySuccess)
		if err != nil || it != nil {
			return it, err
		}
	}
	if hostInstanceID != "" {
		return scanOne(`SELECT `+cols+` FROM tbl_backup_agent_deploys WHERE host_instance_id=? AND status=? AND COALESCE(kind,'platform')<>'sql' ORDER BY created_at DESC LIMIT 1`,
			hostInstanceID, BackupDeploySuccess)
	}
	return nil, nil
}
