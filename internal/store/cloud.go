package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	ArgCloudAPIQPS           = "cloud_api_qps"
	ArgCloudPgApprovers      = "cloud_pg_approvers"
	ArgCloudSlowlogRetention = "cloud_slowlog_retention_days"
)

func (d *DB) ensureCloudSchema() error {
	if err := d.renameLegacyCloudPGTables(); err != nil {
		return err
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS tbl_cloud_accounts (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL DEFAULT '',
			vendor TEXT NOT NULL,
			name TEXT NOT NULL,
			regions TEXT NOT NULL DEFAULT '[]',
			platform_region_ids TEXT NOT NULL DEFAULT '[]',
			env_ids TEXT NOT NULL DEFAULT '[]',
			sync_engines TEXT NOT NULL DEFAULT '[]',
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_cloud_accounts_name ON tbl_cloud_accounts(tenant_id, vendor, name)`,
		`CREATE TABLE IF NOT EXISTS tbl_cloud_account_keys (
			id TEXT PRIMARY KEY,
			account_id TEXT NOT NULL,
			role TEXT NOT NULL,
			secret_id TEXT NOT NULL DEFAULT '',
			secret_key_enc TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_cloud_account_keys ON tbl_cloud_account_keys(account_id, role)`,
		`CREATE TABLE IF NOT EXISTS tbl_cloud_account_users (
			account_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			created_at TEXT NOT NULL,
			PRIMARY KEY (account_id, user_id)
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_cloud_instances (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL DEFAULT '',
			vendor TEXT NOT NULL,
			engine TEXT NOT NULL DEFAULT 'postgres',
			account_id TEXT NOT NULL,
			cloud_instance_id TEXT NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			region TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT '',
			cpu INTEGER NOT NULL DEFAULT 0,
			memory_mb INTEGER NOT NULL DEFAULT 0,
			disk_gb INTEGER NOT NULL DEFAULT 0,
			disk_used_gb INTEGER NOT NULL DEFAULT 0,
			version TEXT NOT NULL DEFAULT '',
			zone TEXT NOT NULL DEFAULT '',
			pay_type TEXT NOT NULL DEFAULT '',
			expire_at TEXT NOT NULL DEFAULT '',
			instance_id TEXT NOT NULL DEFAULT '',
			reconcile TEXT NOT NULL DEFAULT '',
			extras TEXT NOT NULL DEFAULT '{}',
			synced_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_cloud_inst ON tbl_cloud_instances(vendor, account_id, engine, cloud_instance_id)`,
		`CREATE TABLE IF NOT EXISTS tbl_cloud_policies (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL DEFAULT '',
			account_id TEXT NOT NULL DEFAULT '',
			cloud_instance_id TEXT NOT NULL DEFAULT '',
			instance_id TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 0,
			severity TEXT NOT NULL DEFAULT '',
			rule_keyword TEXT NOT NULL DEFAULT '',
			action TEXT NOT NULL DEFAULT '',
			spec_cap_cpu INTEGER NOT NULL DEFAULT 0,
			spec_cap_memory INTEGER NOT NULL DEFAULT 0,
			disk_step_gb INTEGER NOT NULL DEFAULT 10,
			cooldown_min INTEGER NOT NULL DEFAULT 30,
			daily_max INTEGER NOT NULL DEFAULT 1,
			maint_window TEXT NOT NULL DEFAULT '',
			mode TEXT NOT NULL DEFAULT 'ticket',
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_cloud_sync_cursors (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL DEFAULT '',
			vendor TEXT NOT NULL,
			engine TEXT NOT NULL,
			account_id TEXT NOT NULL,
			cloud_instance_id TEXT NOT NULL,
			data_type TEXT NOT NULL,
			cursor_at TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '',
			last_count INTEGER NOT NULL DEFAULT 0,
			fail_count INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_cloud_cursor ON tbl_cloud_sync_cursors(vendor, engine, account_id, cloud_instance_id, data_type)`,
		`CREATE TABLE IF NOT EXISTS tbl_cloud_slow_logs (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL DEFAULT '',
			vendor TEXT NOT NULL,
			engine TEXT NOT NULL,
			account_id TEXT NOT NULL DEFAULT '',
			cloud_instance_id TEXT NOT NULL,
			log_time TEXT NOT NULL,
			db_name TEXT NOT NULL DEFAULT '',
			db_user TEXT NOT NULL DEFAULT '',
			client TEXT NOT NULL DEFAULT '',
			duration_ms INTEGER NOT NULL DEFAULT 0,
			sql_text TEXT NOT NULL DEFAULT '',
			fingerprint TEXT NOT NULL,
			raw_json TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_cloud_slow ON tbl_cloud_slow_logs(vendor, engine, cloud_instance_id, log_time, fingerprint)`,
		`CREATE INDEX IF NOT EXISTS idx_cloud_slow_time ON tbl_cloud_slow_logs(cloud_instance_id, log_time)`,
		`CREATE TABLE IF NOT EXISTS tbl_cloud_events (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL DEFAULT '',
			vendor TEXT NOT NULL,
			account_id TEXT NOT NULL,
			cloud_instance_id TEXT NOT NULL,
			event_type TEXT NOT NULL,
			message TEXT NOT NULL DEFAULT '',
			event_at TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_cloud_event ON tbl_cloud_events(vendor, account_id, cloud_instance_id, event_type, event_at)`,
	}
	for _, s := range stmts {
		if _, err := d.Exec(s); err != nil {
			return err
		}
	}
	if err := d.ensureInstanceCloudAccountColumn(); err != nil {
		return err
	}
	if err := d.ensureCloudAccountPlatformBindings(); err != nil {
		return err
	}
	if err := d.ensureCloudResourceEngineColumn(); err != nil {
		return err
	}
	return d.ensureCloudGlobalArgs()
}

func (d *DB) ensureCloudAccountPlatformBindings() error {
	if err := d.ensureColumnIfMissing("tbl_cloud_accounts", "platform_region_ids", "TEXT NOT NULL DEFAULT '[]'"); err != nil {
		return err
	}
	if err := d.ensureColumnIfMissing("tbl_cloud_accounts", "env_ids", "TEXT NOT NULL DEFAULT '[]'"); err != nil {
		return err
	}
	return d.ensureColumnIfMissing("tbl_cloud_accounts", "sync_engines", "TEXT NOT NULL DEFAULT '[]'")
}

// renameLegacyCloudPGTables 去掉历史表名中的 pg 前缀（多引擎共用）。
func (d *DB) renameLegacyCloudPGTables() error {
	renames := [][2]string{
		{"tbl_cloud_pg_instances", "tbl_cloud_instances"},
		{"tbl_cloud_pg_policies", "tbl_cloud_policies"},
		{"tbl_cloud_pg_events", "tbl_cloud_events"},
	}
	for _, r := range renames {
		oldOK, err := d.tableExistsPublic(r[0])
		if err != nil {
			return err
		}
		newOK, err := d.tableExistsPublic(r[1])
		if err != nil {
			return err
		}
		if oldOK && !newOK {
			if _, err := d.Exec(fmt.Sprintf(`ALTER TABLE %s RENAME TO %s`, r[0], r[1])); err != nil {
				return err
			}
		}
	}
	_, _ = d.Exec(`ALTER INDEX IF EXISTS uq_cloud_pg_inst RENAME TO uq_cloud_inst`)
	return nil
}

func (d *DB) ensureCloudResourceEngineColumn() error {
	var n int
	err := d.db.QueryRow(`
		SELECT COUNT(1) FROM information_schema.columns
		WHERE table_name = 'tbl_cloud_instances' AND column_name = 'engine'
	`).Scan(&n)
	if err != nil {
		return err
	}
	if n == 0 {
		if _, err := d.db.Exec(`ALTER TABLE tbl_cloud_instances ADD COLUMN engine TEXT NOT NULL DEFAULT 'postgres'`); err != nil {
			return err
		}
	}
	if _, err := d.db.Exec(`UPDATE tbl_cloud_instances SET engine='postgres' WHERE COALESCE(engine,'')=''`); err != nil {
		return err
	}
	_, _ = d.db.Exec(`DROP INDEX IF EXISTS uq_cloud_pg_inst`)
	_, _ = d.db.Exec(`DROP INDEX IF EXISTS uq_cloud_inst`)
	_, err = d.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uq_cloud_inst ON tbl_cloud_instances(vendor, account_id, engine, cloud_instance_id)`)
	return err
}

func (d *DB) ensureInstanceCloudAccountColumn() error {
	var n int
	err := d.db.QueryRow(`
		SELECT COUNT(1) FROM information_schema.columns
		WHERE table_name = 'tbl_instances' AND column_name = 'cloud_account_id'
	`).Scan(&n)
	if err != nil {
		return err
	}
	if n == 0 {
		if _, err := d.db.Exec(`ALTER TABLE tbl_instances ADD COLUMN cloud_account_id TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) ensureCloudGlobalArgs() error {
	defaults := []struct{ key, value, desc string }{
		{ArgCloudAPIQPS, "5", "每个云账号每秒最多调用次数"},
		{ArgCloudPgApprovers, "admin", "未关联平台实例的云资源工单审批人"},
		{ArgCloudSlowlogRetention, "15", "云慢日志在本库保留天数"},
	}
	for _, it := range defaults {
		var n int
		if err := d.QueryRow(`SELECT COUNT(1) FROM tbl_global_args WHERE key=?`, it.key).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			if err := d.CreateGlobalArg(it.key, it.value, it.desc); err != nil {
				return err
			}
		}
	}
	return nil
}

type CloudAccount struct {
	ID                string   `json:"id"`
	TenantID          string   `json:"tenant_id"`
	Vendor            string   `json:"vendor"`
	Name              string   `json:"name"`
	Regions           []string `json:"regions"`              // 云厂商地域，如 ap-beijing
	PlatformRegionIDs []string `json:"platform_region_ids"`  // 本平台区域 id
	EnvIDs            []string `json:"env_ids"`              // 本平台环境 id
	SyncEngines       []string `json:"sync_engines"`         // 定时同步的引擎；空则不参与定时任务
	Enabled           bool     `json:"enabled"`
	HasRead           bool     `json:"has_readonly"`
	HasOperate        bool     `json:"has_operate"`
	SecretID          string   `json:"secret_id,omitempty"`
	UserIDs           []string `json:"user_ids,omitempty"`
	CreatedAt         string   `json:"created_at"`
	UpdatedAt         string   `json:"updated_at"`
}

type CloudAccountKey struct {
	AccountID    string
	Role         string
	SecretID     string
	SecretKeyEnc string
}

func (d *DB) CreateCloudAccount(a *CloudAccount) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	a.Vendor = strings.ToLower(strings.TrimSpace(a.Vendor))
	a.Name = strings.TrimSpace(a.Name)
	if a.Name == "" || a.Vendor == "" {
		return fmt.Errorf("厂商和名称必填")
	}
	a.CreatedAt, a.UpdatedAt = now, now
	a.Enabled = true
	if a.Regions == nil {
		a.Regions = []string{}
	}
	if a.PlatformRegionIDs == nil {
		a.PlatformRegionIDs = []string{}
	}
	if a.EnvIDs == nil {
		a.EnvIDs = []string{}
	}
	a.SyncEngines = normalizeSyncEngines(a.SyncEngines)
	_, err := d.Exec(`INSERT INTO tbl_cloud_accounts(id, tenant_id, vendor, name, regions, platform_region_ids, env_ids, sync_engines, enabled, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.TenantID, a.Vendor, a.Name, encodeJSON(a.Regions), encodeJSON(a.PlatformRegionIDs), encodeJSON(a.EnvIDs), encodeJSON(a.SyncEngines), 1, a.CreatedAt, a.UpdatedAt)
	return err
}

// UpdateCloudAccount 更新名称、云地域、平台区域/环境、启用状态；厂商创建后不可改，避免密钥与实例绑定错位。
func (d *DB) UpdateCloudAccount(a *CloudAccount) error {
	a.Name = strings.TrimSpace(a.Name)
	if a.ID == "" || a.Name == "" {
		return fmt.Errorf("名称必填")
	}
	if a.Regions == nil {
		a.Regions = []string{}
	}
	if a.PlatformRegionIDs == nil {
		a.PlatformRegionIDs = []string{}
	}
	if a.EnvIDs == nil {
		a.EnvIDs = []string{}
	}
	a.SyncEngines = normalizeSyncEngines(a.SyncEngines)
	enabled := 0
	if a.Enabled {
		enabled = 1
	}
	a.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	res, err := d.Exec(`UPDATE tbl_cloud_accounts SET name=?, regions=?, platform_region_ids=?, env_ids=?, sync_engines=?, enabled=?, updated_at=? WHERE id=?`,
		a.Name, encodeJSON(a.Regions), encodeJSON(a.PlatformRegionIDs), encodeJSON(a.EnvIDs), encodeJSON(a.SyncEngines), enabled, a.UpdatedAt, a.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("云账号不存在")
	}
	return nil
}

func (d *DB) ListCloudAccounts(tenantID string, onlyEnabled bool) ([]CloudAccount, error) {
	q := `SELECT id, tenant_id, vendor, name, regions, COALESCE(platform_region_ids,'[]'), COALESCE(env_ids,'[]'), COALESCE(sync_engines,'[]'), enabled, created_at, updated_at FROM tbl_cloud_accounts WHERE 1=1`
	var args []any
	if strings.TrimSpace(tenantID) != "" {
		q += ` AND tenant_id=?`
		args = append(args, tenantID)
	}
	if onlyEnabled {
		q += ` AND enabled=1`
	}
	q += ` ORDER BY vendor, name`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CloudAccount
	for rows.Next() {
		a, err := scanCloudAccount(rows)
		if err != nil {
			return nil, err
		}
		d.fillCloudAccount(&a)
		out = append(out, a)
	}
	return out, rows.Err()
}

func scanCloudAccount(sc interface{ Scan(...any) error }) (CloudAccount, error) {
	var a CloudAccount
	var regions, platformRegions, envIDs, syncEngines string
	var enabled int
	err := sc.Scan(&a.ID, &a.TenantID, &a.Vendor, &a.Name, &regions, &platformRegions, &envIDs, &syncEngines, &enabled, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return a, err
	}
	a.Enabled = enabled != 0
	_ = json.Unmarshal([]byte(regions), &a.Regions)
	_ = json.Unmarshal([]byte(platformRegions), &a.PlatformRegionIDs)
	_ = json.Unmarshal([]byte(envIDs), &a.EnvIDs)
	_ = json.Unmarshal([]byte(syncEngines), &a.SyncEngines)
	if a.Regions == nil {
		a.Regions = []string{}
	}
	if a.PlatformRegionIDs == nil {
		a.PlatformRegionIDs = []string{}
	}
	if a.EnvIDs == nil {
		a.EnvIDs = []string{}
	}
	a.SyncEngines = normalizeSyncEngines(a.SyncEngines)
	return a, nil
}

func normalizeSyncEngines(in []string) []string {
	allow := map[string]bool{"postgres": true, "mysql": true, "redis": true, "host": true}
	var out []string
	seen := map[string]bool{}
	for _, e := range in {
		e = strings.TrimSpace(e)
		if !allow[e] || seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	if out == nil {
		out = []string{}
	}
	return out
}

func (d *DB) fillCloudAccount(a *CloudAccount) {
	rows, err := d.Query(`SELECT role, secret_id FROM tbl_cloud_account_keys WHERE account_id=?`, a.ID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var role, sid string
			if rows.Scan(&role, &sid) == nil {
				if role == "readonly" {
					a.HasRead = sid != ""
					a.SecretID = sid
				}
				if role == "operate" {
					a.HasOperate = true
				}
			}
		}
	}
	urows, err := d.Query(`SELECT user_id FROM tbl_cloud_account_users WHERE account_id=?`, a.ID)
	if err == nil {
		defer urows.Close()
		for urows.Next() {
			var id string
			if urows.Scan(&id) == nil {
				a.UserIDs = append(a.UserIDs, id)
			}
		}
	}
}

func (d *DB) GetCloudAccount(id string) (*CloudAccount, error) {
	row := d.QueryRow(`SELECT id, tenant_id, vendor, name, regions, COALESCE(platform_region_ids,'[]'), COALESCE(env_ids,'[]'), COALESCE(sync_engines,'[]'), enabled, created_at, updated_at FROM tbl_cloud_accounts WHERE id=?`, id)
	a, err := scanCloudAccount(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d.fillCloudAccount(&a)
	return &a, nil
}

func (d *DB) SaveCloudAccountKey(accountID, role, secretID, secretEnc string) error {
	role = strings.TrimSpace(role)
	if role != "readonly" && role != "operate" {
		return fmt.Errorf("密钥角色只能是 readonly 或 operate")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var id string
	err := d.QueryRow(`SELECT id FROM tbl_cloud_account_keys WHERE account_id=? AND role=?`, accountID, role).Scan(&id)
	if err == sql.ErrNoRows {
		_, err = d.Exec(`INSERT INTO tbl_cloud_account_keys(id, account_id, role, secret_id, secret_key_enc, updated_at) VALUES(?,?,?,?,?,?)`,
			uuid.NewString(), accountID, role, secretID, secretEnc, now)
		return err
	}
	if err != nil {
		return err
	}
	_, err = d.Exec(`UPDATE tbl_cloud_account_keys SET secret_id=?, secret_key_enc=?, updated_at=? WHERE id=?`, secretID, secretEnc, now, id)
	return err
}

func (d *DB) CloudAccountKey(accountID, role string) (*CloudAccountKey, error) {
	k := &CloudAccountKey{}
	err := d.QueryRow(`SELECT account_id, role, secret_id, secret_key_enc FROM tbl_cloud_account_keys WHERE account_id=? AND role=?`, accountID, role).
		Scan(&k.AccountID, &k.Role, &k.SecretID, &k.SecretKeyEnc)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return k, nil
}

func (d *DB) SetCloudAccountUsers(accountID string, userIDs []string) error {
	if _, err := d.Exec(`DELETE FROM tbl_cloud_account_users WHERE account_id=?`, accountID); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	seen := map[string]bool{}
	for _, id := range userIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if _, err := d.Exec(`INSERT INTO tbl_cloud_account_users(account_id, user_id, created_at) VALUES(?,?,?)`, accountID, id, now); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) UserBoundToCloudAccount(accountID, userID string) (bool, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(1) FROM tbl_cloud_account_users WHERE account_id=? AND user_id=?`, accountID, userID).Scan(&n)
	return n > 0, err
}

func (d *DB) FindCloudAccountByName(tenantID, vendor, name string) (*CloudAccount, error) {
	row := d.QueryRow(`SELECT id, tenant_id, vendor, name, regions, COALESCE(platform_region_ids,'[]'), COALESCE(env_ids,'[]'), COALESCE(sync_engines,'[]'), enabled, created_at, updated_at FROM tbl_cloud_accounts WHERE tenant_id=? AND vendor=? AND name=?`,
		tenantID, vendor, name)
	a, err := scanCloudAccount(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

type CloudInstance struct {
	ID           string         `json:"id"`
	TenantID     string         `json:"tenant_id"`
	Vendor       string         `json:"vendor"`
	Engine       string         `json:"engine"`
	AccountID    string         `json:"account_id"`
	AccountName  string         `json:"account_name,omitempty"`
	CloudID      string         `json:"cloud_instance_id"`
	Name         string         `json:"name"`
	Region       string         `json:"region"`
	Status       string         `json:"status"`
	CPU          int            `json:"cpu"`
	MemoryMB     int            `json:"memory_mb"`
	DiskGB       int            `json:"disk_gb"`
	DiskUsedGB   int            `json:"disk_used_gb"`
	Version      string         `json:"version"`
	Zone         string         `json:"zone"`
	PayType      string         `json:"pay_type"`
	ExpireAt     string         `json:"expire_at"`
	InstanceID   string         `json:"instance_id"`
	InstanceName string         `json:"instance_name,omitempty"`
	Reconcile    string         `json:"reconcile"`
	Extras       map[string]any `json:"extras,omitempty"`
	SyncedAt     string         `json:"synced_at"`
}

func (d *DB) UpsertCloudInstance(it *CloudInstance) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if it.SyncedAt == "" {
		it.SyncedAt = now
	}
	if strings.TrimSpace(it.Engine) == "" {
		it.Engine = "postgres"
	}
	var id string
	err := d.QueryRow(`SELECT id FROM tbl_cloud_instances WHERE vendor=? AND account_id=? AND engine=? AND cloud_instance_id=?`,
		it.Vendor, it.AccountID, it.Engine, it.CloudID).Scan(&id)
	extra := encodeJSON(it.Extras)
	if err == sql.ErrNoRows {
		it.ID = uuid.NewString()
		_, err = d.Exec(`INSERT INTO tbl_cloud_instances(id, tenant_id, vendor, engine, account_id, cloud_instance_id, name, region, status, cpu, memory_mb, disk_gb, disk_used_gb, version, zone, pay_type, expire_at, instance_id, reconcile, extras, synced_at, updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			it.ID, it.TenantID, it.Vendor, it.Engine, it.AccountID, it.CloudID, it.Name, it.Region, it.Status, it.CPU, it.MemoryMB, it.DiskGB, it.DiskUsedGB,
			it.Version, it.Zone, it.PayType, it.ExpireAt, it.InstanceID, it.Reconcile, extra, it.SyncedAt, now)
		return err
	}
	if err != nil {
		return err
	}
	it.ID = id
	_, err = d.Exec(`UPDATE tbl_cloud_instances SET name=?, region=?, status=?, cpu=?, memory_mb=?, disk_gb=?, disk_used_gb=?, version=?, zone=?, pay_type=?, expire_at=?, instance_id=?, reconcile=?, extras=?, synced_at=?, updated_at=? WHERE id=?`,
		it.Name, it.Region, it.Status, it.CPU, it.MemoryMB, it.DiskGB, it.DiskUsedGB, it.Version, it.Zone, it.PayType, it.ExpireAt, it.InstanceID, it.Reconcile, extra, it.SyncedAt, now, id)
	return err
}

func (d *DB) ListCloudInstances(tenantID, reconcile string) ([]CloudInstance, error) {
	return d.ListCloudResources(tenantID, "", reconcile)
}

func (d *DB) ListCloudResources(tenantID, engine, reconcile string) ([]CloudInstance, error) {
	q := `SELECT id, tenant_id, vendor, COALESCE(engine,'postgres'), account_id, cloud_instance_id, name, region, status, cpu, memory_mb, disk_gb, disk_used_gb, version, zone, pay_type, expire_at, instance_id, reconcile, synced_at FROM tbl_cloud_instances WHERE 1=1`
	var args []any
	if tenantID != "" {
		q += ` AND tenant_id=?`
		args = append(args, tenantID)
	}
	if engine != "" {
		q += ` AND COALESCE(engine,'postgres')=?`
		args = append(args, engine)
	}
	if reconcile != "" {
		q += ` AND reconcile=?`
		args = append(args, reconcile)
	}
	q += ` ORDER BY synced_at DESC`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CloudInstance
	for rows.Next() {
		var it CloudInstance
		if err := rows.Scan(&it.ID, &it.TenantID, &it.Vendor, &it.Engine, &it.AccountID, &it.CloudID, &it.Name, &it.Region, &it.Status, &it.CPU, &it.MemoryMB, &it.DiskGB, &it.DiskUsedGB, &it.Version, &it.Zone, &it.PayType, &it.ExpireAt, &it.InstanceID, &it.Reconcile, &it.SyncedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (d *DB) MarkMissingCloudPostgres(accountID string, seen []string) error {
	return d.MarkMissingCloudResources(accountID, "postgres", seen)
}

func (d *DB) MarkMissingCloudResources(accountID, engine string, seen []string) error {
	if engine == "" {
		engine = "postgres"
	}
	rows, err := d.Query(`SELECT id, cloud_instance_id FROM tbl_cloud_instances WHERE account_id=? AND COALESCE(engine,'postgres')=?`, accountID, engine)
	if err != nil {
		return err
	}
	defer rows.Close()
	have := map[string]bool{}
	for _, id := range seen {
		have[id] = true
	}
	var missing []string
	for rows.Next() {
		var id, cid string
		if rows.Scan(&id, &cid) == nil && !have[cid] {
			missing = append(missing, id)
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, id := range missing {
		if _, err := d.Exec(`UPDATE tbl_cloud_instances SET reconcile='missing', updated_at=? WHERE id=?`, now, id); err != nil {
			return err
		}
	}
	return nil
}

func decodeAnyMap(s string) map[string]any {
	s = strings.TrimSpace(s)
	if s == "" || s == "null" {
		return nil
	}
	var m map[string]any
	if json.Unmarshal([]byte(s), &m) != nil {
		return nil
	}
	return m
}

func (d *DB) GetCloudResource(id string) (*CloudInstance, error) {
	row := d.QueryRow(`SELECT id, tenant_id, vendor, COALESCE(engine,'postgres'), account_id, cloud_instance_id, name, region, status, cpu, memory_mb, disk_gb, disk_used_gb, version, zone, pay_type, expire_at, instance_id, reconcile, extras, synced_at FROM tbl_cloud_instances WHERE id=?`, id)
	var it CloudInstance
	var extra string
	err := row.Scan(&it.ID, &it.TenantID, &it.Vendor, &it.Engine, &it.AccountID, &it.CloudID, &it.Name, &it.Region, &it.Status, &it.CPU, &it.MemoryMB, &it.DiskGB, &it.DiskUsedGB, &it.Version, &it.Zone, &it.PayType, &it.ExpireAt, &it.InstanceID, &it.Reconcile, &extra, &it.SyncedAt)
	it.Extras = decodeAnyMap(extra)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &it, nil
}

func (d *DB) ListCloudResourcesByCloudID(vendor, engine, cloudID string) ([]CloudInstance, error) {
	if engine == "" {
		engine = "postgres"
	}
	rows, err := d.Query(`SELECT id, tenant_id, vendor, COALESCE(engine,'postgres'), account_id, cloud_instance_id, name, region, status, cpu, memory_mb, disk_gb, disk_used_gb, version, zone, pay_type, expire_at, instance_id, reconcile, synced_at FROM tbl_cloud_instances WHERE vendor=? AND COALESCE(engine,'postgres')=? AND cloud_instance_id=?`, vendor, engine, cloudID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CloudInstance
	for rows.Next() {
		var it CloudInstance
		if err := rows.Scan(&it.ID, &it.TenantID, &it.Vendor, &it.Engine, &it.AccountID, &it.CloudID, &it.Name, &it.Region, &it.Status, &it.CPU, &it.MemoryMB, &it.DiskGB, &it.DiskUsedGB, &it.Version, &it.Zone, &it.PayType, &it.ExpireAt, &it.InstanceID, &it.Reconcile, &it.SyncedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (d *DB) DeleteCloudResource(id string) error {
	_, err := d.Exec(`DELETE FROM tbl_cloud_instances WHERE id=?`, id)
	return err
}

func (d *DB) UpdateCloudResourceOwnership(id, accountID, instanceID, reconcile string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`UPDATE tbl_cloud_instances SET account_id=?, instance_id=?, reconcile=?, updated_at=? WHERE id=?`,
		accountID, instanceID, reconcile, now, id)
	return err
}

func (d *DB) GetCloudPostgresByCloudID(vendor, cloudID string) (*CloudInstance, error) {
	return d.GetCloudResourceByCloudID(vendor, "postgres", cloudID)
}

func (d *DB) GetCloudResourceByCloudID(vendor, engine, cloudID string) (*CloudInstance, error) {
	if engine == "" {
		engine = "postgres"
	}
	row := d.QueryRow(`SELECT id, tenant_id, vendor, COALESCE(engine,'postgres'), account_id, cloud_instance_id, name, region, status, cpu, memory_mb, disk_gb, disk_used_gb, version, zone, pay_type, expire_at, instance_id, reconcile, extras, synced_at FROM tbl_cloud_instances WHERE vendor=? AND COALESCE(engine,'postgres')=? AND cloud_instance_id=? ORDER BY updated_at DESC`, vendor, engine, cloudID)
	var it CloudInstance
	var extra string
	err := row.Scan(&it.ID, &it.TenantID, &it.Vendor, &it.Engine, &it.AccountID, &it.CloudID, &it.Name, &it.Region, &it.Status, &it.CPU, &it.MemoryMB, &it.DiskGB, &it.DiskUsedGB, &it.Version, &it.Zone, &it.PayType, &it.ExpireAt, &it.InstanceID, &it.Reconcile, &extra, &it.SyncedAt)
	it.Extras = decodeAnyMap(extra)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &it, nil
}

func (d *DB) CountCloudPostgresByCloudID(vendor, cloudID string) (int, error) {
	return d.CountCloudResourcesByCloudID(vendor, "postgres", cloudID)
}

func (d *DB) CountCloudResourcesByCloudID(vendor, engine, cloudID string) (int, error) {
	if engine == "" {
		engine = "postgres"
	}
	var n int
	err := d.QueryRow(`SELECT COUNT(1) FROM tbl_cloud_instances WHERE vendor=? AND COALESCE(engine,'postgres')=? AND cloud_instance_id=?`, vendor, engine, cloudID).Scan(&n)
	return n, err
}

func (d *DB) InstancesByCloudID(tenantID, vendor, cloudID string) ([]Instance, error) {
	q := `SELECT ` + instanceCols + ` FROM tbl_instances WHERE cloud_vendor=? AND cloud_instance_id=?`
	args := []any{vendor, cloudID}
	if tenantID != "" {
		q += ` AND COALESCE(tenant_id,'')=?`
		args = append(args, tenantID)
	}
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Instance
	for rows.Next() {
		it, err := scanInstance(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

func (d *DB) SetInstanceCloudAccount(instanceID, accountID string) error {
	_, err := d.Exec(`UPDATE tbl_instances SET cloud_account_id=?, updated_at=? WHERE id=?`, accountID, time.Now().UTC().Format(time.RFC3339), instanceID)
	return err
}

func (d *DB) ListInstancesMissingCloudAccount(vendor string) ([]Instance, error) {
	rows, err := d.Query(`SELECT `+instanceCols+` FROM tbl_instances WHERE cloud_vendor=? AND COALESCE(cloud_account_id,'')='' AND COALESCE(cloud_instance_id,'')<>''`, vendor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Instance
	for rows.Next() {
		it, err := scanInstance(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

type CloudPolicy struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenant_id"`
	AccountID   string `json:"account_id"`
	CloudID     string `json:"cloud_instance_id"`
	InstanceID  string `json:"instance_id"`
	Enabled     bool   `json:"enabled"`
	Severity    string `json:"severity"`
	RuleKeyword string `json:"rule_keyword"`
	Action      string `json:"action"`
	CapCPU      int    `json:"spec_cap_cpu"`
	CapMemoryMB int    `json:"spec_cap_memory"`
	DiskStepGB  int    `json:"disk_step_gb"`
	CooldownMin int    `json:"cooldown_min"`
	DailyMax    int    `json:"daily_max"`
	Window      string `json:"window"`
	Mode        string `json:"mode"`
	UpdatedAt   string `json:"updated_at"`
}

func (d *DB) SaveCloudPolicy(p *CloudPolicy) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if p.Mode == "" {
		p.Mode = "ticket"
	}
	if p.DiskStepGB <= 0 {
		p.DiskStepGB = 10
	}
	en := 0
	if p.Enabled {
		en = 1
	}
	if p.ID == "" {
		p.ID = uuid.NewString()
		_, err := d.Exec(`INSERT INTO tbl_cloud_policies(id, tenant_id, account_id, cloud_instance_id, instance_id, enabled, severity, rule_keyword, action, spec_cap_cpu, spec_cap_memory, disk_step_gb, cooldown_min, daily_max, maint_window, mode, updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			p.ID, p.TenantID, p.AccountID, p.CloudID, p.InstanceID, en, p.Severity, p.RuleKeyword, p.Action, p.CapCPU, p.CapMemoryMB, p.DiskStepGB, p.CooldownMin, p.DailyMax, p.Window, p.Mode, now)
		p.UpdatedAt = now
		return err
	}
	_, err := d.Exec(`UPDATE tbl_cloud_policies SET account_id=?, cloud_instance_id=?, instance_id=?, enabled=?, severity=?, rule_keyword=?, action=?, spec_cap_cpu=?, spec_cap_memory=?, disk_step_gb=?, cooldown_min=?, daily_max=?, maint_window=?, mode=?, updated_at=? WHERE id=?`,
		p.AccountID, p.CloudID, p.InstanceID, en, p.Severity, p.RuleKeyword, p.Action, p.CapCPU, p.CapMemoryMB, p.DiskStepGB, p.CooldownMin, p.DailyMax, p.Window, p.Mode, now, p.ID)
	p.UpdatedAt = now
	return err
}

func (d *DB) ListCloudPolicies(tenantID string) ([]CloudPolicy, error) {
	q := `SELECT id, tenant_id, account_id, cloud_instance_id, instance_id, enabled, severity, rule_keyword, action, spec_cap_cpu, spec_cap_memory, disk_step_gb, cooldown_min, daily_max, maint_window, mode, updated_at FROM tbl_cloud_policies WHERE 1=1`
	var args []any
	if tenantID != "" {
		q += ` AND tenant_id=?`
		args = append(args, tenantID)
	}
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CloudPolicy
	for rows.Next() {
		var p CloudPolicy
		var en int
		if err := rows.Scan(&p.ID, &p.TenantID, &p.AccountID, &p.CloudID, &p.InstanceID, &en, &p.Severity, &p.RuleKeyword, &p.Action, &p.CapCPU, &p.CapMemoryMB, &p.DiskStepGB, &p.CooldownMin, &p.DailyMax, &p.Window, &p.Mode, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.Enabled = en != 0
		out = append(out, p)
	}
	return out, rows.Err()
}

func (d *DB) ListEnabledCloudPolicies() ([]CloudPolicy, error) {
	list, err := d.ListCloudPolicies("")
	if err != nil {
		return nil, err
	}
	var out []CloudPolicy
	for _, p := range list {
		if p.Enabled {
			out = append(out, p)
		}
	}
	return out, nil
}

type CloudCursor struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	Vendor    string `json:"vendor"`
	Engine    string `json:"engine"`
	AccountID string `json:"account_id"`
	CloudID   string `json:"cloud_instance_id"`
	DataType  string `json:"data_type"`
	CursorAt  string `json:"cursor_at"`
	Status    string `json:"status"`
	Error     string `json:"error"`
	LastCount int    `json:"last_count"`
	FailCount int    `json:"fail_count"`
	UpdatedAt string `json:"updated_at"`
}

func (d *DB) GetCloudCursor(vendor, engine, accountID, cloudID, dataType string) (*CloudCursor, error) {
	row := d.QueryRow(`SELECT id, tenant_id, vendor, engine, account_id, cloud_instance_id, data_type, cursor_at, status, error, last_count, fail_count, updated_at
		FROM tbl_cloud_sync_cursors WHERE vendor=? AND engine=? AND account_id=? AND cloud_instance_id=? AND data_type=?`,
		vendor, engine, accountID, cloudID, dataType)
	var c CloudCursor
	err := row.Scan(&c.ID, &c.TenantID, &c.Vendor, &c.Engine, &c.AccountID, &c.CloudID, &c.DataType, &c.CursorAt, &c.Status, &c.Error, &c.LastCount, &c.FailCount, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (d *DB) SaveCloudCursor(c *CloudCursor) error {
	now := time.Now().UTC().Format(time.RFC3339)
	exist, err := d.GetCloudCursor(c.Vendor, c.Engine, c.AccountID, c.CloudID, c.DataType)
	if err != nil {
		return err
	}
	if exist == nil {
		c.ID = uuid.NewString()
		_, err = d.Exec(`INSERT INTO tbl_cloud_sync_cursors(id, tenant_id, vendor, engine, account_id, cloud_instance_id, data_type, cursor_at, status, error, last_count, fail_count, updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			c.ID, c.TenantID, c.Vendor, c.Engine, c.AccountID, c.CloudID, c.DataType, c.CursorAt, c.Status, c.Error, c.LastCount, c.FailCount, now)
		return err
	}
	c.ID = exist.ID
	_, err = d.Exec(`UPDATE tbl_cloud_sync_cursors SET cursor_at=?, status=?, error=?, last_count=?, fail_count=?, updated_at=? WHERE id=?`,
		c.CursorAt, c.Status, c.Error, c.LastCount, c.FailCount, now, exist.ID)
	return err
}

func (d *DB) CountQueuedCursors() (int, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(1) FROM tbl_cloud_sync_cursors WHERE status='queued'`).Scan(&n)
	return n, err
}

type CloudSlowLog struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenant_id"`
	Vendor      string `json:"vendor"`
	Engine      string `json:"engine"`
	AccountID   string `json:"account_id"`
	CloudID     string `json:"cloud_instance_id"`
	LogTime     string `json:"log_time"`
	DBName      string `json:"db_name"`
	DBUser      string `json:"db_user"`
	Client      string `json:"client"`
	Duration    int    `json:"duration_ms"`
	SQL         string `json:"sql_text"`
	Fingerprint string `json:"fingerprint"`
}

func (d *DB) InsertCloudSlowLog(row *CloudSlowLog) error {
	if row.ID == "" {
		row.ID = uuid.NewString()
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`INSERT INTO tbl_cloud_slow_logs(id, tenant_id, vendor, engine, account_id, cloud_instance_id, log_time, db_name, db_user, client, duration_ms, sql_text, fingerprint, raw_json, created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`,
		row.ID, row.TenantID, row.Vendor, row.Engine, row.AccountID, row.CloudID, row.LogTime, row.DBName, row.DBUser, row.Client, row.Duration, row.SQL, row.Fingerprint, "", now)
	return err
}

func (d *DB) ListCloudSlowLogs(cloudID, from, to, dbName, keyword string, minMS, limit int) ([]CloudSlowLog, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	q := `SELECT id, tenant_id, vendor, engine, account_id, cloud_instance_id, log_time, db_name, db_user, client, duration_ms, sql_text, fingerprint
		FROM tbl_cloud_slow_logs WHERE cloud_instance_id=?`
	args := []any{cloudID}
	if from != "" {
		q += ` AND log_time>=?`
		args = append(args, from)
	}
	if to != "" {
		q += ` AND log_time<=?`
		args = append(args, to)
	}
	if dbName != "" {
		q += ` AND db_name=?`
		args = append(args, dbName)
	}
	if minMS > 0 {
		q += ` AND duration_ms>=?`
		args = append(args, minMS)
	}
	if keyword != "" {
		q += ` AND sql_text LIKE ?`
		args = append(args, "%"+keyword+"%")
	}
	q += ` ORDER BY log_time DESC LIMIT ?`
	args = append(args, limit)
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CloudSlowLog
	for rows.Next() {
		var r CloudSlowLog
		if err := rows.Scan(&r.ID, &r.TenantID, &r.Vendor, &r.Engine, &r.AccountID, &r.CloudID, &r.LogTime, &r.DBName, &r.DBUser, &r.Client, &r.Duration, &r.SQL, &r.Fingerprint); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *DB) PurgeCloudSlowLogs(before string) error {
	_, err := d.Exec(`DELETE FROM tbl_cloud_slow_logs WHERE log_time<>'' AND log_time<?`, before)
	return err
}

func (d *DB) AddCloudEvent(tenantID, vendor, accountID, cloudID, eventType, message, at string) error {
	if at == "" {
		at = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := d.Exec(`INSERT INTO tbl_cloud_events(id, tenant_id, vendor, account_id, cloud_instance_id, event_type, message, event_at, created_at)
		VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`,
		uuid.NewString(), tenantID, vendor, accountID, cloudID, eventType, message, at, time.Now().UTC().Format(time.RFC3339))
	return err
}

func (d *DB) RecentCloudSwitch(vendor, cloudID string, since string) (bool, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(1) FROM tbl_cloud_events WHERE vendor=? AND cloud_instance_id=? AND event_type='switchover' AND event_at>=?`,
		vendor, cloudID, since).Scan(&n)
	return n > 0, err
}

func (d *DB) UpdateIssueSQL(id, sql string) error {
	_, err := d.Exec(`UPDATE tbl_issues SET sql_content=?, updated_at=? WHERE id=?`, sql, time.Now().UTC().Format(time.RFC3339), id)
	return err
}

func (d *DB) CountOpenCloudChanges(cloudID, exceptIssueID string) (int, error) {
	rows, err := d.Query(`SELECT id, sql_content FROM tbl_issues WHERE issue_type IN (?,?,?,?) AND status IN ('pending','approved','running')`,
		IssueTypeCloudPg, IssueTypeCloudMysql, IssueTypeCloudRedis, IssueTypeCloudHost)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var id, raw string
		if rows.Scan(&id, &raw) != nil || id == exceptIssueID {
			continue
		}
		if strings.Contains(raw, `"cloud_instance_id":"`+cloudID+`"`) {
			n++
		}
	}
	return n, rows.Err()
}

func (d *DB) CountCloudActionsSince(cloudID, action, since string) (int, error) {
	rows, err := d.Query(`SELECT sql_content, created_at FROM tbl_issues WHERE issue_type IN (?,?,?,?) AND created_at>=?`,
		IssueTypeCloudPg, IssueTypeCloudMysql, IssueTypeCloudRedis, IssueTypeCloudHost, since)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var raw, at string
		if rows.Scan(&raw, &at) != nil {
			continue
		}
		if strings.Contains(raw, `"cloud_instance_id":"`+cloudID+`"`) && strings.Contains(raw, `"action":"`+action+`"`) {
			n++
		}
	}
	return n, rows.Err()
}

func (d *DB) LastCloudActionAt(cloudID, action string) (string, error) {
	rows, err := d.Query(`SELECT sql_content, created_at FROM tbl_issues WHERE issue_type IN (?,?,?,?) ORDER BY created_at DESC LIMIT 100`,
		IssueTypeCloudPg, IssueTypeCloudMysql, IssueTypeCloudRedis, IssueTypeCloudHost)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var raw, at string
		if rows.Scan(&raw, &at) != nil {
			continue
		}
		if strings.Contains(raw, `"cloud_instance_id":"`+cloudID+`"`) && strings.Contains(raw, `"action":"`+action+`"`) {
			return at, nil
		}
	}
	return "", rows.Err()
}

func (d *DB) ListRunningCloudIssues() ([]*Issue, error) {
	rows, err := d.Query(`SELECT id FROM tbl_issues WHERE issue_type IN (?,?,?,?) AND status=?`,
		IssueTypeCloudPg, IssueTypeCloudMysql, IssueTypeCloudRedis, IssueTypeCloudHost, StatusRunning)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Issue
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			continue
		}
		it, err := d.GetIssue(id)
		if err != nil || it == nil {
			continue
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (d *DB) ListFiringAlertsForHeal() ([]Alert, error) {
	rows, err := d.Query(`SELECT id, severity, title, rule_id, labels, handle, status FROM tbl_alerts WHERE status=? AND handle<>?`, AlertStatusFiring, AlertHandleSilenced)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Alert
	for rows.Next() {
		var a Alert
		var labels string
		if err := rows.Scan(&a.ID, &a.Severity, &a.Title, &a.RuleID, &labels, &a.Handle, &a.Status); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(labels), &a.Labels)
		out = append(out, a)
	}
	return out, rows.Err()
}
