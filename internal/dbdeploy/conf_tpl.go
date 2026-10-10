package dbdeploy

import (
	"bytes"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/template"

	"auto-dba/internal/store"
)

// TemplatedConfEngine 参数走「平台模板 + 集群覆盖 + 平台托管项」渲染的引擎。
func TemplatedConfEngine(engine string) bool {
	return engine == store.DbDeployEngineMySQL || engine == store.DbDeployEnginePostgres
}

// ConfTplVars 模板可用变量。
type ConfTplVars struct {
	BaseDir  string
	DataDir  string
	LogDir   string
	Port     int
	Host     string
	ServerID int
	Role     string
	Topology string
	Version  string
	Major    string
}

// ConfTplVarNames 页面展示用。
var ConfTplVarNames = []string{".BaseDir", ".DataDir", ".LogDir", ".Port", ".Host", ".ServerID", ".Role", ".Topology", ".Version", ".Major"}

var mysqlManagedKeys = []string{
	"basedir", "datadir", "port", "socket", "server_id", "read_only", "super_read_only",
	"rpl_semi_sync_master_enabled", "rpl_semi_sync_slave_enabled", "rpl_semi_sync_master_timeout",
	"rpl_semi_sync_source_enabled", "rpl_semi_sync_replica_enabled", "rpl_semi_sync_source_timeout",
}

var pgManagedKeys = []string{"port", "data_directory", "primary_conninfo", "hot_standby", "synchronous_standby_names"}

// ManagedConfKeys 平台按拓扑 / 角色托管的参数，模板与覆盖中不允许出现。
func ManagedConfKeys(engine string) []string {
	switch engine {
	case store.DbDeployEngineMySQL:
		return mysqlManagedKeys
	case store.DbDeployEnginePostgres:
		return pgManagedKeys
	}
	return nil
}

// BuiltinConfMajors 内置模板的大版本。
func BuiltinConfMajors(engine string) []string {
	switch engine {
	case store.DbDeployEngineMySQL:
		return []string{"5.7", "8.0", "8.4"}
	case store.DbDeployEnginePostgres:
		return []string{store.DbDeployConfTemplateAnyMajor}
	}
	return nil
}

// builtinMajorFor 把集群大版本映射到内置模板：MySQL 8.1–8.3 用 8.0，8.4 及 9.x 用 8.4。
func builtinMajorFor(engine, major string) string {
	if engine != store.DbDeployEngineMySQL {
		return store.DbDeployConfTemplateAnyMajor
	}
	parts := strings.SplitN(major, ".", 2)
	maj, min := atoiSafe(parts[0]), 0
	if len(parts) == 2 {
		min = atoiSafe(parts[1])
	}
	switch {
	case maj == 5:
		return "5.7"
	case maj > 8 || (maj == 8 && min >= 4):
		return "8.4"
	default:
		return "8.0"
	}
}

// BuiltinConfTemplate 内置模板正文。
func BuiltinConfTemplate(engine, major string) (string, bool) {
	switch engine {
	case store.DbDeployEngineMySQL:
		switch major {
		case "5.7":
			return builtinMySQL57, true
		case "8.0":
			return builtinMySQL80, true
		case "8.4":
			return builtinMySQL84, true
		}
	case store.DbDeployEnginePostgres:
		if major == store.DbDeployConfTemplateAnyMajor {
			return builtinPG, true
		}
	}
	return "", false
}

const mysqlTplHeader = `# 可用变量（用双花括号包裹引用）：.BaseDir .DataDir .LogDir .Port .Host .ServerID .Role .Topology .Version .Major
# basedir / datadir / port / socket / server_id / read_only / 半同步 由平台按拓扑与角色追加到 [mysqld] 段尾，请勿在此填写
`

const builtinMySQL57 = `# MySQL 5.7 参数模板（auto-dba 内置）
` + mysqlTplHeader + `[client]
default-character-set = utf8mb4

[mysqld]
character_set_server = utf8mb4
collation_server = utf8mb4_general_ci
skip_name_resolve = ON
max_connections = 2000
max_connect_errors = 100000
transaction_isolation = READ-COMMITTED

# InnoDB
innodb_buffer_pool_size = 1G
innodb_flush_log_at_trx_commit = 1
innodb_flush_method = O_DIRECT
innodb_file_per_table = ON
innodb_io_capacity = 2000

# binlog / 复制
log_bin = mysql-bin
binlog_format = ROW
binlog_row_image = FULL
sync_binlog = 1
gtid_mode = ON
enforce_gtid_consistency = ON
log_slave_updates = ON
expire_logs_days = 7
relay_log = relay-bin
relay_log_recovery = ON

# 日志
log_error = {{.LogDir}}/mysqld.err
slow_query_log = ON
slow_query_log_file = {{.LogDir}}/slow.log
long_query_time = 1
pid_file = {{.DataDir}}/mysqld.pid
`

