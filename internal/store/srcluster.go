package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// StarRocks 集群变更状态 / 类型。
const (
	SrChangePending   = "pending"
	SrChangeRunning   = "running"
	SrChangeSuccess   = "success"
	SrChangeFailed    = "failed"
	SrChangeCancelled = "cancelled"

	SrStepPending = "pending"
	SrStepRunning = "running"
	SrStepSuccess = "success"
	SrStepFailed  = "failed"
	SrStepSkipped = "skipped"

	SrChangeClusterInit     = "cluster_init"
	SrChangeClusterReinit   = "cluster_reinit"
	SrChangeAddFE           = "add_fe"
	SrChangeAddBE           = "add_be"
	SrChangeRestart         = "restart_node"
	SrChangeStartNode       = "start_node"
	SrChangeStopNode        = "stop_node"
	SrChangeRemoveFE        = "remove_fe"
	SrChangeRemoveBE        = "remove_be"
	SrChangeUpgrade         = "upgrade"
	SrChangeUpgradeRollback = "upgrade_rollback"
	SrChangeDecommissionBE  = "decommission_be"
	SrChangeRecommissionBE  = "recommission_be"
	SrChangeConfigApply     = "config_apply"
	SrChangeConfigRollback  = "config_rollback"

	SrClusterDraft   = "draft"
	SrClusterIniting = "initializing"
	SrClusterReady   = "ready"
	SrClusterFailed  = "failed"

	SrNodePending = "pending"
	SrNodeReady   = "ready"
	SrNodeStopped = "stopped"
	SrNodeFailed  = "failed"
	// SrNodeDecommissioning 已执行 DECOMMISSION、FE 尚在迁移 tablet；迁完后由 FE 移除，平台随后删除登记
	SrNodeDecommissioning = "decommissioning"

	// SrSourceDeploy 平台新建装机；SrSourceRegister 登记已有（仅纳管，可注销后重登记）
	SrSourceDeploy   = "deploy"
	SrSourceRegister = "register"

	SrDeployPending = "pending"
	SrDeployRunning = "running"
	SrDeploySuccess = "success"
	SrDeployFailed  = "failed"
)

