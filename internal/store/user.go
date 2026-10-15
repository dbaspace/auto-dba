package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const (
	UserStatusActive   = "active"
	UserStatusLocked   = "locked"
	UserStatusDisabled = "disabled"
	MaxFailedLogins    = 3
)

func scanUserLoginCols(disabled int, failed int, lockedAt string, u *User) {
	u.LoginDisabled = disabled != 0
	u.FailedLoginCount = failed
	u.LockedAt = strings.TrimSpace(lockedAt)
	u.Status = u.ComputeStatus()
}

func (u *User) ComputeStatus() string {
	if u == nil {
		return UserStatusActive
	}
	if u.LoginDisabled {
		return UserStatusDisabled
	}
	if u.FailedLoginCount >= MaxFailedLogins || strings.TrimSpace(u.LockedAt) != "" {
		return UserStatusLocked
	}
	if u.ExternalCooldownActive() {
		return UserStatusLocked
	}
	return UserStatusActive
}

// ExternalCooldownActive LDAP/SSO 冷却未到期。
func (u *User) ExternalCooldownActive() bool {
	if u == nil {
		return false
	}
	raw := strings.TrimSpace(u.ExternalCooldownUntil)
	if raw == "" {
		return false
	}
	until, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return false
	}
	return time.Now().UTC().Before(until.UTC())
}

func (u *User) IsLoginBlocked() bool {
	st := u.ComputeStatus()
	return st == UserStatusDisabled || st == UserStatusLocked
}

func (u *User) LoginBlockedMessage() string {
	switch u.ComputeStatus() {
	case UserStatusDisabled:
		return "账号已禁用，请联系管理员"
	case UserStatusLocked:
		return "账号已锁定，请联系管理员解锁"
	default:
		return ""
	}
}

func (d *DB) GetUserByUsername(username string) (*User, error) {
	u := &User{}
	var roleID sql.NullString
	var source sql.NullString
	var disabled, failed int
	var lockedAt string
	err := d.QueryRow(
		`SELECT id, username, display_name, role, role_id, password_hash, source, created_at,
			COALESCE(login_disabled,0), COALESCE(failed_login_count,0), COALESCE(locked_at,'')
		 FROM tbl_users WHERE username=?`,
		username,
	).Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &roleID, &u.PasswordHash, &source, &u.CreatedAt,
		&disabled, &failed, &lockedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u.RoleID = roleID.String
	u.Source = userSource(source.String)
	scanUserLoginCols(disabled, failed, lockedAt, u)
	d.attachUserProfile(u)
	d.AttachUserRoleIDs(u)
	d.attachExternalLoginAttempt(u)
	return u, nil
}

func (d *DB) GetUserByID(id string) (*User, error) {
	u := &User{}
	var roleID sql.NullString
	var source sql.NullString
	var disabled, failed int
	var lockedAt string
	err := d.QueryRow(
		`SELECT id, username, display_name, role, role_id, password_hash, source, created_at,
			COALESCE(login_disabled,0), COALESCE(failed_login_count,0), COALESCE(locked_at,'')
		 FROM tbl_users WHERE id=?`,
		id,
	).Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &roleID, &u.PasswordHash, &source, &u.CreatedAt,
		&disabled, &failed, &lockedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u.RoleID = roleID.String
	u.Source = userSource(source.String)
	scanUserLoginCols(disabled, failed, lockedAt, u)
	d.attachUserProfile(u)
	d.AttachUserRoleIDs(u)
	d.attachExternalLoginAttempt(u)
	return u, nil
}

func userSource(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return UserSourcePlatform
	}
	return s
}

