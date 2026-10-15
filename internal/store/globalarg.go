package store

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"
)

func (d *DB) invalidateArgCache(key string) {
	if d == nil {
		return
	}
	d.argCacheMu.Lock()
	defer d.argCacheMu.Unlock()
	if d.argCache == nil {
		return
	}
	if key == "" {
		d.argCache = nil
		return
	}
	delete(d.argCache, key)
}

const ArgLoginIdleTimeoutMin = "login_idle_timeout_min"
const ArgUserWatermarkEnabled = "user_watermark_enabled"
const ArgIssueGlobalApproverWhitelist = "issue_global_approver_whitelist"
const ArgTicketDDLLongStmtTimeoutMinutes = "ticket_ddl_long_stmt_timeout_minutes"
const ArgQueryDefaultLimit = "query_default_limit"
const ArgQueryMaxLimit = "query_max_limit"

const (
	defaultQueryDefaultLimit = 1000
	defaultQueryMaxLimit     = 10000
	queryLimitSanityMax      = 100000
)

const defaultTicketDDLLongStmtTimeout = 6 * time.Hour

func (d *DB) ListGlobalArgs() ([]GlobalArg, error) {
	rows, err := d.Query(`SELECT key, value, description, updated_at FROM tbl_global_args ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GlobalArg
	for rows.Next() {
		var a GlobalArg
		if err := rows.Scan(&a.Key, &a.Value, &a.Description, &a.UpdatedAt); err != nil {
			return nil, err
		}
		if a.Key == KeyLdapConfig || a.Key == KeyGoInception {
			continue
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (d *DB) GetGlobalArg(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", nil
	}
	if d != nil {
		d.argCacheMu.Lock()
		if d.argCache != nil {
			if e, ok := d.argCache[key]; ok && time.Since(e.at) < argCacheTTL {
				v := e.value
				d.argCacheMu.Unlock()
				return v, nil
			}
		}
		d.argCacheMu.Unlock()
	}
	var v string
	err := d.QueryRow(`SELECT value FROM tbl_global_args WHERE key=?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		v = ""
		err = nil
	}
	if err != nil {
		return "", err
	}
	if d != nil {
		d.argCacheMu.Lock()
		if d.argCache == nil {
			d.argCache = map[string]argCacheEntry{}
		}
		d.argCache[key] = argCacheEntry{value: v, at: time.Now()}
		d.argCacheMu.Unlock()
	}
	return v, nil
}

func (d *DB) CreateGlobalArg(key, value, desc string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("key 必填")
	}
	var exists int
	_ = d.QueryRow(`SELECT COUNT(1) FROM tbl_global_args WHERE key=?`, key).Scan(&exists)
	if exists > 0 {
		return errors.New("参数键已存在")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`INSERT INTO tbl_global_args(key, value, description, updated_at) VALUES(?,?,?,?)`, key, value, desc, now)
	if err == nil {
		d.invalidateArgCache(key)
	}
	return err
}

func (d *DB) SetGlobalArg(key, value, desc string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var exists int
	_ = d.QueryRow(`SELECT COUNT(1) FROM tbl_global_args WHERE key=?`, key).Scan(&exists)
	var err error
	if exists > 0 {
		_, err = d.Exec(`UPDATE tbl_global_args SET value=?, description=?, updated_at=? WHERE key=?`, value, desc, now, key)
	} else {
		_, err = d.Exec(`INSERT INTO tbl_global_args(key, value, description, updated_at) VALUES(?,?,?,?)`, key, value, desc, now)
	}
	if err == nil {
		d.invalidateArgCache(key)
	}
	return err
}

func (d *DB) DeleteGlobalArg(key string) error {
	res, err := d.Exec(`DELETE FROM tbl_global_args WHERE key=?`, key)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New("参数不存在")
	}
	d.invalidateArgCache(key)
	return nil
}

func (d *DB) BoolArg(key string, def bool) bool {
	v, err := d.GetGlobalArg(key)
	if err != nil || strings.TrimSpace(v) == "" {
		return def
	}
	v = strings.ToLower(strings.TrimSpace(v))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// LoginIdleTimeoutMin 返回登录闲置超时分钟数。空/非法回退 def（通常 30）；显式 0 表示关闭。
func (d *DB) LoginIdleTimeoutMin(def int) int {
	if def < 0 {
		def = 30
	}
	if d == nil {
		return def
	}
	v, err := d.GetGlobalArg(ArgLoginIdleTimeoutMin)
	if err != nil || strings.TrimSpace(v) == "" {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 0 {
		return def
	}
	return n
}

// QueryMaxLimit 最大查询条数硬顶；非法/空回退 10000，静默夹到 [1,100000]。
func (d *DB) QueryMaxLimit() int {
	n := d.intArg(ArgQueryMaxLimit, defaultQueryMaxLimit)
	if n < 1 {
		n = defaultQueryMaxLimit
	}
	if n > queryLimitSanityMax {
		n = queryLimitSanityMax
	}
	return n
}

// QueryDefaultLimit 默认查询条数；非法/空回退 1000，且不超过 QueryMaxLimit。
func (d *DB) QueryDefaultLimit() int {
	max := d.QueryMaxLimit()
	n := d.intArg(ArgQueryDefaultLimit, defaultQueryDefaultLimit)
	if n < 1 {
		n = defaultQueryDefaultLimit
	}
	if n > max {
		n = max
	}
	return n
}

func (d *DB) intArg(key string, def int) int {
	if d == nil {
		return def
	}
	v, err := d.GetGlobalArg(key)
	if err != nil || strings.TrimSpace(v) == "" {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return def
	}
	return n
}

// TicketDDLLongStmtTimeout DDL 工单长耗时语句单条超时；未配置、非法或 <=0 时为 6 小时。
func (d *DB) TicketDDLLongStmtTimeout() time.Duration {
	if d == nil {
		return defaultTicketDDLLongStmtTimeout
	}
	v, err := d.GetGlobalArg(ArgTicketDDLLongStmtTimeoutMinutes)
	if err != nil {
		return defaultTicketDDLLongStmtTimeout
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		return defaultTicketDDLLongStmtTimeout
	}
	return time.Duration(n) * time.Minute
}

// UserWatermarkEnabled 是否开启用户水印。未配置时默认关闭。
func (d *DB) UserWatermarkEnabled() bool {
	if d == nil {
		return false
	}
	return d.BoolArg(ArgUserWatermarkEnabled, false)
}
