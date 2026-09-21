package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	ExternalCaptchaAfter    = 3
	ExternalLockAfter       = 5
	ExternalCooldownMinutes = 10
)

// LoginAttempt 外部认证（SSO/LDAP）按用户名维度的失败计数。
type LoginAttempt struct {
	Username      string
	FailCount     int
	CooldownUntil string // RFC3339 UTC；空表示未冷却
	UpdatedAt     string
}

func NormalizeLoginUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

func (d *DB) GetLoginAttempt(username string) (*LoginAttempt, error) {
	u := NormalizeLoginUsername(username)
	if u == "" {
		return &LoginAttempt{}, nil
	}
	a := &LoginAttempt{Username: u}
	var cool sql.NullString
	err := d.QueryRow(`
		SELECT fail_count, COALESCE(cooldown_until,''), COALESCE(updated_at,'')
		FROM tbl_login_attempt WHERE username=?`, u).
		Scan(&a.FailCount, &cool, &a.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return a, nil
	}
	if err != nil {
		return nil, err
	}
	a.CooldownUntil = cool.String
	return a, nil
}

// EnsureLoginAttemptReady 冷却到期则自动清零；返回可用于登录校验的最新状态。
func (d *DB) EnsureLoginAttemptReady(username string) (*LoginAttempt, error) {
	a, err := d.GetLoginAttempt(username)
	if err != nil || a == nil {
		return a, err
	}
	if strings.TrimSpace(a.CooldownUntil) == "" {
		return a, nil
	}
	until, perr := time.Parse(time.RFC3339, a.CooldownUntil)
	if perr != nil {
		return d.ClearExternalLoginFailure(username)
	}
	if time.Now().UTC().Before(until) {
		return a, nil
	}
	return d.ClearExternalLoginFailure(username)
}

func (a *LoginAttempt) CooldownRemaining() time.Duration {
	if a == nil || strings.TrimSpace(a.CooldownUntil) == "" {
		return 0
	}
	until, err := time.Parse(time.RFC3339, a.CooldownUntil)
	if err != nil {
		return 0
	}
	d := time.Until(until.UTC())
	if d < 0 {
		return 0
	}
	return d
}

func (a *LoginAttempt) InCooldown() bool {
	return a.CooldownRemaining() > 0
}

func (a *LoginAttempt) RequireCaptcha() bool {
	return a != nil && a.FailCount >= ExternalCaptchaAfter
}

func (a *LoginAttempt) RetryAfterSec() int {
	sec := int(a.CooldownRemaining().Seconds())
	if sec < 1 && a.InCooldown() {
		return 1
	}
	return sec
}

func (a *LoginAttempt) CooldownMessage() string {
	sec := a.RetryAfterSec()
	if sec <= 0 {
		return "认证失败次数过多，请稍后再试"
	}
	min := (sec + 59) / 60
	if min < 1 {
		min = 1
	}
	return fmt.Sprintf("认证失败次数过多，请 %d 分钟后再试", min)
}

// BumpExternalLoginFailure 外部认证失败 +1；满 5 次写入 10 分钟冷却。
func (d *DB) BumpExternalLoginFailure(username string) (*LoginAttempt, error) {
	u := NormalizeLoginUsername(username)
	if u == "" {
		return &LoginAttempt{}, nil
	}
	a, err := d.EnsureLoginAttemptReady(u)
	if err != nil {
		return nil, err
	}
	if a.InCooldown() {
		return a, nil
	}
	n := a.FailCount + 1
	cool := ""
	if n >= ExternalLockAfter {
		cool = time.Now().UTC().Add(time.Duration(ExternalCooldownMinutes) * time.Minute).Format(time.RFC3339)
		n = ExternalLockAfter
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = d.Exec(`
		INSERT INTO tbl_login_attempt(username, fail_count, cooldown_until, updated_at)
		VALUES(?,?,?,?)
		ON CONFLICT(username) DO UPDATE SET
			fail_count=EXCLUDED.fail_count,
			cooldown_until=EXCLUDED.cooldown_until,
			updated_at=EXCLUDED.updated_at`,
		u, n, cool, now)
	if err != nil {
		return nil, err
	}
	return &LoginAttempt{Username: u, FailCount: n, CooldownUntil: cool, UpdatedAt: now}, nil
}

func (d *DB) ClearExternalLoginFailure(username string) (*LoginAttempt, error) {
	u := NormalizeLoginUsername(username)
	empty := &LoginAttempt{Username: u}
	if u == "" {
		return empty, nil
	}
	_, err := d.Exec(`DELETE FROM tbl_login_attempt WHERE username=?`, u)
	if err != nil {
		return nil, err
	}
	return empty, nil
}