func (d *DB) ListUsers() ([]User, error) {
	rows, err := d.Query(`
		SELECT id, username, display_name, role, role_id, source, created_at,
			COALESCE(login_disabled,0), COALESCE(failed_login_count,0), COALESCE(locked_at,'')
		FROM tbl_users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		var roleID sql.NullString
		var source sql.NullString
		var disabled, failed int
		var lockedAt string
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &roleID, &source, &u.CreatedAt,
			&disabled, &failed, &lockedAt); err != nil {
			return nil, err
		}
		u.RoleID = roleID.String
		u.Source = userSource(source.String)
		scanUserLoginCols(disabled, failed, lockedAt, &u)
		d.attachUserProfile(&u)
		d.AttachUserRoleIDs(&u)
		d.attachExternalLoginAttempt(&u)
		out = append(out, u)
	}
	return out, rows.Err()
}

type UserProfile struct {
	Avatar    string `json:"avatar,omitempty"`
	Gender    string `json:"gender,omitempty"`
	Email     string `json:"email,omitempty"`
	Phone     string `json:"phone,omitempty"`
	IMType    string `json:"im_type,omitempty"`
	IMAccount string `json:"im_account,omitempty"`
}

func (d *DB) attachUserProfile(u *User) {
	if u == nil || strings.TrimSpace(u.ID) == "" {
		return
	}
	var raw string
	err := d.QueryRow(`SELECT COALESCE(profile,'{}') FROM tbl_users WHERE id=?`, u.ID).Scan(&raw)
	if err != nil || strings.TrimSpace(raw) == "" {
		return
	}
	var p UserProfile
	if json.Unmarshal([]byte(raw), &p) != nil {
		return
	}
	u.Avatar = p.Avatar
	u.Gender = p.Gender
	u.Email = p.Email
	u.Phone = p.Phone
	u.IMType = p.IMType
	u.IMAccount = p.IMAccount
}

func (d *DB) UpdateUserProfile(id, displayName string, p UserProfile) error {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		return errors.New("姓名必填")
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = d.Exec(`UPDATE tbl_users SET display_name=?, profile=? WHERE id=?`, displayName, string(b), id)
	return err
}

func (d *DB) loadUserProfile(id string) UserProfile {
	var p UserProfile
	if strings.TrimSpace(id) == "" {
		return p
	}
	var raw string
	err := d.QueryRow(`SELECT COALESCE(profile,'{}') FROM tbl_users WHERE id=?`, id).Scan(&raw)
	if err != nil || strings.TrimSpace(raw) == "" {
		return p
	}
	_ = json.Unmarshal([]byte(raw), &p)
	return p
}

// SetUserEmail 更新 profile.email，保留其它资料字段。
func (d *DB) SetUserEmail(id, email string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("user id required")
	}
	email = strings.TrimSpace(email)
	if email != "" && !strings.Contains(email, "@") {
		return errors.New("邮箱格式不正确")
	}
	p := d.loadUserProfile(id)
	p.Email = email
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = d.Exec(`UPDATE tbl_users SET profile=? WHERE id=?`, string(b), id)
	return err
}

func (d *DB) SetUserLoginDisabled(id string, disabled bool) error {
	v := 0
	if disabled {
		v = 1
	}
	_, err := d.Exec(`UPDATE tbl_users SET login_disabled=? WHERE id=?`, v, id)
	return err
}

func (d *DB) UnlockUser(id string) error {
	u, err := d.GetUserByID(id)
	if err != nil {
		return err
	}
	if u == nil {
		return errors.New("用户不存在")
	}
	if _, err := d.Exec(`UPDATE tbl_users SET failed_login_count=0, locked_at='' WHERE id=?`, id); err != nil {
		return err
	}
	_, err = d.ClearExternalLoginFailure(u.Username)
	return err
}

// attachExternalLoginAttempt 填充外部认证失败/冷却，并刷新 status。
func (d *DB) attachExternalLoginAttempt(u *User) {
	if d == nil || u == nil || strings.TrimSpace(u.Username) == "" {
		return
	}
	a, err := d.EnsureLoginAttemptReady(u.Username)
	if err != nil || a == nil {
		u.Status = u.ComputeStatus()
		return
	}
	u.ExternalFailCount = a.FailCount
	u.ExternalCooldownUntil = strings.TrimSpace(a.CooldownUntil)
	u.Status = u.ComputeStatus()
}

func (d *DB) RecordLoginFailure(id string) (locked bool, err error) {
	u, err := d.GetUserByID(id)
	if err != nil || u == nil {
		return false, err
	}
	n := u.FailedLoginCount + 1
	lockedAt := u.LockedAt
	if n >= MaxFailedLogins {
		if strings.TrimSpace(lockedAt) == "" {
			lockedAt = time.Now().UTC().Format(time.RFC3339)
		}
		locked = true
	}
	_, err = d.Exec(`UPDATE tbl_users SET failed_login_count=?, locked_at=? WHERE id=?`, n, lockedAt, id)
	return locked, err
}

func (d *DB) ClearLoginFailures(id string) error {
	_, err := d.Exec(`UPDATE tbl_users SET failed_login_count=0, locked_at='' WHERE id=?`, id)
	return err
}

func (d *DB) DeleteUser(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	if _, err := d.Exec(`DELETE FROM tbl_tenant_members WHERE user_id=?`, id); err != nil {
		return err
	}
	if _, err := d.Exec(`DELETE FROM tbl_tenant_grants WHERE to_user_id=?`, id); err != nil {
		return err
	}
	if _, err := d.Exec(`DELETE FROM tbl_user_roles WHERE user_id=?`, id); err != nil {
		return err
	}
	_, err := d.Exec(`DELETE FROM tbl_users WHERE id=?`, id)
	return err
}

func IsBuiltinAdminUser(u *User) bool {
	if u == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(u.Username), "admin")
}
