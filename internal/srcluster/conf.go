package srcluster

import (
	"bytes"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"text/template"

	"auto-dba/internal/store"
)

// Builtin default templates（不含 meta_dir / storage_root_path；下发时由平台追加锁定路径）。
const builtinFeConf = `# Managed by auto-dba — FE runtime aligned with production
LOG_DIR = ${STARROCKS_HOME}/log

DATE = "$(date +%Y%m%d-%H%M%S)"
JAVA_OPTS="-Dlog4j2.formatMsgNoLookups=true -Xmx8192m -XX:+UseG1GC -XX:+PrintGCDetails -XX:+PrintGCDateStamps -Xloggc:${LOG_DIR}/fe.gc.log.$DATE -XX:+PrintConcurrentLocks -Djava.security.policy=${STARROCKS_HOME}/conf/udf_security.policy"

# For jdk 11+, this JAVA_OPTS will be used as default JVM options
JAVA_OPTS_FOR_JDK_11="-Dlog4j2.formatMsgNoLookups=true -Xmx8192m -XX:+UseG1GC -Xlog:gc*:${LOG_DIR}/fe.gc.log.$DATE:time -Djava.security.policy=${STARROCKS_HOME}/conf/udf_security.policy"

##
## the lowercase properties are read by main program.
##

# DEBUG, INFO, WARN, ERROR, FATAL
sys_log_level = INFO
http_port = {{.HTTPPort}}
rpc_port = {{.RPCPort}}
query_port = {{.QueryPort}}
edit_log_port = {{.EditPort}}
mysql_service_nio_enabled = true
max_routine_load_task_num_per_be = 500
audit_log_modules = slow_query, query
max_conn_per_user = 2048
qe_query_timeout_second = 300
qe_slow_log_ms = 5000
time_zone = '+00:00'
empty_load_as_error = false
{{- if .Host}}
frontend_address = {{.Host}}
{{- end}}
`

const builtinBeConf = `# Managed by auto-dba — BE ports / runtime; storage_root_path appended on apply
# Defaults aligned with production: be_port=9060 be_http_port=8040 heartbeat=9050 brpc=8060 starlet=9070
LOG_DIR = {{.LogDir}}
sys_log_level = INFO

be_port = {{.BEPort}}
be_http_port = {{.HTTPPort}}
heartbeat_service_port = {{.HeartbeatPort}}
brpc_port = {{.BRPCPort}}
starlet_port = {{.StarletPort}}
{{- if .PriorityNetworks}}
priority_networks = {{.PriorityNetworks}}
{{- end}}
`

// ConfVars 模板渲染变量。
type ConfVars struct {
	Host             string
	LogDir           string
	MetaDir          string
	StorageRoot      string
	PriorityNetworks string
	HTTPPort         int
	EditPort         int
	QueryPort        int
	RPCPort          int
	HeartbeatPort    int
	BEPort           int
	BRPCPort         int
	StarletPort      int
}

// BuiltinConfTemplate 返回内置正文。
func BuiltinConfTemplate(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "fe":
		return strings.TrimSpace(builtinFeConf) + "\n"
	case "be":
		return strings.TrimSpace(builtinBeConf) + "\n"
	default:
		return ""
	}
}

// EnsureDefaultConfTemplates 种子平台默认模板。
func EnsureDefaultConfTemplates(st *store.DB) error {
	if st == nil {
		return nil
	}
	return st.EnsureSrConfTemplates(map[string]string{
		"fe": BuiltinConfTemplate("fe"),
		"be": BuiltinConfTemplate("be"),
	})
}

// ResetConfTemplatesToBuiltin 强制覆盖平台 FE/BE 模板为当前内置正文。
func ResetConfTemplatesToBuiltin(st *store.DB, operator string) error {
	if st == nil {
		return fmt.Errorf("store 未初始化")
	}
	if strings.TrimSpace(operator) == "" {
		operator = "system"
	}
	for _, role := range []string{"fe", "be"} {
		body := BuiltinConfTemplate(role)
		if err := st.UpsertSrConfTemplate(&store.SrConfTemplate{
			Role: role, Content: body, UpdatedBy: operator,
		}); err != nil {
			return err
		}
	}
	return nil
}

