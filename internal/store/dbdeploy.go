package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// 通用 DB 集群部署（MySQL / PG / Redis / Codis），与 StarRocks 表隔离。

const (
	DbDeployEngineMySQL    = "mysql"
	DbDeployEnginePostgres = "postgres"
	DbDeployEngineRedis    = "redis"
	DbDeployEngineCodis    = "codis"
	DbDeployEngineDNS      = "dns"

	DbDeployStatusDraft     = "draft"
	DbDeployStatusDeploying = "deploying"
	DbDeployStatusReady     = "ready"
	DbDeployStatusFailed    = "failed"
	DbDeployStatusHaPending = "ha_pending"

	DbDeployTopoStandalone    = "standalone"
	DbDeployTopoMasterReplica = "master_replica"
	DbDeployTopoSentinel      = "sentinel"
	DbDeployTopoCodis         = "codis"
	DbDeployTopoCloudEndpoint = "cloud_endpoint"
	DbDeployTopoPdTikvTidb    = "pd_tikv_tidb"
	DbDeployTopoMultiNode     = "multi_node"
	DbDeployTopoReplset       = "replset"
	DbDeployTopoBrokerCluster = "broker_cluster"

	DbDeployKindSelf  = "self"
	DbDeployKindCloud = "cloud"

	DbNodePending = "pending"
	DbNodeReady   = "ready"
	DbNodeStopped = "stopped"
	DbNodeFailed  = "failed"

	DbChangePending   = "pending"
	DbChangeRunning   = "running"
	DbChangeSuccess   = "success"
	DbChangeFailed    = "failed"
	DbChangeCancelled = "cancelled"

	DbStepPending = "pending"
	DbStepRunning = "running"
	DbStepSuccess = "success"
	DbStepFailed  = "failed"
	DbStepSkipped = "skipped"

	DbChangeClusterDeploy = "cluster_deploy"
	DbChangeAddNode       = "add_node"
	DbChangeRemoveNode    = "remove_node"
	DbChangeStartNode     = "start_node"
	DbChangeStopNode      = "stop_node"
	DbChangeRestartNode   = "restart_node"
	DbChangeHaInit        = "ha_init"
)

// DbDeployChange 部署/运维变更单。
type DbDeployChange struct {
	ID          string               `json:"id"`
	ClusterID   string               `json:"cluster_id"`
	Type        string               `json:"type"`
	Status      string               `json:"status"`
	PayloadJSON string               `json:"payload_json,omitempty"`
	LogText     string               `json:"log_text,omitempty"`
	CreatedBy   string               `json:"created_by,omitempty"`
	CreatedAt   string               `json:"created_at"`
	UpdatedAt   string               `json:"updated_at"`
	Steps       []DbDeployChangeStep `json:"steps,omitempty"`
}

// DbDeployChangeStep 变更步骤。
type DbDeployChangeStep struct {
	ID        string `json:"id"`
	ChangeID  string `json:"change_id"`
	Seq       int    `json:"seq"`
	StepKey   string `json:"step_key"`
	Title     string `json:"title"`
	NodeID    string `json:"node_id,omitempty"`
	Status    string `json:"status"`
	LogText   string `json:"log_text,omitempty"`
	StartedAt string `json:"started_at,omitempty"`
	EndedAt   string `json:"ended_at,omitempty"`
}

type DbDeployRelease struct {
	ID         string `json:"id"`
	Engine     string `json:"engine"`
	Version    string `json:"version"`
	Channel    string `json:"channel,omitempty"`
	ObjectKey  string `json:"object_key"`
	Checksum   string `json:"checksum"`
	Filename   string `json:"filename,omitempty"`
	Arch       string `json:"arch,omitempty"`
	OS         string `json:"os,omitempty"`
	PackFormat string `json:"pack_format,omitempty"`
	SizeBytes  int64  `json:"size_bytes,omitempty"`
	Enabled    bool   `json:"enabled"`
	Remark     string `json:"remark,omitempty"`
	CreatedBy  string `json:"created_by,omitempty"`
	UpdatedBy  string `json:"updated_by,omitempty"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
	// 展示（不落库）
	MajorVersion string `json:"major_version,omitempty"`
	Status       string `json:"status,omitempty"` // ready | pending | hashing
}

type DbDeployCluster struct {
	ID            string `json:"id"`
	TenantID      string `json:"tenant_id,omitempty"`
	TenantName    string `json:"tenant_name,omitempty"`
	Engine        string `json:"engine"`
	Name          string `json:"name"`
	Environment   string `json:"environment"`
	Topology      string `json:"topology"`    // standalone | master_replica | sentinel | codis | cloud_endpoint
	DeployKind    string `json:"deploy_kind"` // self | cloud
	ReleaseID     string `json:"release_id,omitempty"`
	Version       string `json:"version,omitempty"`
	SoftwareBase  string `json:"software_base,omitempty"`
	DataRoot      string `json:"data_root,omitempty"`
	HaEnabled     bool   `json:"ha_enabled"`
	HaMode        string `json:"ha_mode,omitempty"`
	ControllerID  string `json:"controller_id,omitempty"`
	HaClusterID   string `json:"ha_cluster_id,omitempty"`
	HaVIP         string `json:"ha_vip,omitempty"`
	HaOptionsJSON string `json:"ha_options_json,omitempty"`
	// 云托管单入口
	CloudVendor      string `json:"cloud_vendor,omitempty"`
	CloudInstanceID  string `json:"cloud_instance_id,omitempty"`
	CloudRegion      string `json:"cloud_region,omitempty"`
	EndpointHost     string `json:"endpoint_host,omitempty"`
	EndpointPort     int    `json:"endpoint_port,omitempty"`
	LinkedInstanceID string `json:"linked_instance_id,omitempty"`
	Status           string `json:"status"`
	Remark           string `json:"remark,omitempty"`
	CreatedBy        string `json:"created_by,omitempty"`
	UpdatedBy        string `json:"updated_by,omitempty"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}

