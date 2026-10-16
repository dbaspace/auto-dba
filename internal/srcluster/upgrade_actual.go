package srcluster

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"auto-dba/internal/store"
)

// procVersionRow SHOW PROC '/frontends' 或 '/backends' 中一行的主机、版本、角色（仅 FE）与存活状态。
type procVersionRow struct {
	Host    string
	Version string
	Role    string
	Alive   string
	// HTTPPort BE 的 HttpPort（探测 /varz）；FE 也可能有，缺省 0
	HTTPPort int
	// 以下仅 FE：ReplayedJournalId、LastHeartbeat、StartTime 列
	ReplayedJournalID string
	LastHeartbeat     string
	StartTime         string
	// Decommissioned / TabletNum 仅 BE：SystemDecommissioned、TabletNum 列
	Decommissioned bool
	TabletNum      string
	// 以下仅 BE：CPU 核数与存储（SHOW PROC 原样字符串，如 "99.951 GB"、"37.02 %"）
	CpuCores      string
	TotalCapacity string
	AvailCapacity string
	DataUsed      string
	UsedPct       string
}

// backendResourceIdx /backends 中 CPU 与存储列的下标，缺列为 -1。
type backendResourceIdx struct {
	cpu, total, avail, dataUsed, usedPct int
}

// backendResourceCols 存储按磁盘口径：TotalCapacity 为数据目录所在盘总量，UsedPct 为该盘使用率（含非 StarRocks 文件）；
// DataTotalCapacity 只是 DataUsed+Avail，不代表盘大小，故不用。
func backendResourceCols(cols []string) backendResourceIdx {
	r := backendResourceIdx{-1, -1, -1, -1, -1}
	for i, c := range cols {
		switch strings.ToLower(strings.TrimSpace(c)) {
		case "cpucores":
			r.cpu = i
		case "totalcapacity":
			r.total = i
		case "availcapacity":
			r.avail = i
		case "datausedcapacity":
			r.dataUsed = i
		case "usedpct":
			r.usedPct = i
		}
	}
	return r
}

// queryProcVersions 读取 SHOW PROC 的 IP（无则 Host，再无则取 Name 的主机段）与 Version 列。
func (e *Engine) queryProcVersions(ctx context.Context, cl *store.SrCluster, path string) ([]procVersionRow, error) {
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return e.queryProcRows(cctx, cl, path, true)
}

// queryProcRows 执行 SHOW PROC；requireVersion 为 true 时缺少 Version 列视为错误。
func (e *Engine) queryProcRows(ctx context.Context, cl *store.SrCluster, path string, requireVersion bool) ([]procVersionRow, error) {
	dsn, err := e.feDSN(cl)
	if err != nil {
		return nil, err
	}
	return queryProcRowsDSN(ctx, dsn, path, requireVersion)
}

func queryProcRowsDSN(ctx context.Context, dsn, path string, requireVersion bool) ([]procVersionRow, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return queryProcRowsDB(ctx, db, path, requireVersion)
}

