package dbdeploy

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"auto-dba/internal/dbmig"
	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

// ProbeNode 探测到的集群节点。
type ProbeNode struct {
	Role   string `json:"role"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
	Source string `json:"source,omitempty"` // connected | replication | instance
}

// InstanceProbe 用资源配置实例账号只读探测的结果，用于登记已有集群时自动填写。
type InstanceProbe struct {
	Engine        string      `json:"engine"`
	Version       string      `json:"version"`
	Major         string      `json:"major"`
	BaseDir       string      `json:"base_dir"`
	DataDir       string      `json:"data_dir"`
	Port          int         `json:"port"`
	Topology      string      `json:"topology"`
	ConnectedRole string      `json:"connected_role"`
	Nodes         []ProbeNode `json:"nodes"`
	Warnings      []string    `json:"warnings,omitempty"`
}

var (
	probeMySQLVerRe = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)`)
	probePGVerRe    = regexp.MustCompile(`^(\d+)(?:\.(\d+))?`)
)

// NormalizeProbedVersion MySQL 去掉 -log / -28 等后缀取 x.y.z；PG 取 x.y（如 "16.4 (Debian 16.4-1)" → 16.4）。
func NormalizeProbedVersion(engine, raw string) string {
	v := strings.TrimSpace(raw)
	switch engine {
	case store.DbDeployEngineMySQL:
		if m := probeMySQLVerRe.FindString(v); m != "" {
			return m
		}
	case store.DbDeployEnginePostgres:
		if m := probePGVerRe.FindString(v); m != "" {
			return m
		}
	}
	return v
}

// InstanceAddrs 实例登记的地址列表（逗号分隔多节点；带 :port 时拆出端口）。
func InstanceAddrs(inst *store.Instance) []ProbeNode {
	raw := strings.TrimSpace(inst.Host)
	if raw == "" {
		raw = strings.TrimSpace(inst.DomainName)
	}
	var out []ProbeNode
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '，' || r == ' ' }) {
		host, port := splitHostPortLoose(part, inst.Port)
		if host != "" {
			out = append(out, ProbeNode{Host: host, Port: port})
		}
	}
	return out
}

func splitHostPortLoose(s string, defPort int) (string, int) {
	s = strings.TrimSpace(s)
	if h, p, err := net.SplitHostPort(s); err == nil {
		if n, e := strconv.Atoi(p); e == nil && n > 0 {
			return h, n
		}
		return h, defPort
	}
	return strings.Trim(s, "[]"), defPort
}

func nodeKey(host string, port int) string {
	return strings.ToLower(strings.TrimSpace(host)) + ":" + strconv.Itoa(port)
}

// MergeProbeNodes 合并当前连接节点、复制上游、下游从库与实例登记的其余地址，去重并确定拓扑。
// 角色只有 primary / replica：单机即一个没有从库的 primary。端口为 0 表示未探测到，不参与去重。
func MergeProbeNodes(self ProbeNode, selfIsReplica bool, upstream *ProbeNode, replicas []ProbeNode, extra []ProbeNode) (nodes []ProbeNode, topology string, warnings []string) {
	seen := map[string]bool{}
	add := func(n ProbeNode) bool {
		if strings.TrimSpace(n.Host) == "" {
			return false
		}
		if n.Port > 0 {
			k := nodeKey(n.Host, n.Port)
			if seen[k] {
				return false
			}
			seen[k] = true
		}
		nodes = append(nodes, n)
		return true
	}
	self.Role, self.Source = "primary", "connected"
	if selfIsReplica {
		if upstream != nil {
			add(ProbeNode{Role: "primary", Host: upstream.Host, Port: upstream.Port, Source: "replication"})
		} else {
			warnings = append(warnings, "当前实例是从库，但未取到主库地址，请补充主库节点")
		}
		self.Role = "replica"
	}
	add(self)
	replicated := false
	for _, r := range replicas {
		r.Role, r.Source = "replica", "replication"
		if add(r) {
			replicated = true
			if r.Port <= 0 {
				warnings = append(warnings, fmt.Sprintf("从库 %s 的端口无法探测，请填写", r.Host))
			}
		}
	}
	extraAdded := false
	for _, e := range extra {
		e.Role, e.Source = "replica", "instance"
		if add(e) {
			extraAdded = true
			warnings = append(warnings, fmt.Sprintf("%s:%d 来自实例登记地址，探测中未出现，请确认是否属于本集群", e.Host, e.Port))
		}
	}
	if extraAdded && !selfIsReplica && !replicated {
		warnings = append(warnings, "实例登记了多个地址但未探测到复制关系，已按「第一个为主、其余为从」生成，请核对")
	}
	if selfIsReplica || len(nodes) > 1 {
		return nodes, store.DbDeployTopoMasterReplica, warnings
	}
	return nodes, store.DbDeployTopoStandalone, warnings
}

var (
	pgDebianConfRe = regexp.MustCompile(`^/etc/postgresql/(\d+)/`)
	pgRHELDataRe   = regexp.MustCompile(`^/var/lib/pgsql/(\d+)/data`)
)