type DbDeployNode struct {
	ID             string `json:"id"`
	ClusterID      string `json:"cluster_id"`
	Role           string `json:"role"` // primary|replica|sentinel|codis_dashboard|codis_proxy|codis_server|single|endpoint
	Host           string `json:"host"`
	Port           int    `json:"port,omitempty"`
	AgentID        string `json:"agent_id,omitempty"`
	HostInstanceID string `json:"host_instance_id,omitempty"`
	SystemdUnit    string `json:"systemd_unit,omitempty"`
	Status         string `json:"status"`
	Remark         string `json:"remark,omitempty"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

type DbDeployAgent struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	BaseURL     string `json:"base_url"`
	Username    string `json:"username,omitempty"`
	PasswordEnc string `json:"-"`
	PasswordSet bool   `json:"password_set"`
	HostInstID  string `json:"host_instance_id,omitempty"`
	Remark      string `json:"remark,omitempty"`
	CreatedBy   string `json:"created_by,omitempty"`
	UpdatedBy   string `json:"updated_by,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// DbDeployAgentDeploy DB Agent 一次 SSH 下发任务。
type DbDeployAgentDeploy struct {
	ID             string `json:"id"`
	HostInstanceID string `json:"host_instance_id"`
	AgentID        string `json:"agent_id,omitempty"`
	Status         string `json:"status"`
	ListenPort     int    `json:"listen_port"`
	RemoteDir      string `json:"remote_dir"`
	BaseURL        string `json:"base_url,omitempty"`
	LogText        string `json:"log_text,omitempty"`
	CreatedBy      string `json:"created_by,omitempty"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	FinishedAt     string `json:"finished_at,omitempty"`
}

// DbDeployOSSBind 引擎级 OSS 绑定（引用配置中心 OSS + 制品 prefix）。
type DbDeployOSSBind struct {
	Engine       string `json:"engine"`
	OSSSettingID string `json:"oss_setting_id"`
	Prefix       string `json:"prefix"`
	UpdatedBy    string `json:"updated_by,omitempty"`
	UpdatedAt    string `json:"updated_at,omitempty"`
	// 展示
	OSSEnvironment string `json:"oss_environment,omitempty"`
	OSSEndpoint    string `json:"oss_endpoint,omitempty"`
	OSSBucket      string `json:"oss_bucket,omitempty"`
}

func (d *DB) ensureDbDeployTables() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS tbl_db_deploy_releases (
			id TEXT PRIMARY KEY,
			engine TEXT NOT NULL,
			version TEXT NOT NULL,
			channel TEXT NOT NULL DEFAULT '',
			object_key TEXT NOT NULL DEFAULT '',
			checksum TEXT NOT NULL DEFAULT '',
			filename TEXT NOT NULL DEFAULT '',
			arch TEXT NOT NULL DEFAULT 'x86_64',
			os TEXT NOT NULL DEFAULT '',
			pack_format TEXT NOT NULL DEFAULT 'tar.gz',
			size_bytes BIGINT NOT NULL DEFAULT 0,
			enabled INTEGER NOT NULL DEFAULT 1,
			remark TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL DEFAULT '',
			updated_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_db_deploy_rel_eng_ver ON tbl_db_deploy_releases(engine, version, arch)`,
		`CREATE TABLE IF NOT EXISTS tbl_db_deploy_clusters (
			id TEXT PRIMARY KEY,
			engine TEXT NOT NULL,
			name TEXT NOT NULL,
			environment TEXT NOT NULL DEFAULT '',
			topology TEXT NOT NULL DEFAULT 'standalone',
			deploy_kind TEXT NOT NULL DEFAULT 'self',
			release_id TEXT NOT NULL DEFAULT '',
			version TEXT NOT NULL DEFAULT '',
			software_base TEXT NOT NULL DEFAULT '',
			data_root TEXT NOT NULL DEFAULT '',
			ha_enabled INTEGER NOT NULL DEFAULT 0,
			ha_mode TEXT NOT NULL DEFAULT '',
			controller_id TEXT NOT NULL DEFAULT '',
			ha_cluster_id TEXT NOT NULL DEFAULT '',
			cloud_vendor TEXT NOT NULL DEFAULT '',
			cloud_instance_id TEXT NOT NULL DEFAULT '',
			cloud_region TEXT NOT NULL DEFAULT '',
			endpoint_host TEXT NOT NULL DEFAULT '',
			endpoint_port INTEGER NOT NULL DEFAULT 0,
			linked_instance_id TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'draft',
			remark TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL DEFAULT '',
			updated_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_db_deploy_cl_eng ON tbl_db_deploy_clusters(engine, updated_at DESC)`,
		`CREATE TABLE IF NOT EXISTS tbl_db_deploy_nodes (
			id TEXT PRIMARY KEY,
			cluster_id TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'single',
			host TEXT NOT NULL,
			port INTEGER NOT NULL DEFAULT 0,
			agent_id TEXT NOT NULL DEFAULT '',
			host_instance_id TEXT NOT NULL DEFAULT '',
			systemd_unit TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			remark TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_db_deploy_nodes_cl ON tbl_db_deploy_nodes(cluster_id)`,
		`CREATE TABLE IF NOT EXISTS tbl_db_deploy_agents (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			base_url TEXT NOT NULL,
			username TEXT NOT NULL DEFAULT '',
			password_enc TEXT NOT NULL DEFAULT '',
			host_instance_id TEXT NOT NULL DEFAULT '',
			remark TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL DEFAULT '',
			updated_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_db_deploy_oss_bind (
			engine TEXT PRIMARY KEY,
			oss_setting_id TEXT NOT NULL DEFAULT '',
			prefix TEXT NOT NULL DEFAULT '',
			updated_by TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_db_deploy_changes (
			id TEXT PRIMARY KEY,
			cluster_id TEXT NOT NULL,
			type TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			payload_json TEXT NOT NULL DEFAULT '',
			log_text TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_db_deploy_ch_cl ON tbl_db_deploy_changes(cluster_id, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS tbl_db_deploy_change_steps (
			id TEXT PRIMARY KEY,
			change_id TEXT NOT NULL,
			seq INTEGER NOT NULL,
			step_key TEXT NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			node_id TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			log_text TEXT NOT NULL DEFAULT '',
			started_at TEXT NOT NULL DEFAULT '',
			ended_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_db_deploy_steps_ch ON tbl_db_deploy_change_steps(change_id, seq)`,
		`CREATE TABLE IF NOT EXISTS tbl_db_deploy_agent_deploys (
			id TEXT PRIMARY KEY,
			host_instance_id TEXT NOT NULL DEFAULT '',
			agent_id TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			listen_port INTEGER NOT NULL DEFAULT 8092,
			remote_dir TEXT NOT NULL DEFAULT '',
			base_url TEXT NOT NULL DEFAULT '',
			log_text TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			finished_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_db_deploy_adeploy_host ON tbl_db_deploy_agent_deploys(host_instance_id, created_at DESC)`,
	}
	for _, s := range stmts {
		if _, err := d.Exec(s); err != nil {
			return err
		}
	}
	for _, col := range []struct{ name, def string }{
		{"deploy_kind", "TEXT NOT NULL DEFAULT 'self'"},
		{"cloud_vendor", "TEXT NOT NULL DEFAULT ''"},
		{"cloud_instance_id", "TEXT NOT NULL DEFAULT ''"},
		{"cloud_region", "TEXT NOT NULL DEFAULT ''"},
		{"endpoint_host", "TEXT NOT NULL DEFAULT ''"},
		{"endpoint_port", "INTEGER NOT NULL DEFAULT 0"},
		{"linked_instance_id", "TEXT NOT NULL DEFAULT ''"},
		{"ha_vip", "TEXT NOT NULL DEFAULT ''"},
		{"ha_options_json", "TEXT NOT NULL DEFAULT ''"},
	} {
		_, _ = d.Exec(`ALTER TABLE tbl_db_deploy_clusters ADD COLUMN IF NOT EXISTS ` + col.name + ` ` + col.def)
	}
	d.ensureDbDeployConfTables()
	return nil
}