// SrAgent Node Agent 登记（独立于 Backup/平台 Agent）。
type SrAgent struct {
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

// SrAgentDeploy Node Agent 一次 SSH 下发任务。
type SrAgentDeploy struct {
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

// SrCluster 集群登记。
type SrCluster struct {
	ID              string `json:"id"`
	TenantID        string `json:"tenant_id,omitempty"`
	TenantName      string `json:"tenant_name,omitempty"`
	Name            string `json:"name"`
	Environment     string `json:"environment"`
	Version         string `json:"version"`
	ReleaseID       string `json:"release_id,omitempty"`
	SoftwareBase    string `json:"software_base"`              // FE 软件根；空则用全局 sr_fe_software_base
	BeSoftwareBase  string `json:"be_software_base,omitempty"` // BE 软件根；空则用全局 sr_be_software_base，不回退 FE
	DataRoot        string `json:"data_root"`                  // BE 数据目录；空则用全局 sr_be_data_root
	FeHTTPPort      int    `json:"fe_http_port"`
	FeEditPort      int    `json:"fe_edit_port"`
	FeQueryPort     int    `json:"fe_query_port"`
	BeHeartbeatPort int    `json:"be_heartbeat_port"`
	FeHost          string `json:"fe_host,omitempty"` // 首 FE 或 VIP，用于控制面连 9030
	FeUser          string `json:"fe_user,omitempty"`
	FePasswordEnc   string `json:"-"`
	FePasswordSet   bool   `json:"fe_password_set"`
	Source          string `json:"source,omitempty"` // deploy | register
	Status          string `json:"status"`
	Remark          string `json:"remark,omitempty"`
	CreatedBy       string `json:"created_by,omitempty"`
	UpdatedBy       string `json:"updated_by,omitempty"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

// EffectiveBeSoftwareBase 仅返回集群上单独配置的 BE 软件根。空表示未覆盖，运行时走全局默认。
func (c *SrCluster) EffectiveBeSoftwareBase() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.BeSoftwareBase)
}

// SrNode 集群节点。
type SrNode struct {
	ID             string `json:"id"`
	ClusterID      string `json:"cluster_id"`
	Component      string `json:"component"` // fe | be
	FeRole         string `json:"fe_role,omitempty"`
	Host           string `json:"host"`
	HTTPPort       int    `json:"http_port,omitempty"`
	EditPort       int    `json:"edit_port,omitempty"`
	HeartbeatPort  int    `json:"heartbeat_port,omitempty"`
	SoftwarePath   string `json:"software_path,omitempty"`
	DataRoot       string `json:"data_root,omitempty"` // BE 数据盘挂载点；空则用集群 data_root
	SystemdUnit    string `json:"systemd_unit,omitempty"`
	HostInstanceID string `json:"host_instance_id,omitempty"`
	AgentID        string `json:"agent_id,omitempty"`
	IsPrimaryFE    bool   `json:"is_primary_fe"`
	Status         string `json:"status"`
	Remark         string `json:"remark,omitempty"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

// SrConfTemplate 平台级 fe.conf / be.conf 默认模板（设置 Tab）。
type SrConfTemplate struct {
	Role      string `json:"role"` // fe | be
	Content   string `json:"content"`
	UpdatedBy string `json:"updated_by,omitempty"`
	UpdatedAt string `json:"updated_at"`
}

// SrClusterConfig 集群级 fe.conf / be.conf 草稿（覆盖平台默认；下发时的优先来源）。
type SrClusterConfig struct {
	ClusterID string `json:"cluster_id"`
	Role      string `json:"role"` // fe | be
	Content   string `json:"content"`
	UpdatedBy string `json:"updated_by,omitempty"`
	UpdatedAt string `json:"updated_at"`
}

// SrNodeConfig 节点级配置草稿（覆盖集群/平台模板）。
type SrNodeConfig struct {
	NodeID    string `json:"node_id"`
	Content   string `json:"content"`
	UpdatedBy string `json:"updated_by,omitempty"`
	UpdatedAt string `json:"updated_at"`
}

// SrChange 变更单。
type SrChange struct {
	ID          string         `json:"id"`
	ClusterID   string         `json:"cluster_id"`
	Type        string         `json:"type"`
	Status      string         `json:"status"`
	PayloadJSON string         `json:"payload_json,omitempty"`
	LogText     string         `json:"log_text,omitempty"`
	CreatedBy   string         `json:"created_by,omitempty"`
	CreatedAt   string         `json:"created_at"`
	UpdatedAt   string         `json:"updated_at"`
	Steps       []SrChangeStep `json:"steps,omitempty"`
}

// SrChangeStep 变更步骤。
type SrChangeStep struct {
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

func (d *DB) ensureSrClusterTables() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS tbl_sr_agents (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL DEFAULT '',
			base_url TEXT NOT NULL DEFAULT '',
			username TEXT NOT NULL DEFAULT '',
			password_enc TEXT NOT NULL DEFAULT '',
			host_instance_id TEXT NOT NULL DEFAULT '',
			remark TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL DEFAULT '',
			updated_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_sr_clusters (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL DEFAULT '',
			environment TEXT NOT NULL DEFAULT '',
			version TEXT NOT NULL DEFAULT '',
			software_base TEXT NOT NULL DEFAULT '/home/dorisdb_base',
			data_root TEXT NOT NULL DEFAULT '/data',
			fe_http_port INTEGER NOT NULL DEFAULT 8030,
			fe_edit_port INTEGER NOT NULL DEFAULT 9010,
			fe_query_port INTEGER NOT NULL DEFAULT 9030,
			be_heartbeat_port INTEGER NOT NULL DEFAULT 9050,
			fe_host TEXT NOT NULL DEFAULT '',
			fe_user TEXT NOT NULL DEFAULT 'root',
			fe_password_enc TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'draft',
			remark TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL DEFAULT '',
			updated_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_sr_nodes (
			id TEXT PRIMARY KEY,
			cluster_id TEXT NOT NULL,
			component TEXT NOT NULL DEFAULT 'fe',
			fe_role TEXT NOT NULL DEFAULT '',
			host TEXT NOT NULL DEFAULT '',
			http_port INTEGER NOT NULL DEFAULT 0,
			edit_port INTEGER NOT NULL DEFAULT 0,
			heartbeat_port INTEGER NOT NULL DEFAULT 0,
			software_path TEXT NOT NULL DEFAULT '',
			data_root TEXT NOT NULL DEFAULT '',
			systemd_unit TEXT NOT NULL DEFAULT '',
			host_instance_id TEXT NOT NULL DEFAULT '',
			agent_id TEXT NOT NULL DEFAULT '',
			is_primary_fe INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'pending',
			remark TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_sr_changes (
			id TEXT PRIMARY KEY,
			cluster_id TEXT NOT NULL,
			type TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			payload_json TEXT NOT NULL DEFAULT '',
			log_text TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_sr_change_steps (
			id TEXT PRIMARY KEY,
			change_id TEXT NOT NULL,
			seq INTEGER NOT NULL DEFAULT 0,
			step_key TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '',
			node_id TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			log_text TEXT NOT NULL DEFAULT '',
			started_at TEXT NOT NULL DEFAULT '',
			ended_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_sr_oss_settings (
			id TEXT PRIMARY KEY DEFAULT 'default',
			oss_setting_id TEXT NOT NULL DEFAULT '',
			prefix TEXT NOT NULL DEFAULT 'starrocks/',
			endpoint TEXT NOT NULL DEFAULT '',
			bucket TEXT NOT NULL DEFAULT '',
			access_key TEXT NOT NULL DEFAULT '',
			secret_enc TEXT NOT NULL DEFAULT '',
			updated_by TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_sr_releases (
			id TEXT PRIMARY KEY,
			version TEXT NOT NULL UNIQUE,
			channel TEXT NOT NULL DEFAULT 'stable',
			jdk_min TEXT NOT NULL DEFAULT '11',
			enabled INTEGER NOT NULL DEFAULT 1,
			remark TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL DEFAULT '',
			updated_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_sr_release_artifacts (
			id TEXT PRIMARY KEY,
			release_id TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT '',
			object_key TEXT NOT NULL DEFAULT '',
			filename TEXT NOT NULL DEFAULT '',
			sha256 TEXT NOT NULL DEFAULT '',
			size_bytes BIGINT NOT NULL DEFAULT 0,
			format TEXT NOT NULL DEFAULT 'tar.gz',
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT '',
			UNIQUE(release_id, role)
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_sr_env_version_policies (
			id TEXT PRIMARY KEY,
			environment TEXT NOT NULL UNIQUE,
			default_release_id TEXT NOT NULL DEFAULT '',
			allowed_release_ids TEXT NOT NULL DEFAULT '[]',
			updated_by TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_sr_agent_deploys (
			id TEXT PRIMARY KEY,
			host_instance_id TEXT NOT NULL DEFAULT '',
			agent_id TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			listen_port INTEGER NOT NULL DEFAULT 8091,
			remote_dir TEXT NOT NULL DEFAULT '',
			base_url TEXT NOT NULL DEFAULT '',
			log_text TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT '',
			finished_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_sr_cluster_configs (
			cluster_id TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL DEFAULT '',
			updated_by TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (cluster_id, role)
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_sr_conf_templates (
			role TEXT PRIMARY KEY,
			content TEXT NOT NULL DEFAULT '',
			updated_by TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_sr_node_configs (
			node_id TEXT PRIMARY KEY,
			content TEXT NOT NULL DEFAULT '',
			updated_by TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sr_agent_deploys_host ON tbl_sr_agent_deploys(host_instance_id, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS tbl_sr_conf_revisions (
			id TEXT PRIMARY KEY,
			cluster_id TEXT NOT NULL DEFAULT '',
			node_id TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT '',
			kind TEXT NOT NULL DEFAULT '',
			content TEXT NOT NULL DEFAULT '',
			content_hash TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL DEFAULT '',
			change_id TEXT NOT NULL DEFAULT '',
			note TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sr_conf_rev_node ON tbl_sr_conf_revisions(node_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_sr_conf_rev_change ON tbl_sr_conf_revisions(change_id)`,
	}
	for _, s := range stmts {
		if _, err := d.Exec(s); err != nil {
			return err
		}
	}
	_, _ = d.Exec(`ALTER TABLE tbl_sr_clusters ADD COLUMN IF NOT EXISTS release_id TEXT NOT NULL DEFAULT ''`)
	_, _ = d.Exec(`ALTER TABLE tbl_sr_clusters ADD COLUMN IF NOT EXISTS be_software_base TEXT NOT NULL DEFAULT ''`)
	_, _ = d.Exec(`ALTER TABLE tbl_sr_clusters ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT ''`)
	_, _ = d.Exec(`ALTER TABLE tbl_sr_oss_settings ADD COLUMN IF NOT EXISTS oss_setting_id TEXT NOT NULL DEFAULT ''`)
	_, _ = d.Exec(`ALTER TABLE tbl_sr_nodes ADD COLUMN IF NOT EXISTS data_root TEXT NOT NULL DEFAULT ''`)
	_, _ = d.Exec(`ALTER TABLE tbl_sr_releases ADD COLUMN IF NOT EXISTS jdk_max TEXT NOT NULL DEFAULT ''`)
	// 历史数据：remark 含「登记已有」视为 register，其余 deploy
	_, _ = d.Exec(`UPDATE tbl_sr_clusters SET source=? WHERE (source='' OR source IS NULL) AND remark LIKE ?`,
		SrSourceRegister, "%登记已有%")
	_, _ = d.Exec(`UPDATE tbl_sr_clusters SET source=? WHERE source='' OR source IS NULL`, SrSourceDeploy)
	return nil
}

func nowSR() string { return time.Now().Format(time.RFC3339) }

// SrAgentEscalateTag 写入 remark 的提权标记。
func SrAgentEscalateTag(mode string) string {
	m := strings.ToLower(strings.TrimSpace(mode))
	if m == "" || m == "none" {
		return ""
	}
	return "escalate:" + m
}

// ParseSrAgentEscalate 从 remark 解析提权模式（如 escalate:sudo_su）。
func ParseSrAgentEscalate(remark string) string {
	for _, p := range strings.Split(remark, "|") {
		for _, q := range strings.Split(p, ",") {
			q = strings.TrimSpace(q)
			if strings.HasPrefix(q, "escalate:") {
				return strings.TrimSpace(strings.TrimPrefix(q, "escalate:"))
			}
		}
	}
	return ""
}

// FormatSrAgentRemark 合并提权标记与可读说明。
func FormatSrAgentRemark(escalate, note string) string {
	note = strings.TrimSpace(note)
	parts := []string{}
	if et := SrAgentEscalateTag(escalate); et != "" {
		parts = append(parts, et)
	}
	// 去掉旧 escalate 段，保留其它说明
	for _, p := range strings.Split(note, "|") {
		p = strings.TrimSpace(p)
		if p == "" || strings.HasPrefix(p, "escalate:") {
			continue
		}
		parts = append(parts, p)
	}
	return strings.Join(parts, "|")
}

// --- Agents ---

func (d *DB) CreateSrAgent(a *SrAgent) error {
	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	now := nowSR()
	a.CreatedAt, a.UpdatedAt = now, now
	_, err := d.Exec(`INSERT INTO tbl_sr_agents
		(id,name,base_url,username,password_enc,host_instance_id,remark,created_by,updated_by,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.Name, a.BaseURL, a.Username, a.PasswordEnc, a.HostInstID, a.Remark,
		a.CreatedBy, a.UpdatedBy, a.CreatedAt, a.UpdatedAt)
	if err != nil {
		return err
	}
	if tid := d.inheritTenantFromInstance(a.HostInstID); tid != "" {
		return d.AssignTenantID("tbl_sr_agents", a.ID, tid)
	}
	return nil
}

func (d *DB) UpdateSrAgent(a *SrAgent) error {
	a.UpdatedAt = nowSR()
	_, err := d.Exec(`UPDATE tbl_sr_agents SET name=?, base_url=?, username=?, password_enc=?,
		host_instance_id=?, remark=?, updated_by=?, updated_at=? WHERE id=?`,
		a.Name, a.BaseURL, a.Username, a.PasswordEnc, a.HostInstID, a.Remark, a.UpdatedBy, a.UpdatedAt, a.ID)
	return err
}

func (d *DB) DeleteSrAgent(id string) error {
	_, err := d.Exec(`DELETE FROM tbl_sr_agents WHERE id=?`, id)
	return err
}

func scanSrAgent(sc interface{ Scan(dest ...any) error }) (*SrAgent, error) {
	var a SrAgent
	var enc string
	if err := sc.Scan(&a.ID, &a.Name, &a.BaseURL, &a.Username, &enc, &a.HostInstID, &a.Remark,
		&a.CreatedBy, &a.UpdatedBy, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, err
	}
	a.PasswordEnc = enc
	a.PasswordSet = strings.TrimSpace(enc) != ""
	return &a, nil
}

func (d *DB) GetSrAgent(id string) (*SrAgent, error) {
	row := d.QueryRow(`SELECT id,name,base_url,username,password_enc,host_instance_id,remark,created_by,updated_by,created_at,updated_at
		FROM tbl_sr_agents WHERE id=?`, id)
	a, err := scanSrAgent(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return a, err
}

func (d *DB) ListSrAgents() ([]SrAgent, error) {
	rows, err := d.Query(`SELECT id,name,base_url,username,password_enc,host_instance_id,remark,created_by,updated_by,created_at,updated_at
		FROM tbl_sr_agents ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SrAgent
	for rows.Next() {
		a, err := scanSrAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (d *DB) FindSrAgentByHostInstance(hostInstanceID string) (*SrAgent, error) {
	hostInstanceID = strings.TrimSpace(hostInstanceID)
	if hostInstanceID == "" {
		return nil, nil
	}
	row := d.QueryRow(`SELECT id,name,base_url,username,password_enc,host_instance_id,remark,created_by,updated_by,created_at,updated_at
		FROM tbl_sr_agents WHERE host_instance_id=? ORDER BY updated_at DESC LIMIT 1`, hostInstanceID)
	a, err := scanSrAgent(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return a, err
}

func (d *DB) CreateSrAgentDeploy(it *SrAgentDeploy) error {
	if it.ID == "" {
		it.ID = uuid.NewString()
	}
	now := nowSR()
	it.CreatedAt, it.UpdatedAt = now, now
	if it.Status == "" {
		it.Status = SrDeployPending
	}
	_, err := d.Exec(`INSERT INTO tbl_sr_agent_deploys
		(id,host_instance_id,agent_id,status,listen_port,remote_dir,base_url,log_text,created_by,created_at,updated_at,finished_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		it.ID, it.HostInstanceID, it.AgentID, it.Status, it.ListenPort, it.RemoteDir, it.BaseURL, it.LogText,
		it.CreatedBy, it.CreatedAt, it.UpdatedAt, it.FinishedAt)
	if err != nil {
		return err
	}
	if tid := d.inheritTenantFromInstance(it.HostInstanceID); tid != "" {
		return d.AssignTenantID("tbl_sr_agent_deploys", it.ID, tid)
	}
	return nil
}

func (d *DB) UpdateSrAgentDeploy(it *SrAgentDeploy) error {
	it.UpdatedAt = nowSR()
	_, err := d.Exec(`UPDATE tbl_sr_agent_deploys SET agent_id=?, status=?, listen_port=?, remote_dir=?, base_url=?,
		log_text=?, updated_at=?, finished_at=? WHERE id=?`,
		it.AgentID, it.Status, it.ListenPort, it.RemoteDir, it.BaseURL, it.LogText, it.UpdatedAt, it.FinishedAt, it.ID)
	return err
}

func (d *DB) GetSrAgentDeploy(id string) (*SrAgentDeploy, error) {
	it := &SrAgentDeploy{}
	err := d.QueryRow(`SELECT id,host_instance_id,agent_id,status,listen_port,remote_dir,base_url,log_text,created_by,created_at,updated_at,finished_at
		FROM tbl_sr_agent_deploys WHERE id=?`, id).Scan(
		&it.ID, &it.HostInstanceID, &it.AgentID, &it.Status, &it.ListenPort, &it.RemoteDir, &it.BaseURL, &it.LogText,
		&it.CreatedBy, &it.CreatedAt, &it.UpdatedAt, &it.FinishedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return it, nil
}

// AgentDeployStaleAfter 超过该时长仍为 pending/running 的 Agent 下发视为后端重启遗留，不再阻塞新下发。
const AgentDeployStaleAfter = 15 * time.Minute

// AgentDeployActive Agent 下发记录是否仍在进行（时间无法解析时按进行中处理）。
func AgentDeployActive(status, createdAt string, now time.Time) bool {
	if status != SrDeployPending && status != SrDeployRunning {
		return false
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(createdAt))
	if err != nil {
		return true
	}
	return now.Sub(t) < AgentDeployStaleAfter
}

func (d *DB) ListSrAgentDeploys(hostInstanceID string, limit int) ([]SrAgentDeploy, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT id,host_instance_id,agent_id,status,listen_port,remote_dir,base_url,log_text,created_by,created_at,updated_at,finished_at
		FROM tbl_sr_agent_deploys WHERE 1=1`
	var args []any
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
	var out []SrAgentDeploy
	for rows.Next() {
		var it SrAgentDeploy
		if err := rows.Scan(&it.ID, &it.HostInstanceID, &it.AgentID, &it.Status, &it.ListenPort, &it.RemoteDir, &it.BaseURL, &it.LogText,
			&it.CreatedBy, &it.CreatedAt, &it.UpdatedAt, &it.FinishedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	if out == nil {
		out = []SrAgentDeploy{}
	}
	return out, rows.Err()
}

// --- Clusters ---

func (d *DB) CreateSrCluster(c *SrCluster) error {
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	c.SoftwareBase = strings.TrimRight(strings.TrimSpace(c.SoftwareBase), "/")
	c.BeSoftwareBase = strings.TrimRight(strings.TrimSpace(c.BeSoftwareBase), "/")
	c.DataRoot = strings.TrimRight(strings.TrimSpace(c.DataRoot), "/")
	if c.FeHTTPPort <= 0 {
		c.FeHTTPPort = 8030
	}
	if c.FeEditPort <= 0 {
		c.FeEditPort = 9010
	}
	if c.FeQueryPort <= 0 {
		c.FeQueryPort = 9030
	}
	if c.BeHeartbeatPort <= 0 {
		c.BeHeartbeatPort = 9050
	}
	if c.FeUser == "" {
		c.FeUser = "root"
	}
	if c.Status == "" {
		c.Status = SrClusterDraft
	}
	if c.Source != SrSourceRegister {
		c.Source = SrSourceDeploy
	}
	now := nowSR()
	c.CreatedAt, c.UpdatedAt = now, now
	_, err := d.Exec(`INSERT INTO tbl_sr_clusters
		(id,name,environment,version,release_id,software_base,be_software_base,data_root,fe_http_port,fe_edit_port,fe_query_port,be_heartbeat_port,
		 fe_host,fe_user,fe_password_enc,source,status,remark,created_by,updated_by,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.Name, c.Environment, c.Version, c.ReleaseID, c.SoftwareBase, c.BeSoftwareBase, c.DataRoot,
		c.FeHTTPPort, c.FeEditPort, c.FeQueryPort, c.BeHeartbeatPort,
		c.FeHost, c.FeUser, c.FePasswordEnc, c.Source, c.Status, c.Remark,
		c.CreatedBy, c.UpdatedBy, c.CreatedAt, c.UpdatedAt)
	if err != nil {
		return err
	}
	return d.AssignTenantID("tbl_sr_clusters", c.ID, c.TenantID)
}

func (d *DB) UpdateSrCluster(c *SrCluster) error {
	c.UpdatedAt = nowSR()
	if c.Source != SrSourceRegister && c.Source != SrSourceDeploy {
		c.Source = SrSourceDeploy
	}
	_, err := d.Exec(`UPDATE tbl_sr_clusters SET name=?, environment=?, version=?, release_id=?, software_base=?, be_software_base=?, data_root=?,
		fe_http_port=?, fe_edit_port=?, fe_query_port=?, be_heartbeat_port=?, fe_host=?, fe_user=?, fe_password_enc=?,
		source=?, status=?, remark=?, updated_by=?, updated_at=? WHERE id=?`,
		c.Name, c.Environment, c.Version, c.ReleaseID, c.SoftwareBase, c.BeSoftwareBase, c.DataRoot,
		c.FeHTTPPort, c.FeEditPort, c.FeQueryPort, c.BeHeartbeatPort,
		c.FeHost, c.FeUser, c.FePasswordEnc, c.Source, c.Status, c.Remark, c.UpdatedBy, c.UpdatedAt, c.ID)
	return err
}

func (d *DB) DeleteSrCluster(id string) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(d.rewrite(`DELETE FROM tbl_sr_change_steps WHERE change_id IN (SELECT id FROM tbl_sr_changes WHERE cluster_id=?)`), id); err != nil {
		return err
	}
	if _, err := tx.Exec(d.rewrite(`DELETE FROM tbl_sr_changes WHERE cluster_id=?`), id); err != nil {
		return err
	}
	if _, err := tx.Exec(d.rewrite(`DELETE FROM tbl_sr_cluster_configs WHERE cluster_id=?`), id); err != nil {
		return err
	}
	if _, err := tx.Exec(d.rewrite(`DELETE FROM tbl_sr_node_configs WHERE node_id IN (SELECT id FROM tbl_sr_nodes WHERE cluster_id=?)`), id); err != nil {
		return err
	}
	if _, err := tx.Exec(d.rewrite(`DELETE FROM tbl_sr_nodes WHERE cluster_id=?`), id); err != nil {
		return err
	}
	if _, err := tx.Exec(d.rewrite(`DELETE FROM tbl_sr_clusters WHERE id=?`), id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// 版本表可能尚未建好，放在事务外且忽略错误，避免阻塞删集群
	_ = d.DeleteSrConfRevisionsByCluster(id)
	return nil
}

const srClusterSelectCols = `id, COALESCE(tenant_id,''), name, environment, version, release_id, software_base, COALESCE(be_software_base,''), data_root, fe_http_port, fe_edit_port, fe_query_port, be_heartbeat_port,
		fe_host, fe_user, fe_password_enc, COALESCE(source,''), status, remark, created_by, updated_by, created_at, updated_at`

func scanSrCluster(sc interface{ Scan(dest ...any) error }) (*SrCluster, error) {
	var c SrCluster
	var enc string
	if err := sc.Scan(&c.ID, &c.TenantID, &c.Name, &c.Environment, &c.Version, &c.ReleaseID, &c.SoftwareBase, &c.BeSoftwareBase, &c.DataRoot,
		&c.FeHTTPPort, &c.FeEditPort, &c.FeQueryPort, &c.BeHeartbeatPort,
		&c.FeHost, &c.FeUser, &enc, &c.Source, &c.Status, &c.Remark,
		&c.CreatedBy, &c.UpdatedBy, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	c.FePasswordEnc = enc
	c.FePasswordSet = strings.TrimSpace(enc) != ""
	if c.Source != SrSourceRegister {
		c.Source = SrSourceDeploy
	}
	return &c, nil
}

func (d *DB) GetSrCluster(id string) (*SrCluster, error) {
	row := d.QueryRow(`SELECT `+srClusterSelectCols+` FROM tbl_sr_clusters WHERE id=?`, id)
	c, err := scanSrCluster(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return c, err
}

func (d *DB) ListSrClusters(env string) ([]SrCluster, error) {
	return d.ListSrClustersInTenant("", env)
}

func (d *DB) ListSrClustersInTenant(tenantID, env string) ([]SrCluster, error) {
	q := `SELECT ` + srClusterSelectCols + ` FROM tbl_sr_clusters WHERE 1=1`
	args := []any{}
	if tid := strings.TrimSpace(tenantID); tid != "" {
		q += ` AND COALESCE(tenant_id,'')=?`
		args = append(args, tid)
	}
	if strings.TrimSpace(env) != "" {
		q += ` AND environment=?`
		args = append(args, env)
	}
	q += ` ORDER BY created_at DESC`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SrCluster
	for rows.Next() {
		c, err := scanSrCluster(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// --- Nodes ---

func (d *DB) CreateSrNode(n *SrNode) error {
	if n.ID == "" {
		n.ID = uuid.NewString()
	}
	if n.Status == "" {
		n.Status = SrNodePending
	}
	now := nowSR()
	n.CreatedAt, n.UpdatedAt = now, now
	prim := 0
	if n.IsPrimaryFE {
		prim = 1
	}
	_, err := d.Exec(`INSERT INTO tbl_sr_nodes
		(id,cluster_id,component,fe_role,host,http_port,edit_port,heartbeat_port,software_path,data_root,systemd_unit,
		 host_instance_id,agent_id,is_primary_fe,status,remark,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		n.ID, n.ClusterID, n.Component, n.FeRole, n.Host, n.HTTPPort, n.EditPort, n.HeartbeatPort,
		n.SoftwarePath, n.DataRoot, n.SystemdUnit, n.HostInstanceID, n.AgentID, prim, n.Status, n.Remark, n.CreatedAt, n.UpdatedAt)
	if err != nil {
		return err
	}
	return d.inheritTenantFromParent("tbl_sr_nodes", n.ID, "tbl_sr_clusters", n.ClusterID)
}

func (d *DB) UpdateSrNode(n *SrNode) error {
	n.UpdatedAt = nowSR()
	prim := 0
	if n.IsPrimaryFE {
		prim = 1
	}
	_, err := d.Exec(`UPDATE tbl_sr_nodes SET component=?, fe_role=?, host=?, http_port=?, edit_port=?, heartbeat_port=?,
		software_path=?, data_root=?, systemd_unit=?, host_instance_id=?, agent_id=?, is_primary_fe=?, status=?, remark=?, updated_at=? WHERE id=?`,
		n.Component, n.FeRole, n.Host, n.HTTPPort, n.EditPort, n.HeartbeatPort,
		n.SoftwarePath, n.DataRoot, n.SystemdUnit, n.HostInstanceID, n.AgentID, prim, n.Status, n.Remark, n.UpdatedAt, n.ID)
	return err
}

func (d *DB) DeleteSrNode(id string) error {
	_, _ = d.Exec(`DELETE FROM tbl_sr_node_configs WHERE node_id=?`, id)
	_ = d.DeleteSrConfRevisionsByNode(id)
	_, err := d.Exec(`DELETE FROM tbl_sr_nodes WHERE id=?`, id)
	return err
}

func scanSrNode(sc interface{ Scan(dest ...any) error }) (*SrNode, error) {
	var n SrNode
	var prim int
	if err := sc.Scan(&n.ID, &n.ClusterID, &n.Component, &n.FeRole, &n.Host, &n.HTTPPort, &n.EditPort, &n.HeartbeatPort,
		&n.SoftwarePath, &n.DataRoot, &n.SystemdUnit, &n.HostInstanceID, &n.AgentID, &prim, &n.Status, &n.Remark, &n.CreatedAt, &n.UpdatedAt); err != nil {
		return nil, err
	}
	n.IsPrimaryFE = prim != 0
	return &n, nil
}

func (d *DB) GetSrNode(id string) (*SrNode, error) {
	row := d.QueryRow(`SELECT id,cluster_id,component,fe_role,host,http_port,edit_port,heartbeat_port,software_path,data_root,systemd_unit,
		host_instance_id,agent_id,is_primary_fe,status,remark,created_at,updated_at FROM tbl_sr_nodes WHERE id=?`, id)
	n, err := scanSrNode(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return n, err
}

func (d *DB) ListSrNodes(clusterID string) ([]SrNode, error) {
	return d.listSrNodesWhere(`cluster_id=?`, clusterID)
}

// ListSrNodesByAgent 使用该 Node Agent 的节点（删除 Agent 前校验）。
func (d *DB) ListSrNodesByAgent(agentID string) ([]SrNode, error) {
	return d.listSrNodesWhere(`agent_id=?`, strings.TrimSpace(agentID))
}

// ListSrNodesByHostRef 登记在该主机实例、或 host 为主机任一地址的节点（删除主机前校验）。
func (d *DB) ListSrNodesByHostRef(hostInstID string, addrs []string) ([]SrNode, error) {
	where, args := hostRefWhere(hostInstID, addrs)
	return d.listSrNodesWhere(where, args...)
}

// RebindSrNodesAgent 该主机上 agent_id 为空或指向已删除 Agent 的节点改绑到 agentID，返回改绑数量。
func (d *DB) RebindSrNodesAgent(hostInstID string, addrs []string, agentID string) (int, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return 0, nil
	}
	nodes, err := d.ListSrNodesByHostRef(hostInstID, addrs)
	if err != nil {
		return 0, err
	}
	exists := func(id string) bool {
		a, err := d.GetSrAgent(id)
		return err != nil || a != nil
	}
	n := 0
	for i := range nodes {
		if !nodeNeedsAgentRebind(nodes[i].AgentID, agentID, exists) {
			continue
		}
		nodes[i].AgentID = agentID
		if err := d.UpdateSrNode(&nodes[i]); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// nodeNeedsAgentRebind 节点当前 agent_id 为空或指向已不存在的 Agent 时才改绑；查询出错按存在处理，避免误改。
func nodeNeedsAgentRebind(cur, agentID string, exists func(string) bool) bool {
	cur = strings.TrimSpace(cur)
	if cur == agentID {
		return false
	}
	return cur == "" || !exists(cur)
}

// hostRefWhere host_instance_id=? OR LOWER(host) IN (...)；addrs 须已小写。
func hostRefWhere(hostInstID string, addrs []string) (string, []any) {
	var conds []string
	var args []any
	if id := strings.TrimSpace(hostInstID); id != "" {
		conds = append(conds, `host_instance_id=?`)
		args = append(args, id)
	}
	if len(addrs) > 0 {
		ph := make([]string, len(addrs))
		for i, a := range addrs {
			ph[i] = "?"
			args = append(args, strings.ToLower(strings.TrimSpace(a)))
		}
		conds = append(conds, `LOWER(host) IN (`+strings.Join(ph, ",")+`)`)
	}
	if len(conds) == 0 {
		return "1=0", nil
	}
	return "(" + strings.Join(conds, " OR ") + ")", args
}

func (d *DB) listSrNodesWhere(where string, args ...any) ([]SrNode, error) {
	rows, err := d.Query(`SELECT id,cluster_id,component,fe_role,host,http_port,edit_port,heartbeat_port,software_path,data_root,systemd_unit,
		host_instance_id,agent_id,is_primary_fe,status,remark,created_at,updated_at
		FROM tbl_sr_nodes WHERE `+where+` ORDER BY is_primary_fe DESC, component, host`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SrNode
	for rows.Next() {
		n, err := scanSrNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *n)
	}
	return out, rows.Err()
}

// --- Changes ---

func (d *DB) CreateSrChange(ch *SrChange) error {
	if ch.ID == "" {
		ch.ID = uuid.NewString()
	}
	if ch.Status == "" {
		ch.Status = SrChangePending
	}
	now := nowSR()
	ch.CreatedAt, ch.UpdatedAt = now, now
	_, err := d.Exec(`INSERT INTO tbl_sr_changes (id,cluster_id,type,status,payload_json,log_text,created_by,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		ch.ID, ch.ClusterID, ch.Type, ch.Status, ch.PayloadJSON, ch.LogText, ch.CreatedBy, ch.CreatedAt, ch.UpdatedAt)
	if err != nil {
		return err
	}
	return d.inheritTenantFromParent("tbl_sr_changes", ch.ID, "tbl_sr_clusters", ch.ClusterID)
}

func (d *DB) UpdateSrChangeStatus(id, status, logAppend string) error {
	now := nowSR()
	appendText := strings.TrimSpace(logAppend)
	if appendText == "" {
		_, err := d.Exec(`UPDATE tbl_sr_changes SET status=?, updated_at=? WHERE id=?`, status, now, id)
		return err
	}
	var old string
	err := d.QueryRow(`SELECT COALESCE(log_text,'') FROM tbl_sr_changes WHERE id=?`, id).Scan(&old)
	if err != nil {
		return err
	}
	newLog := appendText
	if strings.TrimSpace(old) != "" {
		newLog = strings.TrimRight(old, "\n") + "\n" + appendText
	}
	_, err = d.Exec(`UPDATE tbl_sr_changes SET status=?, log_text=?, updated_at=? WHERE id=?`, status, newLog, now, id)
	return err
}

func (d *DB) GetSrChange(id string) (*SrChange, error) {
	row := d.QueryRow(`SELECT id,cluster_id,type,status,payload_json,log_text,created_by,created_at,updated_at FROM tbl_sr_changes WHERE id=?`, id)
	var ch SrChange
	if err := row.Scan(&ch.ID, &ch.ClusterID, &ch.Type, &ch.Status, &ch.PayloadJSON, &ch.LogText, &ch.CreatedBy, &ch.CreatedAt, &ch.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	steps, err := d.ListSrChangeSteps(id)
	if err != nil {
		return nil, err
	}
	ch.Steps = steps
	return &ch, nil
}

func (d *DB) ListSrChanges(clusterID string, limit int) ([]SrChange, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows *sql.Rows
	var err error
	if strings.TrimSpace(clusterID) != "" {
		rows, err = d.Query(`SELECT id,cluster_id,type,status,payload_json,log_text,created_by,created_at,updated_at
			FROM tbl_sr_changes WHERE cluster_id=? ORDER BY created_at DESC LIMIT ?`, clusterID, limit)
	} else {
		rows, err = d.Query(`SELECT id,cluster_id,type,status,payload_json,log_text,created_by,created_at,updated_at
			FROM tbl_sr_changes ORDER BY created_at DESC LIMIT ?`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SrChange
	for rows.Next() {
		var ch SrChange
		if err := rows.Scan(&ch.ID, &ch.ClusterID, &ch.Type, &ch.Status, &ch.PayloadJSON, &ch.LogText, &ch.CreatedBy, &ch.CreatedAt, &ch.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, ch)
	}
	return out, rows.Err()
}

func (d *DB) CreateSrChangeStep(s *SrChangeStep) error {
	if s.ID == "" {
		s.ID = uuid.NewString()
	}
	if s.Status == "" {
		s.Status = SrStepPending
	}
	_, err := d.Exec(`INSERT INTO tbl_sr_change_steps
		(id,change_id,seq,step_key,title,node_id,status,log_text,started_at,ended_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		s.ID, s.ChangeID, s.Seq, s.StepKey, s.Title, s.NodeID, s.Status, s.LogText, s.StartedAt, s.EndedAt)
	return err
}

func (d *DB) UpdateSrChangeStep(s *SrChangeStep) error {
	_, err := d.Exec(`UPDATE tbl_sr_change_steps SET status=?, log_text=?, started_at=?, ended_at=? WHERE id=?`,
		s.Status, s.LogText, s.StartedAt, s.EndedAt, s.ID)
	return err
}

func (d *DB) ListSrChangeSteps(changeID string) ([]SrChangeStep, error) {
	rows, err := d.Query(`SELECT id,change_id,seq,step_key,title,node_id,status,log_text,started_at,ended_at
		FROM tbl_sr_change_steps WHERE change_id=? ORDER BY seq ASC`, changeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SrChangeStep
	for rows.Next() {
		var s SrChangeStep
		if err := rows.Scan(&s.ID, &s.ChangeID, &s.Seq, &s.StepKey, &s.Title, &s.NodeID, &s.Status, &s.LogText, &s.StartedAt, &s.EndedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (d *DB) CountSrFENodes(clusterID string) (int, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(1) FROM tbl_sr_nodes WHERE cluster_id=? AND component='fe'`, clusterID).Scan(&n)
	return n, err
}

// --- Conf templates (platform defaults) & cluster configs ---

func (d *DB) ListSrConfTemplates() ([]SrConfTemplate, error) {
	rows, err := d.Query(`SELECT role, content, updated_by, updated_at FROM tbl_sr_conf_templates ORDER BY role`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SrConfTemplate
	for rows.Next() {
		var t SrConfTemplate
		if err := rows.Scan(&t.Role, &t.Content, &t.UpdatedBy, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (d *DB) GetSrConfTemplate(role string) (*SrConfTemplate, error) {
	role = strings.ToLower(strings.TrimSpace(role))
	row := d.QueryRow(`SELECT role, content, updated_by, updated_at FROM tbl_sr_conf_templates WHERE role=?`, role)
	var t SrConfTemplate
	if err := row.Scan(&t.Role, &t.Content, &t.UpdatedBy, &t.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &t, nil
}

func (d *DB) UpsertSrConfTemplate(t *SrConfTemplate) error {
	if t == nil {
		return fmt.Errorf("template 为空")
	}
	role := strings.ToLower(strings.TrimSpace(t.Role))
	if role != "fe" && role != "be" {
		return fmt.Errorf("role 须为 fe 或 be")
	}
	t.Role = role
	t.UpdatedAt = nowSR()
	_, err := d.Exec(`INSERT INTO tbl_sr_conf_templates(role, content, updated_by, updated_at)
		VALUES(?,?,?,?)
		ON CONFLICT(role) DO UPDATE SET content=EXCLUDED.content, updated_by=EXCLUDED.updated_by, updated_at=EXCLUDED.updated_at`,
		t.Role, t.Content, t.UpdatedBy, t.UpdatedAt)
	return err
}

// EnsureSrConfTemplates 写入内置默认正文。
// - 空库种子
// - system / 空 updated_by：始终对齐最新内置
// - 仍为旧版短模板（缺 JAVA_OPTS / be_http_port 等）时强制升级，避免库里陈旧内容挡住现网模板
func (d *DB) EnsureSrConfTemplates(defaults map[string]string) error {
	for role, body := range defaults {
		role = strings.ToLower(strings.TrimSpace(role))
		if role != "fe" && role != "be" {
			continue
		}
		exist, err := d.GetSrConfTemplate(role)
		if err != nil {
			return err
		}
		need := exist == nil || strings.TrimSpace(exist.Content) == ""
		if !need && exist != nil {
			by := strings.TrimSpace(exist.UpdatedBy)
			if by == "" || by == "system" {
				need = strings.TrimSpace(exist.Content) != strings.TrimSpace(body)
			} else if outdatedSrConfTemplate(role, exist.Content) {
				need = true
			}
		}
		if !need {
			continue
		}
		if err := d.UpsertSrConfTemplate(&SrConfTemplate{Role: role, Content: body, UpdatedBy: "system"}); err != nil {
			return err
		}
	}
	return nil
}

func outdatedSrConfTemplate(role, content string) bool {
	c := content
	switch role {
	case "fe":
		return !strings.Contains(c, "JAVA_OPTS_FOR_JDK_11") ||
			strings.Contains(c, "ports / runtime; data paths appended")
	case "be":
		return !strings.Contains(c, "be_http_port") ||
			strings.Contains(c, "webserver_port")
	default:
		return false
	}
}

func (d *DB) ListSrClusterConfigs(clusterID string) ([]SrClusterConfig, error) {
	rows, err := d.Query(`SELECT cluster_id, role, content, updated_by, updated_at
		FROM tbl_sr_cluster_configs WHERE cluster_id=? ORDER BY role`, clusterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SrClusterConfig
	for rows.Next() {
		var c SrClusterConfig
		if err := rows.Scan(&c.ClusterID, &c.Role, &c.Content, &c.UpdatedBy, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (d *DB) GetSrClusterConfig(clusterID, role string) (*SrClusterConfig, error) {
	row := d.QueryRow(`SELECT cluster_id, role, content, updated_by, updated_at
		FROM tbl_sr_cluster_configs WHERE cluster_id=? AND role=?`, clusterID, role)
	var c SrClusterConfig
	if err := row.Scan(&c.ClusterID, &c.Role, &c.Content, &c.UpdatedBy, &c.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

func (d *DB) UpsertSrClusterConfig(c *SrClusterConfig) error {
	if c == nil {
		return fmt.Errorf("config 为空")
	}
	role := strings.ToLower(strings.TrimSpace(c.Role))
	if role != "fe" && role != "be" {
		return fmt.Errorf("role 须为 fe 或 be")
	}
	c.Role = role
	c.UpdatedAt = nowSR()
	_, err := d.Exec(`INSERT INTO tbl_sr_cluster_configs(cluster_id, role, content, updated_by, updated_at)
		VALUES(?,?,?,?,?)
		ON CONFLICT(cluster_id, role) DO UPDATE SET content=EXCLUDED.content, updated_by=EXCLUDED.updated_by, updated_at=EXCLUDED.updated_at`,
		c.ClusterID, c.Role, c.Content, c.UpdatedBy, c.UpdatedAt)
	return err
}

func (d *DB) GetSrNodeConfig(nodeID string) (*SrNodeConfig, error) {
	row := d.QueryRow(`SELECT node_id, content, updated_by, updated_at FROM tbl_sr_node_configs WHERE node_id=?`, nodeID)
	var c SrNodeConfig
	if err := row.Scan(&c.NodeID, &c.Content, &c.UpdatedBy, &c.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

func (d *DB) UpsertSrNodeConfig(c *SrNodeConfig) error {
	if c == nil || strings.TrimSpace(c.NodeID) == "" {
		return fmt.Errorf("node_id 必填")
	}
	c.UpdatedAt = nowSR()
	_, err := d.Exec(`INSERT INTO tbl_sr_node_configs(node_id, content, updated_by, updated_at)
		VALUES(?,?,?,?)
		ON CONFLICT(node_id) DO UPDATE SET content=EXCLUDED.content, updated_by=EXCLUDED.updated_by, updated_at=EXCLUDED.updated_at`,
		c.NodeID, c.Content, c.UpdatedBy, c.UpdatedAt)
	return err
}

func (d *DB) DeleteSrNodeConfig(nodeID string) error {
	_, err := d.Exec(`DELETE FROM tbl_sr_node_configs WHERE node_id=?`, nodeID)
	return err
}

func ValidateOddFE(count int) error {
	if count <= 0 {
		return fmt.Errorf("至少需要 1 个 FE")
	}
	if count%2 == 0 {
		return fmt.Errorf("FE 节点数须为奇数，当前 %d", count)
	}
	return nil
}