const builtinMySQL80 = `# MySQL 8.0 参数模板（auto-dba 内置）
` + mysqlTplHeader + `[client]
default-character-set = utf8mb4

[mysqld]
character_set_server = utf8mb4
collation_server = utf8mb4_0900_ai_ci
skip_name_resolve = ON
max_connections = 2000
max_connect_errors = 100000
transaction_isolation = READ-COMMITTED

# InnoDB
innodb_buffer_pool_size = 1G
innodb_flush_log_at_trx_commit = 1
innodb_flush_method = O_DIRECT
innodb_file_per_table = ON
innodb_io_capacity = 2000

# binlog / 复制
log_bin = mysql-bin
binlog_format = ROW
binlog_row_image = FULL
sync_binlog = 1
gtid_mode = ON
enforce_gtid_consistency = ON
log_slave_updates = ON
binlog_expire_logs_seconds = 604800
relay_log = relay-bin
relay_log_recovery = ON

# 日志
log_error = {{.LogDir}}/mysqld.err
slow_query_log = ON
slow_query_log_file = {{.LogDir}}/slow.log
long_query_time = 1
pid_file = {{.DataDir}}/mysqld.pid
`

const builtinMySQL84 = `# MySQL 8.4 LTS 参数模板（auto-dba 内置）
` + mysqlTplHeader + `[client]
default-character-set = utf8mb4

[mysqld]
character_set_server = utf8mb4
collation_server = utf8mb4_0900_ai_ci
skip_name_resolve = ON
max_connections = 2000
max_connect_errors = 100000
transaction_isolation = READ-COMMITTED

# InnoDB
innodb_buffer_pool_size = 1G
innodb_flush_log_at_trx_commit = 1
innodb_flush_method = O_DIRECT
innodb_file_per_table = ON
innodb_io_capacity = 2000

# binlog / 复制（8.4 默认 ROW，log_slave_updates 已更名 log_replica_updates）
log_bin = mysql-bin
binlog_row_image = FULL
sync_binlog = 1
gtid_mode = ON
enforce_gtid_consistency = ON
log_replica_updates = ON
binlog_expire_logs_seconds = 604800
relay_log = relay-bin
relay_log_recovery = ON

# 日志
log_error = {{.LogDir}}/mysqld.err
slow_query_log = ON
slow_query_log_file = {{.LogDir}}/slow.log
long_query_time = 1
pid_file = {{.DataDir}}/mysqld.pid
`

const builtinPG = `# PostgreSQL 参数模板（auto-dba 内置，适用于 13+）
# 可用变量（用双花括号包裹引用）：.DataDir .LogDir .Port .Host .Role .Topology .Version .Major
# port / data_directory / primary_conninfo / hot_standby / synchronous_standby_names 由平台按拓扑与角色追加，请勿在此填写
listen_addresses = '*'
max_connections = 500
superuser_reserved_connections = 5

# 内存
shared_buffers = 1GB
effective_cache_size = 3GB
work_mem = 8MB
maintenance_work_mem = 256MB

# WAL / 复制
wal_level = replica
wal_log_hints = on
max_wal_senders = 16
max_replication_slots = 16
wal_keep_size = 1GB
hot_standby_feedback = on
checkpoint_timeout = 15min
max_wal_size = 4GB
min_wal_size = 1GB

# 日志
logging_collector = on
log_directory = 'log'
log_filename = 'postgresql-%Y-%m-%d.log'
log_rotation_age = 1d
log_min_duration_statement = 1000
log_line_prefix = '%m [%p] %u@%d '
`

// confKey 解析一行参数名（注释 / 空行 / 段头返回空）。MySQL 中 - 与 _ 等价，loose- 前缀不影响判定。
func confKey(engine, line string) string {
	t := strings.TrimSpace(line)
	if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, ";") || strings.HasPrefix(t, "[") {
		return ""
	}
	k := t
	if i := strings.Index(t, "="); i >= 0 {
		k = t[:i]
	}
	k = strings.ToLower(strings.TrimSpace(k))
	if engine == store.DbDeployEngineMySQL {
		k = strings.ReplaceAll(k, "-", "_")
		k = strings.TrimPrefix(k, "loose_")
	}
	return k
}

