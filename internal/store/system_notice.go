package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	SystemNoticeModeEveryLogin      = "every_login"
	SystemNoticeModeOnce            = "once"
	SystemNoticeModeOnVersionChange = "on_version_change"

	// DefaultTicketSubmitFreezeMessage 发版冻结开启且未自定义文案时的默认提示。
	DefaultTicketSubmitFreezeMessage = "平台正在发版维护，暂不可提交工单，请稍后再试"
)

// SystemNotice 平台级登录弹框通知模板（兼发版工单提交冻结开关）。
type SystemNotice struct {
	ID                         string `json:"id"`
	Title                      string `json:"title"`
	Body                       string `json:"body"`
	Enabled                    bool   `json:"enabled"`
	ShowMode                   string `json:"show_mode"`
	Version                    int    `json:"version"`
	TicketSubmitFreeze         bool   `json:"ticket_submit_freeze"`
	TicketSubmitFreezeMessage  string `json:"ticket_submit_freeze_message"`
	UpdatedBy                  string `json:"updated_by,omitempty"`
	UpdatedAt                  string `json:"updated_at"`
	CreatedAt                  string `json:"created_at"`
}

func NormalizeSystemNoticeShowMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case SystemNoticeModeEveryLogin:
		return SystemNoticeModeEveryLogin
	case SystemNoticeModeOnce:
		return SystemNoticeModeOnce
	case SystemNoticeModeOnVersionChange, "":
		return SystemNoticeModeOnVersionChange
	default:
		return SystemNoticeModeOnVersionChange
	}
}

const systemNoticeSelectCols = `id, title, body, enabled, show_mode, version, updated_by, updated_at, created_at,
		COALESCE(ticket_submit_freeze,0), COALESCE(ticket_submit_freeze_message,'')`

