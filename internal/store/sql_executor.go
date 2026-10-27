package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// SQLExecutor 工单 SQL 执行器。EnvCodes 是环境管理里的 code，一台可绑多个。
type SQLExecutor struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	BaseURL     string   `json:"base_url"`
	Username    string   `json:"username"`
	PasswordEnc string   `json:"-"`
	EnvCodes    []string `json:"env_codes"`
	Enabled     bool     `json:"enabled"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

const ArgTicketLocalIDC = "ticket_local_idc"

func (d *DB) ensureSQLExecutorTable() error {
	_, err := d.Exec(`CREATE TABLE IF NOT EXISTS tbl_sql_executors (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		base_url TEXT NOT NULL,
		username TEXT NOT NULL,
		password TEXT NOT NULL,
		env_codes TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 1,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`)
	return err
}

func (d *DB) ListSQLExecutors() ([]SQLExecutor, error) {
	rows, err := d.Query(`SELECT id, name, base_url, username, password, env_codes, enabled, created_at, updated_at
		FROM tbl_sql_executors ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SQLExecutor
	for rows.Next() {
		e, err := scanSQLExecutor(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (d *DB) GetSQLExecutor(id string) (*SQLExecutor, error) {
	row := d.QueryRow(`SELECT id, name, base_url, username, password, env_codes, enabled, created_at, updated_at
		FROM tbl_sql_executors WHERE id=?`, id)
	e, err := scanSQLExecutor(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (d *DB) SaveSQLExecutor(e *SQLExecutor) error {
	if e == nil {
		return errors.New("执行器为空")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if strings.TrimSpace(e.ID) == "" {
		e.ID = uuid.NewString()
		e.CreatedAt = now
	}
	if e.CreatedAt == "" {
		e.CreatedAt = now
	}
	e.UpdatedAt = now
	raw, err := json.Marshal(e.EnvCodes)
	if err != nil {
		return err
	}
	enabled := 0
	if e.Enabled {
		enabled = 1
	}
	_, err = d.Exec(`INSERT INTO tbl_sql_executors(id, name, base_url, username, password, env_codes, enabled, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name, base_url=excluded.base_url, username=excluded.username,
			password=excluded.password, env_codes=excluded.env_codes, enabled=excluded.enabled, updated_at=excluded.updated_at`,
		e.ID, e.Name, e.BaseURL, e.Username, e.PasswordEnc, string(raw), enabled, e.CreatedAt, e.UpdatedAt)
	return err
}

func (d *DB) FindSQLExecutorByName(name string) (*SQLExecutor, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil
	}
	row := d.QueryRow(`SELECT id, name, base_url, username, password, env_codes, enabled, created_at, updated_at
		FROM tbl_sql_executors WHERE name=?`, name)
	e, err := scanSQLExecutor(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (d *DB) ensureSQLExecutorDeployColumns() error {
	for _, ddl := range []string{
		`ALTER TABLE tbl_backup_agent_deploys ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'platform'`,
		`ALTER TABLE tbl_backup_agent_deploys ADD COLUMN IF NOT EXISTS payload TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := d.Exec(ddl); err != nil {
			msg := strings.ToLower(err.Error())
			if strings.Contains(msg, "duplicate") || strings.Contains(msg, "already exists") {
				continue
			}
			return err
		}
	}
	return nil
}

func (d *DB) SetBackupAgentDeployKind(id, kind, payload string) error {
	_, err := d.Exec(`UPDATE tbl_backup_agent_deploys SET kind=?, payload=? WHERE id=?`, kind, payload, id)
	return err
}

func (d *DB) ListSQLExecutorDeploys(hostInstanceID string, limit int) ([]BackupAgentDeploy, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT id, host_instance_id, agent_id, status, listen_port, remote_dir, base_url, log_text, created_by, created_at, updated_at, finished_at, COALESCE(payload,'')
		FROM tbl_backup_agent_deploys WHERE kind='sql'`
	var args []any
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
			&it.CreatedBy, &it.CreatedAt, &it.UpdatedAt, &it.FinishedAt, &it.Payload); err != nil {
			return nil, err
		}
		it.Kind = "sql"
		out = append(out, it)
	}
	if out == nil {
		out = []BackupAgentDeploy{}
	}
	return out, rows.Err()
}

// FindLatestSuccessSQLExecutorDeploy 返回该执行器最近一次下发成功记录。
func (d *DB) FindLatestSuccessSQLExecutorDeploy(agentID string) (*BackupAgentDeploy, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, nil
	}
	it := &BackupAgentDeploy{}
	err := d.QueryRow(
		`SELECT id, host_instance_id, agent_id, status, listen_port, remote_dir, base_url, log_text, created_by, created_at, updated_at, finished_at, COALESCE(payload,'')
		 FROM tbl_backup_agent_deploys
		 WHERE kind='sql' AND status=? AND agent_id=?
		 ORDER BY created_at DESC LIMIT 1`,
		BackupDeploySuccess, agentID,
	).Scan(
		&it.ID, &it.HostInstanceID, &it.AgentID, &it.Status, &it.ListenPort, &it.RemoteDir, &it.BaseURL, &it.LogText,
		&it.CreatedBy, &it.CreatedAt, &it.UpdatedAt, &it.FinishedAt, &it.Payload,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	it.Kind = "sql"
	return it, nil
}

// MapLatestSQLExecutorHostByAgent 每个执行器对应最近一次成功下发的主机。
func (d *DB) MapLatestSQLExecutorHostByAgent() (map[string]string, error) {
	rows, err := d.Query(
		`SELECT agent_id, host_instance_id FROM tbl_backup_agent_deploys
		 WHERE kind='sql' AND status=? AND agent_id<>''
		 ORDER BY created_at DESC`,
		BackupDeploySuccess,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var agentID, hostID string
		if err := rows.Scan(&agentID, &hostID); err != nil {
			return nil, err
		}
		agentID = strings.TrimSpace(agentID)
		if agentID == "" {
			continue
		}
		if _, ok := out[agentID]; ok {
			continue
		}
		out[agentID] = strings.TrimSpace(hostID)
	}
	return out, rows.Err()
}

func (d *DB) DeleteSQLExecutor(id string) error {
	_, err := d.Exec(`DELETE FROM tbl_sql_executors WHERE id=?`, id)
	return err
}

type sqlExecScanner interface {
	Scan(dest ...any) error
}

func scanSQLExecutor(s sqlExecScanner) (SQLExecutor, error) {
	var e SQLExecutor
	var raw string
	var enabled int
	err := s.Scan(&e.ID, &e.Name, &e.BaseURL, &e.Username, &e.PasswordEnc, &raw, &enabled, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return e, err
	}
	e.Enabled = enabled != 0
	_ = json.Unmarshal([]byte(raw), &e.EnvCodes)
	if e.EnvCodes == nil {
		e.EnvCodes = []string{}
	}
	return e, nil
}