func isManagedKey(engine, key string) bool {
	for _, m := range ManagedConfKeys(engine) {
		if m == key {
			return true
		}
	}
	return false
}

// ValidateConfTemplate 模板：不得含托管键，且能用示例变量渲染。
func ValidateConfTemplate(engine, content string) error {
	if err := rejectManagedKeys(engine, content); err != nil {
		return err
	}
	if _, err := renderConfTpl(content, ConfTplVars{
		BaseDir: "/usr/local/x", DataDir: "/data/x", LogDir: "/data/x/log", Port: 3306,
		Host: "127.0.0.1", ServerID: 1, Role: "primary", Topology: "standalone", Version: "0", Major: "0",
	}); err != nil {
		return fmt.Errorf("模板无法渲染: %w", err)
	}
	return nil
}

// ValidateClusterConf 集群覆盖：只允许 key = value（MySQL 允许无值开关），不允许段头、托管键与模板变量。
func ValidateClusterConf(engine, content string) error {
	for i, line := range strings.Split(content, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, ";") {
			continue
		}
		if strings.HasPrefix(t, "[") {
			return fmt.Errorf("第 %d 行：覆盖内容不支持段头 %s（MySQL 覆盖统一作用于 [mysqld]）", i+1, t)
		}
		if strings.Contains(t, "{{") {
			return fmt.Errorf("第 %d 行：覆盖内容不支持模板变量", i+1)
		}
		if engine == store.DbDeployEnginePostgres && !strings.Contains(t, "=") {
			return fmt.Errorf("第 %d 行：格式应为 key = value", i+1)
		}
	}
	return rejectManagedKeys(engine, content)
}

