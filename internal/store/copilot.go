package store

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/google/uuid"
)

// CopilotSession 多轮对话会话（Cursor 复用 agent_id）。
type CopilotSession struct {
	ID            string
	TenantID      string
	Provider      string
	CursorAgentID string
	Model         string
	Title         string
	CreatedAt     string
	UpdatedAt     string
}

// CopilotSessionListItem 历史列表项。
type CopilotSessionListItem struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Provider  string `json:"provider,omitempty"`
	Model     string `json:"model,omitempty"`
	TurnCount int    `json:"turn_count"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// CopilotCacheRow 问答缓存（相同问题复用，免调大模型）。
type CopilotCacheRow struct {
	Fingerprint string
	TenantID    string
	AlertID     string
	Question    string
	Answer      string
	Model       string
	Provider    string
	HitCount    int
	CreatedAt   string
	UpdatedAt   string
}

// CopilotTurn 会话轮次。
type CopilotTurn struct {
	ID        string
	SessionID string
	Role      string // user | assistant
	Content   string
	CreatedAt string
}

func (d *DB) ensureCopilotTables() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS tbl_copilot_sessions (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL DEFAULT '',
			provider TEXT NOT NULL DEFAULT '',
			cursor_agent_id TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_copilot_turns (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_copilot_cache (
			fingerprint TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL DEFAULT '',
			alert_id TEXT NOT NULL DEFAULT '',
			question TEXT NOT NULL,
			answer TEXT NOT NULL,
			model TEXT NOT NULL DEFAULT '',
			provider TEXT NOT NULL DEFAULT '',
			hit_count INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_copilot_sess_upd ON tbl_copilot_sessions(updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_copilot_turns_sess ON tbl_copilot_turns(session_id, created_at)`,
	}
	for _, s := range stmts {
		if _, err := d.Exec(s); err != nil {
			return err
		}
	}
	return d.ensureCopilotSessionColumns()
}

// ensureCopilotSessionColumns 旧库补 title 等列（幂等；migrateFast skip 路径也会调用）。
func (d *DB) ensureCopilotSessionColumns() error {
	if _, err := d.Exec(`ALTER TABLE tbl_copilot_sessions ADD COLUMN IF NOT EXISTS title TEXT NOT NULL DEFAULT ''`); err != nil {
		// 兼容不支持 IF NOT EXISTS 的方言：忽略「已存在」类错误
		if _, err2 := d.Exec(`ALTER TABLE tbl_copilot_sessions ADD COLUMN title TEXT NOT NULL DEFAULT ''`); err2 != nil {
			msg := strings.ToLower(err2.Error())
			if !strings.Contains(msg, "duplicate") && !strings.Contains(msg, "already exists") && !strings.Contains(msg, "exists") {
				return err2
			}
		}
	}
	return nil
}

// CopilotCacheFingerprint 规范化问题指纹（不计图片）。
func CopilotCacheFingerprint(tenantID, alertID, question string) string {
	q := strings.ToLower(strings.TrimSpace(question))
	q = strings.Join(strings.Fields(q), " ")
	sum := sha256.Sum256([]byte(strings.TrimSpace(tenantID) + "|" + strings.TrimSpace(alertID) + "|" + q))
	return hex.EncodeToString(sum[:])
}

func (d *DB) GetCopilotCache(fp string) (*CopilotCacheRow, error) {
	row := &CopilotCacheRow{}
	err := d.QueryRow(`SELECT fingerprint, tenant_id, alert_id, question, answer, model, provider, hit_count, created_at, updated_at
		FROM tbl_copilot_cache WHERE fingerprint=?`, fp).
		Scan(&row.Fingerprint, &row.TenantID, &row.AlertID, &row.Question, &row.Answer, &row.Model, &row.Provider, &row.HitCount, &row.CreatedAt, &row.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return row, nil
}

func (d *DB) TouchCopilotCache(fp string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`UPDATE tbl_copilot_cache SET hit_count=hit_count+1, updated_at=? WHERE fingerprint=?`, now, fp)
	return err
}

func (d *DB) UpsertCopilotCache(row *CopilotCacheRow) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if row.CreatedAt == "" {
		row.CreatedAt = now
	}
	row.UpdatedAt = now
	_, err := d.Exec(`INSERT INTO tbl_copilot_cache(fingerprint, tenant_id, alert_id, question, answer, model, provider, hit_count, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,1,?,?)
		ON CONFLICT(fingerprint) DO UPDATE SET answer=excluded.answer, model=excluded.model, provider=excluded.provider,
			hit_count=tbl_copilot_cache.hit_count+1, updated_at=excluded.updated_at`,
		row.Fingerprint, row.TenantID, row.AlertID, row.Question, row.Answer, row.Model, row.Provider, row.CreatedAt, row.UpdatedAt)
	return err
}

func copilotMissingTitleColumn(err error) bool {
	if err == nil {
		return false
	}
	m := strings.ToLower(err.Error())
	return strings.Contains(m, "title") && (strings.Contains(m, "does not exist") || strings.Contains(m, "42703"))
}

