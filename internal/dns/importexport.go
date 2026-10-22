package dns

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

var csvHeader = []string{"zone", "range", "name", "type", "data", "ttl", "status", "description"}

// ExportCSV 导出记录。zoneID、rangeID 为空表示不过滤。MX 的 data 写成「优先级 邮件主机」。
func ExportCSV(s *Snapshot, zoneID, rangeID string) ([]byte, error) {
	zones, ranges := zoneMap(s), rangeMap(s)
	recs := filterRecords(s, zoneID, rangeID)
	var buf bytes.Buffer
	buf.WriteString("\ufeff")
	w := csv.NewWriter(&buf)
	_ = w.Write(csvHeader)
	for _, r := range recs {
		z, ok := zones[r.ZoneID]
		if !ok {
			continue
		}
		_ = w.Write([]string{
			z.Origin, ranges[r.RangeID].Name, r.Name, r.Type, csvData(r), strconv.FormatInt(r.TTL, 10), r.Status, r.Description,
		})
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

func csvData(r RecordSnap) string {
	if r.Type == "MX" {
		if h, p, ok := DecodeMX(r.Data); ok {
			return strconv.Itoa(p) + " " + h
		}
	}
	return r.Data
}

// ExportZone 按 BIND 格式导出一个域在一个解析范围内的运行中记录。
func ExportZone(s *Snapshot, zoneID, rangeID string) ([]byte, error) {
	z, ok := zoneMap(s)[zoneID]
	if !ok {
		return nil, fmt.Errorf("解析域不存在")
	}
	rg, ok := rangeMap(s)[rangeID]
	if !ok {
		return nil, fmt.Errorf("解析范围不存在")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "; auto-dba DNS 导出 · 解析范围 %s\n", rg.Name)
	fmt.Fprintf(&b, "$ORIGIN %s\n", z.Origin)
	ttl := z.TTL
	if ttl <= 0 {
		ttl = 600
	}
	fmt.Fprintf(&b, "$TTL %d\n", ttl)
	for _, r := range filterRecords(s, zoneID, rangeID) {
		if r.Status != StatusActive {
			continue
		}
		data := r.Data
		switch r.Type {
		case "MX":
			if h, p, ok := DecodeMX(r.Data); ok {
				data = strconv.Itoa(p) + " " + absName(h)
			}
		case "CNAME":
			data = absName(r.Data)
		case "TXT":
			data = strconv.Quote(r.Data)
		}
		fmt.Fprintf(&b, "%-24s %6d IN %-5s %s\n", r.Name, r.TTL, r.Type, data)
	}
	return []byte(b.String()), nil
}

func absName(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, ".") {
		return s
	}
	return s + "."
}

func filterRecords(s *Snapshot, zoneID, rangeID string) []RecordSnap {
	out := []RecordSnap{}
	for _, r := range s.Records {
		if zoneID != "" && r.ZoneID != zoneID {
			continue
		}
		if rangeID != "" && r.RangeID != rangeID {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.ZoneID != b.ZoneID {
			return a.ZoneID < b.ZoneID
		}
		if a.RangeID != b.RangeID {
			return a.RangeID < b.RangeID
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		return a.Data < b.Data
	})
	return out
}

func zoneMap(s *Snapshot) map[string]Zone {
	m := map[string]Zone{}
	for _, z := range s.Zones {
		m[z.ID] = z
	}
	return m
}

func rangeMap(s *Snapshot) map[string]Range {
	m := map[string]Range{}
	for _, r := range s.Ranges {
		m[r.ID] = r
	}
	return m
}

// ImportRow 文件里的一行。Zone 为域名，Range 为范围名称。
type ImportRow struct {
	Line        int
	Zone        string
	Range       string
	Name        string
	Type        string
	Data        string
	TTL         int64
	Status      string
	Description string
	// Skip 非空表示该行不导入（如 SOA、NS 等不支持的类型）。
	Skip string
	Err  string
}

// ParseCSV 解析导出格式的 CSV。表头必须包含 zone,range,name,type,data，其余列可省略。
func ParseCSV(r io.Reader) ([]ImportRow, error) {
	cr := csv.NewReader(bufio.NewReader(r))
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true
	head, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("读取表头失败: %w", err)
	}
	idx := map[string]int{}
	for i, h := range head {
		h = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\ufeff")))
		idx[h] = i
	}
	for _, k := range []string{"zone", "range", "name", "type", "data"} {
		if _, ok := idx[k]; !ok {
			return nil, fmt.Errorf("缺少列 %s", k)
		}
	}
	var rows []ImportRow
	line := 1
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		line++
		if err != nil {
			rows = append(rows, ImportRow{Line: line, Err: err.Error()})
			continue
		}
		get := func(k string) string {
			i, ok := idx[k]
			if !ok || i >= len(rec) {
				return ""
			}
			return strings.TrimSpace(rec[i])
		}
		if strings.Join(rec, "") == "" {
			continue
		}
		row := ImportRow{
			Line: line, Zone: get("zone"), Range: get("range"), Name: get("name"), Type: strings.ToUpper(get("type")),
			Data: get("data"), Status: get("status"), Description: get("description"),
		}
		if t := get("ttl"); t != "" {
			n, err := strconv.ParseInt(t, 10, 64)
			if err != nil {
				row.Err = "TTL 无效"
			}
			row.TTL = n
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// ParseZoneFile 解析 BIND 格式的 zone 文件，仅支持单行记录。origin 为 $ORIGIN 缺省值。
func ParseZoneFile(r io.Reader, origin string) ([]ImportRow, error) {
	origin = strings.ToLower(strings.TrimSpace(origin))
	if origin != "" {
		origin = absName(origin)
	}
	var defTTL int64 = 600
	var rows []ImportRow
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	line, last := 0, ""
	for sc.Scan() {
		line++
		raw := stripComment(sc.Text())
		if strings.TrimSpace(raw) == "" {
			continue
		}
		indented := raw[0] == ' ' || raw[0] == '\t'
		toks := zoneTokens(raw)
		if len(toks) == 0 {
			continue
		}
		switch strings.ToUpper(toks[0]) {
		case "$ORIGIN":
			if len(toks) > 1 {
				origin = absName(strings.ToLower(toks[1]))
			}
			continue
		case "$TTL":
			if len(toks) > 1 {
				if n, err := strconv.ParseInt(toks[1], 10, 64); err == nil {
					defTTL = n
				}
			}
			continue
		case "$INCLUDE", "$GENERATE":
			rows = append(rows, ImportRow{Line: line, Skip: "不支持 " + toks[0]})
			continue
		}
		if strings.Contains(raw, "(") {
			rows = append(rows, ImportRow{Line: line, Skip: "不支持多行记录"})
			continue
		}
		row := ImportRow{Line: line, Zone: origin, TTL: defTTL, Status: StatusActive}
		if indented {
			row.Name = last
		} else {
			row.Name, toks = toks[0], toks[1:]
		}
		for len(toks) > 0 {
			t := strings.ToUpper(toks[0])
			if n, err := strconv.ParseInt(t, 10, 64); err == nil {
				row.TTL = n
				toks = toks[1:]
				continue
			}
			if t == "IN" || t == "CH" || t == "HS" {
				toks = toks[1:]
				continue
			}
			break
		}
		if len(toks) == 0 {
			row.Err = "缺少记录类型"
			rows = append(rows, row)
			continue
		}
		row.Type, toks = strings.ToUpper(toks[0]), toks[1:]
		last = row.Name
		if origin == "" {
			row.Err = "缺少 $ORIGIN"
			rows = append(rows, row)
			continue
		}
		name, err := relName(row.Name, origin)
		if err != nil {
			row.Err = err.Error()
			rows = append(rows, row)
			continue
		}
		row.Name = name
		if _, err := NormalizeType(row.Type); err != nil {
			row.Skip = "不支持 " + row.Type + " 类型"
			rows = append(rows, row)
			continue
		}
		switch row.Type {
		case "TXT":
			row.Data = strings.Join(toks, "")
		case "MX":
			if len(toks) != 2 {
				row.Err = "MX 格式应为 优先级 邮件主机"
			} else {
				row.Data = toks[0] + " " + expandName(toks[1], origin)
			}
		case "CNAME":
			if len(toks) != 1 {
				row.Err = "CNAME 只能有一个目标"
			} else {
				row.Data = expandName(toks[0], origin)
			}
		default:
			if len(toks) != 1 {
				row.Err = row.Type + " 只能有一个地址"
			} else {
				row.Data = toks[0]
			}
		}
		rows = append(rows, row)
	}
	return rows, sc.Err()
}

func stripComment(s string) string {
	inQ := false
	for i, r := range s {
		switch r {
		case '"':
			inQ = !inQ
		case ';':
			if !inQ {
				return s[:i]
			}
		}
	}
	return s
}

// zoneTokens 按空白切分，引号内的内容作为一个整体（去掉引号）。
func zoneTokens(s string) []string {
	var out []string
	var cur strings.Builder
	inQ, has := false, false
	for _, r := range s {
		switch {
		case r == '"':
			inQ = !inQ
			has = true
		case !inQ && (r == ' ' || r == '\t'):
			if has {
				out = append(out, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteRune(r)
			has = true
		}
	}
	if has {
		out = append(out, cur.String())
	}
	return out
}

func relName(name, origin string) (string, error) {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "@" || n == origin {
		return "@", nil
	}
	if !strings.HasSuffix(n, ".") {
		return n, nil
	}
	if strings.HasSuffix(n, "."+origin) {
		return strings.TrimSuffix(n, "."+origin), nil
	}
	return "", fmt.Errorf("%s 不在 %s 内", name, origin)
}

// expandName 相对名补全为不带结尾点的完整域名。
func expandName(s, origin string) string {
	s = strings.TrimSpace(s)
	if s == "@" {
		return strings.TrimSuffix(origin, ".")
	}
	if strings.HasSuffix(s, ".") {
		return strings.TrimSuffix(s, ".")
	}
	return s + "." + strings.TrimSuffix(origin, ".")
}

// parseMXText 接受「优先级 邮件主机」或库内的「邮件主机|优先级」。
func parseMXText(s string) (string, int, bool) {
	if h, p, ok := DecodeMX(s); ok {
		return h, p, true
	}
	f := strings.Fields(s)
	if len(f) != 2 {
		return "", 0, false
	}
	p, err := strconv.Atoi(f[0])
	if err != nil {
		return "", 0, false
	}
	return f[1], p, true
}

const (
	ImportCreate      = "create"
	ImportUpdate      = "update"
	ImportUnchanged   = "unchanged"
	ImportError       = "error"
	ImportUnsupported = "unsupported"
)

type ImportItem struct {
	Line    int         `json:"line"`
	Action  string      `json:"action"`
	Message string      `json:"message,omitempty"`
	Zone    string      `json:"zone"`
	Range   string      `json:"range"`
	Before  *RecordSnap `json:"before,omitempty"`
	After   *RecordSnap `json:"after,omitempty"`
}

type ImportPlan struct {
	Items  []ImportItem   `json:"items"`
	Counts map[string]int `json:"counts"`
	Ops    []Op           `json:"ops"`
}

// PlanImport 对比文件与现有数据。按 域+范围+主机名+类型+值 匹配：不存在则新增，
// TTL、状态或备注不同则修改，其余不变。导入从不删除记录。
// rangeName 非空时覆盖每行的范围（zone 文件导入用）。
func PlanImport(s *Snapshot, rows []ImportRow, rangeName string) *ImportPlan {
	zonesByOrigin := map[string]Zone{}
	for _, z := range s.Zones {
		zonesByOrigin[normName(z.Origin)] = z
	}
	rangesByName := map[string]Range{}
	for _, r := range s.Ranges {
		rangesByName[strings.ToLower(r.Name)] = r
	}
	existing := map[string]RecordSnap{}
	for _, r := range s.Records {
		existing[importKey(r.ZoneID, r.RangeID, r.Name, r.Type, r.Data)] = r
	}
	plan := &ImportPlan{Counts: map[string]int{}, Ops: []Op{}}
	seen := map[string]int{}
	add := func(it ImportItem) {
		plan.Items = append(plan.Items, it)
		plan.Counts[it.Action]++
	}
	for _, row := range rows {
		rg := row.Range
		if strings.TrimSpace(rangeName) != "" {
			rg = rangeName
		}
		it := ImportItem{Line: row.Line, Zone: row.Zone, Range: rg}
		if row.Skip != "" {
			it.Action, it.Message = ImportUnsupported, row.Skip
			add(it)
			continue
		}
		fail := func(msg string) {
			it.Action, it.Message = ImportError, msg
			add(it)
		}
		if row.Err != "" {
			fail(row.Err)
			continue
		}
		z, ok := zonesByOrigin[normName(row.Zone)]
		if !ok {
			fail("解析域 " + row.Zone + " 不存在")
			continue
		}
		r, ok := rangesByName[strings.ToLower(strings.TrimSpace(rg))]
		if !ok {
			fail("解析范围 " + rg + " 不存在")
			continue
		}
		if r.Name == DefaultRangeName {
			fail("不能导入到 DEFAULT 解析范围")
			continue
		}
		rt, err := NormalizeType(row.Type)
		if err != nil {
			it.Action, it.Message = ImportUnsupported, "不支持 "+row.Type+" 类型"
			add(it)
			continue
		}
		host, err := NormalizeHost(row.Name)
		if err != nil {
			fail(err.Error())
			continue
		}
		var data string
		if rt == "MX" {
			h, p, ok := parseMXText(row.Data)
			if !ok {
				fail("MX 值应为「优先级 邮件主机」")
				continue
			}
			data, err = EncodeMX(h, p)
		} else {
			data, err = NormalizeData(rt, row.Data, "", 0)
		}
		if err != nil {
			fail(err.Error())
			continue
		}
		ttl := row.TTL
		if ttl == 0 {
			ttl = 600
		}
		if _, err := NormalizeTTL(ttl); err != nil {
			fail(err.Error())
			continue
		}
		status := NormalizeStatus(row.Status)
		if status == "" {
			fail("状态无效")
			continue
		}
		after := RecordSnap{
			Name: host, Type: rt, Data: data, TTL: ttl, ZoneID: z.ID, RangeID: r.ID, Description: row.Description, Status: status,
		}
		key := importKey(z.ID, r.ID, host, rt, data)
		if prev, dup := seen[key]; dup {
			fail(fmt.Sprintf("与第 %d 行重复", prev))
			continue
		}
		seen[key] = row.Line
		cur, ok := existing[key]
		if !ok {
			it.Action, it.After = ImportCreate, &after
			plan.Ops = append(plan.Ops, RecordOp(ActionCreate, "", after))
			add(it)
			continue
		}
		if row.Description == "" {
			after.Description = cur.Description
		}
		if strings.TrimSpace(row.Status) == "" {
			after.Status = cur.Status
		}
		after.ID = cur.ID
		c := cur
		it.Before = &c
		if cur.TTL == after.TTL && cur.Status == after.Status && cur.Description == after.Description {
			it.Action = ImportUnchanged
			add(it)
			continue
		}
		it.Action, it.After = ImportUpdate, &after
		op := RecordOp(ActionUpdate, cur.ID, after)
		op.Expect = mustJSON(cur)
		plan.Ops = append(plan.Ops, op)
		add(it)
	}
	if plan.Items == nil {
		plan.Items = []ImportItem{}
	}
	return plan
}

func importKey(zoneID, rangeID, name, typ, data string) string {
	if typ == "CNAME" || typ == "MX" {
		data = strings.ToLower(data)
	}
	return zoneID + "|" + rangeID + "|" + strings.ToLower(name) + "|" + typ + "|" + data
}