func queryProcRowsDB(ctx context.Context, db *sql.DB, path string, requireVersion bool) ([]procVersionRow, error) {
	rows, err := db.QueryContext(ctx, "SHOW PROC '"+path+"'")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	idx := map[string]int{}
	for i, c := range cols {
		idx[strings.ToLower(c)] = i
	}
	col := func(name string) int {
		if i, ok := idx[name]; ok {
			return i
		}
		return -1
	}
	ipIdx := col("ip")
	if ipIdx < 0 {
		ipIdx = col("host")
	}
	nameIdx, verIdx := col("name"), col("version")
	roleIdx, aliveIdx := col("role"), col("alive")
	journalIdx, heartbeatIdx, startIdx := col("replayedjournalid"), col("lastheartbeat"), col("starttime")
	decomIdx, tabletIdx := col("systemdecommissioned"), col("tabletnum")
	httpPortIdx := col("httpport")
	res := backendResourceCols(cols)
	if verIdx < 0 && requireVersion {
		return nil, fmt.Errorf("SHOW PROC '%s' 无 Version 列", path)
	}
	var out []procVersionRow
	for rows.Next() {
		vals := make([]sql.RawBytes, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		cell := func(i int) string {
			if i < 0 || i >= len(vals) {
				return ""
			}
			return strings.TrimSpace(string(vals[i]))
		}
		host := cell(ipIdx)
		if host == "" {
			// FE Name 形如 10.100.113.151_9010_1790435392900
			host, _, _ = strings.Cut(cell(nameIdx), "_")
		}
		httpPort := 0
		if s := cell(httpPortIdx); s != "" {
			fmt.Sscanf(s, "%d", &httpPort)
		}
		out = append(out, procVersionRow{
			Host:     host,
			Version:  cell(verIdx),
			Role:     strings.ToUpper(cell(roleIdx)),
			Alive:    strings.ToLower(cell(aliveIdx)),
			HTTPPort: httpPort,

			ReplayedJournalID: cell(journalIdx),
			LastHeartbeat:     cell(heartbeatIdx),
			StartTime:         cell(startIdx),

			Decommissioned: procTruthy(cell(decomIdx)),
			TabletNum:      cell(tabletIdx),

			CpuCores:      cell(res.cpu),
			TotalCapacity: cell(res.total),
			AvailCapacity: cell(res.avail),
			DataUsed:      cell(res.dataUsed),
			UsedPct:       cell(res.usedPct),
		})
	}
	return out, rows.Err()
}

// NodeLive 节点在 StarRocks 中的实时状态；Role 仅 FE 有值（LEADER/FOLLOWER/OBSERVER）。
type NodeLive struct {
	Role    string `json:"role,omitempty"`
	Alive   string `json:"alive,omitempty"`
	Version string `json:"version,omitempty"`
	// 以下仅 FE：元数据回放进度、最近心跳、启动时间（SHOW PROC '/frontends' 原样字符串）
	ReplayedJournalID string `json:"replayed_journal_id,omitempty"`
	LastHeartbeat     string `json:"last_heartbeat,omitempty"`
	StartTime         string `json:"start_time,omitempty"`
	// Decommissioned BE 正在 DECOMMISSION（仍可 CANCEL 恢复）；TabletNum 为剩余 tablet 数
	Decommissioned bool   `json:"decommissioned,omitempty"`
	TabletNum      string `json:"tablet_num,omitempty"`
	// 以下仅 BE：CPU 核数与存储（SHOW PROC '/backends' 原样字符串）；DataUsed 为 StarRocks 数据占用
	CpuCores      string `json:"cpu_cores,omitempty"`
	TotalCapacity string `json:"total_capacity,omitempty"`
	AvailCapacity string `json:"avail_capacity,omitempty"`
	DataUsed      string `json:"data_used,omitempty"`
	UsedPct       string `json:"used_pct,omitempty"`
}

// mapNodeLive 按组件与主机把 SHOW PROC 结果对应到节点；未出现在结果中的节点不在返回值里。
func mapNodeLive(nodes []store.SrNode, fe, be []procVersionRow) map[string]NodeLive {
	out := map[string]NodeLive{}
	for _, n := range nodes {
		rows := be
		if n.Component == "fe" {
			rows = fe
		}
		for _, r := range rows {
			if hostsEqual(n.Host, r.Host) {
				live := NodeLive{Alive: r.Alive, Version: r.Version}
				if n.Component == "fe" {
					live.Role = r.Role
					live.ReplayedJournalID = r.ReplayedJournalID
					live.LastHeartbeat = r.LastHeartbeat
					live.StartTime = r.StartTime
				} else {
					live.Decommissioned = r.Decommissioned
					live.TabletNum = r.TabletNum
					live.CpuCores = r.CpuCores
					live.TotalCapacity = r.TotalCapacity
					live.AvailCapacity = r.AvailCapacity
					live.DataUsed = r.DataUsed
					live.UsedPct = r.UsedPct
				}
				out[n.ID] = live
				break
			}
		}
	}
	return out
}

// ClusterLive 集群详情节点表的实时数据（node_id → NodeLive），并删除已缩容完成的 BE 登记（removed）。
// 生产集群 BE 多，每次请求只解析一次凭据、只建一个 FE 连接，/frontends 与 /backends 各查一次，缩容收尾复用同一份 /backends。
// FE 查询失败返回错误；BE 查询失败时仅返回 FE 部分且不做缩容收尾。
func (e *Engine) ClusterLive(ctx context.Context, cl *store.SrCluster) (map[string]NodeLive, []store.SrNode, error) {
	if e == nil || e.Store == nil || cl == nil || e.Cfg.DryRun {
		return map[string]NodeLive{}, nil, nil
	}
	nodes, err := e.Store.ListSrNodes(cl.ID)
	if err != nil {
		return nil, nil, err
	}
	dsn, err := e.feDSNWithNodes(cl, nodes)
	if err != nil {
		return nil, nil, err
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, nil, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	fe, err := queryProcRowsDB(cctx, db, "/frontends", false)
	if err != nil {
		return nil, nil, fmt.Errorf("SHOW PROC '/frontends': %w", err)
	}
	be, beErr := queryProcRowsDB(cctx, db, "/backends", false)
	var removed []store.SrNode
	if beErr == nil {
		removed, _ = e.reconcileRemovedWith(cl, nodes, "", func() ([]procVersionRow, error) { return be, nil })
		if len(removed) > 0 {
			_ = e.SyncRegistry(cl, removed, "system", false)
			nodes = withoutNodes(nodes, removed)
		}
	}
	live := mapNodeLive(nodes, fe, be)
	reconcileNodeReadyFromAlive(e.Store, nodes, live)
	return live, removed, nil
}

// reconcileNodeReadyFromAlive：SHOW PROC Alive=true 且库状态为 stopped（非下线中）时写回 ready，避免详情误显「已停止」。
func reconcileNodeReadyFromAlive(st *store.DB, nodes []store.SrNode, live map[string]NodeLive) {
	if st == nil || live == nil {
		return
	}
	for i := range nodes {
		n := &nodes[i]
		if n.Status != store.SrNodeStopped {
			continue
		}
		lv, ok := live[n.ID]
		if !ok || !procTruthy(lv.Alive) || lv.Decommissioned {
			continue
		}
		n.Status = store.SrNodeReady
		_ = st.UpdateSrNode(n)
	}
}

func withoutNodes(nodes, drop []store.SrNode) []store.SrNode {
	gone := make(map[string]bool, len(drop))
	for _, n := range drop {
		gone[n.ID] = true
	}
	out := make([]store.SrNode, 0, len(nodes))
	for _, n := range nodes {
		if !gone[n.ID] {
			out = append(out, n)
		}
	}
	return out
}

// mapNodeVersions 按组件与主机把 SHOW PROC 结果对应到节点；未出现在结果中的节点不在返回值里。
func mapNodeVersions(nodes []store.SrNode, fe, be []procVersionRow) map[string]string {
	out := map[string]string{}
	for _, n := range nodes {
		rows := be
		if n.Component == "fe" {
			rows = fe
		}
		for _, r := range rows {
			if hostsEqual(n.Host, r.Host) {
				out[n.ID] = r.Version
				break
			}
		}
	}
	return out
}

// mismatchedNodes 实际版本与目标不符（含未在 SHOW PROC 中出现）的节点及说明。
func mismatchedNodes(nodes []store.SrNode, actual map[string]string, want string) ([]store.SrNode, []string) {
	var bad []store.SrNode
	var desc []string
	for _, n := range nodes {
		ver, ok := actual[n.ID]
		if ok && versionMatches(ver, want) {
			continue
		}
		if !ok || ver == "" {
			ver = "未上报"
		}
		bad = append(bad, n)
		desc = append(desc, fmt.Sprintf("%s %s=%s", strings.ToUpper(n.Component), n.Host, ver))
	}
	return bad, desc
}

// nodeActualVersions 各节点实际运行的版本（node_id → Version），来自 SHOW PROC frontends / backends。
func (e *Engine) nodeActualVersions(ctx context.Context, cl *store.SrCluster, nodes []store.SrNode) (map[string]string, error) {
	fe, err := e.queryProcVersions(ctx, cl, "/frontends")
	if err != nil {
		return nil, fmt.Errorf("SHOW PROC '/frontends': %w", err)
	}
	be, err := e.queryProcVersions(ctx, cl, "/backends")
	if err != nil {
		return nil, fmt.Errorf("SHOW PROC '/backends': %w", err)
	}
	return mapNodeVersions(nodes, fe, be), nil
}

// releaseIDForVersion 在版本目录中找与实际版本匹配的 release（用于补升级时记录真实的升级前版本）。
func releaseIDForVersion(rels []store.SrRelease, actual string) string {
	for _, r := range rels {
		if versionMatches(actual, r.Version) {
			return r.ID
		}
	}
	return ""
}
