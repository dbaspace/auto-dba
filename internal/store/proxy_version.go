package store

import (
	"database/sql"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ProxyConfVersion 代理配置版本快照。
type ProxyConfVersion struct {
	ID           string `json:"id"`
	ServiceID    string `json:"service_id"`
	VersionNo    int    `json:"version_no"`
	ConfPath     string `json:"conf_path,omitempty"`
	ConfContent  string `json:"conf_content,omitempty"`
	BackendsJSON string `json:"backends_json,omitempty"`
	VIP          string `json:"vip,omitempty"`
	ListenPort   int    `json:"listen_port,omitempty"`
	ChangeSource string `json:"change_source"` // create|save|push|restore|regenerate
	Remark       string `json:"remark,omitempty"`
	Operator     string `json:"operator,omitempty"`
	OperatorIP   string `json:"operator_ip,omitempty"`
	CreatedAt    string `json:"created_at"`
}

const proxyConfVersionCols = `id, service_id, version_no, conf_path, conf_content, backends_json, vip, listen_port, change_source, remark, operator, operator_ip, created_at`

const ProxyConfVersionKeepN = 50

func (d *DB) ensureProxyVersionTable() error {
	if err := d.ensureProxyTables(); err != nil {
		return err
	}
	_, err := d.Exec(`CREATE TABLE IF NOT EXISTS tbl_proxy_conf_versions (
		id TEXT PRIMARY KEY,
		service_id TEXT NOT NULL,
		version_no INTEGER NOT NULL,
		conf_path TEXT NOT NULL DEFAULT '',
		conf_content TEXT NOT NULL DEFAULT '',
		backends_json TEXT NOT NULL DEFAULT '',
		vip TEXT NOT NULL DEFAULT '',
		listen_port INTEGER NOT NULL DEFAULT 0,
		change_source TEXT NOT NULL DEFAULT '',
		remark TEXT NOT NULL DEFAULT '',
		operator TEXT NOT NULL DEFAULT '',
		operator_ip TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		UNIQUE (service_id, version_no)
	)`)
	if err != nil {
		return err
	}
	_, err = d.Exec(`CREATE INDEX IF NOT EXISTS idx_proxy_conf_ver_svc ON tbl_proxy_conf_versions(service_id, version_no DESC)`)
	return err
}

func scanProxyConfVersion(sc interface {
	Scan(dest ...any) error
}) (*ProxyConfVersion, error) {
	it := &ProxyConfVersion{}
	if err := sc.Scan(
		&it.ID, &it.ServiceID, &it.VersionNo, &it.ConfPath, &it.ConfContent, &it.BackendsJSON,
		&it.VIP, &it.ListenPort, &it.ChangeSource, &it.Remark, &it.Operator, &it.OperatorIP, &it.CreatedAt,
	); err != nil {
		return nil, err
	}
	return it, nil
}

func (d *DB) NextProxyConfVersionNo(serviceID string) (int, error) {
	var n sql.NullInt64
	err := d.QueryRow(`SELECT MAX(version_no) FROM tbl_proxy_conf_versions WHERE service_id=?`, serviceID).Scan(&n)
	if err != nil {
		return 0, err
	}
	if !n.Valid {
		return 1, nil
	}
	return int(n.Int64) + 1, nil
}

func (d *DB) InsertProxyConfVersion(it *ProxyConfVersion) error {
	if err := d.ensureProxyVersionTable(); err != nil {
		return err
	}
	if it.ID == "" {
		it.ID = uuid.NewString()
	}
	if it.CreatedAt == "" {
		it.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if it.VersionNo <= 0 {
		n, err := d.NextProxyConfVersionNo(it.ServiceID)
		if err != nil {
			return err
		}
		it.VersionNo = n
	}
	_, err := d.Exec(
		`INSERT INTO tbl_proxy_conf_versions(`+proxyConfVersionCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		it.ID, it.ServiceID, it.VersionNo, it.ConfPath, it.ConfContent, it.BackendsJSON,
		it.VIP, it.ListenPort, it.ChangeSource, it.Remark, it.Operator, it.OperatorIP, it.CreatedAt,
	)
	return err
}

// SaveProxyConfVersionIfChanged 配置内容变化时写入版本；未变化返回 (nil, nil)。
func (d *DB) SaveProxyConfVersionIfChanged(s *ProxyService, source, operator, operatorIP, remark string) (*ProxyConfVersion, error) {
	if err := d.ensureProxyVersionTable(); err != nil {
		return nil, err
	}
	latest, err := d.GetLatestProxyConfVersion(s.ID)
	if err != nil {
		return nil, err
	}
	if latest != nil &&
		latest.ConfContent == s.ConfContent &&
		latest.ConfPath == s.ConfPath &&
		latest.BackendsJSON == s.BackendsJSON &&
		latest.VIP == s.VIP &&
		latest.ListenPort == s.ListenPort {
		return nil, nil
	}
	it := &ProxyConfVersion{
		ServiceID:    s.ID,
		ConfPath:     s.ConfPath,
		ConfContent:  s.ConfContent,
		BackendsJSON: s.BackendsJSON,
		VIP:          s.VIP,
		ListenPort:   s.ListenPort,
		ChangeSource: strings.TrimSpace(source),
		Remark:       strings.TrimSpace(remark),
		Operator:     operator,
		OperatorIP:   operatorIP,
	}
	if it.ChangeSource == "" {
		it.ChangeSource = "save"
	}
	if err := d.InsertProxyConfVersion(it); err != nil {
		return nil, err
	}
	_ = d.PruneProxyConfVersions(s.ID, ProxyConfVersionKeepN)
	return it, nil
}

func (d *DB) GetLatestProxyConfVersion(serviceID string) (*ProxyConfVersion, error) {
	if err := d.ensureProxyVersionTable(); err != nil {
		return nil, err
	}
	it, err := scanProxyConfVersion(d.QueryRow(
		`SELECT `+proxyConfVersionCols+` FROM tbl_proxy_conf_versions WHERE service_id=? ORDER BY version_no DESC LIMIT 1`,
		serviceID,
	))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return it, err
}

func (d *DB) ListProxyConfVersions(serviceID string, limit, offset int) ([]ProxyConfVersion, int, error) {
	if err := d.ensureProxyVersionTable(); err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	var total int
	if err := d.QueryRow(`SELECT COUNT(1) FROM tbl_proxy_conf_versions WHERE service_id=?`, serviceID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := d.Query(
		`SELECT `+proxyConfVersionCols+` FROM tbl_proxy_conf_versions WHERE service_id=? ORDER BY version_no DESC LIMIT ? OFFSET ?`,
		serviceID, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []ProxyConfVersion{}
	for rows.Next() {
		it, err := scanProxyConfVersion(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *it)
	}
	return out, total, rows.Err()
}

func (d *DB) GetProxyConfVersion(serviceID, versionID string) (*ProxyConfVersion, error) {
	if err := d.ensureProxyVersionTable(); err != nil {
		return nil, err
	}
	versionID = strings.TrimSpace(versionID)
	it, err := scanProxyConfVersion(d.QueryRow(
		`SELECT `+proxyConfVersionCols+` FROM tbl_proxy_conf_versions WHERE service_id=? AND id=?`,
		serviceID, versionID,
	))
	if err == sql.ErrNoRows {
		it, err = scanProxyConfVersion(d.QueryRow(
			`SELECT `+proxyConfVersionCols+` FROM tbl_proxy_conf_versions WHERE service_id=? AND CAST(version_no AS TEXT)=?`,
			serviceID, versionID,
		))
	}
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return it, err
}

func (d *DB) PruneProxyConfVersions(serviceID string, keep int) error {
	if keep <= 0 {
		keep = ProxyConfVersionKeepN
	}
	rows, err := d.Query(
		`SELECT id FROM tbl_proxy_conf_versions WHERE service_id=? ORDER BY version_no DESC`,
		serviceID,
	)
	if err != nil {
		return err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(ids) <= keep {
		return nil
	}
	for _, id := range ids[keep:] {
		if _, err := d.Exec(`DELETE FROM tbl_proxy_conf_versions WHERE id=?`, id); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) DeleteProxyConfVersions(serviceID string) error {
	if err := d.ensureProxyVersionTable(); err != nil {
		return err
	}
	_, err := d.Exec(`DELETE FROM tbl_proxy_conf_versions WHERE service_id=?`, serviceID)
	return err
}

// AttachProxyConfVersion 填充服务的当前配置版本号。
func (d *DB) AttachProxyConfVersion(s *ProxyService) {
	if s == nil || s.ID == "" {
		return
	}
	latest, err := d.GetLatestProxyConfVersion(s.ID)
	if err == nil && latest != nil {
		s.ConfVersion = latest.VersionNo
	}
}

// ApplyProxyConfVersion 将历史版本内容写回服务（不写入新版本记录）。
func ApplyProxyConfVersion(s *ProxyService, ver *ProxyConfVersion) {
	if s == nil || ver == nil {
		return
	}
	s.ConfPath = ver.ConfPath
	s.ConfContent = ver.ConfContent
	s.BackendsJSON = ver.BackendsJSON
	s.VIP = ver.VIP
	s.ListenPort = ver.ListenPort
}
