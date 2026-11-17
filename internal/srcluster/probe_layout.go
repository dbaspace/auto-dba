package srcluster

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"auto-dba/internal/store"
)

// LayoutProbeResult 登记/重探时自动发现的 FE/BE 软件根与 BE 数据目录。
type LayoutProbeResult struct {
	FeSoftwareBase string   `json:"fe_software_base,omitempty"`
	BeSoftwareBase string   `json:"be_software_base,omitempty"`
	BeDataRoot     string   `json:"be_data_root,omitempty"` // 完整 storage_root_path
	FeInternalLog  string   `json:"fe_internal_log_dir,omitempty"`
	FeMetaDir      string   `json:"fe_meta_dir,omitempty"`
	BeMinidumpDir  string   `json:"be_sys_minidump_dir,omitempty"`
	BeStorageRoot  string   `json:"be_storage_root_path,omitempty"`
	SampleFEHost   string   `json:"sample_fe_host,omitempty"`
	SampleBEHost   string   `json:"sample_be_host,omitempty"`
	BeHTTPPort     int      `json:"be_http_port,omitempty"`
	Warnings       []string `json:"warnings,omitempty"`
	// BENodes 每台 BE 自己的软件根与数据目录。各台不一致时 BeSoftwareBase / BeDataRoot 留空。
	BENodes []LayoutBENode `json:"be_nodes,omitempty"`
}

// LayoutBENode 单台 BE 从 /varz 读到的安装根与数据目录。
type LayoutBENode struct {
	Host         string `json:"host,omitempty"`
	HTTPPort     int    `json:"http_port,omitempty"`
	SoftwareBase string `json:"software_base,omitempty"`
	DataRoot     string `json:"data_root,omitempty"`
	MinidumpDir  string `json:"sys_minidump_dir,omitempty"`
	StorageRoot  string `json:"storage_root_path,omitempty"`
}

var (
	reVarzMinidump = regexp.MustCompile(`(?m)^sys_minidump_dir=(.+)$`)
	reVarzStorage  = regexp.MustCompile(`(?m)^storage_root_path=(.+)$`)
)

// InferFeSoftwareBaseFromPath 从含 fe 目录段的路径（meta、log 等）推断 FE software_base。
// 取名为 fe、且下一段是 meta/log/minidump 或路径到此结束的那一层，不用最后一个 /fe/。
func InferFeSoftwareBaseFromPath(p string) string {
	base, _ := inferRoleSoftwareBase(p, "fe")
	return base
}

// InferFeSoftwareBaseFromInternalLog 从 internal_log_dir（…/fe/log）推断 FE software_base。
func InferFeSoftwareBaseFromInternalLog(internalLogDir string) string {
	return InferFeSoftwareBaseFromPath(internalLogDir)
}

func softwareBasePathDepth(p string) int {
	p = strings.Trim(strings.TrimSpace(p), "/")
	if p == "" {
		return 0
	}
	n := 0
	for _, s := range strings.Split(p, "/") {
		if s != "" {
			n++
		}
	}
	return n
}

// pickDeeperSoftwareBase 在多个推断结果中取路径更深的非空值。
// 深度相同时保留 a，不因字符串更长而改选。
func pickDeeperSoftwareBase(a, b string) string {
	a = strings.TrimRight(strings.TrimSpace(a), "/")
	b = strings.TrimRight(strings.TrimSpace(b), "/")
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	da, db := softwareBasePathDepth(a), softwareBasePathDepth(b)
	if db > da {
		return b
	}
	return a
}

// alignFeSoftwareBaseWithBE 若 FE 根是 BE 根的严格前缀（探测过浅），则对齐到 BE。
// FE 根已经是 dorisdb_base 目录时不对齐，避免把真实安装根改成更深的旁路路径。
func alignFeSoftwareBaseWithBE(fe, be string) (aligned string, did bool) {
	fe = strings.TrimRight(strings.TrimSpace(fe), "/")
	be = strings.TrimRight(strings.TrimSpace(be), "/")
	if fe == "" || be == "" || fe == be {
		return fe, false
	}
	if lastPathSegment(fe) == "dorisdb_base" {
		return fe, false
	}
	if strings.HasPrefix(be, fe+"/") {
		return be, true
	}
	return fe, false
}

