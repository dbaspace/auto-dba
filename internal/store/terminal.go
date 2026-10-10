package store

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	TermStatusCreated  = "created"
	TermStatusRunning  = "running"
	TermStatusFinished = "finished"
	TermStatusFailed   = "failed"
	TermStatusExpired  = "expired"
)

type TerminalSession struct {
	ID          string
	TenantID    string
	ActionName  string
	ExecContent string
	Status      string
	CreatedBy   string
	InstanceID  string
	Host        string
	ExpiresAt   string
	CreatedAt   string
	UpdatedAt   string
}

func (d *DB) ensureTerminalSessionsTable() error {
	_, err := d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_terminal_sessions (
		id TEXT PRIMARY KEY,
		action_name TEXT NOT NULL,
		exec_content TEXT NOT NULL,
		status TEXT NOT NULL,
		created_by TEXT NOT NULL DEFAULT '',
		instance_id TEXT NOT NULL DEFAULT '',
		host TEXT NOT NULL DEFAULT '',
		expires_at TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`)
	return err
}

func (d *DB) CreateTerminalSession(s *TerminalSession) error {
	if s == nil {
		return errors.New("nil session")
	}
	if s.ID == "" {
		s.ID = uuid.NewString()
	}
	now := time.Now().UTC().Format(time.RFC3339)
	s.CreatedAt, s.UpdatedAt = now, now
	if s.Status == "" {
		s.Status = TermStatusCreated
	}
	_, err := d.Exec(
		`INSERT INTO tbl_terminal_sessions(id, action_name, exec_content, status, created_by, instance_id, host, expires_at, created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?)`,
		s.ID, s.ActionName, s.ExecContent, s.Status, s.CreatedBy, s.InstanceID, s.Host, s.ExpiresAt, s.CreatedAt, s.UpdatedAt,
	)
	if err != nil {
		return err
	}
	if s.TenantID == "" {
		s.TenantID = d.inheritTenantFromInstance(s.InstanceID)
	}
	if s.TenantID != "" {
		return d.AssignTenantID("tbl_terminal_sessions", s.ID, s.TenantID)
	}
	return nil
}

func (d *DB) GetTerminalSession(id string) (*TerminalSession, error) {
	s := &TerminalSession{}
	err := d.QueryRow(
		`SELECT id, action_name, exec_content, status, created_by, instance_id, host, expires_at, created_at, updated_at
		 FROM tbl_terminal_sessions WHERE id=?`, id,
	).Scan(&s.ID, &s.ActionName, &s.ExecContent, &s.Status, &s.CreatedBy, &s.InstanceID, &s.Host, &s.ExpiresAt, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (d *DB) UpdateTerminalSessionStatus(id, status string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`UPDATE tbl_terminal_sessions SET status=?, updated_at=? WHERE id=?`, status, now, id)
	return err
}