func (d *DB) GetEnabledSystemNotice() (*SystemNotice, error) {
	it, err := scanSystemNotice(d.QueryRow(`
		SELECT ` + systemNoticeSelectCols + `
		FROM tbl_system_notices WHERE enabled=1 ORDER BY updated_at DESC LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return it, err
}

func (d *DB) GetSystemNoticeSettings() (*SystemNotice, error) {
	it, err := d.GetEnabledSystemNotice()
	if err != nil || it != nil {
		return it, err
	}
	it, err = scanSystemNotice(d.QueryRow(`
		SELECT ` + systemNoticeSelectCols + `
		FROM tbl_system_notices ORDER BY updated_at DESC LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return it, err
}

func (d *DB) GetSystemNotice(id string) (*SystemNotice, error) {
	it, err := scanSystemNotice(d.QueryRow(`
		SELECT `+systemNoticeSelectCols+`
		FROM tbl_system_notices WHERE id=?`, strings.TrimSpace(id)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return it, err
}

// GetTicketSubmitFreeze 读取当前系统通知设置行上的发版冻结状态。
func (d *DB) GetTicketSubmitFreeze() (enabled bool, message string, err error) {
	it, err := d.GetSystemNoticeSettings()
	if err != nil {
		return false, "", err
	}
	if it == nil {
		return false, DefaultTicketSubmitFreezeMessage, nil
	}
	msg := strings.TrimSpace(it.TicketSubmitFreezeMessage)
	if msg == "" {
		msg = DefaultTicketSubmitFreezeMessage
	}
	return it.TicketSubmitFreeze, msg, nil
}

// CheckTicketSubmitAllowed 发版冻结开启时禁止非 system 提交者新建工单。
func (d *DB) CheckTicketSubmitAllowed(submitter string) error {
	if strings.EqualFold(strings.TrimSpace(submitter), "system") {
		return nil
	}
	enabled, msg, err := d.GetTicketSubmitFreeze()
	if err != nil {
		return err
	}
	if !enabled {
		return nil
	}
	if strings.TrimSpace(msg) == "" {
		msg = DefaultTicketSubmitFreezeMessage
	}
	return errors.New(msg)
}

// UpsertSystemNotice 保存模板；bumpVersion 为真时 version+1。启用时关闭其它启用行。
func (d *DB) UpsertSystemNotice(it *SystemNotice, bumpVersion bool, actor string) error {
	if it == nil {
		return errors.New("notice 必填")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	it.Title = strings.TrimSpace(it.Title)
	it.Body = strings.TrimSpace(it.Body)
	it.TicketSubmitFreezeMessage = strings.TrimSpace(it.TicketSubmitFreezeMessage)
	it.ShowMode = NormalizeSystemNoticeShowMode(it.ShowMode)
	it.UpdatedBy = strings.TrimSpace(actor)
	it.UpdatedAt = now
	if it.Title == "" {
		return errors.New("标题必填")
	}
	if it.ID == "" {
		exist, err := d.GetSystemNoticeSettings()
		if err != nil {
			return err
		}
		if exist != nil {
			it.ID = exist.ID
			it.CreatedAt = exist.CreatedAt
			it.Version = exist.Version
		}
	}
	if it.ID == "" {
		it.ID = uuid.NewString()
		it.CreatedAt = now
		if it.Version < 1 {
			it.Version = 1
		}
		if bumpVersion && it.Version < 1 {
			it.Version = 1
		}
		_, err := d.Exec(`INSERT INTO tbl_system_notices(id, title, body, enabled, show_mode, version, updated_by, updated_at, created_at, ticket_submit_freeze, ticket_submit_freeze_message)
			VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			it.ID, it.Title, it.Body, boolToInt(it.Enabled), it.ShowMode, it.Version, it.UpdatedBy, it.UpdatedAt, it.CreatedAt,
			boolToInt(it.TicketSubmitFreeze), it.TicketSubmitFreezeMessage)
		if err != nil {
			return err
		}
	} else {
		cur, err := d.GetSystemNotice(it.ID)
		if err != nil {
			return err
		}
		if cur == nil {
			return errors.New("通知不存在")
		}
		it.CreatedAt = cur.CreatedAt
		it.Version = cur.Version
		if bumpVersion {
			it.Version = cur.Version + 1
			if it.Version < 1 {
				it.Version = 1
			}
		}
		_, err = d.Exec(`UPDATE tbl_system_notices SET title=?, body=?, enabled=?, show_mode=?, version=?, updated_by=?, updated_at=?, ticket_submit_freeze=?, ticket_submit_freeze_message=? WHERE id=?`,
			it.Title, it.Body, boolToInt(it.Enabled), it.ShowMode, it.Version, it.UpdatedBy, it.UpdatedAt,
			boolToInt(it.TicketSubmitFreeze), it.TicketSubmitFreezeMessage, it.ID)
		if err != nil {
			return err
		}
	}
	if it.Enabled {
		_, err := d.Exec(`UPDATE tbl_system_notices SET enabled=0 WHERE id<>? AND enabled=1`, it.ID)
		return err
	}
	return nil
}

func scanSystemNotice(row interface{ Scan(dest ...any) error }) (*SystemNotice, error) {
	var it SystemNotice
	var enabled int
	var freeze int
	if err := row.Scan(&it.ID, &it.Title, &it.Body, &enabled, &it.ShowMode, &it.Version, &it.UpdatedBy, &it.UpdatedAt, &it.CreatedAt, &freeze, &it.TicketSubmitFreezeMessage); err != nil {
		return nil, err
	}
	it.Enabled = enabled != 0
	it.TicketSubmitFreeze = freeze != 0
	it.ShowMode = NormalizeSystemNoticeShowMode(it.ShowMode)
	return &it, nil
}

func (d *DB) GetSystemNoticeAck(userID, noticeID string) (version int, ok bool, err error) {
	userID = strings.TrimSpace(userID)
	noticeID = strings.TrimSpace(noticeID)
	if userID == "" || noticeID == "" {
		return 0, false, nil
	}
	err = d.QueryRow(`SELECT notice_version FROM tbl_system_notice_acks WHERE user_id=? AND notice_id=?`, userID, noticeID).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return version, true, nil
}

func (d *DB) AckSystemNotice(userID, noticeID string, version int) error {
	userID = strings.TrimSpace(userID)
	noticeID = strings.TrimSpace(noticeID)
	if userID == "" || noticeID == "" {
		return errors.New("user_id 与 notice_id 必填")
	}
	if version < 1 {
		version = 1
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`INSERT INTO tbl_system_notice_acks(user_id, notice_id, notice_version, acked_at)
		VALUES(?,?,?,?)
		ON CONFLICT(user_id, notice_id) DO UPDATE SET notice_version=EXCLUDED.notice_version, acked_at=EXCLUDED.acked_at`,
		userID, noticeID, version, now)
	return err
}

// ShouldShowSystemNotice 按 show_mode 与 ack 判断是否应对该用户弹出。
func ShouldShowSystemNotice(n *SystemNotice, ackVersion int, hasAck bool) bool {
	if n == nil || !n.Enabled {
		return false
	}
	switch NormalizeSystemNoticeShowMode(n.ShowMode) {
	case SystemNoticeModeEveryLogin:
		return true
	case SystemNoticeModeOnce:
		return !hasAck
	default: // on_version_change
		if !hasAck {
			return true
		}
		return ackVersion < n.Version
	}
}
