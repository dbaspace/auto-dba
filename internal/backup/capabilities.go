package backup

import (
	"encoding/json"
	"fmt"
	"strings"
)

// 平台 Agent 能力组。
const (
	GroupBackup = "backup"
	GroupRDB    = "rdb"
	GroupHA     = "ha"
	GroupK8s    = "k8s" // 反向隧道 Agent（非 SSH）；供 K8s 纳管引用
)

// 备份控制可选实例类型（与 store.NormalizeDBType 对齐）。
const (
	BackupEnginePostgres  = "postgres"
	BackupEngineMySQL     = "mysql"
	BackupEngineTiDB      = "tidb"
	BackupEngineOceanBase = "oceanbase"
)

// 细项状态。
const (
	CapOK      = "ok"
	CapMissing = "missing"
	CapUnknown = "unknown"
	CapSkipped = "skipped"
)

// CapItem 单项能力探测结果。
type CapItem struct {
	Key        string `json:"key"`
	Group      string `json:"group"`
	Title      string `json:"title,omitempty"`
	Status     string `json:"status"`
	Detail     string `json:"detail,omitempty"`
	Hint       string `json:"hint,omitempty"`
	Tool       string `json:"tool,omitempty"`
	FoundPath  string `json:"found_path,omitempty"`
	MissingCmd string `json:"missing_cmd,omitempty"`
}

// CapabilitiesReport 能力总览。
type CapabilitiesReport struct {
	Groups        []string  `json:"enabled_groups"`
	BackupEngines []string  `json:"backup_engines,omitempty"`
	Items         []CapItem `json:"items"`
	OK            bool      `json:"ok"`
}

// DefaultEnabledGroups 下发默认勾选。
func DefaultEnabledGroups() []string {
	return []string{GroupBackup, GroupRDB}
}

// DefaultBackupEngines 勾选「备份控制」但未指定类型时的默认（兼容旧行为）。
func DefaultBackupEngines() []string {
	return []string{BackupEnginePostgres, BackupEngineMySQL}
}

// NormalizeGroups 规范化并去重。
func NormalizeGroups(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, g := range in {
		g = strings.ToLower(strings.TrimSpace(g))
		switch g {
		case GroupBackup, GroupRDB, GroupHA, GroupK8s:
			if !seen[g] {
				seen[g] = true
				out = append(out, g)
			}
		}
	}
	return out
}

// NormalizeBackupEngines 规范化备份实例类型。
func NormalizeBackupEngines(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range in {
		e = strings.ToLower(strings.TrimSpace(e))
		switch e {
		case "pg", "postgresql":
			e = BackupEnginePostgres
		case "ob":
			e = BackupEngineOceanBase
		}
		switch e {
		case BackupEnginePostgres, BackupEngineMySQL, BackupEngineTiDB, BackupEngineOceanBase:
			if !seen[e] {
				seen[e] = true
				out = append(out, e)
			}
		}
	}
	return out
}

// ParseBackupEnginesCSV 从 engines 字段解析备份实例类型（忽略 redis/ha 等标记）。
func ParseBackupEnginesCSV(s string) []string {
	return NormalizeBackupEngines(strings.Split(s, ","))
}

func BackupEnginesCSV(engines []string) string {
	return strings.Join(NormalizeBackupEngines(engines), ",")
}

// ComposeAgentEngines 写入 Agent.engines：备份类型 + 可选 redis/ha 标记。
func ComposeAgentEngines(backupEngines, groups []string) string {
	parts := NormalizeBackupEngines(backupEngines)
	for _, g := range NormalizeGroups(groups) {
		if g == GroupRDB {
			parts = append(parts, "redis")
		}
		if g == GroupHA {
			parts = append(parts, "ha")
		}
	}
	return strings.Join(parts, ",")
}

// RequiredKeysForGroups 勾选组对应的必检细项（备份未指定类型时默认 postgres+mysql）。
func RequiredKeysForGroups(groups []string) []string {
	return RequiredKeys(groups, nil)
}

// RequiredKeys 按能力组与备份实例类型生成必检细项。
func RequiredKeys(groups []string, backupEngines []string) []string {
	var keys []string
	for _, g := range groups {
		switch g {
		case GroupBackup:
			engines := NormalizeBackupEngines(backupEngines)
			if len(engines) == 0 {
				engines = DefaultBackupEngines()
			}
			keys = append(keys, requiredKeysForBackupEngines(engines)...)
		case GroupRDB:
			keys = append(keys, "redis_rdb")
		case GroupHA:
			keys = append(keys, "ha_mha")
		}
	}
	return keys
}

func requiredKeysForBackupEngines(engines []string) []string {
	var keys []string
	for _, e := range engines {
		switch e {
		case BackupEnginePostgres:
			keys = append(keys, "logical_pg")
		case BackupEngineMySQL:
			keys = append(keys, "logical_mysql")
		case BackupEngineTiDB:
			keys = append(keys, "logical_tidb")
		case BackupEngineOceanBase:
			keys = append(keys, "logical_oceanbase")
		}
	}
	return keys
}