// EffectiveConfTemplate 节点草稿 > 集群草稿 > 平台模板 > 内置。
// nodeID 可空；为空时跳过节点级。
func EffectiveConfTemplate(st *store.DB, clusterID, role, nodeID string) (content string, source string, err error) {
	role = strings.ToLower(strings.TrimSpace(role))
	if role != "fe" && role != "be" {
		return "", "", fmt.Errorf("role 须为 fe 或 be")
	}
	if st != nil && strings.TrimSpace(nodeID) != "" {
		nc, e := st.GetSrNodeConfig(nodeID)
		if e != nil {
			return "", "", e
		}
		if nc != nil && strings.TrimSpace(nc.Content) != "" {
			return nc.Content, "node", nil
		}
	}
	if st != nil && strings.TrimSpace(clusterID) != "" {
		cfg, e := st.GetSrClusterConfig(clusterID, role)
		if e != nil {
			return "", "", e
		}
		if cfg != nil && strings.TrimSpace(cfg.Content) != "" {
			return cfg.Content, "cluster", nil
		}
	}
	if st != nil {
		tpl, e := st.GetSrConfTemplate(role)
		if e != nil {
			return "", "", e
		}
		if tpl != nil && strings.TrimSpace(tpl.Content) != "" {
			return tpl.Content, "platform", nil
		}
	}
	return BuiltinConfTemplate(role), "builtin", nil
}

// softwareBaseForRole 按组件选 FE/BE 软件根。
func softwareBaseForRole(cl *store.SrCluster, role string) string {
	if cl == nil {
		return ""
	}
	if strings.ToLower(role) == "be" {
		return strings.TrimRight(cl.EffectiveBeSoftwareBase(), "/")
	}
	return strings.TrimRight(strings.TrimSpace(cl.SoftwareBase), "/")
}

// resolveStorageRoot：data_root 已是完整 storage_root_path（以 /storage 结尾）则原样使用；否则拼 {data_root}/be/storage。
func resolveStorageRoot(dataRoot string) string {
	dr := filepath.ToSlash(strings.TrimSpace(dataRoot))
	if dr == "" {
		dr = "/data"
	}
	trim := strings.TrimRight(dr, "/")
	if strings.HasSuffix(strings.ToLower(trim), "/storage") || strings.EqualFold(filepath.Base(trim), "storage") {
		return trim
	}
	return filepath.ToSlash(filepath.Join(trim, "be", "storage"))
}

// BuildConfVars 从集群/节点推导端口与锁定路径。
func BuildConfVars(cl *store.SrCluster, n *store.SrNode) ConfVars {
	role := "fe"
	soft := ""
	dataRoot := "/data"
	if cl != nil {
		if strings.TrimSpace(cl.DataRoot) != "" {
			dataRoot = strings.TrimSpace(cl.DataRoot)
		}
		if n != nil && strings.TrimSpace(n.SoftwarePath) != "" {
			soft = strings.TrimSpace(n.SoftwarePath)
		} else if n != nil {
			role = strings.ToLower(n.Component)
			soft = softwareBaseForRole(cl, role) + "/" + role
		}
	}
	if n != nil {
		role = strings.ToLower(n.Component)
		if dr := strings.TrimSpace(n.DataRoot); dr != "" {
			dataRoot = dr
		}
		if soft == "" && cl != nil {
			soft = softwareBaseForRole(cl, role) + "/" + role
		}
	}
	v := ConfVars{
		LogDir: filepath.ToSlash(filepath.Join(soft, "log")),
		// FE meta 落在软件目录（现网：/home/dorisdb_base/fe/meta）
		MetaDir: filepath.ToSlash(filepath.Join(soft, "meta")),
		// BE storage：登记探测常写入完整 storage_root_path；新建仍用 {data_root}/be/storage
		StorageRoot: resolveStorageRoot(dataRoot),
		BEPort:      9060,
		BRPCPort:    8060,
		StarletPort: 9070,
		RPCPort:     9020,
	}
	if n != nil {
		v.Host = strings.TrimSpace(n.Host)
		v.PriorityNetworks = guessPriorityNetworks(n.Host)
	}
	if cl != nil {
		v.HTTPPort = cl.FeHTTPPort
		v.EditPort = cl.FeEditPort
		v.QueryPort = cl.FeQueryPort
		v.HeartbeatPort = cl.BeHeartbeatPort
	}
	if v.HTTPPort <= 0 {
		v.HTTPPort = 8030
	}
	if v.EditPort <= 0 {
		v.EditPort = 9010
	}
	if v.QueryPort <= 0 {
		v.QueryPort = 9030
	}
	if v.HeartbeatPort <= 0 {
		v.HeartbeatPort = 9050
	}
	if n != nil {
		if n.HTTPPort > 0 {
			v.HTTPPort = n.HTTPPort
		}
		if n.EditPort > 0 {
			v.EditPort = n.EditPort
		}
		if n.HeartbeatPort > 0 {
			v.HeartbeatPort = n.HeartbeatPort
		}
		if role == "be" {
			// BE HTTP 默认 8040（be_http_port）；节点未单独记 http_port 时用 8040
			if n.HTTPPort <= 0 {
				v.HTTPPort = 8040
			}
		}
	}
	return v
}

