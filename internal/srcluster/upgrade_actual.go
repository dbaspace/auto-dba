package srcluster

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"auto-dba/internal/store"
)

// procVersionRow SHOW PROC '/frontends' 或 '/backends' 中一行的主机与版本。
type procVersionRow struct {
	Host    string
	Version string
}

// queryProcVersions 读取 SHOW PROC 的 IP（无则 Host，再无则取 Name 的主机段）与 Version 列。
func (e *Engine) queryProcVersions(ctx context.Context, cl *store.SrCluster, path string) ([]procVersionRow, error) {
	dsn, err := e.feDSN(cl)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	rows, err := db.QueryContext(cctx, "SHOW PROC '"+path+"'")
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
	if verIdx < 0 {
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
		out = append(out, procVersionRow{Host: host, Version: cell(verIdx)})
	}
	return out, rows.Err()
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