// backupProbeKeys 某备份类型需探测的细项（含可选物理工具）。
func backupProbeKeys(engines []string) map[string]bool {
	out := map[string]bool{}
	for _, e := range NormalizeBackupEngines(engines) {
		switch e {
		case BackupEnginePostgres:
			out["logical_pg"] = true
			out["physical_pg"] = true
		case BackupEngineMySQL:
			out["logical_mysql"] = true
			out["physical_mysql"] = true
		case BackupEngineTiDB:
			out["logical_tidb"] = true
			out["physical_mysql"] = true
		case BackupEngineOceanBase:
			out["logical_oceanbase"] = true
		}
	}
	return out
}

// CapCatalog 全量细项定义（含提示）。
func CapCatalog() []CapItem {
	return []CapItem{
		{Key: "logical_pg", Group: GroupBackup, Title: "PostgreSQL 逻辑备份客户端", Tool: "pg_dump", Hint: "apt/yum 安装 postgresql-client（需包含 pg_dump）"},
		{Key: "logical_mysql", Group: GroupBackup, Title: "MySQL 逻辑备份客户端", Tool: "mysqldump", Hint: "apt/yum 安装 mysql-client 或 mariadb-client（需包含 mysqldump）"},
		{Key: "logical_tidb", Group: GroupBackup, Title: "TiDB 逻辑备份客户端", Tool: "mysqldump", Hint: "TiDB 兼容 MySQL 协议；安装 mysql-client（含 mysqldump）即可"},
		{Key: "logical_oceanbase", Group: GroupBackup, Title: "OceanBase 逻辑备份客户端", Tool: "mysqldump", Hint: "OceanBase MySQL 模式可使用 mysqldump；请安装 mysql-client"},
		{Key: "physical_pg", Group: GroupBackup, Title: "PostgreSQL 物理备份工具", Tool: "pg_basebackup", Hint: "安装 postgresql 客户端工具包（需包含 pg_basebackup）"},
		{Key: "physical_mysql", Group: GroupBackup, Title: "MySQL 物理备份工具", Tool: "xtrabackup 或 mariabackup", Hint: "安装 Percona XtraBackup 或 MariaBackup（二选一即可）"},
		{Key: "redis_rdb", Group: GroupRDB, Title: "Redis RDB 拉取客户端", Tool: "redis-cli", Hint: "apt/yum 安装 redis-tools / redis（需包含 redis-cli）"},
		{Key: "ha_mha", Group: GroupHA, Title: "MHA 环境检查工具", Tool: "masterha_check_status", Hint: "安装 MHA 并将 masterha_* 加入 PATH（通常与 Manager 同机）"},
		{Key: "runtime_disk", Group: "runtime", Title: "工作目录磁盘空间", Tool: "df", Hint: "保证 Agent workdir 所在分区可用空间 ≥ 5GB"},
	}
}

func EncodeCapabilities(r *CapabilitiesReport) string {
	if r == nil {
		return "{}"
	}
	b, _ := json.Marshal(r)
	return string(b)
}

func DecodeCapabilities(s string) *CapabilitiesReport {
	s = strings.TrimSpace(s)
	if s == "" {
		return &CapabilitiesReport{}
	}
	var r CapabilitiesReport
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		return &CapabilitiesReport{}
	}
	return &r
}

// HasCapability 判断报告中某细项是否 ok。
func HasCapability(r *CapabilitiesReport, key string) bool {
	if r == nil {
		return false
	}
	for _, it := range r.Items {
		if it.Key == key && it.Status == CapOK {
			return true
		}
	}
	return false
}

// HasGroupEnabled 是否启用某组。
func HasGroupEnabled(r *CapabilitiesReport, group string) bool {
	if r == nil {
		return false
	}
	for _, g := range r.Groups {
		if g == group {
			return true
		}
	}
	return false
}

func GroupsCSV(groups []string) string {
	return strings.Join(NormalizeGroups(groups), ",")
}

func ParseGroupsCSV(s string) []string {
	parts := strings.Split(s, ",")
	return NormalizeGroups(parts)
}

// ProbeResult 单项探测原始结果。
type ProbeResult struct {
	OK   bool
	Path string
}

// BuildReportFromProbe 根据本机探测结果生成报告（备份类型默认 postgres+mysql）。
func BuildReportFromProbe(enabled []string, probe map[string]ProbeResult) *CapabilitiesReport {
	return BuildReportFromProbeEngines(enabled, nil, probe)
}