func (d *DB) GetCopilotSession(id string) (*CopilotSession, error) {
	s := &CopilotSession{}
	err := d.QueryRow(`SELECT id, tenant_id, provider, cursor_agent_id, model, COALESCE(title,''), created_at, updated_at FROM tbl_copilot_sessions WHERE id=?`, id).
		Scan(&s.ID, &s.TenantID, &s.Provider, &s.CursorAgentID, &s.Model, &s.Title, &s.CreatedAt, &s.UpdatedAt)
	if err != nil && copilotMissingTitleColumn(err) {
		err = d.QueryRow(`SELECT id, tenant_id, provider, cursor_agent_id, model, created_at, updated_at FROM tbl_copilot_sessions WHERE id=?`, id).
			Scan(&s.ID, &s.TenantID, &s.Provider, &s.CursorAgentID, &s.Model, &s.CreatedAt, &s.UpdatedAt)
	}
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (d *DB) SaveCopilotSession(s *CopilotSession) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if s.ID == "" {
		s.ID = uuid.NewString()
	}
	if s.CreatedAt == "" {
		s.CreatedAt = now
	}
	s.UpdatedAt = now
	_, err := d.Exec(`INSERT INTO tbl_copilot_sessions(id, tenant_id, provider, cursor_agent_id, model, title, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			cursor_agent_id=CASE WHEN excluded.cursor_agent_id<>'' THEN excluded.cursor_agent_id ELSE tbl_copilot_sessions.cursor_agent_id END,
			model=CASE WHEN excluded.model<>'' THEN excluded.model ELSE tbl_copilot_sessions.model END,
			provider=CASE WHEN excluded.provider<>'' THEN excluded.provider ELSE tbl_copilot_sessions.provider END,
			title=CASE WHEN tbl_copilot_sessions.title='' AND excluded.title<>'' THEN excluded.title ELSE tbl_copilot_sessions.title END,
			updated_at=excluded.updated_at`,
		s.ID, s.TenantID, s.Provider, s.CursorAgentID, s.Model, s.Title, s.CreatedAt, s.UpdatedAt)
	if err != nil && copilotMissingTitleColumn(err) {
		_, err = d.Exec(`INSERT INTO tbl_copilot_sessions(id, tenant_id, provider, cursor_agent_id, model, created_at, updated_at)
			VALUES(?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET
				cursor_agent_id=CASE WHEN excluded.cursor_agent_id<>'' THEN excluded.cursor_agent_id ELSE tbl_copilot_sessions.cursor_agent_id END,
				model=CASE WHEN excluded.model<>'' THEN excluded.model ELSE tbl_copilot_sessions.model END,
				provider=CASE WHEN excluded.provider<>'' THEN excluded.provider ELSE tbl_copilot_sessions.provider END,
				updated_at=excluded.updated_at`,
			s.ID, s.TenantID, s.Provider, s.CursorAgentID, s.Model, s.CreatedAt, s.UpdatedAt)
	}
	return err
}

// EnsureCopilotSession 确保会话存在；title 仅在首次写入。
func (d *DB) EnsureCopilotSession(id, tenantID, provider, model, title string) error {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	title = strings.TrimSpace(title)
	if runes := []rune(title); len(runes) > 80 {
		title = string(runes[:80]) + "…"
	}
	return d.SaveCopilotSession(&CopilotSession{
		ID: id, TenantID: tenantID, Provider: provider, Model: model, Title: title,
	})
}

func (d *DB) AddCopilotTurn(sessionID, role, content string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`INSERT INTO tbl_copilot_turns(id, session_id, role, content, created_at) VALUES(?,?,?,?,?)`,
		uuid.NewString(), sessionID, role, content, now)
	if err != nil {
		return err
	}
	_, _ = d.Exec(`UPDATE tbl_copilot_sessions SET updated_at=? WHERE id=?`, now, sessionID)
	return nil
}

func (d *DB) ListCopilotTurns(sessionID string, limit int) ([]CopilotTurn, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := d.Query(`SELECT id, session_id, role, content, created_at FROM tbl_copilot_turns
		WHERE session_id=? ORDER BY created_at ASC LIMIT ?`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CopilotTurn
	for rows.Next() {
		var t CopilotTurn
		if err := rows.Scan(&t.ID, &t.SessionID, &t.Role, &t.Content, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListCopilotSessions 按更新时间倒序列出会话（含标题与轮次数）。
func (d *DB) ListCopilotSessions(tenantID string, limit int) ([]CopilotSessionListItem, error) {
	if limit <= 0 {
		limit = 50
	}
	tenantID = strings.TrimSpace(tenantID)
	// 标题取自首轮 user（不依赖 s.title，避免旧库缺列报 42703）；有 title 列时启动迁移会补齐供写入。
	q := `SELECT s.id, COALESCE((
			SELECT LEFT(t.content, 80) FROM tbl_copilot_turns t WHERE t.session_id=s.id AND t.role='user' ORDER BY t.created_at ASC LIMIT 1
		), '新对话') AS title,
		s.provider, s.model,
		(SELECT COUNT(*) FROM tbl_copilot_turns t2 WHERE t2.session_id=s.id) AS turn_count,
		s.created_at, s.updated_at
		FROM tbl_copilot_sessions s`
	var args []any
	if tenantID != "" {
		q += ` WHERE s.tenant_id=? OR s.tenant_id=''`
		args = append(args, tenantID)
	}
	q += ` ORDER BY s.updated_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CopilotSessionListItem
	for rows.Next() {
		var it CopilotSessionListItem
		if err := rows.Scan(&it.ID, &it.Title, &it.Provider, &it.Model, &it.TurnCount, &it.CreatedAt, &it.UpdatedAt); err != nil {
			return nil, err
		}
		// 标题过长时列表侧再压一下
		if r := []rune(it.Title); len(r) > 48 {
			it.Title = string(r[:48]) + "…"
		}
		out = append(out, it)
	}
	return out, rows.Err()
}
