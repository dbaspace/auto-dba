package ha

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"auto-dba/internal/store"

	_ "github.com/go-sql-driver/mysql"
	"database/sql"
)

// ProxySQLOptions 存在控制器 remark JSON。
type ProxySQLOptions struct {
	WriterHG  int `json:"writer_hostgroup"`
	ReaderHG  int `json:"reader_hostgroup"`
	AdminPort int `json:"admin_port"`
}

func ParseProxySQLOptions(remark string) ProxySQLOptions {
	opt := ProxySQLOptions{WriterHG: 10, ReaderHG: 20, AdminPort: 6032}
	s := strings.TrimSpace(remark)
	if strings.HasPrefix(s, "{") {
		_ = json.Unmarshal([]byte(s), &opt)
	}
	if opt.WriterHG <= 0 {
		opt.WriterHG = 10
	}
	if opt.ReaderHG <= 0 {
		opt.ReaderHG = 20
	}
	if opt.AdminPort <= 0 {
		opt.AdminPort = 6032
	}
	return opt
}

func proxySQLAdminAddr(ctrl ControllerAuth, opt ProxySQLOptions) (host string, port int, err error) {
	raw := strings.TrimSpace(ctrl.BaseURL)
	raw = strings.TrimPrefix(raw, "mysql://")
	raw = strings.TrimPrefix(raw, "http://")
	raw = strings.TrimPrefix(raw, "https://")
	if h, p, ok := strings.Cut(raw, ":"); ok {
		host = h
		port, _ = strconv.Atoi(strings.TrimSuffix(p, "/"))
	} else {
		host = strings.TrimSuffix(raw, "/")
	}
	if host == "" {
		return "", 0, fmt.Errorf("ProxySQL Base URL 无效")
	}
	if port <= 0 {
		port = opt.AdminPort
	}
	return host, port, nil
}

// ProxySQLPromoteWriter 将新主放入 writer HG，旧主移入 reader（计划切换后流量切换）。
func ProxySQLPromoteWriter(ctx context.Context, ctrl ControllerAuth, newMaster, oldMaster *store.Instance) error {
	opt := ParseProxySQLOptions(ctrl.AppConf)
	host, port, err := proxySQLAdminAddr(ctrl, opt)
	if err != nil {
		return err
	}
	newH := strings.TrimSpace(newMaster.Host)
	if newH == "" {
		newH = strings.TrimSpace(newMaster.DomainName)
	}
	if newH == "" || newMaster.Port <= 0 {
		return fmt.Errorf("新主缺少 host/port")
	}
	oldH, oldP := "", 0
	if oldMaster != nil {
		oldH = strings.TrimSpace(oldMaster.Host)
		if oldH == "" {
			oldH = strings.TrimSpace(oldMaster.DomainName)
		}
		oldP = oldMaster.Port
	}
	user := strings.TrimSpace(ctrl.Username)
	pass := ctrl.Password
	if user == "" {
		return fmt.Errorf("ProxySQL 须配置 Admin 用户名（勿依赖默认 admin/admin）")
	}
	dsn := fmt.Sprintf("%s:%s@tcp(%s)/main?timeout=10s&readTimeout=15s&parseTime=true&interpolateParams=true",
		user, pass, net.JoinHostPort(host, strconv.Itoa(port)))
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(0)
	db.SetConnMaxLifetime(20 * time.Second)
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := db.PingContext(cctx); err != nil {
		return fmt.Errorf("连接 ProxySQL Admin 失败: %w", err)
	}
	exec := func(q string, args ...any) error {
		_, e := db.ExecContext(cctx, q, args...)
		return e
	}
	// 清空原 writer HG，避免双写；新主独占 writer
	if err := exec(`DELETE FROM mysql_servers WHERE hostgroup_id=?`, opt.WriterHG); err != nil {
		return fmt.Errorf("清理 writer HG: %w", err)
	}
	if err := exec(`INSERT INTO mysql_servers(hostgroup_id,hostname,port,status,weight) VALUES(?,?,?,'ONLINE',1000)`,
		opt.WriterHG, newH, newMaster.Port); err != nil {
		return fmt.Errorf("写入 writer HG: %w", err)
	}
	_ = exec(`DELETE FROM mysql_servers WHERE hostgroup_id=? AND hostname=? AND port=?`, opt.ReaderHG, newH, newMaster.Port)
	if oldH != "" && oldP > 0 {
		_ = exec(`DELETE FROM mysql_servers WHERE hostgroup_id=? AND hostname=? AND port=?`, opt.ReaderHG, oldH, oldP)
		_ = exec(`INSERT INTO mysql_servers(hostgroup_id,hostname,port,status,weight) VALUES(?,?,?,'ONLINE',100)`,
			opt.ReaderHG, oldH, oldP)
	}
	if err := exec(`LOAD MYSQL SERVERS TO RUNTIME`); err != nil {
		return err
	}
	_ = exec(`SAVE MYSQL SERVERS TO DISK`)
	return nil
}

// ProxySQLEnvCheck 探测 Admin 连通。
func ProxySQLEnvCheck(ctx context.Context, ctrl ControllerAuth) (ok bool, summary string, checks []map[string]any) {
	ok = true
	add := func(name string, good bool, detail string) {
		checks = append(checks, map[string]any{"name": name, "ok": good, "detail": detail})
		if !good {
			ok = false
		}
	}
	opt := ParseProxySQLOptions(ctrl.AppConf)
	host, port, err := proxySQLAdminAddr(ctrl, opt)
	if err != nil {
		add("addr", false, err.Error())
		return false, "ProxySQL 地址无效", checks
	}
	add("addr", true, net.JoinHostPort(host, strconv.Itoa(port)))
	user := firstNonEmptyHA(ctrl.Username, "")
	if user == "" {
		add("auth", false, "须配置 Admin 用户名")
		return false, "ProxySQL 未配置鉴权", checks
	}
	dsn := fmt.Sprintf("%s:%s@tcp(%s)/main?timeout=5s&interpolateParams=true",
		user, ctrl.Password, net.JoinHostPort(host, strconv.Itoa(port)))
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		add("connect", false, err.Error())
		return false, "无法打开连接", checks
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(0)
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if err := db.PingContext(cctx); err != nil {
		add("connect", false, err.Error())
		return false, "ProxySQL Admin 不可达", checks
	}
	add("connect", true, "ping ok")
	var n int
	_ = db.QueryRowContext(cctx, `SELECT COUNT(*) FROM mysql_servers`).Scan(&n)
	add("mysql_servers", true, fmt.Sprintf("%d 行", n))
	summary = "ProxySQL 环境检查通过"
	return ok, summary, checks
}