// pgBaseFromBinDir BINDIR 的上一级即软件目录（/usr/lib/postgresql/15/bin → /usr/lib/postgresql/15）。
func pgBaseFromBinDir(bindir string) string {
	b := strings.TrimRight(strings.TrimSpace(bindir), "/")
	if b == "" || !strings.HasPrefix(b, "/") {
		return ""
	}
	if i := strings.LastIndex(b, "/"); i > 0 {
		return b[:i]
	}
	return ""
}

// pgBaseFromLayout 按发行版默认布局推断：Debian/Ubuntu 与 RHEL PGDG。
func pgBaseFromLayout(configFile, dataDir string) string {
	if m := pgDebianConfRe.FindStringSubmatch(strings.TrimSpace(configFile)); m != nil {
		return "/usr/lib/postgresql/" + m[1]
	}
	for _, p := range []string{dataDir, configFile} {
		if m := pgRHELDataRe.FindStringSubmatch(strings.TrimSpace(p)); m != nil {
			return "/usr/pgsql-" + m[1]
		}
	}
	return ""
}

func pgSoftwareBase(ctx context.Context, db *sql.DB, p *InstanceProbe, user string) {
	var bindir string
	if err := db.QueryRowContext(ctx, `SELECT setting FROM pg_config WHERE name = 'BINDIR'`).Scan(&bindir); err == nil {
		if b := pgBaseFromBinDir(bindir); b != "" {
			p.BaseDir = b
			return
		}
	}
	var cfg string
	_ = db.QueryRowContext(ctx, `SHOW config_file`).Scan(&cfg)
	if b := pgBaseFromLayout(cfg, p.DataDir); b != "" {
		p.BaseDir = b
		p.Warnings = append(p.Warnings, fmt.Sprintf("软件目录 %s 按发行版默认布局推断，请核对", b))
		return
	}
	p.Warnings = append(p.Warnings, fmt.Sprintf("无法读取软件目录：pg_config 需超级用户（可执行 GRANT SELECT ON pg_config TO %s 后重新探测），请手动填写实际安装目录", user))
}