// InferBeSoftwareBaseFromMinidump 从 sys_minidump_dir（…/be 或 …/be/…）推断 BE software_base。
func InferBeSoftwareBaseFromMinidump(minidumpDir string) string {
	base, _ := inferRoleSoftwareBase(minidumpDir, "be")
	return base
}

func lastPathSegment(p string) string {
	p = strings.Trim(strings.TrimSpace(p), "/")
	if p == "" {
		return ""
	}
	parts := strings.Split(p, "/")
	return strings.ToLower(parts[len(parts)-1])
}

// inferRoleSoftwareBase 截出 role（fe/be）这一层的父目录。
// anchored 表示路径里确实有该目录段；没有时原样返回，调用方不能把它当成已确认的安装根。
func inferRoleSoftwareBase(p, role string) (base string, anchored bool) {
	p = strings.TrimSpace(p)
	p = strings.TrimRight(p, "/")
	if p == "" {
		return "", false
	}
	parts := strings.Split(p, "/")
	nextOK := map[string]bool{"meta": true, "log": true, "minidump": true}
	for i := 0; i < len(parts); i++ {
		if !strings.EqualFold(parts[i], role) {
			continue
		}
		if i+1 < len(parts) && !nextOK[strings.ToLower(parts[i+1])] {
			continue
		}
		parent := strings.TrimRight(strings.Join(parts[:i], "/"), "/")
		if parent == "" {
			if strings.HasPrefix(p, "/") {
				return "/", true
			}
			return p, false
		}
		return parent, true
	}
	return p, false
}

// chooseFeSoftwareBase 以 meta 推断为安装根。日志目录只在 meta 为空时补位。
// 同深度且末段都是 dorisdb_base 时，有一条等于全局默认则采用默认。
// 两条都不是默认、又没有 fe 目录锚点时返回 uncertain，调用方不要写入节点。
func chooseFeSoftwareBase(fromMeta, fromLog string, metaAnchored, logAnchored bool) (chosen string, uncertain bool) {
	fromMeta = strings.TrimRight(strings.TrimSpace(fromMeta), "/")
	fromLog = strings.TrimRight(strings.TrimSpace(fromLog), "/")
	if fromMeta == "" && fromLog == "" {
		return "", false
	}
	if metaAnchored && fromMeta != "" && logAnchored && fromLog != "" && fromMeta != fromLog {
		if p := preferDefaultDorisSibling(fromMeta, fromLog); p != "" {
			return p, false
		}
		return fromMeta, false
	}
	if metaAnchored && fromMeta != "" {
		return fromMeta, false
	}
	if logAnchored && fromLog != "" {
		return fromLog, false
	}
	if fromMeta != "" && fromLog != "" && fromMeta != fromLog {
		if p := preferDefaultDorisSibling(fromMeta, fromLog); p != "" {
			return p, false
		}
		return "", true
	}
	if fromMeta != "" {
		return fromMeta, false
	}
	return fromLog, false
}

// preferDefaultDorisSibling 两条都是同深度的 …/dorisdb_base 时，返回其中等于全局默认的那条。
func preferDefaultDorisSibling(a, b string) string {
	a = strings.TrimRight(strings.TrimSpace(a), "/")
	b = strings.TrimRight(strings.TrimSpace(b), "/")
	if a == "" || b == "" || a == b {
		return ""
	}
	if softwareBasePathDepth(a) != softwareBasePathDepth(b) {
		return ""
	}
	if lastPathSegment(a) != "dorisdb_base" || lastPathSegment(b) != "dorisdb_base" {
		return ""
	}
	def := strings.TrimRight(store.DefaultSrSoftwareBase, "/")
	if a == def || b == def {
		return def
	}
	return ""
}

