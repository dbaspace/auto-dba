package ha

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v4/stdlib"

	"auto-dba/internal/store"
)

var instanceLoader func(instanceID string) (*store.Instance, error)

func SetInstanceLoader(fn func(instanceID string) (*store.Instance, error)) {
	instanceLoader = fn
}

func loadInstance(instanceID string) (*store.Instance, error) {
	if instanceLoader == nil {
		return nil, fmt.Errorf("instance loader 未设置")
	}
	return instanceLoader(instanceID)
}

func openMySQL(host string, port int, user, password string) (*sql.DB, error) {
	cfg := mysql.NewConfig()
	cfg.User = user
	cfg.Passwd = password
	cfg.Net = "tcp"
	cfg.Addr = fmt.Sprintf("%s:%d", host, port)
	cfg.Timeout = 8 * time.Second
	cfg.ReadTimeout = 15 * time.Second
	cfg.Params = map[string]string{"charset": "utf8mb4", "parseTime": "true"}
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(20 * time.Second)
	return db, nil
}

func openPostgres(host string, port int, user, password string) (*sql.DB, error) {
	u := fmt.Sprintf("postgres://%s@%s:%d/postgres?sslmode=disable&connect_timeout=8",
		urlUserInfo(user, password), host, port)
	db, err := sql.Open("pgx", u)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(20 * time.Second)
	return db, nil
}

func urlUserInfo(user, password string) string {
	// 简单转义
	esc := func(s string) string {
		r := strings.NewReplacer("@", "%40", ":", "%3A", "/", "%2F", "?", "%3F", "#", "%23")
		return r.Replace(s)
	}
	if password == "" {
		return esc(user)
	}
	return esc(user) + ":" + esc(password)
}

func memberBase(m store.HAMember) MemberLive {
	return MemberLive{
		InstanceID:   m.InstanceID,
		InstanceName: m.InstanceName,
		Host:         strings.TrimSpace(m.Host),
		Port:         m.Port,
		Role:         m.Role,
		Registered:   m.Role,
		Healthy:      false,
	}
}

func probeMySQL(ctx context.Context, cluster *store.HACluster, passwordFn PasswordFn, out *Topology) (*Topology, error) {
	for i := range cluster.Members {
		m := cluster.Members[i]
		live := memberBase(m)
		inst, err := loadInstance(m.InstanceID)
		if err != nil || inst == nil {
			live.Error = "无法加载实例"
			out.Members = append(out.Members, live)
			continue
		}
		pass, err := passwordFn(inst)
		if err != nil {
			live.Error = err.Error()
			out.Members = append(out.Members, live)
			continue
		}
		host := strings.TrimSpace(inst.Host)
		if host == "" {
			host = strings.TrimSpace(inst.DomainName)
		}
		db, err := openMySQL(host, inst.Port, inst.Username, pass)
		if err != nil {
			live.Error = err.Error()
			out.Members = append(out.Members, live)
			continue
		}
		func() {
			defer db.Close()
			if err := db.PingContext(ctx); err != nil {
				live.Error = err.Error()
				return
			}
			live.Healthy = true
			live.Host = host
			live.Port = inst.Port
			mode := strings.ToLower(strings.TrimSpace(cluster.Mode))
			if mode == "mgr" {
				ok, role, detail, warn := readMGRMember(ctx, db, host, inst.Port)
				if warn != "" {
					out.Warnings = appendWarning(out.Warnings, warn)
				}
				if ok {
					live.Role = role
					live.Detail = detail
					return
				}
			}
			if mode == "pxc" {
				ok, role, detail, warn := readPXCStatus(ctx, db)
				if warn != "" {
					out.Warnings = appendWarning(out.Warnings, warn)
				}
				if ok {
					live.Role = role
					live.Detail = detail
					return
				}
			}
			if filled, detail, lag := readMySQLReplicaStatus(ctx, db); filled {
				live.Role = "replica"
				live.Detail = detail
				live.LagSeconds = lag
				return
			}
			live.Role = "primary"
			var ro int
			if err := db.QueryRowContext(ctx, `SELECT @@read_only`).Scan(&ro); err == nil && ro == 1 {
				live.Detail = "疑似主库; read_only=ON"
			} else {
				live.Detail = "无副本状态（疑似主库）"
			}
		}()
		out.Members = append(out.Members, live)
	}
	return out, nil
}

