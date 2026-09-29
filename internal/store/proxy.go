package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ProxyService 接入层代理服务（HAProxy / Nginx / LVS）。
type ProxyService struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Kind         string `json:"kind"` // haproxy | nginx | lvs
	EnvID        string `json:"env_id,omitempty"`
	AgentID      string `json:"agent_id,omitempty"`
	AgentBaseURL string `json:"agent_base_url,omitempty"`
	AgentUser    string `json:"agent_user,omitempty"`
	PasswordEnc  string `json:"-"`
	PasswordSet  bool   `json:"password_set"`
	ConfPath     string `json:"conf_path"`
	ConfContent  string `json:"conf_content,omitempty"`
	SystemdUnit  string `json:"systemd_unit,omitempty"`
	VIP          string `json:"vip,omitempty"`
	ListenPort   int    `json:"listen_port,omitempty"`
	BackendsJSON string `json:"backends_json,omitempty"`
	OptionsJSON  string `json:"options_json,omitempty"`
	Remark       string `json:"remark,omitempty"`
	Status       string `json:"status"` // draft | active | error
	PushedAt     string `json:"pushed_at,omitempty"`
	ConfVersion  int    `json:"conf_version,omitempty"` // 当前最新配置版本号（派生）
	CreatedBy    string `json:"created_by,omitempty"`
	UpdatedBy    string `json:"updated_by,omitempty"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

func (d *DB) ensureProxyTables() error {
	_, err := d.Exec(`CREATE TABLE IF NOT EXISTS tbl_proxy_services (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		kind TEXT NOT NULL,
		env_id TEXT NOT NULL DEFAULT '',
		agent_id TEXT NOT NULL DEFAULT '',
		agent_base_url TEXT NOT NULL DEFAULT '',
		agent_user TEXT NOT NULL DEFAULT '',
		password_enc TEXT NOT NULL DEFAULT '',
		conf_path TEXT NOT NULL DEFAULT '',
		conf_content TEXT NOT NULL DEFAULT '',
		systemd_unit TEXT NOT NULL DEFAULT '',
		vip TEXT NOT NULL DEFAULT '',
		listen_port INTEGER NOT NULL DEFAULT 0,
		backends_json TEXT NOT NULL DEFAULT '',
		options_json TEXT NOT NULL DEFAULT '',
		remark TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'draft',
		pushed_at TEXT NOT NULL DEFAULT '',
		created_by TEXT NOT NULL DEFAULT '',
		updated_by TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`)
	return err
}

func ValidProxyKind(k string) bool {
	switch strings.ToLower(strings.TrimSpace(k)) {
	case "haproxy", "nginx", "lvs":
		return true
	default:
		return false
	}
}

func (d *DB) ListProxyServices(kind, envID string) ([]ProxyService, error) {
	if err := d.ensureProxyTables(); err != nil {
		return nil, err
	}
	q := `SELECT id, name, kind, env_id, agent_id, agent_base_url, agent_user, password_enc, conf_path, conf_content,
		systemd_unit, vip, listen_port, backends_json, options_json, remark, status, pushed_at, created_by, updated_by, created_at, updated_at
		FROM tbl_proxy_services WHERE 1=1`
	var args []any
	if k := strings.ToLower(strings.TrimSpace(kind)); k != "" {
		q += ` AND kind=?`
		args = append(args, k)
	}
	if e := strings.TrimSpace(envID); e != "" {
		q += ` AND env_id=?`
		args = append(args, e)
	}
	q += ` ORDER BY updated_at DESC`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProxyService
	for rows.Next() {
		var s ProxyService
		if err := rows.Scan(&s.ID, &s.Name, &s.Kind, &s.EnvID, &s.AgentID, &s.AgentBaseURL, &s.AgentUser, &s.PasswordEnc,
			&s.ConfPath, &s.ConfContent, &s.SystemdUnit, &s.VIP, &s.ListenPort, &s.BackendsJSON, &s.OptionsJSON,
			&s.Remark, &s.Status, &s.PushedAt, &s.CreatedBy, &s.UpdatedBy, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		s.PasswordSet = strings.TrimSpace(s.PasswordEnc) != ""
		s.PasswordEnc = ""
		out = append(out, s)
	}
	return out, nil
}

func (d *DB) GetProxyService(id string) (*ProxyService, error) {
	if err := d.ensureProxyTables(); err != nil {
		return nil, err
	}
	s := &ProxyService{}
	err := d.QueryRow(`SELECT id, name, kind, env_id, agent_id, agent_base_url, agent_user, password_enc, conf_path, conf_content,
		systemd_unit, vip, listen_port, backends_json, options_json, remark, status, pushed_at, created_by, updated_by, created_at, updated_at
		FROM tbl_proxy_services WHERE id=?`, id).Scan(
		&s.ID, &s.Name, &s.Kind, &s.EnvID, &s.AgentID, &s.AgentBaseURL, &s.AgentUser, &s.PasswordEnc,
		&s.ConfPath, &s.ConfContent, &s.SystemdUnit, &s.VIP, &s.ListenPort, &s.BackendsJSON, &s.OptionsJSON,
		&s.Remark, &s.Status, &s.PushedAt, &s.CreatedBy, &s.UpdatedBy, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.PasswordSet = strings.TrimSpace(s.PasswordEnc) != ""
	return s, nil
}

func (d *DB) UpsertProxyService(s *ProxyService) error {
	if err := d.ensureProxyTables(); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if s.ID == "" {
		s.ID = uuid.NewString()
		s.CreatedAt = now
	}
	if s.Status == "" {
		s.Status = "draft"
	}
	s.Kind = strings.ToLower(strings.TrimSpace(s.Kind))
	s.UpdatedAt = now
	_, err := d.Exec(`INSERT INTO tbl_proxy_services(
		id, name, kind, env_id, agent_id, agent_base_url, agent_user, password_enc, conf_path, conf_content,
		systemd_unit, vip, listen_port, backends_json, options_json, remark, status, pushed_at, created_by, updated_by, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
		name=excluded.name, kind=excluded.kind, env_id=excluded.env_id, agent_id=excluded.agent_id,
		agent_base_url=excluded.agent_base_url, agent_user=excluded.agent_user,
		password_enc=CASE WHEN excluded.password_enc='' THEN tbl_proxy_services.password_enc ELSE excluded.password_enc END,
		conf_path=excluded.conf_path, conf_content=excluded.conf_content, systemd_unit=excluded.systemd_unit,
		vip=excluded.vip, listen_port=excluded.listen_port, backends_json=excluded.backends_json,
		options_json=excluded.options_json, remark=excluded.remark, status=excluded.status,
		pushed_at=excluded.pushed_at, updated_by=excluded.updated_by, updated_at=excluded.updated_at`,
		s.ID, s.Name, s.Kind, s.EnvID, s.AgentID, s.AgentBaseURL, s.AgentUser, s.PasswordEnc, s.ConfPath, s.ConfContent,
		s.SystemdUnit, s.VIP, s.ListenPort, s.BackendsJSON, s.OptionsJSON, s.Remark, s.Status, s.PushedAt,
		s.CreatedBy, s.UpdatedBy, s.CreatedAt, s.UpdatedAt)
	return err
}

func (d *DB) DeleteProxyService(id string) error {
	if err := d.ensureProxyTables(); err != nil {
		return err
	}
	_ = d.DeleteProxyConfVersions(id)
	_, err := d.Exec(`DELETE FROM tbl_proxy_services WHERE id=?`, id)
	return err
}

func (d *DB) MarkProxyPushed(id, by string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`UPDATE tbl_proxy_services SET status='active', pushed_at=?, updated_by=?, updated_at=? WHERE id=?`,
		now, by, now, id)
	return err
}

// FindProxyByVIP 按 VIP（忽略 /cidr）查找已占用记录；excludeID 用于更新时排除自身。
func (d *DB) FindProxyByVIP(vip, excludeID string) (*ProxyService, error) {
	if err := d.ensureProxyTables(); err != nil {
		return nil, err
	}
	host := strings.TrimSpace(vip)
	if i := strings.IndexByte(host, '/'); i >= 0 {
		host = host[:i]
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, nil
	}
	list, err := d.ListProxyServices("", "")
	if err != nil {
		return nil, err
	}
	for i := range list {
		s := &list[i]
		if excludeID != "" && s.ID == excludeID {
			continue
		}
		other := strings.TrimSpace(s.VIP)
		if i := strings.IndexByte(other, '/'); i >= 0 {
			other = other[:i]
		}
		other = strings.TrimSpace(other)
		if other != "" && strings.EqualFold(other, host) {
			return s, nil
		}
	}
	return nil, nil
}

// FindHAClusterByVIP 检查 HA 集群是否已占用该 VIP。
func (d *DB) FindHAClusterByVIP(vip string) (*HACluster, error) {
	host := strings.TrimSpace(vip)
	if i := strings.IndexByte(host, '/'); i >= 0 {
		host = host[:i]
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, nil
	}
	list, err := d.ListHAClusters("", "", "")
	if err != nil {
		return nil, err
	}
	for i := range list {
		c := &list[i]
		other := strings.TrimSpace(c.VIP)
		if j := strings.IndexByte(other, '/'); j >= 0 {
			other = other[:j]
		}
		other = strings.TrimSpace(other)
		if other != "" && strings.EqualFold(other, host) {
			return c, nil
		}
	}
	return nil, nil
}