func rejectManagedKeys(engine, content string) error {
	var bad []string
	seen := map[string]bool{}
	for _, line := range strings.Split(content, "\n") {
		if k := confKey(engine, line); k != "" && isManagedKey(engine, k) && !seen[k] {
			seen[k] = true
			bad = append(bad, k)
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("以下参数由平台按拓扑与角色托管，请勿填写：%s", strings.Join(bad, ", "))
	}
	return nil
}

func renderConfTpl(content string, vars ConfTplVars) (string, error) {
	tpl, err := template.New("conf").Option("missingkey=error").Parse(content)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, vars); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// mysqldRange [mysqld] 段内容行区间 [start, end)；无该段返回 -1, -1。
func mysqldRange(lines []string) (int, int) {
	start := -1
	for i, l := range lines {
		t := strings.ToLower(strings.TrimSpace(l))
		if strings.HasPrefix(t, "[") {
			if start >= 0 {
				return start, i
			}
			if t == "[mysqld]" {
				start = i + 1
			}
		}
	}
	if start < 0 {
		return -1, -1
	}
	return start, len(lines)
}

// insertAtSectionEnd 在 [start,end) 末尾（跳过尾部空行）插入 block。
func insertAtSectionEnd(lines []string, start, end int, block []string) []string {
	at := end
	for at > start && strings.TrimSpace(lines[at-1]) == "" {
		at--
	}
	out := make([]string, 0, len(lines)+len(block))
	out = append(out, lines[:at]...)
	out = append(out, block...)
	out = append(out, lines[at:]...)
	return out
}

// applyClusterOverrides 合并集群覆盖：同名参数替换最后一次出现，否则追加；MySQL 只在 [mysqld] 段内生效。
func applyClusterOverrides(engine, base, overrides string) string {
	lines := strings.Split(strings.TrimRight(base, "\n"), "\n")
	start, end := 0, len(lines)
	if engine == store.DbDeployEngineMySQL {
		start, end = mysqldRange(lines)
		if start < 0 {
			lines = append(lines, "", "[mysqld]")
			start, end = len(lines), len(lines)
		}
	}
	var appendLines []string
	for _, ov := range strings.Split(overrides, "\n") {
		k := confKey(engine, ov)
		if k == "" {
			continue
		}
		newLine := strings.TrimSpace(ov)
		replaced := false
		for i := end - 1; i >= start; i-- {
			if confKey(engine, lines[i]) == k {
				lines[i] = newLine
				replaced = true
				break
			}
		}
		if !replaced {
			appendLines = append(appendLines, newLine)
		}
	}
	if len(appendLines) > 0 {
		block := append([]string{"# ---- 集群覆盖 ----"}, appendLines...)
		lines = insertAtSectionEnd(lines, start, end, block)
	}
	return strings.Join(lines, "\n") + "\n"
}

// stripManagedLines 去掉模板 / 覆盖中残留的托管键（以平台托管块为准）。
func stripManagedLines(engine, content string) string {
	var out []string
	for _, l := range strings.Split(content, "\n") {
		if k := confKey(engine, l); k != "" && isManagedKey(engine, k) {
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

func mysqlManagedBlock(cl *store.DbDeployCluster, n *store.DbDeployNode, major string, v ConfTplVars) []string {
	b := []string{
		"# ---- auto-dba 托管（按拓扑与角色生成，勿手工修改）----",
		"basedir = " + v.BaseDir,
		"datadir = " + v.DataDir,
		fmt.Sprintf("port = %d", v.Port),
		"socket = " + v.DataDir + "/mysql.sock",
		fmt.Sprintf("server_id = %d", v.ServerID),
	}
	if cl.Topology == store.DbDeployTopoMasterReplica {
		src, rep := "master", "slave"
		if builtinMajorFor(store.DbDeployEngineMySQL, major) == "8.4" {
			src, rep = "source", "replica"
		}
		b = append(b, fmt.Sprintf("plugin-load-add = semisync_%s.so;semisync_%s.so", src, rep))
		if n.Role == "primary" || n.Role == "single" {
			b = append(b,
				fmt.Sprintf("rpl_semi_sync_%s_enabled = 1", src),
				fmt.Sprintf("rpl_semi_sync_%s_timeout = 10000", src),
			)
		} else {
			b = append(b, fmt.Sprintf("rpl_semi_sync_%s_enabled = 1", rep))
		}
	}
	if n.Role == "replica" {
		b = append(b, "read_only = ON", "super_read_only = ON")
	}
	return b
}

func pgManagedBlock(cl *store.DbDeployCluster, n *store.DbDeployNode, primary *store.DbDeployNode, v ConfTplVars) []string {
	b := []string{
		"# ---- auto-dba 托管（按拓扑与角色生成，勿手工修改）----",
		fmt.Sprintf("port = %d", v.Port),
		fmt.Sprintf("data_directory = '%s'", v.DataDir),
	}
	if cl.Topology == store.DbDeployTopoMasterReplica && (n.Role == "primary" || n.Role == "single") {
		b = append(b, "synchronous_standby_names = '*'")
	}
	if n.Role == "replica" && primary != nil {
		b = append(b,
			fmt.Sprintf("primary_conninfo = 'host=%s port=%d user=replicator application_name=%s'", primary.Host, nodePort(cl, primary), n.Host),
			"hot_standby = on",
		)
	}
	return b
}

// ConfTplSource 渲染所用模板来源。
type ConfTplSource struct {
	Kind  string `json:"kind"`  // platform | builtin
	Major string `json:"major"` // 模板大版本，* 为通用
	Label string `json:"label"`
}

func majorLabel(engine, major string) string {
	name := map[string]string{store.DbDeployEngineMySQL: "MySQL", store.DbDeployEnginePostgres: "PostgreSQL"}[engine]
	if major == store.DbDeployConfTemplateAnyMajor {
		return name + " 通用"
	}
	return name + " " + major
}

// effectiveConfTemplate 平台 (engine, major) > 平台 (engine, 内置对应大版本) > 平台 (engine, *) > 内置。
func effectiveConfTemplate(st *store.DB, engine, major string) (string, ConfTplSource) {
	bm := builtinMajorFor(engine, major)
	if st != nil {
		tried := map[string]bool{}
		for _, m := range []string{major, bm, store.DbDeployConfTemplateAnyMajor} {
			if m == "" || tried[m] {
				continue
			}
			tried[m] = true
			if t, err := st.GetDbDeployConfTemplate(engine, m); err == nil && t != nil && strings.TrimSpace(t.Content) != "" {
				return t.Content, ConfTplSource{Kind: "platform", Major: m, Label: "平台模板 · " + majorLabel(engine, m)}
			}
		}
	}
	body, _ := BuiltinConfTemplate(engine, bm)
	return body, ConfTplSource{Kind: "builtin", Major: bm, Label: "内置模板 · " + majorLabel(engine, bm)}
}

// clusterMajor 集群版本对应的大版本；未绑定版本时 MySQL 按 8.0。
func clusterMajor(cl *store.DbDeployCluster) string {
	if v := strings.TrimSpace(cl.Version); v != "" {
		return MajorVersion(cl.Engine, v)
	}
	if cl.Engine == store.DbDeployEngineMySQL {
		return "8.0"
	}
	return store.DbDeployConfTemplateAnyMajor
}

// RenderTemplatedConf MySQL / PG：模板 → 变量渲染 → 集群覆盖 → 剔除并追加托管项。
// overrides 为 nil 时读取已保存的集群覆盖。
func RenderTemplatedConf(st *store.DB, cl *store.DbDeployCluster, n *store.DbDeployNode, primary *store.DbDeployNode, overrides *string) (confPath, content string, src ConfTplSource, err error) {
	soft, data, port := softBase(cl, n), dataRoot(cl), nodePort(cl, n)
	major := clusterMajor(cl)
	vars := ConfTplVars{
		BaseDir: soft, DataDir: data, LogDir: data + "/log", Port: port, Host: n.Host,
		ServerID: serverID(n.Host, port), Role: n.Role, Topology: cl.Topology, Version: cl.Version, Major: major,
	}
	tpl, src := effectiveConfTemplate(st, cl.Engine, major)
	body, err := renderConfTpl(tpl, vars)
	if err != nil {
		return "", "", src, fmt.Errorf("%s 渲染失败: %w", src.Label, err)
	}
	ov := ""
	if overrides != nil {
		ov = *overrides
	} else if st != nil {
		if c, e := st.GetDbDeployClusterConf(cl.ID); e == nil && c != nil {
			ov = c.Content
		}
	}
	if strings.TrimSpace(ov) != "" {
		if err := ValidateClusterConf(cl.Engine, ov); err != nil {
			return "", "", src, fmt.Errorf("集群参数覆盖: %w", err)
		}
		body = applyClusterOverrides(cl.Engine, body, ov)
	}
	body = stripManagedLines(cl.Engine, body)
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	if cl.Engine == store.DbDeployEngineMySQL {
		start, end := mysqldRange(lines)
		if start < 0 {
			lines = append(lines, "", "[mysqld]")
			start, end = len(lines), len(lines)
		}
		lines = insertAtSectionEnd(lines, start, end, append([]string{""}, mysqlManagedBlock(cl, n, major, vars)...))
		confPath = filepath.ToSlash(filepath.Join(soft, "my.cnf"))
	} else {
		lines = append(lines, "")
		lines = append(lines, pgManagedBlock(cl, n, primary, vars)...)
		confPath = filepath.ToSlash(filepath.Join(data, "postgresql.conf"))
	}
	return confPath, strings.Join(lines, "\n") + "\n", src, nil
}

// ConfTemplateView 设置页展示：已保存的平台模板 + 未自定义的内置大版本。
type ConfTemplateView struct {
	Major     string `json:"major"`
	Label     string `json:"label"`
	Content   string `json:"content"`
	Custom    bool   `json:"custom"` // 已保存为平台模板
	Builtin   bool   `json:"builtin"`
	UpdatedBy string `json:"updated_by,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

var confMajorRe = regexp.MustCompile(`^(\*|\d+(\.\d+)?)$`)

// ValidConfMajor 大版本取值：* 或 8.0 / 16 这类。
func ValidConfMajor(major string) bool { return confMajorRe.MatchString(strings.TrimSpace(major)) }

// ListConfTemplateViews 合并平台模板与内置大版本。
func ListConfTemplateViews(st *store.DB, engine string) ([]ConfTemplateView, error) {
	saved, err := st.ListDbDeployConfTemplates(engine)
	if err != nil {
		return nil, err
	}
	byMajor := map[string]store.DbDeployConfTemplate{}
	for _, t := range saved {
		byMajor[t.Major] = t
	}
	majors := map[string]bool{}
	for _, m := range BuiltinConfMajors(engine) {
		majors[m] = true
	}
	for m := range byMajor {
		majors[m] = true
	}
	var out []ConfTemplateView
	for m := range majors {
		builtin, isBuiltin := BuiltinConfTemplate(engine, m)
		v := ConfTemplateView{Major: m, Label: majorLabel(engine, m), Builtin: isBuiltin, Content: builtin}
		if t, ok := byMajor[m]; ok {
			v.Content, v.Custom, v.UpdatedBy, v.UpdatedAt = t.Content, true, t.UpdatedBy, t.UpdatedAt
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Major == store.DbDeployConfTemplateAnyMajor {
			return true
		}
		if out[j].Major == store.DbDeployConfTemplateAnyMajor {
			return false
		}
		return versionLess(out[i].Major, out[j].Major)
	})
	return out, nil
}

func versionLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x = atoiSafe(pa[i])
		}
		if i < len(pb) {
			y = atoiSafe(pb[i])
		}
		if x != y {
			return x < y
		}
	}
	return a < b
}