func nowDbDeploy() string { return time.Now().UTC().Format(time.RFC3339) }

func (d *DB) CreateDbDeployChange(ch *DbDeployChange) error {
	if ch.ID == "" {
		ch.ID = uuid.NewString()
	}
	if ch.Status == "" {
		ch.Status = DbChangePending
	}
	now := nowDbDeploy()
	ch.CreatedAt, ch.UpdatedAt = now, now
	_, err := d.Exec(`INSERT INTO tbl_db_deploy_changes(id,cluster_id,type,status,payload_json,log_text,created_by,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		ch.ID, ch.ClusterID, ch.Type, ch.Status, ch.PayloadJSON, ch.LogText, ch.CreatedBy, ch.CreatedAt, ch.UpdatedAt)
	if err != nil {
		return err
	}
	return d.inheritTenantFromParent("tbl_db_deploy_changes", ch.ID, "tbl_db_deploy_clusters", ch.ClusterID)
}

func (d *DB) UpdateDbDeployChangeStatus(id, status, logAppend string) error {
	now := nowDbDeploy()
	appendText := strings.TrimSpace(logAppend)
	if appendText == "" {
		_, err := d.Exec(`UPDATE tbl_db_deploy_changes SET status=?, updated_at=? WHERE id=?`, status, now, id)
		return err
	}
	var old string
	if err := d.QueryRow(`SELECT COALESCE(log_text,'') FROM tbl_db_deploy_changes WHERE id=?`, id).Scan(&old); err != nil {
		return err
	}
	newLog := appendText
	if strings.TrimSpace(old) != "" {
		newLog = strings.TrimRight(old, "\n") + "\n" + appendText
	}
	_, err := d.Exec(`UPDATE tbl_db_deploy_changes SET status=?, log_text=?, updated_at=? WHERE id=?`, status, newLog, now, id)
	return err
}

func (d *DB) GetDbDeployChange(id string) (*DbDeployChange, error) {
	row := d.QueryRow(`SELECT id,cluster_id,type,status,payload_json,log_text,created_by,created_at,updated_at FROM tbl_db_deploy_changes WHERE id=?`, id)
	var ch DbDeployChange
	if err := row.Scan(&ch.ID, &ch.ClusterID, &ch.Type, &ch.Status, &ch.PayloadJSON, &ch.LogText, &ch.CreatedBy, &ch.CreatedAt, &ch.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	steps, err := d.ListDbDeployChangeSteps(id)
	if err != nil {
		return nil, err
	}
	ch.Steps = steps
	return &ch, nil
}

func (d *DB) ListDbDeployChanges(clusterID string, limit int) ([]DbDeployChange, error) {
	return d.ListDbDeployChangesInTenant("", clusterID, limit)
}

func (d *DB) ListDbDeployChangesInTenant(tenantID, clusterID string, limit int) ([]DbDeployChange, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows *sql.Rows
	var err error
	tid := strings.TrimSpace(tenantID)
	cid := strings.TrimSpace(clusterID)
	switch {
	case tid != "" && cid != "":
		rows, err = d.Query(`SELECT c.id,c.cluster_id,c.type,c.status,c.payload_json,c.log_text,c.created_by,c.created_at,c.updated_at
			FROM tbl_db_deploy_changes c
			JOIN tbl_db_deploy_clusters cl ON cl.id=c.cluster_id
			WHERE c.cluster_id=? AND COALESCE(cl.tenant_id,'')=?
			ORDER BY c.created_at DESC LIMIT ?`, cid, tid, limit)
	case tid != "":
		rows, err = d.Query(`SELECT c.id,c.cluster_id,c.type,c.status,c.payload_json,c.log_text,c.created_by,c.created_at,c.updated_at
			FROM tbl_db_deploy_changes c
			JOIN tbl_db_deploy_clusters cl ON cl.id=c.cluster_id
			WHERE COALESCE(cl.tenant_id,'')=?
			ORDER BY c.created_at DESC LIMIT ?`, tid, limit)
	case cid != "":
		rows, err = d.Query(`SELECT id,cluster_id,type,status,payload_json,log_text,created_by,created_at,updated_at
			FROM tbl_db_deploy_changes WHERE cluster_id=? ORDER BY created_at DESC LIMIT ?`, cid, limit)
	default:
		rows, err = d.Query(`SELECT id,cluster_id,type,status,payload_json,log_text,created_by,created_at,updated_at
			FROM tbl_db_deploy_changes ORDER BY created_at DESC LIMIT ?`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DbDeployChange
	for rows.Next() {
		var ch DbDeployChange
		if err := rows.Scan(&ch.ID, &ch.ClusterID, &ch.Type, &ch.Status, &ch.PayloadJSON, &ch.LogText, &ch.CreatedBy, &ch.CreatedAt, &ch.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, ch)
	}
	if out == nil {
		out = []DbDeployChange{}
	}
	return out, rows.Err()
}

func (d *DB) CreateDbDeployChangeStep(s *DbDeployChangeStep) error {
	if s.ID == "" {
		s.ID = uuid.NewString()
	}
	if s.Status == "" {
		s.Status = DbStepPending
	}
	_, err := d.Exec(`INSERT INTO tbl_db_deploy_change_steps(id,change_id,seq,step_key,title,node_id,status,log_text,started_at,ended_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		s.ID, s.ChangeID, s.Seq, s.StepKey, s.Title, s.NodeID, s.Status, s.LogText, s.StartedAt, s.EndedAt)
	return err
}

func (d *DB) UpdateDbDeployChangeStep(s *DbDeployChangeStep) error {
	_, err := d.Exec(`UPDATE tbl_db_deploy_change_steps SET status=?, log_text=?, started_at=?, ended_at=? WHERE id=?`,
		s.Status, s.LogText, s.StartedAt, s.EndedAt, s.ID)
	return err
}

func (d *DB) ListDbDeployChangeSteps(changeID string) ([]DbDeployChangeStep, error) {
	rows, err := d.Query(`SELECT id,change_id,seq,step_key,title,node_id,status,log_text,started_at,ended_at
		FROM tbl_db_deploy_change_steps WHERE change_id=? ORDER BY seq`, changeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DbDeployChangeStep
	for rows.Next() {
		var s DbDeployChangeStep
		if err := rows.Scan(&s.ID, &s.ChangeID, &s.Seq, &s.StepKey, &s.Title, &s.NodeID, &s.Status, &s.LogText, &s.StartedAt, &s.EndedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if out == nil {
		out = []DbDeployChangeStep{}
	}
	return out, rows.Err()
}

func (d *DB) GetDbDeployNode(id string) (*DbDeployNode, error) {
	row := d.QueryRow(`SELECT id, cluster_id, role, host, port, agent_id, host_instance_id, systemd_unit, status, remark, created_at, updated_at
		FROM tbl_db_deploy_nodes WHERE id=?`, strings.TrimSpace(id))
	var n DbDeployNode
	if err := row.Scan(&n.ID, &n.ClusterID, &n.Role, &n.Host, &n.Port, &n.AgentID, &n.HostInstanceID, &n.SystemdUnit, &n.Status, &n.Remark, &n.CreatedAt, &n.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &n, nil
}

func (d *DB) UpdateDbDeployNodeStatus(id, status string) error {
	now := nowDbDeploy()
	_, err := d.Exec(`UPDATE tbl_db_deploy_nodes SET status=?, updated_at=? WHERE id=?`, status, now, id)
	return err
}

func normalizeDbEngine(e string) string {
	switch strings.ToLower(strings.TrimSpace(e)) {
	case "mysql":
		return DbDeployEngineMySQL
	case "postgres", "pg", "postgresql":
		return DbDeployEnginePostgres
	case "redis":
		return DbDeployEngineRedis
	case "codis":
		return DbDeployEngineCodis
	default:
		return strings.ToLower(strings.TrimSpace(e))
	}
}

func (d *DB) ListDbDeployReleases(engine string) ([]DbDeployRelease, error) {
	q := `SELECT id, engine, version, channel, object_key, checksum, filename, arch, os, pack_format, size_bytes, enabled, remark, created_by, updated_by, created_at, updated_at
		FROM tbl_db_deploy_releases`
	args := []any{}
	if e := normalizeDbEngine(engine); e != "" {
		q += ` WHERE engine=?`
		args = append(args, e)
	}
	q += ` ORDER BY engine, version DESC`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DbDeployRelease
	for rows.Next() {
		var r DbDeployRelease
		var en int
		if err := rows.Scan(&r.ID, &r.Engine, &r.Version, &r.Channel, &r.ObjectKey, &r.Checksum, &r.Filename, &r.Arch, &r.OS, &r.PackFormat, &r.SizeBytes, &en, &r.Remark, &r.CreatedBy, &r.UpdatedBy, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.Enabled = en == 1
		out = append(out, r)
	}
	if out == nil {
		out = []DbDeployRelease{}
	}
	return out, rows.Err()
}

func (d *DB) GetDbDeployRelease(id string) (*DbDeployRelease, error) {
	row := d.QueryRow(`SELECT id, engine, version, channel, object_key, checksum, filename, arch, os, pack_format, size_bytes, enabled, remark, created_by, updated_by, created_at, updated_at
		FROM tbl_db_deploy_releases WHERE id=?`, strings.TrimSpace(id))
	var r DbDeployRelease
	var en int
	err := row.Scan(&r.ID, &r.Engine, &r.Version, &r.Channel, &r.ObjectKey, &r.Checksum, &r.Filename, &r.Arch, &r.OS, &r.PackFormat, &r.SizeBytes, &en, &r.Remark, &r.CreatedBy, &r.UpdatedBy, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.Enabled = en == 1
	return &r, nil
}

func (d *DB) UpsertDbDeployRelease(r *DbDeployRelease) error {
	if r == nil {
		return errors.New("nil release")
	}
	r.Engine = normalizeDbEngine(r.Engine)
	r.Version = strings.TrimSpace(r.Version)
	r.ObjectKey = strings.TrimSpace(r.ObjectKey)
	r.Checksum = strings.TrimSpace(r.Checksum)
	if r.Engine == "" || r.Version == "" {
		return errors.New("engine/version 必填")
	}
	if r.Arch == "" {
		r.Arch = "x86_64"
	}
	if r.PackFormat == "" {
		r.PackFormat = "tar.gz"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	en := 0
	if r.Enabled {
		en = 1
	}
	if r.ID == "" {
		r.ID = uuid.NewString()
		r.CreatedAt = now
	}
	r.UpdatedAt = now
	_, err := d.Exec(
		`INSERT INTO tbl_db_deploy_releases(id, engine, version, channel, object_key, checksum, filename, arch, os, pack_format, size_bytes, enabled, remark, created_by, updated_by, created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(id) DO UPDATE SET engine=EXCLUDED.engine, version=EXCLUDED.version, channel=EXCLUDED.channel,
		 object_key=EXCLUDED.object_key, checksum=EXCLUDED.checksum, filename=EXCLUDED.filename, arch=EXCLUDED.arch, os=EXCLUDED.os,
		 pack_format=EXCLUDED.pack_format, size_bytes=EXCLUDED.size_bytes, enabled=EXCLUDED.enabled, remark=EXCLUDED.remark,
		 updated_by=EXCLUDED.updated_by, updated_at=EXCLUDED.updated_at`,
		r.ID, r.Engine, r.Version, r.Channel, r.ObjectKey, r.Checksum, r.Filename, r.Arch, r.OS, r.PackFormat, r.SizeBytes, en, r.Remark, r.CreatedBy, r.UpdatedBy, r.CreatedAt, r.UpdatedAt,
	)
	return err
}

func (d *DB) DeleteDbDeployRelease(id string) error {
	_, err := d.Exec(`DELETE FROM tbl_db_deploy_releases WHERE id=?`, strings.TrimSpace(id))
	return err
}

func (d *DB) ListDbDeployClusters(engine string) ([]DbDeployCluster, error) {
	return d.ListDbDeployClustersInTenant("", engine)
}

func (d *DB) ListDbDeployClustersInTenant(tenantID, engine string) ([]DbDeployCluster, error) {
	q := `SELECT ` + dbDeployClusterSelectCols + `
		FROM tbl_db_deploy_clusters WHERE 1=1`
	args := []any{}
	if tid := strings.TrimSpace(tenantID); tid != "" {
		q += ` AND COALESCE(tenant_id,'')=?`
		args = append(args, tid)
	}
	if e := normalizeDbEngine(engine); e != "" {
		q += ` AND engine=?`
		args = append(args, e)
	}
	q += ` ORDER BY updated_at DESC`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DbDeployCluster
	for rows.Next() {
		c, err := scanDbDeployCluster(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	if out == nil {
		out = []DbDeployCluster{}
	}
	return out, rows.Err()
}

func (d *DB) GetDbDeployCluster(id string) (*DbDeployCluster, error) {
	row := d.QueryRow(`SELECT `+dbDeployClusterSelectCols+`
		FROM tbl_db_deploy_clusters WHERE id=?`, strings.TrimSpace(id))
	c, err := scanDbDeployCluster(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return c, err
}

type dbDeployClusterScanner interface {
	Scan(dest ...any) error
}

const dbDeployClusterSelectCols = `id, COALESCE(tenant_id,''), engine, name, environment, topology, COALESCE(deploy_kind,'self'), release_id, version, software_base, data_root,
		ha_enabled, ha_mode, controller_id, ha_cluster_id,
		COALESCE(ha_vip,''), COALESCE(ha_options_json,''),
		COALESCE(cloud_vendor,''), COALESCE(cloud_instance_id,''), COALESCE(cloud_region,''),
		COALESCE(endpoint_host,''), COALESCE(endpoint_port,0), COALESCE(linked_instance_id,''),
		status, remark, created_by, updated_by, created_at, updated_at`

func scanDbDeployCluster(row dbDeployClusterScanner) (*DbDeployCluster, error) {
	var c DbDeployCluster
	var ha int
	err := row.Scan(&c.ID, &c.TenantID, &c.Engine, &c.Name, &c.Environment, &c.Topology, &c.DeployKind, &c.ReleaseID, &c.Version, &c.SoftwareBase, &c.DataRoot,
		&ha, &c.HaMode, &c.ControllerID, &c.HaClusterID,
		&c.HaVIP, &c.HaOptionsJSON,
		&c.CloudVendor, &c.CloudInstanceID, &c.CloudRegion, &c.EndpointHost, &c.EndpointPort, &c.LinkedInstanceID,
		&c.Status, &c.Remark, &c.CreatedBy, &c.UpdatedBy, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	c.HaEnabled = ha == 1
	if c.DeployKind == "" {
		c.DeployKind = DbDeployKindSelf
	}
	return &c, nil
}

func (d *DB) UpsertDbDeployCluster(c *DbDeployCluster) error {
	if c == nil {
		return errors.New("nil cluster")
	}
	c.Engine = normalizeDbEngine(c.Engine)
	c.Name = strings.TrimSpace(c.Name)
	if c.Engine == "" || c.Name == "" {
		return errors.New("engine/name 必填")
	}
	if c.DeployKind == "" {
		if c.Topology == DbDeployTopoCloudEndpoint {
			c.DeployKind = DbDeployKindCloud
		} else {
			c.DeployKind = DbDeployKindSelf
		}
	}
	c.DeployKind = strings.ToLower(strings.TrimSpace(c.DeployKind))
	if c.Topology == "" {
		if c.DeployKind == DbDeployKindCloud {
			c.Topology = DbDeployTopoCloudEndpoint
		} else {
			c.Topology = DbDeployTopoStandalone
		}
	}
	if err := validateDbDeployClusterShape(c); err != nil {
		return err
	}
	if c.Status == "" {
		c.Status = DbDeployStatusDraft
	}
	if c.DeployKind == DbDeployKindCloud {
		c.HaEnabled = false
		c.HaMode = ""
		c.ControllerID = ""
		c.HaVIP = ""
		c.HaOptionsJSON = ""
		if c.EndpointHost == "" {
			return errors.New("云托管须填写 endpoint_host（访问入口）")
		}
		if c.EndpointPort <= 0 {
			c.EndpointPort = defaultDbDeployEndpointPort(c.Engine)
		}
		// 云可不绑 release
	} else if c.ReleaseID != "" {
		rel, err := d.GetDbDeployRelease(c.ReleaseID)
		if err != nil {
			return err
		}
		if rel == nil || !rel.Enabled {
			return fmt.Errorf("版本目录不存在或未启用")
		}
		if rel.Engine != c.Engine {
			return fmt.Errorf("版本引擎与集群引擎不一致")
		}
		c.Version = rel.Version
	}
	if c.HaEnabled {
		if c.Engine != DbDeployEngineMySQL {
			return errors.New("本期部署内 HA 接入仅支持 MySQL")
		}
		if c.Topology != DbDeployTopoMasterReplica {
			return errors.New("启用 HA 时拓扑须为 master_replica")
		}
		mode := strings.ToLower(strings.TrimSpace(c.HaMode))
		if mode == "" {
			return errors.New("启用 HA 时须指定 ha_mode")
		}
		if !ValidHAMode("mysql", mode) {
			return fmt.Errorf("HA 模式 %s 无效", mode)
		}
		if strings.TrimSpace(c.ControllerID) == "" && mode != "manual" && mode != "async_repl" {
			return errors.New("启用 HA 时须指定已登记的 controller_id（manual/async_repl 除外）")
		}
		if cid := strings.TrimSpace(c.ControllerID); cid != "" {
			ctrl, err := d.GetHAController(cid)
			if err != nil {
				return err
			}
			if ctrl == nil {
				return errors.New("HA 控制器不存在")
			}
			if !CompatibleHAController("mysql", mode, ctrl.Kind) {
				return fmt.Errorf("控制器类型 %s 与模式 %s 不匹配", ctrl.Kind, mode)
			}
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	ha := 0
	if c.HaEnabled {
		ha = 1
	}
	if c.ID == "" {
		c.ID = uuid.NewString()
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	_, err := d.Exec(
		`INSERT INTO tbl_db_deploy_clusters(id, engine, name, environment, topology, deploy_kind, release_id, version, software_base, data_root,
			ha_enabled, ha_mode, controller_id, ha_cluster_id, ha_vip, ha_options_json,
			cloud_vendor, cloud_instance_id, cloud_region, endpoint_host, endpoint_port, linked_instance_id,
			status, remark, created_by, updated_by, created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(id) DO UPDATE SET name=EXCLUDED.name, environment=EXCLUDED.environment, topology=EXCLUDED.topology,
		 deploy_kind=EXCLUDED.deploy_kind,
		 release_id=EXCLUDED.release_id, version=EXCLUDED.version, software_base=EXCLUDED.software_base, data_root=EXCLUDED.data_root,
		 ha_enabled=EXCLUDED.ha_enabled, ha_mode=EXCLUDED.ha_mode, controller_id=EXCLUDED.controller_id, ha_cluster_id=EXCLUDED.ha_cluster_id,
		 ha_vip=EXCLUDED.ha_vip, ha_options_json=EXCLUDED.ha_options_json,
		 cloud_vendor=EXCLUDED.cloud_vendor, cloud_instance_id=EXCLUDED.cloud_instance_id, cloud_region=EXCLUDED.cloud_region,
		 endpoint_host=EXCLUDED.endpoint_host, endpoint_port=EXCLUDED.endpoint_port, linked_instance_id=EXCLUDED.linked_instance_id,
		 status=EXCLUDED.status, remark=EXCLUDED.remark, updated_by=EXCLUDED.updated_by, updated_at=EXCLUDED.updated_at`,
		c.ID, c.Engine, c.Name, c.Environment, c.Topology, c.DeployKind, c.ReleaseID, c.Version, c.SoftwareBase, c.DataRoot,
		ha, c.HaMode, c.ControllerID, c.HaClusterID, c.HaVIP, c.HaOptionsJSON,
		c.CloudVendor, c.CloudInstanceID, c.CloudRegion, c.EndpointHost, c.EndpointPort, c.LinkedInstanceID,
		c.Status, c.Remark, c.CreatedBy, c.UpdatedBy, c.CreatedAt, c.UpdatedAt,
	)
	if err != nil {
		return err
	}
	return d.AssignTenantID("tbl_db_deploy_clusters", c.ID, c.TenantID)
}

// dbDeployCloudEngines 允许 cloud_endpoint（与 dbdeploy 注册表对齐；store 不依赖 dbdeploy）。
var dbDeployCloudEngines = map[string]bool{
	DbDeployEngineMySQL: true, DbDeployEnginePostgres: true, DbDeployEngineRedis: true,
	"tidb": true, "cockroachdb": true, "oceanbase": true, "oracle": true, "sqlserver": true,
	"clickhouse": true, "mongodb": true, "elasticsearch": true,
}

func defaultDbDeployEndpointPort(engine string) int {
	switch normalizeDbEngine(engine) {
	case DbDeployEnginePostgres:
		return 5432
	case DbDeployEngineRedis, DbDeployEngineCodis:
		return 6379
	case "tidb":
		return 4000
	case "cockroachdb":
		return 26257
	case "oceanbase":
		return 2881
	case "oracle":
		return 1521
	case "sqlserver":
		return 1433
	case "clickhouse":
		return 9000
	case "mongodb":
		return 27017
	case "cassandra":
		return 9042
	case "elasticsearch":
		return 9200
	case "kafka":
		return 9092
	default:
		return 3306
	}
}

func validateDbDeployClusterShape(c *DbDeployCluster) error {
	eng := normalizeDbEngine(c.Engine)
	c.Engine = eng
	if c.DeployKind == DbDeployKindCloud {
		if eng == DbDeployEngineCodis {
			return errors.New("云托管不支持 Codis，请登记云 Redis 单入口")
		}
		if !dbDeployCloudEngines[eng] {
			return fmt.Errorf("引擎 %s 不支持云托管单入口登记", eng)
		}
		if c.Topology != DbDeployTopoCloudEndpoint {
			return errors.New("云托管拓扑须为 cloud_endpoint（单入口）")
		}
		return nil
	}
	switch eng {
	case DbDeployEngineMySQL, DbDeployEnginePostgres:
		if c.Topology != DbDeployTopoStandalone && c.Topology != DbDeployTopoMasterReplica {
			return fmt.Errorf("自建 %s 拓扑仅支持 standalone / master_replica", eng)
		}
	case DbDeployEngineRedis:
		switch c.Topology {
		case DbDeployTopoStandalone, DbDeployTopoMasterReplica, DbDeployTopoSentinel, DbDeployTopoCodis:
		default:
			return fmt.Errorf("自建 redis 拓扑不支持 %s", c.Topology)
		}
	case DbDeployEngineCodis:
		if c.Topology != DbDeployTopoCodis {
			return errors.New("codis 引擎拓扑须为 codis")
		}
	case "tidb":
		switch c.Topology {
		case DbDeployTopoPdTikvTidb, DbDeployTopoStandalone:
		default:
			return fmt.Errorf("自建 tidb 拓扑仅支持 pd_tikv_tidb / standalone")
		}
	case "cockroachdb":
		switch c.Topology {
		case DbDeployTopoMultiNode, DbDeployTopoStandalone:
		default:
			return fmt.Errorf("自建 cockroachdb 拓扑仅支持 multi_node / standalone")
		}
	case "clickhouse":
		switch c.Topology {
		case DbDeployTopoStandalone, DbDeployTopoMasterReplica:
		default:
			return fmt.Errorf("自建 clickhouse 拓扑仅支持 standalone / master_replica")
		}
	case "mongodb":
		switch c.Topology {
		case DbDeployTopoStandalone, DbDeployTopoReplset:
		default:
			return fmt.Errorf("自建 mongodb 拓扑仅支持 standalone / replset")
		}
	case "cassandra":
		switch c.Topology {
		case DbDeployTopoStandalone, DbDeployTopoMultiNode:
		default:
			return fmt.Errorf("自建 cassandra 拓扑仅支持 standalone / multi_node")
		}
	case "elasticsearch":
		switch c.Topology {
		case DbDeployTopoStandalone, DbDeployTopoMultiNode:
		default:
			return fmt.Errorf("自建 elasticsearch 拓扑仅支持 standalone / multi_node")
		}
	case "kafka":
		switch c.Topology {
		case DbDeployTopoStandalone, DbDeployTopoBrokerCluster:
		default:
			return fmt.Errorf("自建 kafka 拓扑仅支持 standalone / broker_cluster")
		}
	case "oceanbase", "sqlserver":
		if c.Topology != DbDeployTopoStandalone && c.Topology != DbDeployTopoMasterReplica {
			return fmt.Errorf("自建 %s 拓扑仅支持 standalone / master_replica", eng)
		}
	case "oracle":
		if c.Topology != DbDeployTopoStandalone {
			return fmt.Errorf("自建 oracle 拓扑仅支持 standalone")
		}
	case DbDeployEngineDNS:
		if c.Topology != DbDeployTopoStandalone && c.Topology != DbDeployTopoMultiNode {
			return fmt.Errorf("DNS 拓扑仅支持 standalone / multi_node")
		}
	default:
		return fmt.Errorf("未知部署引擎: %s", eng)
	}
	if c.Topology == DbDeployTopoCloudEndpoint {
		return errors.New("cloud_endpoint 仅用于云托管 deploy_kind=cloud")
	}
	return nil
}

func (d *DB) DeleteDbDeployCluster(id string) error {
	id = strings.TrimSpace(id)
	if _, err := d.Exec(`DELETE FROM tbl_db_deploy_change_steps WHERE change_id IN (SELECT id FROM tbl_db_deploy_changes WHERE cluster_id=?)`, id); err != nil {
		return err
	}
	if _, err := d.Exec(`DELETE FROM tbl_db_deploy_changes WHERE cluster_id=?`, id); err != nil {
		return err
	}
	if _, err := d.Exec(`DELETE FROM tbl_db_deploy_nodes WHERE cluster_id=?`, id); err != nil {
		return err
	}
	_ = d.DeleteDbDeployClusterConf(id)
	_, err := d.Exec(`DELETE FROM tbl_db_deploy_clusters WHERE id=?`, id)
	return err
}

func (d *DB) ListDbDeployNodes(clusterID string) ([]DbDeployNode, error) {
	rows, err := d.Query(`SELECT id, cluster_id, role, host, port, agent_id, host_instance_id, systemd_unit, status, remark, created_at, updated_at
		FROM tbl_db_deploy_nodes WHERE cluster_id=? ORDER BY role, host`, strings.TrimSpace(clusterID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DbDeployNode
	for rows.Next() {
		var n DbDeployNode
		if err := rows.Scan(&n.ID, &n.ClusterID, &n.Role, &n.Host, &n.Port, &n.AgentID, &n.HostInstanceID, &n.SystemdUnit, &n.Status, &n.Remark, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	if out == nil {
		out = []DbDeployNode{}
	}
	return out, rows.Err()
}

// ListDbDeployNodesByHost 所有集群中位于该主机的节点（端口冲突校验用）。
func (d *DB) ListDbDeployNodesByHost(host string) ([]DbDeployNode, error) {
	return d.listDbDeployNodesWhere(`LOWER(host)=?`, strings.ToLower(strings.TrimSpace(host)))
}

// ListDbDeployNodesByAgent 使用该 DB Agent 的节点（删除 Agent 前校验）。
func (d *DB) ListDbDeployNodesByAgent(agentID string) ([]DbDeployNode, error) {
	return d.listDbDeployNodesWhere(`agent_id=?`, strings.TrimSpace(agentID))
}

// ListDbDeployNodesByHostRef 登记在该主机实例、或 host 为主机任一地址的节点（删除主机前校验）。
func (d *DB) ListDbDeployNodesByHostRef(hostInstID string, addrs []string) ([]DbDeployNode, error) {
	where, args := hostRefWhere(hostInstID, addrs)
	return d.listDbDeployNodesWhere(where, args...)
}

func (d *DB) UpdateDbDeployNodeAgent(id, agentID string) error {
	_, err := d.Exec(`UPDATE tbl_db_deploy_nodes SET agent_id=?, updated_at=? WHERE id=?`, agentID, nowDbDeploy(), id)
	return err
}

// RebindDbDeployNodesAgent 该主机上 agent_id 为空或指向已删除 Agent 的节点改绑到 agentID，返回改绑数量。
func (d *DB) RebindDbDeployNodesAgent(hostInstID string, addrs []string, agentID string) (int, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return 0, nil
	}
	nodes, err := d.ListDbDeployNodesByHostRef(hostInstID, addrs)
	if err != nil {
		return 0, err
	}
	exists := func(id string) bool {
		a, err := d.GetDbDeployAgent(id)
		return err != nil || a != nil
	}
	n := 0
	for _, node := range nodes {
		if !nodeNeedsAgentRebind(node.AgentID, agentID, exists) {
			continue
		}
		if err := d.UpdateDbDeployNodeAgent(node.ID, agentID); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func (d *DB) listDbDeployNodesWhere(where string, args ...any) ([]DbDeployNode, error) {
	rows, err := d.Query(`SELECT id, cluster_id, role, host, port, agent_id, host_instance_id, systemd_unit, status, remark, created_at, updated_at
		FROM tbl_db_deploy_nodes WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DbDeployNode{}
	for rows.Next() {
		var n DbDeployNode
		if err := rows.Scan(&n.ID, &n.ClusterID, &n.Role, &n.Host, &n.Port, &n.AgentID, &n.HostInstanceID, &n.SystemdUnit, &n.Status, &n.Remark, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (d *DB) UpsertDbDeployNode(n *DbDeployNode) error {
	if n == nil {
		return errors.New("nil node")
	}
	n.ClusterID = strings.TrimSpace(n.ClusterID)
	n.Host = strings.TrimSpace(n.Host)
	if n.ClusterID == "" || n.Host == "" {
		return errors.New("cluster_id/host 必填")
	}
	if n.Role == "" {
		n.Role = "single"
	}
	if n.Status == "" {
		n.Status = "pending"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if n.ID == "" {
		n.ID = uuid.NewString()
		n.CreatedAt = now
	}
	n.UpdatedAt = now
	_, err := d.Exec(
		`INSERT INTO tbl_db_deploy_nodes(id, cluster_id, role, host, port, agent_id, host_instance_id, systemd_unit, status, remark, created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(id) DO UPDATE SET role=EXCLUDED.role, host=EXCLUDED.host, port=EXCLUDED.port, agent_id=EXCLUDED.agent_id,
		 host_instance_id=EXCLUDED.host_instance_id, systemd_unit=EXCLUDED.systemd_unit, status=EXCLUDED.status, remark=EXCLUDED.remark, updated_at=EXCLUDED.updated_at`,
		n.ID, n.ClusterID, n.Role, n.Host, n.Port, n.AgentID, n.HostInstanceID, n.SystemdUnit, n.Status, n.Remark, n.CreatedAt, n.UpdatedAt,
	)
	if err != nil {
		return err
	}
	return d.inheritTenantFromParent("tbl_db_deploy_nodes", n.ID, "tbl_db_deploy_clusters", n.ClusterID)
}

func (d *DB) DeleteDbDeployNode(id string) error {
	_, err := d.Exec(`DELETE FROM tbl_db_deploy_nodes WHERE id=?`, strings.TrimSpace(id))
	return err
}

func (d *DB) ListDbDeployAgents() ([]DbDeployAgent, error) {
	rows, err := d.Query(`SELECT id, name, base_url, username, password_enc, host_instance_id, remark, created_by, updated_by, created_at, updated_at FROM tbl_db_deploy_agents ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DbDeployAgent
	for rows.Next() {
		var a DbDeployAgent
		if err := rows.Scan(&a.ID, &a.Name, &a.BaseURL, &a.Username, &a.PasswordEnc, &a.HostInstID, &a.Remark, &a.CreatedBy, &a.UpdatedBy, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		a.PasswordSet = a.PasswordEnc != ""
		a.PasswordEnc = ""
		out = append(out, a)
	}
	if out == nil {
		out = []DbDeployAgent{}
	}
	return out, rows.Err()
}

func (d *DB) GetDbDeployAgent(id string) (*DbDeployAgent, error) {
	row := d.QueryRow(`SELECT id, name, base_url, username, password_enc, host_instance_id, remark, created_by, updated_by, created_at, updated_at FROM tbl_db_deploy_agents WHERE id=?`, strings.TrimSpace(id))
	var a DbDeployAgent
	err := row.Scan(&a.ID, &a.Name, &a.BaseURL, &a.Username, &a.PasswordEnc, &a.HostInstID, &a.Remark, &a.CreatedBy, &a.UpdatedBy, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.PasswordSet = a.PasswordEnc != ""
	return &a, nil
}

func (d *DB) UpsertDbDeployAgent(a *DbDeployAgent) error {
	if a == nil {
		return errors.New("nil agent")
	}
	a.Name = strings.TrimSpace(a.Name)
	a.BaseURL = strings.TrimSpace(a.BaseURL)
	if a.Name == "" || a.BaseURL == "" {
		return errors.New("name/base_url 必填")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	isNew := false
	if a.ID == "" {
		a.ID = uuid.NewString()
		a.CreatedAt = now
		isNew = true
	} else if a.PasswordEnc == "" {
		old, _ := d.GetDbDeployAgent(a.ID)
		if old != nil {
			a.PasswordEnc = old.PasswordEnc
		}
	}
	a.UpdatedAt = now
	_, err := d.Exec(
		`INSERT INTO tbl_db_deploy_agents(id, name, base_url, username, password_enc, host_instance_id, remark, created_by, updated_by, created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(id) DO UPDATE SET name=EXCLUDED.name, base_url=EXCLUDED.base_url, username=EXCLUDED.username,
		 password_enc=EXCLUDED.password_enc, host_instance_id=EXCLUDED.host_instance_id, remark=EXCLUDED.remark,
		 updated_by=EXCLUDED.updated_by, updated_at=EXCLUDED.updated_at`,
		a.ID, a.Name, a.BaseURL, a.Username, a.PasswordEnc, a.HostInstID, a.Remark, a.CreatedBy, a.UpdatedBy, a.CreatedAt, a.UpdatedAt,
	)
	if err != nil {
		return err
	}
	if isNew {
		if tid := d.inheritTenantFromInstance(a.HostInstID); tid != "" {
			return d.AssignTenantID("tbl_db_deploy_agents", a.ID, tid)
		}
	}
	return nil
}

func (d *DB) DeleteDbDeployAgent(id string) error {
	_, err := d.Exec(`DELETE FROM tbl_db_deploy_agents WHERE id=?`, strings.TrimSpace(id))
	return err
}

func (d *DB) FindDbDeployAgentByHostInstance(hostInstanceID string) (*DbDeployAgent, error) {
	hostInstanceID = strings.TrimSpace(hostInstanceID)
	if hostInstanceID == "" {
		return nil, nil
	}
	row := d.QueryRow(`SELECT id, name, base_url, username, password_enc, host_instance_id, remark, created_by, updated_by, created_at, updated_at
		FROM tbl_db_deploy_agents WHERE host_instance_id=? ORDER BY updated_at DESC LIMIT 1`, hostInstanceID)
	var a DbDeployAgent
	err := row.Scan(&a.ID, &a.Name, &a.BaseURL, &a.Username, &a.PasswordEnc, &a.HostInstID, &a.Remark, &a.CreatedBy, &a.UpdatedBy, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.PasswordSet = a.PasswordEnc != ""
	return &a, nil
}

func (d *DB) CreateDbDeployAgentDeploy(it *DbDeployAgentDeploy) error {
	if it.ID == "" {
		it.ID = uuid.NewString()
	}
	now := nowDbDeploy()
	it.CreatedAt, it.UpdatedAt = now, now
	if it.Status == "" {
		it.Status = "pending"
	}
	_, err := d.Exec(`INSERT INTO tbl_db_deploy_agent_deploys
		(id,host_instance_id,agent_id,status,listen_port,remote_dir,base_url,log_text,created_by,created_at,updated_at,finished_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		it.ID, it.HostInstanceID, it.AgentID, it.Status, it.ListenPort, it.RemoteDir, it.BaseURL, it.LogText,
		it.CreatedBy, it.CreatedAt, it.UpdatedAt, it.FinishedAt)
	return err
}

func (d *DB) UpdateDbDeployAgentDeploy(it *DbDeployAgentDeploy) error {
	it.UpdatedAt = nowDbDeploy()
	_, err := d.Exec(`UPDATE tbl_db_deploy_agent_deploys SET agent_id=?, status=?, listen_port=?, remote_dir=?, base_url=?,
		log_text=?, updated_at=?, finished_at=? WHERE id=?`,
		it.AgentID, it.Status, it.ListenPort, it.RemoteDir, it.BaseURL, it.LogText, it.UpdatedAt, it.FinishedAt, it.ID)
	return err
}

func (d *DB) GetDbDeployAgentDeploy(id string) (*DbDeployAgentDeploy, error) {
	it := &DbDeployAgentDeploy{}
	err := d.QueryRow(`SELECT id,host_instance_id,agent_id,status,listen_port,remote_dir,base_url,log_text,created_by,created_at,updated_at,finished_at
		FROM tbl_db_deploy_agent_deploys WHERE id=?`, id).Scan(
		&it.ID, &it.HostInstanceID, &it.AgentID, &it.Status, &it.ListenPort, &it.RemoteDir, &it.BaseURL, &it.LogText,
		&it.CreatedBy, &it.CreatedAt, &it.UpdatedAt, &it.FinishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return it, nil
}

func (d *DB) ListDbDeployAgentDeploys(hostInstanceID string, limit int) ([]DbDeployAgentDeploy, error) {
	return d.ListDbDeployAgentDeploysInTenant("", hostInstanceID, limit)
}

func (d *DB) ListDbDeployAgentDeploysInTenant(tenantID, hostInstanceID string, limit int) ([]DbDeployAgentDeploy, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT id,host_instance_id,agent_id,status,listen_port,remote_dir,base_url,log_text,created_by,created_at,updated_at,finished_at
		FROM tbl_db_deploy_agent_deploys WHERE 1=1`
	var args []any
	q, args = appendTenantFilter(q, args, tenantID)
	if strings.TrimSpace(hostInstanceID) != "" {
		q += ` AND host_instance_id=?`
		args = append(args, hostInstanceID)
	}
	q += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DbDeployAgentDeploy
	for rows.Next() {
		var it DbDeployAgentDeploy
		if err := rows.Scan(&it.ID, &it.HostInstanceID, &it.AgentID, &it.Status, &it.ListenPort, &it.RemoteDir, &it.BaseURL, &it.LogText,
			&it.CreatedBy, &it.CreatedAt, &it.UpdatedAt, &it.FinishedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	if out == nil {
		out = []DbDeployAgentDeploy{}
	}
	return out, rows.Err()
}

func (d *DB) GetDbDeployOSSBind(engine string) (*DbDeployOSSBind, error) {
	engine = normalizeDbEngine(engine)
	row := d.QueryRow(`SELECT engine, oss_setting_id, prefix, updated_by, updated_at FROM tbl_db_deploy_oss_bind WHERE engine=?`, engine)
	var b DbDeployOSSBind
	err := row.Scan(&b.Engine, &b.OSSSettingID, &b.Prefix, &b.UpdatedBy, &b.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		def := "db-artifacts/" + engine + "/"
		return &DbDeployOSSBind{Engine: engine, Prefix: def}, nil
	}
	if err != nil {
		return nil, err
	}
	d.fillDbDeployOSSDisplay(&b)
	return &b, nil
}

func (d *DB) fillDbDeployOSSDisplay(b *DbDeployOSSBind) {
	if b == nil || strings.TrimSpace(b.OSSSettingID) == "" {
		return
	}
	oss, err := d.GetOSSSetting(b.OSSSettingID)
	if err != nil || oss == nil {
		return
	}
	b.OSSEnvironment = oss.Environment
	b.OSSEndpoint = oss.Endpoint
	b.OSSBucket = oss.Bucket
}

func (d *DB) UpsertDbDeployOSSBind(b *DbDeployOSSBind) error {
	if b == nil {
		return errors.New("nil bind")
	}
	b.Engine = normalizeDbEngine(b.Engine)
	if b.Engine == "" {
		return errors.New("engine 必填")
	}
	if b.Prefix == "" {
		b.Prefix = "db-artifacts/" + b.Engine + "/"
	}
	if !strings.HasSuffix(b.Prefix, "/") {
		b.Prefix += "/"
	}
	b.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(
		`INSERT INTO tbl_db_deploy_oss_bind(engine, oss_setting_id, prefix, updated_by, updated_at) VALUES(?,?,?,?,?)
		 ON CONFLICT(engine) DO UPDATE SET oss_setting_id=EXCLUDED.oss_setting_id, prefix=EXCLUDED.prefix, updated_by=EXCLUDED.updated_by, updated_at=EXCLUDED.updated_at`,
		b.Engine, b.OSSSettingID, b.Prefix, b.UpdatedBy, b.UpdatedAt,
	)
	return err
}