// BuildReportFromProbeEngines 按能力组与备份实例类型生成报告。
func BuildReportFromProbeEngines(enabled []string, backupEngines []string, probe map[string]ProbeResult) *CapabilitiesReport {
	enabled = NormalizeGroups(enabled)
	engines := NormalizeBackupEngines(backupEngines)
	hasBackup := false
	for _, g := range enabled {
		if g == GroupBackup {
			hasBackup = true
			break
		}
	}
	if hasBackup && len(engines) == 0 {
		engines = DefaultBackupEngines()
	}
	if !hasBackup {
		engines = nil
	}
	req := map[string]bool{}
	for _, k := range RequiredKeys(enabled, engines) {
		req[k] = true
	}
	enabledSet := map[string]bool{}
	for _, g := range enabled {
		enabledSet[g] = true
	}
	probeKeys := backupProbeKeys(engines)
	rep := &CapabilitiesReport{Groups: enabled, BackupEngines: engines, OK: true}
	for _, def := range CapCatalog() {
		it := def
		if def.Group != "runtime" && !enabledSet[def.Group] {
			it.Status = CapSkipped
			it.Detail = "未启用该能力组，跳过检查"
			rep.Items = append(rep.Items, it)
			continue
		}
		if def.Group == GroupBackup && !probeKeys[def.Key] {
			it.Status = CapSkipped
			it.Detail = "未选择该实例类型，跳过检查"
			rep.Items = append(rep.Items, it)
			continue
		}
		pr := probe[def.Key]
		if !pr.OK && (def.Key == "logical_tidb" || def.Key == "logical_oceanbase") {
			if alt, ok := probe["logical_mysql"]; ok && alt.OK {
				pr = alt
			}
		}
		if pr.OK {
			it.Status = CapOK
			it.FoundPath = strings.TrimSpace(pr.Path)
			if it.FoundPath != "" {
				it.Detail = fmt.Sprintf("已找到命令 %s → %s", primaryTool(def.Tool), it.FoundPath)
			} else {
				it.Detail = fmt.Sprintf("已找到命令 %s", primaryTool(def.Tool))
			}
		} else {
			it.Status = CapMissing
			it.MissingCmd = primaryTool(def.Tool)
			if def.Key == "physical_mysql" {
				it.MissingCmd = "xtrabackup / mariabackup"
				it.Detail = "未在 PATH 中找到命令：xtrabackup 或 mariabackup（二者任一即可）"
			} else if def.Key == "runtime_disk" {
				it.MissingCmd = "disk"
				it.Detail = "workdir 磁盘可用空间不足 5GB，或无法读取 df"
			} else {
				it.Detail = fmt.Sprintf("未在 PATH 中找到命令：%s", it.MissingCmd)
			}
			if req[def.Key] {
				rep.OK = false
			}
		}
		rep.Items = append(rep.Items, it)
	}
	return rep
}

func primaryTool(tool string) string {
	tool = strings.TrimSpace(tool)
	if i := strings.Index(tool, "|"); i > 0 {
		return strings.TrimSpace(tool[:i])
	}
	if i := strings.Index(tool, " 或 "); i > 0 {
		return strings.TrimSpace(tool[:i])
	}
	if i := strings.Index(tool, " "); i > 0 {
		return strings.TrimSpace(tool[:i])
	}
	return tool
}

// MissingRequired 返回勾选组内缺失的必检项。
func MissingRequired(rep *CapabilitiesReport) []CapItem {
	if rep == nil {
		return nil
	}
	req := map[string]bool{}
	for _, k := range RequiredKeys(rep.Groups, rep.BackupEngines) {
		req[k] = true
	}
	var out []CapItem
	for _, it := range rep.Items {
		if req[it.Key] && it.Status != CapOK {
			out = append(out, it)
		}
	}
	return out
}

// FormatMissingLine 单条缺失说明（给 UI / 日志）。
func FormatMissingLine(it CapItem) string {
	title := it.Title
	if title == "" {
		title = it.Key
	}
	cmd := it.MissingCmd
	if cmd == "" {
		cmd = primaryTool(it.Tool)
	}
	line := fmt.Sprintf("【%s】缺少命令：%s", title, cmd)
	if it.Detail != "" {
		line += " — " + it.Detail
	}
	if it.Hint != "" {
		line += "。建议：" + it.Hint
	}
	return line
}

// FormatPreflightSummary 完整预检摘要：先列缺失，再列已通过。
func FormatPreflightSummary(rep *CapabilitiesReport) string {
	if rep == nil {
		return "无预检结果"
	}
	var b strings.Builder
	miss := MissingRequired(rep)
	if len(miss) == 0 {
		b.WriteString("必选工具检查通过。\n")
	} else {
		b.WriteString(fmt.Sprintf("缺少 %d 项必选工具：\n", len(miss)))
		for _, m := range miss {
			b.WriteString("  · " + FormatMissingLine(m) + "\n")
		}
	}
	var okItems []string
	for _, it := range rep.Items {
		if it.Status == CapOK {
			okItems = append(okItems, fmt.Sprintf("%s（%s）", it.Title, primaryTool(it.Tool)))
		}
	}
	if len(okItems) > 0 {
		b.WriteString("已检测到：")
		b.WriteString(strings.Join(okItems, "；"))
	}
	return strings.TrimSpace(b.String())
}
