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
}

var (
	reVarzMinidump = regexp.MustCompile(`(?m)^sys_minidump_dir=(.+)$`)
	reVarzStorage  = regexp.MustCompile(`(?m)^storage_root_path=(.+)$`)
)

// InferFeSoftwareBaseFromPath 从含 /fe/ 的路径（meta、log 等）推断 FE software_base。
func InferFeSoftwareBaseFromPath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimRight(p, "/")
	if p == "" {
		return ""
	}
	lower := strings.ToLower(p)
	if idx := strings.LastIndex(lower, "/fe/"); idx >= 0 {
		return strings.TrimRight(p[:idx], "/")
	}
	if strings.HasSuffix(lower, "/fe") {
		return strings.TrimRight(p[:len(p)-len("/fe")], "/")
	}
	return p
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
	if da > db {
		return a
	}
	if len(b) > len(a) {
		return b
	}
	return a
}

// alignFeSoftwareBaseWithBE 若 FE 根是 BE 根的严格前缀（探测过浅），则对齐到 BE。
func alignFeSoftwareBaseWithBE(fe, be string) (aligned string, did bool) {
	fe = strings.TrimRight(strings.TrimSpace(fe), "/")
	be = strings.TrimRight(strings.TrimSpace(be), "/")
	if fe == "" || be == "" || fe == be {
		return fe, false
	}
	if strings.HasPrefix(be, fe+"/") {
		return be, true
	}
	return fe, false
}

