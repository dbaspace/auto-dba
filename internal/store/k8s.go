package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	K8sStatusUnknown   = "unknown"
	K8sStatusHealthy   = "healthy"
	K8sStatusDegraded  = "degraded"
	K8sStatusUnreachable = "unreachable"

	K8sAccessDirect = "direct"
	K8sAccessAgent  = "agent"

	K8sAuthKubeconfig = "kubeconfig"
	K8sAuthToken      = "token"

	K8sVisibilityGlobal = "global"
	K8sVisibilityPrivate = "private"
)

// K8sCluster 纳管集群。
type K8sCluster struct {
	ID                 string `json:"id"`
	TenantID           string `json:"tenant_id,omitempty"`
	Name               string `json:"name"`
	EnvID              string `json:"env_id,omitempty"`
	RegionID           string `json:"region_id,omitempty"`
	AccessMode         string `json:"access_mode"`
	AuthType           string `json:"auth_type"`
	APIServer          string `json:"api_server,omitempty"`
	CredentialEnc      string `json:"-"`
	CredentialSet      bool   `json:"credential_set"`
	CAData             string `json:"ca_data,omitempty"`
	AgentID            string `json:"agent_id,omitempty"`
	ImpersonateEnabled bool   `json:"impersonate_enabled"`
	Version            string `json:"version,omitempty"`
	NodeCount          int    `json:"node_count"`
	Status             string `json:"status"`
	LastProbeAt        string `json:"last_probe_at,omitempty"`
	LastProbeError     string `json:"last_probe_error,omitempty"`
	Visibility         string `json:"visibility"`
	Remark             string `json:"remark,omitempty"`
	CreatedBy          string `json:"created_by,omitempty"`
	UpdatedBy          string `json:"updated_by,omitempty"`
	CreatedAt          string `json:"created_at"`
	UpdatedAt          string `json:"updated_at"`
}

// K8sNamespaceBinding 命名空间与租户绑定。
type K8sNamespaceBinding struct {
	ID        string `json:"id"`
	ClusterID string `json:"cluster_id"`
	Namespace string `json:"namespace"`
	TenantID  string `json:"tenant_id"`
	Visibility string `json:"visibility"`
	CreatedBy string `json:"created_by,omitempty"`
	CreatedAt string `json:"created_at"`
}

// K8sAuditLog 审计。
type K8sAuditLog struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id,omitempty"`
	ClusterID string `json:"cluster_id"`
	Namespace string `json:"namespace,omitempty"`
	Username  string `json:"username"`
	Verb      string `json:"verb"`
	Resource  string `json:"resource"`
	Name      string `json:"name,omitempty"`
	Diff      string `json:"diff,omitempty"`
	Result    string `json:"result"`
	Message   string `json:"message,omitempty"`
	CreatedAt string `json:"created_at"`
}

