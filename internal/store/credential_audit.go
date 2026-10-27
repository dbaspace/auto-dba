package store

import (
	"time"

	"github.com/google/uuid"
)

// CredentialAudit 明文密码查看审计。
type CredentialAudit struct {
	ID         string `json:"id"`
	UserID     string `json:"user_id"`
	Username   string `json:"username"`
	AccountID  string `json:"account_id,omitempty"`
	InstanceID string `json:"instance_id,omitempty"`
	Target     string `json:"target,omitempty"`
	Success    bool   `json:"success"`
	Message    string `json:"message,omitempty"`
	IP         string `json:"ip"`
	UserAgent  string `json:"user_agent,omitempty"`
	ClientType string `json:"client_type,omitempty"`
	CreatedAt  string `json:"created_at"`
}

func (d *DB) ensureCredentialAuditsTable() error {
	_, err := d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_credential_audits (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL DEFAULT '',
		username TEXT NOT NULL DEFAULT '',
		account_id TEXT NOT NULL DEFAULT '',
		instance_id TEXT NOT NULL DEFAULT '',
		target TEXT NOT NULL DEFAULT '',
		success INTEGER NOT NULL DEFAULT 0,
		message TEXT NOT NULL DEFAULT '',
		ip TEXT NOT NULL DEFAULT '',
		user_agent TEXT NOT NULL DEFAULT '',
		client_type TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL
	)`)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_credential_audits_created ON tbl_credential_audits(created_at DESC)`)
	return err
}

func (d *DB) CreateCredentialAudit(it *CredentialAudit) error {
	if it == nil {
		return nil
	}
	if it.ID == "" {
		it.ID = uuid.NewString()
	}
	if it.CreatedAt == "" {
		it.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if len(it.UserAgent) > 512 {
		it.UserAgent = it.UserAgent[:512]
	}
	success := 0
	if it.Success {
		success = 1
	}
	_, err := d.Exec(`INSERT INTO tbl_credential_audits(
		id, user_id, username, account_id, instance_id, target, success, message, ip, user_agent, client_type, created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		it.ID, it.UserID, it.Username, it.AccountID, it.InstanceID, it.Target, success, it.Message,
		it.IP, it.UserAgent, it.ClientType, it.CreatedAt)
	return err
}

// PurgeClientAccessAudits 按保留天数清理查询/接口/凭证审计。
func (d *DB) PurgeClientAccessAudits(retentionDays int) (queryN, apiN, credN int64, err error) {
	if retentionDays <= 0 {
		retentionDays = defaultAuditRetentionDays
	}
	cutoff := time.Now().UTC().Add(-time.Duration(retentionDays) * 24 * time.Hour).Format(time.RFC3339)
	res, e := d.Exec(`DELETE FROM tbl_query_audits WHERE created_at < ?`, cutoff)
	if e != nil {
		return 0, 0, 0, e
	}
	queryN, _ = res.RowsAffected()
	res, e = d.Exec(`DELETE FROM tbl_client_api_audits WHERE created_at < ?`, cutoff)
	if e != nil {
		return queryN, 0, 0, e
	}
	apiN, _ = res.RowsAffected()
	res, e = d.Exec(`DELETE FROM tbl_credential_audits WHERE created_at < ?`, cutoff)
	if e != nil {
		return queryN, apiN, 0, e
	}
	credN, _ = res.RowsAffected()
	return queryN, apiN, credN, nil
}