// ProbeInstance 用实例账号连库，只执行只读查询。
func ProbeInstance(ctx context.Context, inst *store.Instance, secretKey string) (*InstanceProbe, error) {
	engine := store.NormalizeDBType(inst.DBType)
	if engine != store.DbDeployEngineMySQL && engine != store.DbDeployEnginePostgres {
		return nil, fmt.Errorf("仅支持 MySQL / PostgreSQL 实例，当前 %s", inst.DBType)
	}
	addrs := InstanceAddrs(inst)
	if len(addrs) == 0 {
		return nil, fmt.Errorf("实例未配置主机")
	}
	if strings.TrimSpace(inst.Username) == "" {
		return nil, fmt.Errorf("实例未配置账号，请先在资源配置中补充")
	}
	pwd, err := secret.Decrypt(secretKey, inst.Password)
	if err != nil {
		return nil, fmt.Errorf("解密实例密码失败: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	self := addrs[0]
	db, err := dbmig.Open(ctx, engine, self.Host, self.Port, inst.Username, pwd, "")
	if err != nil {
		return nil, fmt.Errorf("连接 %s:%d 失败: %w", self.Host, self.Port, err)
	}
	defer db.Close()

	p := &InstanceProbe{Engine: engine, Port: self.Port}
	var (
		selfIsReplica bool
		upstream      *ProbeNode
		replicas      []ProbeNode
	)
	if engine == store.DbDeployEngineMySQL {
		selfIsReplica, upstream, replicas = probeMySQL(ctx, db, p, self.Port)
	} else {
		selfIsReplica, upstream, replicas = probePG(ctx, db, p, self.Port)
		pgSoftwareBase(ctx, db, p, inst.Username)
	}
	if p.Port > 0 {
		self.Port = p.Port
	}
	p.Major = MajorVersion(engine, p.Version)
	nodes, topo, warns := MergeProbeNodes(self, selfIsReplica, upstream, replicas, addrs[1:])
	p.Nodes, p.Topology = nodes, topo
	p.Warnings = append(p.Warnings, warns...)
	for _, n := range nodes {
		if n.Source == "connected" {
			p.ConnectedRole = n.Role
		}
	}
	return p, nil
}

// queryMaps 把结果集读成 列名→字符串（列名小写）。
func queryMaps(ctx context.Context, db *sql.DB, q string) ([]map[string]string, error) {
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out []map[string]string
	for rows.Next() {
		vals := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		m := make(map[string]string, len(cols))
		for i, c := range cols {
			m[strings.ToLower(c)] = vals[i].String
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func firstNonEmpty(m map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(m[k]); v != "" {
			return v
		}
	}
	return ""
}

func probeMySQL(ctx context.Context, db *sql.DB, p *InstanceProbe, defPort int) (bool, *ProbeNode, []ProbeNode) {
	var ver, base, data sql.NullString
	var port, ro sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT VERSION(), @@basedir, @@datadir, @@port, @@read_only`).Scan(&ver, &base, &data, &port, &ro); err != nil {
		p.Warnings = append(p.Warnings, "读取版本与目录失败: "+err.Error())
	}
	p.Version = NormalizeProbedVersion(store.DbDeployEngineMySQL, ver.String)
	p.BaseDir = strings.TrimRight(base.String, "/")
	p.DataDir = strings.TrimRight(data.String, "/")
	if port.Valid && port.Int64 > 0 {
		p.Port = int(port.Int64)
		defPort = p.Port
	}

	var upstream *ProbeNode
	status, err := queryMaps(ctx, db, `SHOW REPLICA STATUS`)
	if err != nil {
		status, err = queryMaps(ctx, db, `SHOW SLAVE STATUS`)
	}
	if err != nil {
		p.Warnings = append(p.Warnings, "无法读取复制状态（账号可能缺少 REPLICATION CLIENT 权限）: "+err.Error())
	}
	isReplica := len(status) > 0
	if isReplica {
		host := firstNonEmpty(status[0], "source_host", "master_host")
		pt, _ := strconv.Atoi(firstNonEmpty(status[0], "source_port", "master_port"))
		if pt <= 0 {
			pt = defPort
		}
		if host != "" {
			upstream = &ProbeNode{Host: host, Port: pt}
		}
	}

	var replicas []ProbeNode
	hosts, err := queryMaps(ctx, db, `SHOW REPLICAS`)
	if err != nil {
		hosts, err = queryMaps(ctx, db, `SHOW SLAVE HOSTS`)
	}
	if err == nil {
		for _, h := range hosts {
			host := firstNonEmpty(h, "host")
			pt, _ := strconv.Atoi(firstNonEmpty(h, "port"))
			if pt <= 0 {
				pt = defPort
			}
			if host != "" {
				replicas = append(replicas, ProbeNode{Host: host, Port: pt})
			}
		}
	}
	if len(replicas) == 0 {
		dumps, err := queryMaps(ctx, db, `SELECT HOST FROM information_schema.PROCESSLIST WHERE COMMAND IN ('Binlog Dump','Binlog Dump GTID')`)
		if err == nil && len(dumps) > 0 {
			for _, d := range dumps {
				host, _ := splitHostPortLoose(d["host"], 0)
				if host != "" {
					replicas = append(replicas, ProbeNode{Host: host})
				}
			}
			p.Warnings = append(p.Warnings, "从库未配置 report_host，按 Binlog Dump 连接的来源 IP 识别，请核对")
		}
	}
	if !isReplica && ro.Valid && ro.Int64 == 1 && len(replicas) == 0 {
		p.Warnings = append(p.Warnings, "实例为 read_only 但未发现复制上游，请确认角色")
	}
	return isReplica, upstream, replicas
}

func probePG(ctx context.Context, db *sql.DB, p *InstanceProbe, defPort int) (bool, *ProbeNode, []ProbeNode) {
	var ver string
	if err := db.QueryRowContext(ctx, `SHOW server_version`).Scan(&ver); err != nil {
		p.Warnings = append(p.Warnings, "读取版本失败: "+err.Error())
	}
	p.Version = NormalizeProbedVersion(store.DbDeployEnginePostgres, ver)
	var dataDir string
	if err := db.QueryRowContext(ctx, `SHOW data_directory`).Scan(&dataDir); err != nil {
		p.Warnings = append(p.Warnings, "无法读取 data_directory（需超级用户或 pg_read_all_settings）")
	}
	p.DataDir = strings.TrimRight(dataDir, "/")
	var portStr string
	if err := db.QueryRowContext(ctx, `SHOW port`).Scan(&portStr); err == nil {
		if n, _ := strconv.Atoi(strings.TrimSpace(portStr)); n > 0 {
			p.Port = n
			defPort = n
		}
	}
	var inRecovery bool
	if err := db.QueryRowContext(ctx, `SELECT pg_is_in_recovery()`).Scan(&inRecovery); err != nil {
		p.Warnings = append(p.Warnings, "无法判断主备角色: "+err.Error())
	}

	var upstream *ProbeNode
	var replicas []ProbeNode
	if inRecovery {
		var host string
		var port int
		err := db.QueryRowContext(ctx, `SELECT COALESCE(sender_host,''), COALESCE(sender_port,0) FROM pg_stat_wal_receiver`).Scan(&host, &port)
		if err == nil && host != "" {
			if port <= 0 {
				port = defPort
			}
			upstream = &ProbeNode{Host: host, Port: port}
		}
		return true, upstream, nil
	}
	rows, err := queryMaps(ctx, db, `SELECT COALESCE(host(client_addr),'') AS addr FROM pg_stat_replication`)
	if err != nil {
		p.Warnings = append(p.Warnings, "无法读取 pg_stat_replication: "+err.Error())
	}
	for _, r := range rows {
		if h := strings.TrimSpace(r["addr"]); h != "" {
			replicas = append(replicas, ProbeNode{Host: h})
		}
	}
	return false, nil, replicas
}
