package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// HAMHAConf 集群级 MHA app.cnf 草稿（平台侧保存；下发到 Manager Agent）。
type HAMHAConf struct {
	ClusterID   string `json:"cluster_id"`
	ConfPath    string `json:"conf_path"`
	Content     string `json:"content"`
	OptionsJSON string `json:"options_json,omitempty"`
	UpdatedBy   string `json:"updated_by,omitempty"`
	UpdatedAt   string `json:"updated_at"`
}

func (d *DB) ensureHAMHAConfTable() error {
	_, err := d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_ha_mha_confs (
		cluster_id TEXT PRIMARY KEY,
		conf_path TEXT NOT NULL DEFAULT '/etc/mha/app.cnf',
		content TEXT NOT NULL DEFAULT '',
		options_json TEXT NOT NULL DEFAULT '',
		updated_by TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL
	)`)
	return err
}

func (d *DB) GetHAMHAConf(clusterID string) (*HAMHAConf, error) {
	if err := d.ensureHAMHAConfTable(); err != nil {
		return nil, err
	}
	it := &HAMHAConf{}
	err := d.QueryRow(
		`SELECT cluster_id, conf_path, content, COALESCE(options_json,''), COALESCE(updated_by,''), updated_at
		 FROM tbl_ha_mha_confs WHERE cluster_id=?`, clusterID,
	).Scan(&it.ClusterID, &it.ConfPath, &it.Content, &it.OptionsJSON, &it.UpdatedBy, &it.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return it, nil
}

func (d *DB) UpsertHAMHAConf(it *HAMHAConf) error {
	if err := d.ensureHAMHAConfTable(); err != nil {
		return err
	}
	if it.ClusterID == "" {
		return errors.New("cluster_id required")
	}
	if it.ConfPath == "" {
		it.ConfPath = "/etc/mha/app.cnf"
	}
	it.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(
		`INSERT INTO tbl_ha_mha_confs(cluster_id, conf_path, content, options_json, updated_by, updated_at)
		 VALUES(?,?,?,?,?,?)
		 ON CONFLICT(cluster_id) DO UPDATE SET
		   conf_path=EXCLUDED.conf_path, content=EXCLUDED.content, options_json=EXCLUDED.options_json,
		   updated_by=EXCLUDED.updated_by, updated_at=EXCLUDED.updated_at`,
		it.ClusterID, it.ConfPath, it.Content, it.OptionsJSON, it.UpdatedBy, it.UpdatedAt,
	)
	return err
}

func (d *DB) FindInstanceByHostPort(dbType, host string, port int) (*Instance, error) {
	return d.FindInstanceByHostPortInTenant("", dbType, host, port)
}

// FindInstanceByHostPortInTenant 按租户 + host:port + 引擎查找；tenantID 空则不限租户（仅兼容旧调用）。
func (d *DB) FindInstanceByHostPortInTenant(tenantID, dbType, host string, port int) (*Instance, error) {
	host = strings.TrimSpace(host)
	if host == "" || port <= 0 {
		return nil, nil
	}
	q := `SELECT ` + instanceCols + ` FROM tbl_instances
		 WHERE LOWER(TRIM(host))=LOWER(TRIM(?)) AND port=? AND LOWER(TRIM(db_type))=LOWER(TRIM(?))`
	args := []any{host, port, dbType}
	if tid := strings.TrimSpace(tenantID); tid != "" {
		q += ` AND COALESCE(tenant_id,'')=?`
		args = append(args, tid)
	}
	q += ` LIMIT 1`
	row := d.QueryRow(q, args...)
	it, err := scanInstance(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return it, err
}

// EnsureInstanceFromHost 若不存在则创建引擎实例登记（默认租户）。
func (d *DB) EnsureInstanceFromHost(dbType, name, host string, port int, envID string) (*Instance, error) {
	return d.EnsureInstanceFromHostInTenant("", dbType, name, host, port, envID)
}

// EnsureInstanceFromHostInTenant 在指定租户内复用或登记实例；tenantID 空则落默认租户，且只匹配该租户。
func (d *DB) EnsureInstanceFromHostInTenant(tenantID, dbType, name, host string, port int, envID string) (*Instance, error) {
	if port <= 0 {
		port = 3306
	}
	tid := strings.TrimSpace(tenantID)
	if tid == "" {
		tid = DefaultTenantID
	}
	exist, err := d.FindInstanceByHostPortInTenant(tid, dbType, host, port)
	if err != nil {
		return nil, err
	}
	if exist != nil {
		return exist, nil
	}
	if name == "" {
		name = host
	}
	it := &Instance{
		ID:          uuid.NewString(),
		TenantID:    tid,
		Name:        name,
		Host:        host,
		Port:        port,
		DBType:      dbType,
		EnvID:       envID,
		Approvers:   []string{},
		Tags:        []string{"ha-from-dbdeploy"},
		Description: "由高可用维护自集群部署节点自动登记",
	}
	if err := d.CreateInstance(it); err != nil {
		return nil, err
	}
	return it, nil
}

// SetDbDeployClusterHALink 回写 db-deploy 集群的 ha_cluster_id。
func (d *DB) SetDbDeployClusterHALink(clusterID, haClusterID, controllerID string) error {
	_, err := d.Exec(
		`UPDATE tbl_db_deploy_clusters SET ha_cluster_id=?, controller_id=COALESCE(NULLIF(?,''), controller_id), updated_at=? WHERE id=?`,
		haClusterID, controllerID, time.Now().UTC().Format(time.RFC3339), clusterID,
	)
	return err
}

// MarkDbDeployHAReady 将 ha_pending 升为 ready（HA 接入成功后）。
func (d *DB) MarkDbDeployHAReady(clusterID string) error {
	_, err := d.Exec(
		`UPDATE tbl_db_deploy_clusters SET status=?, updated_at=? WHERE id=? AND status=?`,
		DbDeployStatusReady, time.Now().UTC().Format(time.RFC3339), clusterID, DbDeployStatusHaPending,
	)
	return err
}