// ParseBEVarz 解析 BE /varz 文本，取出 sys_minidump_dir 与 storage_root_path。
func ParseBEVarz(body string) (minidump, storage string) {
	if m := reVarzMinidump.FindStringSubmatch(body); len(m) == 2 {
		minidump = strings.TrimSpace(m[1])
	}
	if m := reVarzStorage.FindStringSubmatch(body); len(m) == 2 {
		storage = strings.TrimSpace(m[1])
		// 多盘时可能为 path,medium:hdd;path2 — 取第一段路径
		if i := strings.IndexAny(storage, ",;"); i >= 0 {
			storage = strings.TrimSpace(storage[:i])
		}
	}
	return minidump, storage
}

// ProbeLayout 连接 FE 查配置 + BE /varz，推断登记用路径（不写库）。
func (e *Engine) ProbeLayout(ctx context.Context, feHost, user, pass string, queryPort int) (*LayoutProbeResult, error) {
	feHost = strings.TrimSpace(feHost)
	if feHost == "" {
		return nil, fmt.Errorf("fe_host 为空")
	}
	if queryPort <= 0 {
		queryPort = 9030
	}
	user = strings.TrimSpace(user)
	if user == "" {
		user = "root"
	}
	cctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	dsn := buildFEDSN(user, pass, feHost, queryPort)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	out := &LayoutProbeResult{SampleFEHost: feHost}

	var fromLog, fromMeta string
	var logAnchored, metaAnchored bool
	internalLog, logErr := queryFeLogDir(cctx, db)
	if logErr != nil {
		out.Warnings = append(out.Warnings, "读取 FE sys_log_dir 失败: "+logErr.Error())
	} else {
		out.FeInternalLog = internalLog
		fromLog, logAnchored = inferRoleSoftwareBase(internalLog, "fe")
	}
	metaDir, err := queryFrontendConfigValue(cctx, db, "meta_dir")
	if err != nil {
		out.Warnings = append(out.Warnings, "读取 FE meta_dir 失败: "+err.Error())
	} else {
		out.FeMetaDir = metaDir
		fromMeta, metaAnchored = inferRoleSoftwareBase(metaDir, "fe")
	}
	chosen, uncertain := chooseFeSoftwareBase(fromMeta, fromLog, metaAnchored, logAnchored)
	if uncertain {
		out.Warnings = append(out.Warnings, fmt.Sprintf(
			"FE 软件安装根无法确定（meta_dir=%s，sys_log_dir=%s），未写入软件目录，避免升级替换错目录",
			displayPath(metaDir), displayPath(internalLog)))
	} else {
		out.FeSoftwareBase = chosen
		if fromLog != "" && fromMeta != "" && strings.TrimRight(fromLog, "/") != strings.TrimRight(fromMeta, "/") {
			out.Warnings = append(out.Warnings, fmt.Sprintf(
				"FE sys_log_dir 与 meta_dir 推断不一致：%s vs %s，软件目录采用 %s",
				strings.TrimRight(fromLog, "/"), strings.TrimRight(fromMeta, "/"), displayPath(chosen)))
		}
	}

	beRows, err := queryProcRowsDB(cctx, db, "/backends", false)
	if err != nil {
		out.Warnings = append(out.Warnings, "SHOW PROC '/backends' 失败: "+err.Error())
		return out, nil
	}
	if len(beRows) == 0 {
		out.Warnings = append(out.Warnings, "集群无 BE 节点，无法探测 BE 路径")
		return out, nil
	}
	failed := false
	for i := range beRows {
		r := &beRows[i]
		if strings.TrimSpace(r.Host) == "" {
			continue
		}
		port := r.HTTPPort
		if port <= 0 {
			port = 8040
		}
		body, ferr := fetchBEVarz(cctx, r.Host, port)
		if ferr != nil {
			failed = true
			out.Warnings = append(out.Warnings, fmt.Sprintf("拉取 BE %s:%d/varz 失败: %v", r.Host, port, ferr))
			continue
		}
		out.BENodes = append(out.BENodes, layoutBENodeFromVarz(r.Host, port, body))
	}
	assignBEProbeSummary(out, failed)
	if aligned, did := alignFeSoftwareBaseWithBE(out.FeSoftwareBase, out.BeSoftwareBase); did {
		out.Warnings = append(out.Warnings, fmt.Sprintf(
			"FE 探测过浅（%s），已与 BE 对齐为 %s", out.FeSoftwareBase, aligned))
		out.FeSoftwareBase = aligned
	}
	return out, nil
}

func displayPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return "—"
	}
	return p
}

func layoutBENodeFromVarz(host string, port int, body string) LayoutBENode {
	md, st := ParseBEVarz(body)
	st = strings.TrimRight(strings.TrimSpace(st), "/")
	return LayoutBENode{
		Host:         host,
		HTTPPort:     port,
		MinidumpDir:  strings.TrimSpace(md),
		StorageRoot:  st,
		SoftwareBase: InferBeSoftwareBaseFromMinidump(md),
		DataRoot:     st,
	}
}

// assignBEProbeSummary 全部成功且路径一致时填写集群级字段；任一失败或不一致则集群级软件根、数据目录留空。
func assignBEProbeSummary(out *LayoutProbeResult, failed bool) {
	if out == nil || len(out.BENodes) == 0 {
		return
	}
	out.SampleBEHost = out.BENodes[0].Host
	out.BeHTTPPort = out.BENodes[0].HTTPPort
	if failed {
		out.Warnings = append(out.Warnings, "有 BE 未能读取 /varz，集群级软件目录与数据目录不回填")
		return
	}
	soft := out.BENodes[0].SoftwareBase
	data := out.BENodes[0].DataRoot
	dump := out.BENodes[0].MinidumpDir
	storage := out.BENodes[0].StorageRoot
	softSame, dataSame, dumpSame, storageSame := true, true, true, true
	for _, n := range out.BENodes[1:] {
		if n.SoftwareBase != soft {
			softSame = false
		}
		if n.DataRoot != data {
			dataSame = false
		}
		if n.MinidumpDir != dump {
			dumpSame = false
		}
		if n.StorageRoot != storage {
			storageSame = false
		}
	}
	if softSame {
		out.BeSoftwareBase = soft
	}
	if dataSame {
		out.BeDataRoot = data
	}
	if dumpSame {
		out.BeMinidumpDir = dump
	}
	if storageSame {
		out.BeStorageRoot = storage
	}
	if !softSame || !dataSame {
		out.Warnings = append(out.Warnings, "BE 节点软件目录或数据目录不一致，已按主机分别登记")
	}
}

func queryFeLogDir(ctx context.Context, db *sql.DB) (string, error) {
	v, err := queryFrontendConfigValue(ctx, db, "sys_log_dir")
	if err == nil && strings.HasPrefix(strings.TrimSpace(v), "/") {
		return strings.TrimSpace(v), nil
	}
	v2, err2 := queryFrontendConfigValue(ctx, db, "internal_log_dir")
	if err2 == nil && strings.HasPrefix(strings.TrimSpace(v2), "/") {
		return strings.TrimSpace(v2), nil
	}
	if err2 != nil {
		return "", err2
	}
	if err != nil {
		return "", err
	}
	return "", fmt.Errorf("未找到配置项 sys_log_dir")
}

// frontendConfigIndexes 解析 ADMIN SHOW FRONTEND CONFIG 的列。
// 3.2 列名为 Key、AliasNames、Value。列名缺失且不少于 6 列时，Value 在下标 2，不能把 AliasNames 当成目录。
func frontendConfigIndexes(cols []string) (keyIdx, aliasIdx, valIdx int) {
	keyIdx, aliasIdx, valIdx = -1, -1, -1
	idx := map[string]int{}
	named := false
	for i, c := range cols {
		n := strings.ToLower(strings.TrimSpace(c))
		if n == "" {
			continue
		}
		named = true
		idx[n] = i
	}
	for _, name := range []string{"key", "variable_name", "name", "configkey"} {
		if i, ok := idx[name]; ok {
			keyIdx = i
			break
		}
	}
	for _, name := range []string{"aliasnames", "alias", "aliases"} {
		if i, ok := idx[name]; ok {
			aliasIdx = i
			break
		}
	}
	for _, name := range []string{"value", "configvalue"} {
		if i, ok := idx[name]; ok {
			valIdx = i
			break
		}
	}
	if keyIdx < 0 {
		keyIdx = 0
	}
	if !named && len(cols) >= 6 {
		if aliasIdx < 0 {
			aliasIdx = 1
		}
		if valIdx < 0 {
			valIdx = 2
		}
	}
	if valIdx < 0 && len(cols) > 1 {
		valIdx = 1
	}
	return keyIdx, aliasIdx, valIdx
}