// K8sOperator 已登记 Operator。
type K8sOperator struct {
	ID        string `json:"id"`
	ClusterID string `json:"cluster_id"`
	TenantID  string `json:"tenant_id,omitempty"`
	Engine    string `json:"engine"`
	Name      string `json:"name"`
	Version   string `json:"version,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Status    string `json:"status"`
	UpdatedAt string `json:"updated_at"`
	CreatedAt string `json:"created_at"`
}

// K8sDBCluster 平台登记的数据库集群。
type K8sDBCluster struct {
	ID         string `json:"id"`
	TenantID   string `json:"tenant_id,omitempty"`
	ClusterID  string `json:"cluster_id"`
	Namespace  string `json:"namespace"`
	Engine     string `json:"engine"`
	Operator   string `json:"operator"`
	CRKind     string `json:"cr_kind"`
	CRName     string `json:"cr_name"`
	SpecJSON   string `json:"spec_json,omitempty"`
	InstanceID string `json:"instance_id,omitempty"`
	Status     string `json:"status"`
	Endpoint   string `json:"endpoint,omitempty"`
	CreatedBy  string `json:"created_by,omitempty"`
	UpdatedBy  string `json:"updated_by,omitempty"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

func (d *DB) ensureK8sTables() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS tbl_k8s_clusters (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL DEFAULT '',
			env_id TEXT NOT NULL DEFAULT '',
			region_id TEXT NOT NULL DEFAULT '',
			access_mode TEXT NOT NULL DEFAULT 'direct',
			auth_type TEXT NOT NULL DEFAULT 'kubeconfig',
			api_server TEXT NOT NULL DEFAULT '',
			credential_enc TEXT NOT NULL DEFAULT '',
			ca_data TEXT NOT NULL DEFAULT '',
			agent_id TEXT NOT NULL DEFAULT '',
			impersonate_enabled INTEGER NOT NULL DEFAULT 0,
			version TEXT NOT NULL DEFAULT '',
			node_count INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'unknown',
			last_probe_at TEXT NOT NULL DEFAULT '',
			last_probe_error TEXT NOT NULL DEFAULT '',
			visibility TEXT NOT NULL DEFAULT 'global',
			remark TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL DEFAULT '',
			updated_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_k8s_clusters_tenant ON tbl_k8s_clusters(tenant_id)`,
		`CREATE TABLE IF NOT EXISTS tbl_k8s_namespace_bindings (
			id TEXT PRIMARY KEY,
			cluster_id TEXT NOT NULL DEFAULT '',
			namespace TEXT NOT NULL DEFAULT '',
			tenant_id TEXT NOT NULL DEFAULT '',
			visibility TEXT NOT NULL DEFAULT 'global',
			created_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_k8s_ns_bind ON tbl_k8s_namespace_bindings(cluster_id, namespace, tenant_id)`,
		`CREATE INDEX IF NOT EXISTS idx_k8s_ns_bind_tenant ON tbl_k8s_namespace_bindings(tenant_id)`,
		`CREATE TABLE IF NOT EXISTS tbl_k8s_audit_logs (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL DEFAULT '',
			cluster_id TEXT NOT NULL DEFAULT '',
			namespace TEXT NOT NULL DEFAULT '',
			username TEXT NOT NULL DEFAULT '',
			verb TEXT NOT NULL DEFAULT '',
			resource TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL DEFAULT '',
			diff TEXT NOT NULL DEFAULT '',
			result TEXT NOT NULL DEFAULT '',
			message TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_k8s_audit_cluster ON tbl_k8s_audit_logs(cluster_id, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS tbl_k8s_operators (
			id TEXT PRIMARY KEY,
			cluster_id TEXT NOT NULL DEFAULT '',
			tenant_id TEXT NOT NULL DEFAULT '',
			engine TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL DEFAULT '',
			version TEXT NOT NULL DEFAULT '',
			namespace TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_k8s_db_clusters (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL DEFAULT '',
			cluster_id TEXT NOT NULL DEFAULT '',
			namespace TEXT NOT NULL DEFAULT '',
			engine TEXT NOT NULL DEFAULT '',
			operator TEXT NOT NULL DEFAULT '',
			cr_kind TEXT NOT NULL DEFAULT '',
			cr_name TEXT NOT NULL DEFAULT '',
			spec_json TEXT NOT NULL DEFAULT '',
			instance_id TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT '',
			endpoint TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL DEFAULT '',
			updated_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_k8s_db_clusters_cluster ON tbl_k8s_db_clusters(cluster_id)`,
	}
	for _, s := range stmts {
		if _, err := d.db.Exec(s); err != nil {
			return err
		}
	}
	return nil
}

func scanK8sCluster(s interface{ Scan(...any) error }) (*K8sCluster, error) {
	var c K8sCluster
	var imp int
	err := s.Scan(
		&c.ID, &c.TenantID, &c.Name, &c.EnvID, &c.RegionID, &c.AccessMode, &c.AuthType,
		&c.APIServer, &c.CredentialEnc, &c.CAData, &c.AgentID, &imp,
		&c.Version, &c.NodeCount, &c.Status, &c.LastProbeAt, &c.LastProbeError,
		&c.Visibility, &c.Remark, &c.CreatedBy, &c.UpdatedBy, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	c.ImpersonateEnabled = imp != 0
	c.CredentialSet = strings.TrimSpace(c.CredentialEnc) != ""
	return &c, nil
}

const k8sClusterCols = `id, tenant_id, name, env_id, region_id, access_mode, auth_type,
	api_server, credential_enc, ca_data, agent_id, impersonate_enabled,
	version, node_count, status, last_probe_at, last_probe_error,
	visibility, remark, created_by, updated_by, created_at, updated_at`

func (d *DB) ListK8sClustersInTenant(tenantID string) ([]K8sCluster, error) {
	q := `SELECT ` + k8sClusterCols + ` FROM tbl_k8s_clusters WHERE 1=1`
	args := []any{}
	if tenantID != "" {
		q += ` AND (tenant_id=? OR id IN (SELECT cluster_id FROM tbl_k8s_namespace_bindings WHERE tenant_id=?))`
		args = append(args, tenantID, tenantID)
	}
	q += ` ORDER BY name`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []K8sCluster
	for rows.Next() {
		c, err := scanK8sCluster(rows)
		if err != nil {
			return nil, err
		}
		c.CredentialEnc = ""
		out = append(out, *c)
	}
	if out == nil {
		out = []K8sCluster{}
	}
	return out, rows.Err()
}

func (d *DB) GetK8sCluster(id string) (*K8sCluster, error) {
	row := d.QueryRow(`SELECT `+k8sClusterCols+` FROM tbl_k8s_clusters WHERE id=?`, id)
	c, err := scanK8sCluster(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return c, err
}

func (d *DB) GetK8sClusterInTenant(id, tenantID string) (*K8sCluster, error) {
	c, err := d.GetK8sCluster(id)
	if err != nil || c == nil {
		return c, err
	}
	if tenantID == "" || c.TenantID == tenantID {
		return c, nil
	}
	n, err := d.Count(`SELECT COUNT(1) FROM tbl_k8s_namespace_bindings WHERE cluster_id=? AND tenant_id=?`, id, tenantID)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, nil
	}
	return c, nil
}

func (d *DB) CreateK8sCluster(c *K8sCluster) error {
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	now := time.Now().UTC().Format(time.RFC3339)
	c.CreatedAt, c.UpdatedAt = now, now
	if c.AccessMode == "" {
		c.AccessMode = K8sAccessDirect
	}
	if c.AuthType == "" {
		c.AuthType = K8sAuthKubeconfig
	}
	if c.Status == "" {
		c.Status = K8sStatusUnknown
	}
	if c.Visibility == "" {
		c.Visibility = K8sVisibilityGlobal
	}
	imp := 0
	if c.ImpersonateEnabled {
		imp = 1
	}
	_, err := d.Exec(`INSERT INTO tbl_k8s_clusters(
		id, tenant_id, name, env_id, region_id, access_mode, auth_type,
		api_server, credential_enc, ca_data, agent_id, impersonate_enabled,
		version, node_count, status, last_probe_at, last_probe_error,
		visibility, remark, created_by, updated_by, created_at, updated_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.TenantID, c.Name, c.EnvID, c.RegionID, c.AccessMode, c.AuthType,
		c.APIServer, c.CredentialEnc, c.CAData, c.AgentID, imp,
		c.Version, c.NodeCount, c.Status, c.LastProbeAt, c.LastProbeError,
		c.Visibility, c.Remark, c.CreatedBy, c.UpdatedBy, c.CreatedAt, c.UpdatedAt,
	)
	return err
}

func (d *DB) UpdateK8sCluster(c *K8sCluster) error {
	c.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	imp := 0
	if c.ImpersonateEnabled {
		imp = 1
	}
	_, err := d.Exec(`UPDATE tbl_k8s_clusters SET
		name=?, env_id=?, region_id=?, access_mode=?, auth_type=?,
		api_server=?, credential_enc=?, ca_data=?, agent_id=?, impersonate_enabled=?,
		version=?, node_count=?, status=?, last_probe_at=?, last_probe_error=?,
		visibility=?, remark=?, updated_by=?, updated_at=?
		WHERE id=?`,
		c.Name, c.EnvID, c.RegionID, c.AccessMode, c.AuthType,
		c.APIServer, c.CredentialEnc, c.CAData, c.AgentID, imp,
		c.Version, c.NodeCount, c.Status, c.LastProbeAt, c.LastProbeError,
		c.Visibility, c.Remark, c.UpdatedBy, c.UpdatedAt, c.ID,
	)
	return err
}

func (d *DB) UpdateK8sClusterProbe(id, status, version string, nodeCount int, errMsg string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`UPDATE tbl_k8s_clusters SET status=?, version=?, node_count=?, last_probe_at=?, last_probe_error=?, updated_at=? WHERE id=?`,
		status, version, nodeCount, now, errMsg, now, id)
	return err
}

func (d *DB) DeleteK8sCluster(id string) error {
	_, _ = d.Exec(`DELETE FROM tbl_k8s_namespace_bindings WHERE cluster_id=?`, id)
	_, _ = d.Exec(`DELETE FROM tbl_k8s_operators WHERE cluster_id=?`, id)
	_, err := d.Exec(`DELETE FROM tbl_k8s_clusters WHERE id=?`, id)
	return err
}

func (d *DB) ListK8sNamespaceBindings(clusterID, tenantID string) ([]K8sNamespaceBinding, error) {
	q := `SELECT id, cluster_id, namespace, tenant_id, visibility, created_by, created_at FROM tbl_k8s_namespace_bindings WHERE 1=1`
	args := []any{}
	if clusterID != "" {
		q += ` AND cluster_id=?`
		args = append(args, clusterID)
	}
	if tenantID != "" {
		q += ` AND tenant_id=?`
		args = append(args, tenantID)
	}
	q += ` ORDER BY namespace`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []K8sNamespaceBinding
	for rows.Next() {
		var b K8sNamespaceBinding
		if err := rows.Scan(&b.ID, &b.ClusterID, &b.Namespace, &b.TenantID, &b.Visibility, &b.CreatedBy, &b.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	if out == nil {
		out = []K8sNamespaceBinding{}
	}
	return out, rows.Err()
}

func (d *DB) UpsertK8sNamespaceBinding(b *K8sNamespaceBinding) error {
	if b.ID == "" {
		b.ID = uuid.NewString()
	}
	if b.CreatedAt == "" {
		b.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if b.Visibility == "" {
		b.Visibility = K8sVisibilityGlobal
	}
	_, err := d.Exec(`INSERT INTO tbl_k8s_namespace_bindings(id, cluster_id, namespace, tenant_id, visibility, created_by, created_at)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(cluster_id, namespace, tenant_id) DO UPDATE SET visibility=EXCLUDED.visibility`,
		b.ID, b.ClusterID, b.Namespace, b.TenantID, b.Visibility, b.CreatedBy, b.CreatedAt)
	return err
}

func (d *DB) DeleteK8sNamespaceBinding(id string) error {
	_, err := d.Exec(`DELETE FROM tbl_k8s_namespace_bindings WHERE id=?`, id)
	return err
}

// AllowedNamespaces 当前租户在集群上可访问的命名空间；归属方返回 nil 表示全部。
func (d *DB) AllowedNamespaces(clusterID, tenantID string, ownerTenant string) ([]string, bool, error) {
	if tenantID == "" || tenantID == ownerTenant {
		return nil, true, nil // all
	}
	list, err := d.ListK8sNamespaceBindings(clusterID, tenantID)
	if err != nil {
		return nil, false, err
	}
	out := make([]string, 0, len(list))
	for _, b := range list {
		out = append(out, b.Namespace)
	}
	return out, false, nil
}

func (d *DB) CreateK8sAuditLog(a *K8sAuditLog) error {
	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	if a.CreatedAt == "" {
		a.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := d.Exec(`INSERT INTO tbl_k8s_audit_logs(
		id, tenant_id, cluster_id, namespace, username, verb, resource, name, diff, result, message, created_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.TenantID, a.ClusterID, a.Namespace, a.Username, a.Verb, a.Resource, a.Name, a.Diff, a.Result, a.Message, a.CreatedAt)
	return err
}

func (d *DB) ListK8sAuditLogs(clusterID, tenantID string, limit int) ([]K8sAuditLog, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT id, tenant_id, cluster_id, namespace, username, verb, resource, name, diff, result, message, created_at
		FROM tbl_k8s_audit_logs WHERE 1=1`
	args := []any{}
	if clusterID != "" {
		q += ` AND cluster_id=?`
		args = append(args, clusterID)
	}
	if tenantID != "" {
		q += ` AND tenant_id=?`
		args = append(args, tenantID)
	}
	q += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []K8sAuditLog
	for rows.Next() {
		var a K8sAuditLog
		if err := rows.Scan(&a.ID, &a.TenantID, &a.ClusterID, &a.Namespace, &a.Username, &a.Verb, &a.Resource, &a.Name, &a.Diff, &a.Result, &a.Message, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if out == nil {
		out = []K8sAuditLog{}
	}
	return out, rows.Err()
}

func (d *DB) UpsertK8sOperator(o *K8sOperator) error {
	if o.ID == "" {
		o.ID = uuid.NewString()
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if o.CreatedAt == "" {
		o.CreatedAt = now
	}
	o.UpdatedAt = now
	_, err := d.Exec(`INSERT INTO tbl_k8s_operators(id, cluster_id, tenant_id, engine, name, version, namespace, status, updated_at, created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET version=EXCLUDED.version, namespace=EXCLUDED.namespace, status=EXCLUDED.status, updated_at=EXCLUDED.updated_at`,
		o.ID, o.ClusterID, o.TenantID, o.Engine, o.Name, o.Version, o.Namespace, o.Status, o.UpdatedAt, o.CreatedAt)
	return err
}

func (d *DB) ListK8sOperators(clusterID string) ([]K8sOperator, error) {
	rows, err := d.Query(`SELECT id, cluster_id, tenant_id, engine, name, version, namespace, status, updated_at, created_at
		FROM tbl_k8s_operators WHERE cluster_id=? ORDER BY engine`, clusterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []K8sOperator
	for rows.Next() {
		var o K8sOperator
		if err := rows.Scan(&o.ID, &o.ClusterID, &o.TenantID, &o.Engine, &o.Name, &o.Version, &o.Namespace, &o.Status, &o.UpdatedAt, &o.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	if out == nil {
		out = []K8sOperator{}
	}
	return out, rows.Err()
}

func (d *DB) CreateK8sDBCluster(c *K8sDBCluster) error {
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	now := time.Now().UTC().Format(time.RFC3339)
	c.CreatedAt, c.UpdatedAt = now, now
	_, err := d.Exec(`INSERT INTO tbl_k8s_db_clusters(
		id, tenant_id, cluster_id, namespace, engine, operator, cr_kind, cr_name, spec_json,
		instance_id, status, endpoint, created_by, updated_by, created_at, updated_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.TenantID, c.ClusterID, c.Namespace, c.Engine, c.Operator, c.CRKind, c.CRName, c.SpecJSON,
		c.InstanceID, c.Status, c.Endpoint, c.CreatedBy, c.UpdatedBy, c.CreatedAt, c.UpdatedAt)
	return err
}

func (d *DB) UpdateK8sDBCluster(c *K8sDBCluster) error {
	c.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`UPDATE tbl_k8s_db_clusters SET
		status=?, endpoint=?, instance_id=?, spec_json=?, updated_by=?, updated_at=? WHERE id=?`,
		c.Status, c.Endpoint, c.InstanceID, c.SpecJSON, c.UpdatedBy, c.UpdatedAt, c.ID)
	return err
}

func (d *DB) GetK8sDBCluster(id string) (*K8sDBCluster, error) {
	row := d.QueryRow(`SELECT id, tenant_id, cluster_id, namespace, engine, operator, cr_kind, cr_name, spec_json,
		instance_id, status, endpoint, created_by, updated_by, created_at, updated_at FROM tbl_k8s_db_clusters WHERE id=?`, id)
	var c K8sDBCluster
	err := row.Scan(&c.ID, &c.TenantID, &c.ClusterID, &c.Namespace, &c.Engine, &c.Operator, &c.CRKind, &c.CRName, &c.SpecJSON,
		&c.InstanceID, &c.Status, &c.Endpoint, &c.CreatedBy, &c.UpdatedBy, &c.CreatedAt, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &c, err
}

func (d *DB) ListK8sDBClustersInTenant(tenantID, clusterID string) ([]K8sDBCluster, error) {
	q := `SELECT id, tenant_id, cluster_id, namespace, engine, operator, cr_kind, cr_name, spec_json,
		instance_id, status, endpoint, created_by, updated_by, created_at, updated_at FROM tbl_k8s_db_clusters WHERE 1=1`
	args := []any{}
	if tenantID != "" {
		q += ` AND tenant_id=?`
		args = append(args, tenantID)
	}
	if clusterID != "" {
		q += ` AND cluster_id=?`
		args = append(args, clusterID)
	}
	q += ` ORDER BY created_at DESC`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []K8sDBCluster
	for rows.Next() {
		var c K8sDBCluster
		if err := rows.Scan(&c.ID, &c.TenantID, &c.ClusterID, &c.Namespace, &c.Engine, &c.Operator, &c.CRKind, &c.CRName, &c.SpecJSON,
			&c.InstanceID, &c.Status, &c.Endpoint, &c.CreatedBy, &c.UpdatedBy, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if out == nil {
		out = []K8sDBCluster{}
	}
	return out, rows.Err()
}

func (d *DB) DeleteK8sDBCluster(id string) error {
	_, err := d.Exec(`DELETE FROM tbl_k8s_db_clusters WHERE id=?`, id)
	return err
}

func (d *DB) ListAllK8sClusters() ([]K8sCluster, error) {
	rows, err := d.Query(`SELECT ` + k8sClusterCols + ` FROM tbl_k8s_clusters`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []K8sCluster
	for rows.Next() {
		c, err := scanK8sCluster(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// EnvIsProd 根据环境 code/name 判断是否生产。
func (d *DB) EnvIsProd(envID string) bool {
	if envID == "" {
		return false
	}
	env, err := d.GetEnvironment(envID)
	if err != nil || env == nil {
		return strings.Contains(strings.ToLower(envID), "prod")
	}
	code := strings.ToLower(env.Code + " " + env.Name)
	return strings.Contains(code, "prod") || strings.Contains(code, "生产")
}

func K8sImpersonateUser(role string) (user string, groups []string) {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "admin", "administrator":
		return "keeplus:admin", []string{"keeplus:admin"}
	case "ops", "operator", "运维":
		return "keeplus:operator", []string{"keeplus:operator"}
	default:
		return "keeplus:viewer", []string{"keeplus:viewer"}
	}
}

func FormatK8sErr(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%v", err)
}