func guessPriorityNetworks(host string) string {
	ip := net.ParseIP(strings.TrimSpace(host))
	if ip == nil || ip.To4() == nil {
		return ""
	}
	// 与现网一致：优先写节点 IP（非 CIDR）
	return ip.To4().String()
}

// RenderConf 渲染 Go text/template。
func RenderConf(tmpl string, vars ConfVars) (string, error) {
	t, err := template.New("srconf").Option("missingkey=error").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("模板解析失败: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("模板渲染失败: %w", err)
	}
	return buf.String(), nil
}

// AppendLockedDataPaths 追加平台锁定的数据路径（草稿禁止手写这些键）。
func AppendLockedDataPaths(role, body string, vars ConfVars) string {
	role = strings.ToLower(strings.TrimSpace(role))
	body = strings.TrimRight(body, "\n") + "\n\n"
	body += "# --- locked by auto-dba (data paths; do not set in draft) ---\n"
	switch role {
	case "fe":
		body += fmt.Sprintf("meta_dir = %s\n", vars.MetaDir)
	case "be":
		body += fmt.Sprintf("storage_root_path = %s\n", vars.StorageRoot)
	}
	return body
}

// StripLockedDataDirKeys 去掉数据目录锁定行（编辑区不含这些键；下发时再追加）。
func StripLockedDataDirKeys(content string) string {
	var keep []string
	for _, line := range strings.Split(content, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "# --- locked by auto-dba") {
			continue
		}
		if lockedDataDirKeyRe.MatchString(line) {
			continue
		}
		keep = append(keep, line)
	}
	return strings.TrimRight(strings.Join(keep, "\n"), "\n") + "\n"
}

// NodeConfPreview 单节点配置预览。
type NodeConfPreview struct {
	NodeID     string   `json:"node_id"`
	Host       string   `json:"host"`
	Role       string   `json:"role"`
	RemotePath string   `json:"remote_path"`
	Source     string   `json:"source"` // node|cluster|platform|builtin
	Content    string   `json:"content"`  // 最终将写入（含锁定路径）
	Editable   string   `json:"editable"` // 可编辑区（无锁定路径）
	Vars       ConfVars `json:"vars"`
	HasDraft   bool     `json:"has_draft"`
}

// PreviewClusterConfs 为集群各节点渲染有效配置。
func PreviewClusterConfs(st *store.DB, cl *store.SrCluster, nodes []store.SrNode) ([]NodeConfPreview, error) {
	if cl == nil {
		return nil, fmt.Errorf("集群为空")
	}
	out := make([]NodeConfPreview, 0, len(nodes))
	for i := range nodes {
		n := &nodes[i]
		path, content, source, err := MaterializeConf(st, cl, n)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", n.Host, err)
		}
		nc, _ := st.GetSrNodeConfig(n.ID)
		out = append(out, NodeConfPreview{
			NodeID:     n.ID,
			Host:       n.Host,
			Role:       n.Component,
			RemotePath: path,
			Source:     source,
			Content:    content,
			Editable:   StripLockedDataDirKeys(content),
			Vars:       BuildConfVars(cl, n),
			HasDraft:   nc != nil && strings.TrimSpace(nc.Content) != "",
		})
	}
	return out, nil
}

// MaterializeConf 取有效模板 → 校验 → 渲染 → 追加锁定路径。
func MaterializeConf(st *store.DB, cl *store.SrCluster, n *store.SrNode) (remotePath, content, source string, err error) {
	if cl == nil || n == nil {
		return "", "", "", fmt.Errorf("集群或节点为空")
	}
	role := strings.ToLower(strings.TrimSpace(n.Component))
	tmpl, source, err := EffectiveConfTemplate(st, cl.ID, role, n.ID)
	if err != nil {
		return "", "", "", err
	}
	tmpl = StripLockedDataDirKeys(tmpl)
	if err := RejectLockedDataDirKeys(tmpl); err != nil {
		return "", "", source, fmt.Errorf("%s 模板: %w", source, err)
	}
	vars := BuildConfVars(cl, n)
	rendered, err := RenderConf(tmpl, vars)
	if err != nil {
		return "", "", source, err
	}
	content = AppendLockedDataPaths(role, rendered, vars)
	soft := strings.TrimSpace(n.SoftwarePath)
	if soft == "" {
		soft = softwareBaseForRole(cl, role) + "/" + role
	}
	remotePath = filepath.ToSlash(filepath.Join(soft, "conf", role+".conf"))
	return remotePath, content, source, nil
}