func readMySQLReplicaStatus(ctx context.Context, db *sql.DB) (bool, string, *float64) {
	for _, q := range []string{`SHOW REPLICA STATUS`, `SHOW SLAVE STATUS`} {
		rows, err := db.QueryContext(ctx, q)
		if err != nil {
			continue
		}
		cols, err := rows.Columns()
		if err != nil {
			rows.Close()
			continue
		}
		if !rows.Next() {
			rows.Close()
			return false, "", nil
		}
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			rows.Close()
			continue
		}
		rows.Close()
		get := func(names ...string) any {
			for _, name := range names {
				for i, c := range cols {
					if strings.EqualFold(c, name) {
						return vals[i]
					}
				}
			}
			return nil
		}
		io := stringify(get("Replica_IO_Running", "Slave_IO_Running"))
		sqlr := stringify(get("Replica_SQL_Running", "Slave_SQL_Running"))
		lag := toFloatPtr(get("Seconds_Behind_Source", "Seconds_Behind_Master"))
		return true, fmt.Sprintf("IO=%s SQL=%s", io, sqlr), lag
	}
	return false, "", nil
}

func appendWarning(list []string, msg string) []string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return list
	}
	for _, x := range list {
		if x == msg {
			return list
		}
	}
	return append(list, msg)
}

func readMGRMember(ctx context.Context, db *sql.DB, host string, port int) (ok bool, role, detail, warn string) {
	rows, err := db.QueryContext(ctx, `
		SELECT MEMBER_HOST, MEMBER_PORT, MEMBER_STATE, MEMBER_ROLE
		FROM performance_schema.replication_group_members`)
	if err != nil {
		return false, "", "", "MGR 探测失败（无组复制表或无权限）: " + err.Error()
	}
	defer rows.Close()
	var matched bool
	var anyRow bool
	for rows.Next() {
		anyRow = true
		var mh string
		var mp int
		var state, mrole string
		if err := rows.Scan(&mh, &mp, &state, &mrole); err != nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(mh), strings.TrimSpace(host)) && (mp == port || mp == 0) {
			matched = true
			role = "replica"
			if strings.EqualFold(mrole, "PRIMARY") {
				role = "primary"
			}
			detail = fmt.Sprintf("MGR %s/%s", strings.TrimSpace(mrole), strings.TrimSpace(state))
			if !strings.EqualFold(state, "ONLINE") {
				warn = "存在非 ONLINE 的 MGR 成员: " + state
			}
		}
	}
	if !anyRow {
		return false, "", "", "MGR 组内无成员行，请确认 mode 是否为 mgr"
	}
	if !matched {
		return true, "replica", "已连上但未匹配到本组 MEMBER_HOST", "MGR 成员地址与实例 host 不一致"
	}
	return true, role, detail, warn
}

func readPXCStatus(ctx context.Context, db *sql.DB) (ok bool, role, detail, warn string) {
	rows, err := db.QueryContext(ctx, `
		SHOW STATUS WHERE Variable_name IN (
			'wsrep_ready','wsrep_cluster_size','wsrep_local_state_comment','wsrep_cluster_status'
		)`)
	if err != nil {
		return false, "", "", "PXC 探测失败: " + err.Error()
	}
	defer rows.Close()
	vals := map[string]string{}
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) == nil {
			vals[strings.ToLower(k)] = v
		}
	}
	if len(vals) == 0 {
		return false, "", "", "未读到 wsrep 状态，请确认是否为 PXC/Galera"
	}
	ready := vals["wsrep_ready"]
	size := vals["wsrep_cluster_size"]
	st := vals["wsrep_local_state_comment"]
	cs := vals["wsrep_cluster_status"]
	detail = fmt.Sprintf("wsrep_ready=%s size=%s state=%s cluster=%s", ready, size, st, cs)
	role = "replica"
	if strings.EqualFold(cs, "Primary") && (strings.EqualFold(st, "Synced") || strings.EqualFold(st, "Donor/Desynced") || st != "") {
		var ro int
		_ = db.QueryRowContext(ctx, `SELECT @@read_only`).Scan(&ro)
		if ro == 0 {
			role = "primary"
		}
	}
	if !strings.EqualFold(ready, "ON") {
		warn = "wsrep_ready 非 ON"
	}
	return true, role, detail, warn
}

func stringify(v any) string {
	if v == nil {
		return "-"
	}
	switch t := v.(type) {
	case []byte:
		return string(t)
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

func toFloatPtr(v any) *float64 {
	if v == nil {
		return nil
	}
	var f float64
	switch t := v.(type) {
	case float64:
		f = t
	case int64:
		f = float64(t)
	case []byte:
		if _, err := fmt.Sscanf(string(t), "%f", &f); err != nil {
			return nil
		}
	case string:
		if _, err := fmt.Sscanf(t, "%f", &f); err != nil {
			return nil
		}
	default:
		return nil
	}
	return &f
}