func configKeyMatches(keyCell, aliasCell, want string) bool {
	want = strings.ToLower(strings.TrimSpace(want))
	if want == "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(keyCell), want) {
		return true
	}
	for _, a := range strings.FieldsFunc(aliasCell, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '|'
	}) {
		if strings.EqualFold(strings.TrimSpace(a), want) {
			return true
		}
	}
	return false
}

func queryFrontendConfigValue(ctx context.Context, db *sql.DB, key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", fmt.Errorf("config key 为空")
	}
	// StarRocks: ADMIN SHOW FRONTEND CONFIG[ LIKE 'key']
	q := "ADMIN SHOW FRONTEND CONFIG LIKE '" + strings.ReplaceAll(key, "'", "") + "'"
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		// 部分版本不支持 LIKE，退回全量扫描
		rows, err = db.QueryContext(ctx, "ADMIN SHOW FRONTEND CONFIG")
		if err != nil {
			return "", err
		}
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	keyIdx, aliasIdx, valIdx := frontendConfigIndexes(cols)
	for rows.Next() {
		vals := make([]sql.RawBytes, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return "", err
		}
		cell := func(i int) string {
			if i < 0 || i >= len(vals) {
				return ""
			}
			return strings.TrimSpace(string(vals[i]))
		}
		if !configKeyMatches(cell(keyIdx), cell(aliasIdx), key) {
			continue
		}
		val := cell(valIdx)
		if !strings.HasPrefix(val, "/") {
			continue
		}
		return val, nil
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("未找到配置项 %s", key)
}

func fetchBEVarz(ctx context.Context, host string, port int) (string, error) {
	host = strings.TrimSpace(host)
	if host == "" || port <= 0 {
		return "", fmt.Errorf("host/port 无效")
	}
	url := fmt.Sprintf("http://%s:%d/varz", host, port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	cli := &http.Client{Timeout: 8 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func findLayoutBE(nodes []LayoutBENode, host string) *LayoutBENode {
	for i := range nodes {
		if hostsEqual(nodes[i].Host, host) {
			return &nodes[i]
		}
	}
	return nil
}

// ApplyLayoutProbe 将探测结果写回集群与节点路径（不改 Agent / 不停服）。
func (e *Engine) ApplyLayoutProbe(cl *store.SrCluster, nodes []store.SrNode, res *LayoutProbeResult, who string) error {
	if e == nil || e.Store == nil || cl == nil || res == nil {
		return fmt.Errorf("参数无效")
	}
	if s := strings.TrimSpace(res.FeSoftwareBase); s != "" {
		cl.SoftwareBase = s
	}
	// 各 BE 不一致或有节点未读到时，这两个字段为空，不改集群级目录。
	if s := strings.TrimSpace(res.BeSoftwareBase); s != "" {
		cl.BeSoftwareBase = s
	}
	if s := strings.TrimSpace(res.BeDataRoot); s != "" {
		cl.DataRoot = s
	}
	if who != "" {
		cl.UpdatedBy = who
	}
	if err := e.Store.UpdateSrCluster(cl); err != nil {
		return err
	}
	feBase := strings.TrimRight(strings.TrimSpace(res.FeSoftwareBase), "/")
	for i := range nodes {
		n := &nodes[i]
		switch strings.ToLower(n.Component) {
		case "fe":
			if feBase != "" {
				n.SoftwarePath = feBase + "/fe"
			}
		case "be":
			bn := findLayoutBE(res.BENodes, n.Host)
			if bn == nil {
				continue
			}
			if s := strings.TrimRight(strings.TrimSpace(bn.SoftwareBase), "/"); s != "" {
				n.SoftwarePath = s + "/be"
			}
			if dr := strings.TrimRight(strings.TrimSpace(bn.DataRoot), "/"); dr != "" {
				n.DataRoot = dr
			}
		}
		if err := e.Store.UpdateSrNode(n); err != nil {
			return err
		}
	}
	return nil
}