// InferBeSoftwareBaseFromMinidump 从 sys_minidump_dir（…/be 或 …/be/…）推断 BE software_base。
func InferBeSoftwareBaseFromMinidump(minidumpDir string) string {
	p := strings.TrimSpace(minidumpDir)
	p = strings.TrimRight(p, "/")
	if p == "" {
		return ""
	}
	lower := strings.ToLower(p)
	if idx := strings.LastIndex(lower, "/be/"); idx >= 0 {
		return strings.TrimRight(p[:idx], "/")
	}
	if strings.HasSuffix(lower, "/be") {
		return strings.TrimRight(p[:len(p)-len("/be")], "/")
	}
	return p
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
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
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
	internalLog, err := queryFrontendConfigValue(cctx, db, "internal_log_dir")
	if err != nil {
		out.Warnings = append(out.Warnings, "读取 FE internal_log_dir 失败: "+err.Error())
	} else {
		out.FeInternalLog = internalLog
		fromLog = InferFeSoftwareBaseFromPath(internalLog)
	}
	metaDir, err := queryFrontendConfigValue(cctx, db, "meta_dir")
	if err != nil {
		out.Warnings = append(out.Warnings, "读取 FE meta_dir 失败: "+err.Error())
	} else {
		out.FeMetaDir = metaDir
		fromMeta = InferFeSoftwareBaseFromPath(metaDir)
	}
	out.FeSoftwareBase = pickDeeperSoftwareBase(fromLog, fromMeta)
	if fromLog != "" && fromMeta != "" {
		a := strings.TrimRight(fromLog, "/")
		b := strings.TrimRight(fromMeta, "/")
		if a != b {
			out.Warnings = append(out.Warnings, fmt.Sprintf(
				"FE internal_log_dir 与 meta_dir 推断不一致：%s vs %s，已取更深路径 %s", a, b, out.FeSoftwareBase))
		}
	}

	beRows, err := queryProcRowsDB(cctx, db, "/backends", false)
	if err != nil {
		out.Warnings = append(out.Warnings, "SHOW PROC '/backends' 失败: "+err.Error())
		return out, nil
	}
	var sample *procVersionRow
	for i := range beRows {
		r := &beRows[i]
		if procTruthy(r.Alive) || r.Alive == "" {
			sample = r
			break
		}
	}
	if sample == nil && len(beRows) > 0 {
		sample = &beRows[0]
	}
	if sample == nil {
		out.Warnings = append(out.Warnings, "集群无 BE 节点，无法探测 BE 路径")
		return out, nil
	}
	out.SampleBEHost = sample.Host
	port := sample.HTTPPort
	if port <= 0 {
		port = 8040
	}
	out.BeHTTPPort = port

	varz, err := fetchBEVarz(cctx, sample.Host, port)
	if err != nil {
		out.Warnings = append(out.Warnings, fmt.Sprintf("拉取 BE %s:%d/varz 失败: %v", sample.Host, port, err))
		return out, nil
	}
	minidump, storage := ParseBEVarz(varz)
	out.BeMinidumpDir = minidump
	out.BeStorageRoot = storage
	out.BeSoftwareBase = InferBeSoftwareBaseFromMinidump(minidump)
	out.BeDataRoot = storage

	if aligned, did := alignFeSoftwareBaseWithBE(out.FeSoftwareBase, out.BeSoftwareBase); did {
		out.Warnings = append(out.Warnings, fmt.Sprintf(
			"FE 探测过浅（%s），已与 BE 对齐为 %s", out.FeSoftwareBase, aligned))
		out.FeSoftwareBase = aligned
	}

	// 多 BE 路径不一致时提示
	for i := range beRows {
		r := &beRows[i]
		if r.Host == "" || hostsEqual(r.Host, sample.Host) {
			continue
		}
		p := r.HTTPPort
		if p <= 0 {
			p = 8040
		}
		body, err := fetchBEVarz(cctx, r.Host, p)
		if err != nil {
			continue
		}
		md, st := ParseBEVarz(body)
		bs := InferBeSoftwareBaseFromMinidump(md)
		if (bs != "" && out.BeSoftwareBase != "" && bs != out.BeSoftwareBase) ||
			(st != "" && out.BeDataRoot != "" && st != out.BeDataRoot) {
			out.Warnings = append(out.Warnings, fmt.Sprintf("BE %s 路径与采样节点不一致，请按节点核对后手改", r.Host))
			break
		}
	}
	return out, nil
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
	idx := map[string]int{}
	for i, c := range cols {
		idx[strings.ToLower(strings.TrimSpace(c))] = i
	}
	keyIdx := -1
	valIdx := -1
	for _, name := range []string{"key", "variable_name", "name", "configkey"} {
		if i, ok := idx[name]; ok {
			keyIdx = i
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
	if valIdx < 0 && len(cols) > 1 {
		valIdx = 1
	}
	want := strings.ToLower(key)
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
		if strings.EqualFold(cell(keyIdx), want) || strings.ToLower(cell(keyIdx)) == want {
			return cell(valIdx), nil
		}
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

// ApplyLayoutProbe 将探测结果写回集群与节点路径（不改 Agent / 不停服）。
func (e *Engine) ApplyLayoutProbe(cl *store.SrCluster, nodes []store.SrNode, res *LayoutProbeResult, who string) error {
	if e == nil || e.Store == nil || cl == nil || res == nil {
		return fmt.Errorf("参数无效")
	}
	if s := strings.TrimSpace(res.FeSoftwareBase); s != "" {
		cl.SoftwareBase = s
	}
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
	feBase := strings.TrimRight(strings.TrimSpace(cl.SoftwareBase), "/")
	beBase := strings.TrimRight(cl.EffectiveBeSoftwareBase(), "/")
	for i := range nodes {
		n := &nodes[i]
		switch strings.ToLower(n.Component) {
		case "fe":
			if feBase != "" {
				n.SoftwarePath = feBase + "/fe"
			}
		case "be":
			if beBase != "" {
				n.SoftwarePath = beBase + "/be"
			}
			if dr := strings.TrimSpace(cl.DataRoot); dr != "" {
				n.DataRoot = dr
			}
		}
		if err := e.Store.UpdateSrNode(n); err != nil {
			return err
		}
	}
	return nil
}
