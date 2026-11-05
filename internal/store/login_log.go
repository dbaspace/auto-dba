package store

import (
	"net"
	"strings"
	"time"

	"github.com/google/uuid"
)

// relocatePrivateLoginIP 把误写在登录 IP 上的内网地址挪到内网 IP，并清空登录 IP。
// 公网地址保持不动。历史记录没有保存过公网出口，这里不补写。
func relocatePrivateLoginIP(ip, lan string) (string, string, bool) {
	ip = strings.TrimSpace(ip)
	lan = strings.TrimSpace(lan)
	if !isPrivateLoginIP(ip) {
		return ip, lan, false
	}
	if lan == "" {
		lan = ip
	}
	return "", lan, true
}

func isPrivateLoginIP(s string) bool {
	ip := net.ParseIP(strings.TrimSpace(s))
	return ip != nil && ip.IsPrivate()
}

func (d *DB) relocatePrivateLoginIPs() error {
	rows, err := d.Query(`SELECT id, ip, COALESCE(lan_ip,'') FROM tbl_login_logs WHERE TRIM(ip) <> ''`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type fix struct{ id, lan string }
	var pending []fix
	for rows.Next() {
		var id, ip, lan string
		if err := rows.Scan(&id, &ip, &lan); err != nil {
			return err
		}
		_, lan, changed := relocatePrivateLoginIP(ip, lan)
		if !changed {
			continue
		}
		pending = append(pending, fix{id: id, lan: lan})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, it := range pending {
		if _, err := d.Exec(`UPDATE tbl_login_logs SET ip = '', lan_ip = ? WHERE id = ?`, it.lan, it.id); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) CreateLoginLog(it *LoginLog) error {
	if it == nil {
		return nil
	}
	if it.ID == "" {
		it.ID = uuid.NewString()
	}
	if it.LoggedAt == "" {
		it.LoggedAt = time.Now().UTC().Format(time.RFC3339)
	}
	it.Username = strings.TrimSpace(it.Username)
	it.DisplayName = strings.TrimSpace(it.DisplayName)
	it.IP = strings.TrimSpace(it.IP)
	it.LanIP = strings.TrimSpace(it.LanIP)
	it.UserAgent = strings.TrimSpace(it.UserAgent)
	if len(it.UserAgent) > 512 {
		it.UserAgent = it.UserAgent[:512]
	}
	success := 0
	if it.Success {
		success = 1
	}
	_, err := d.Exec(
		`INSERT INTO tbl_login_logs(id, user_id, username, display_name, success, message, ip, lan_ip, user_agent, logged_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?)`,
		it.ID, it.UserID, it.Username, it.DisplayName, success, it.Message, it.IP, it.LanIP, it.UserAgent, it.LoggedAt,
	)
	return err
}

type LoginLogFilter struct {
	Username string
	Success  *bool
	Page     int
	PageSize int
}

func (d *DB) ListLoginLogs(f LoginLogFilter) ([]LoginLog, int, error) {
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.PageSize <= 0 || f.PageSize > 200 {
		f.PageSize = 20
	}
	where := `WHERE 1=1`
	args := []any{}
	if s := strings.TrimSpace(f.Username); s != "" {
		where += ` AND username LIKE ?`
		args = append(args, "%"+s+"%")
	}
	if f.Success != nil {
		v := 0
		if *f.Success {
			v = 1
		}
		where += ` AND success=?`
		args = append(args, v)
	}
	var total int
	if err := d.QueryRow(`SELECT COUNT(1) FROM tbl_login_logs `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	q := `SELECT id, user_id, username, display_name, success, message, ip, COALESCE(lan_ip,''), user_agent, logged_at
		FROM tbl_login_logs ` + where + ` ORDER BY logged_at DESC LIMIT ? OFFSET ?`
	args = append(args, f.PageSize, (f.Page-1)*f.PageSize)
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []LoginLog{}
	for rows.Next() {
		var it LoginLog
		var success int
		if err := rows.Scan(&it.ID, &it.UserID, &it.Username, &it.DisplayName, &success, &it.Message, &it.IP, &it.LanIP, &it.UserAgent, &it.LoggedAt); err != nil {
			return nil, 0, err
		}
		it.Success = success != 0
		out = append(out, it)
	}
	return out, total, rows.Err()
}
