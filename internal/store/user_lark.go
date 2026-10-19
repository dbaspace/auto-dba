package store

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// LoggedInUserIDs 登录过本平台的用户：会话表有记录，或登录日志有成功登录（会话表上线前的登录只在日志里）。
func (d *DB) LoggedInUserIDs() (map[string]bool, error) {
	rows, err := d.Query(`
		SELECT DISTINCT user_id FROM tbl_user_sessions
		UNION
		SELECT id FROM tbl_users u WHERE EXISTS (
			SELECT 1 FROM tbl_login_logs l
			WHERE l.success=1 AND (l.user_id=u.id OR l.username=u.username))`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if id = strings.TrimSpace(id); id != "" {
			out[id] = true
		}
	}
	return out, rows.Err()
}

func (d *DB) saveUserProfile(id string, p UserProfile) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = d.Exec(`UPDATE tbl_users SET profile=? WHERE id=?`, string(b), id)
	return err
}

// SetUserLarkEmail 管理员手填飞书邮箱；变更后清掉旧的飞书身份，等待重新同步。
func (d *DB) SetUserLarkEmail(id, larkEmail string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("user id required")
	}
	larkEmail = strings.TrimSpace(larkEmail)
	if larkEmail != "" && !strings.Contains(larkEmail, "@") {
		return errors.New("飞书邮箱格式不正确")
	}
	p := d.loadUserProfile(id)
	if strings.EqualFold(p.LarkEmail, larkEmail) {
		return nil
	}
	if isLarkIMType(p.IMType) && p.LarkID != "" && p.IMAccount == p.LarkID {
		p.IMAccount = ""
	}
	p.LarkEmail = larkEmail
	p.LarkID, p.LarkOpenID, p.LarkSyncTime = "", "", ""
	return d.saveUserProfile(id, p)
}

// SetUserLarkIdentity 写入同步得到的飞书身份；企业邮箱不变。matchedEmail 非空时记为飞书邮箱。
func (d *DB) SetUserLarkIdentity(id, larkID, openID, matchedEmail string, at time.Time) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("user id required")
	}
	p := d.loadUserProfile(id)
	p.LarkID = strings.TrimSpace(larkID)
	p.LarkOpenID = strings.TrimSpace(openID)
	if e := strings.TrimSpace(matchedEmail); e != "" {
		p.LarkEmail = e
	}
	p.LarkSyncTime = at.UTC().Format(time.RFC3339)
	if p.LarkID != "" {
		p.IMType = "lark"
		p.IMAccount = p.LarkID
	}
	return d.saveUserProfile(id, p)
}

func isLarkIMType(t string) bool {
	t = strings.ToLower(strings.TrimSpace(t))
	return t == "" || t == "lark" || t == "feishu"
}
