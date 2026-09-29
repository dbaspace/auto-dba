package pgspace

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"auto-dba/internal/store"
)

type scanMode int

const (
	scanNone scanMode = iota
	scanOne
	scanAll
)

type analyzeOptions struct {
	scanMode   scanMode
	specificDb string
	topN       int
	onProgress progressFn
}

type progressFn func(stage string, dbDone, dbTotal int, currentDB string) error

type analysis struct {
	Tablespaces   []*store.SpaceRow
	Databases     []*store.SpaceRow
	Tables        []*store.SpaceRow
	Indexes       []*store.SpaceRow
	UnusedIndexes []*store.SpaceRow
	Sequences     []*store.SpaceRow
	ScannedDbs    []string
	Warnings      []string
}

type dbInfo struct {
	Name string
	Size int64
}

func (s *Service) analyzeOne(ctx context.Context, t connTarget, opts analyzeOptions) *analysis {
	res := &analysis{}
	pass, err := s.decryptPassword(t.inst)
	if err != nil {
		res.Warnings = append(res.Warnings, "get credential failed: "+err.Error())
		return res
	}
	user := strings.TrimSpace(t.inst.Username)
	if user == "" || pass == "" {
		res.Warnings = append(res.Warnings, "credential empty")
		return res
	}

	stmtMs := s.statementTimeoutMs()
	shardRegex := s.shardRegex()
	topN := opts.topN
	if topN <= 0 {
		topN = pgSpaceDefaultTopN
	}

	report := func(stage string, dbDone, dbTotal int, currentDB string) bool {
		if opts.onProgress == nil {
			return false
		}
		if err := opts.onProgress(stage, dbDone, dbTotal, currentDB); err != nil {
			res.Warnings = append(res.Warnings, "collect stopped: "+err.Error())
			return true
		}
		return false
	}

	if report("cluster", 0, 0, "") {
		return res
	}
	clusterConn, cerr := openPgSpaceConn(ctx, t.host, t.port, user, pass, defaultDB, stmtMs)
	if cerr != nil {
		res.Warnings = append(res.Warnings, "connect postgres db failed: "+cerr.Error())
		return res
	}
	dbList := queryClusterLevel(ctx, clusterConn, res)
	_ = clusterConn.Close()

	scanDbs := s.selectScanDbs(opts, dbList, res)
	if len(scanDbs) == 0 {
		return res
	}

	for i, dbName := range scanDbs {
		select {
		case <-ctx.Done():
			res.Warnings = append(res.Warnings, "context canceled during db scan")
			return res
		default:
		}
		if report("scan_db", i, len(scanDbs), dbName) {
			return res
		}
		conn, e := openPgSpaceConn(ctx, t.host, t.port, user, pass, dbName, stmtMs)
		if e != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("connect db %s failed: %s", dbName, e.Error()))
			continue
		}
		queryDbLevel(ctx, conn, dbName, shardRegex, topN, res)
		_ = conn.Close()
		res.ScannedDbs = append(res.ScannedDbs, dbName)
	}
	report("scan_db", len(scanDbs), len(scanDbs), "")
	return res
}

func queryClusterLevel(ctx context.Context, conn *sql.DB, res *analysis) []dbInfo {
	if rows, err := conn.QueryContext(ctx, pgSpaceTablespaceSQL); err != nil {
		res.Warnings = append(res.Warnings, "tablespace query failed: "+err.Error())
	} else {
		for rows.Next() {
			var name string
			var size sql.NullInt64
			if rows.Scan(&name, &size) == nil {
				res.Tablespaces = append(res.Tablespaces, &store.SpaceRow{
					ObjectType: "tablespace", ObjectName: name, ShardCount: 1, SizeBytes: nullableInt64(size),
				})
			}
		}
		rows.Close()
	}

	var dbList []dbInfo
	if rows, err := conn.QueryContext(ctx, pgSpaceDatabaseSQL); err != nil {
		res.Warnings = append(res.Warnings, "database query failed: "+err.Error())
	} else {
		for rows.Next() {
			var name, tsName string
			var size, datAge sql.NullInt64
			if rows.Scan(&name, &size, &tsName, &datAge) == nil {
				res.Databases = append(res.Databases, &store.SpaceRow{
					ObjectType: "database", ObjectName: name, DbName: name, TablespaceName: tsName,
					ShardCount: 1, SizeBytes: nullableInt64(size), DatFrozenXIDAge: nullableInt64(datAge),
				})
				var sz int64
				if size.Valid {
					sz = size.Int64
				}
				dbList = append(dbList, dbInfo{Name: name, Size: sz})
			}
		}
		rows.Close()
	}
	return dbList
}

func queryDbLevel(ctx context.Context, conn *sql.DB, dbName, shardRegex string, topN int, res *analysis) {
	tableMap := map[string]*store.SpaceRow{}
	tableOrder := []string{}

	scanTables := func(q string) {
		rows, err := conn.QueryContext(ctx, q, shardRegex, topN)
		if err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s tables query failed: %s", dbName, err.Error()))
			return
		}
		defer rows.Close()
		for rows.Next() {
			var schema, logical string
			var shard, total, table, index, toast, live, dead sql.NullInt64
			var seqScan, tblIdx, hbr, hbh, nIns, nUpd, nDel, xidAge sql.NullInt64
			var lastVac, lastAutoVac, lastAna, lastAutoAna sql.NullTime
			if err := rows.Scan(&schema, &logical, &shard, &total, &table, &index, &toast, &live, &dead,
				&seqScan, &tblIdx, &hbr, &hbh, &nIns, &nUpd, &nDel,
				&lastVac, &lastAutoVac, &lastAna, &lastAutoAna, &xidAge); err != nil {
				continue
			}
			key := schema + "." + logical
			if _, ok := tableMap[key]; ok {
				continue
			}
			row := &store.SpaceRow{
				ObjectType: "table", DbName: dbName, SchemaName: schema, ObjectName: logical,
				ShardCount: valOr1(shard),
				SizeBytes:  nullableInt64(total), TableBytes: nullableInt64(table),
				IndexBytes: nullableInt64(index), ToastBytes: nullableInt64(toast),
				NLiveTup: nullableInt64(live), NDeadTup: nullableInt64(dead),
				SeqScan: nullableInt64(seqScan), TblIdxScan: nullableInt64(tblIdx),
				HeapBlksRead: nullableInt64(hbr), HeapBlksHit: nullableInt64(hbh),
				NTupIns: nullableInt64(nIns), NTupUpd: nullableInt64(nUpd), NTupDel: nullableInt64(nDel),
				LastVacuum: timePtrNull(lastVac), LastAutovacuum: timePtrNull(lastAutoVac),
				LastAnalyze: timePtrNull(lastAna), LastAutoanalyze: timePtrNull(lastAutoAna),
				RelFrozenXIDAge: nullableInt64(xidAge),
			}
			fillBloat(row)
			fillTableDerived(row)
			tableMap[key] = row
			tableOrder = append(tableOrder, key)
		}
	}
	scanTables(pgSpaceTablesSQL)
	scanTables(pgSpaceHotTablesSQL)
	for _, k := range tableOrder {
		res.Tables = append(res.Tables, tableMap[k])
	}

	if rows, err := conn.QueryContext(ctx, pgSpaceSequencesSQL, shardRegex, topN); err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%s sequences query failed: %s", dbName, err.Error()))
	} else {
		for rows.Next() {
			var schema, logical string
			var shard, last, maxV, minV, rem sql.NullInt64
			var used sql.NullFloat64
			var cycle sql.NullBool
			if rows.Scan(&schema, &logical, &shard, &last, &maxV, &minV, &cycle, &used, &rem) != nil {
				continue
			}
			row := &store.SpaceRow{
				ObjectType: "sequence", DbName: dbName, SchemaName: schema, ObjectName: logical,
				ShardCount:   valOr1(shard),
				SeqLastValue: nullableInt64(last), SeqMaxValue: nullableInt64(maxV), SeqMinValue: nullableInt64(minV),
				SeqUsedRatio: nullableFloat64(used), SeqRemaining: nullableInt64(rem),
			}
			if cycle.Valid {
				v := cycle.Bool
				row.SeqCycle = &v
			}
			res.Sequences = append(res.Sequences, row)
		}
		rows.Close()
	}

	idxMap := map[string]*store.SpaceRow{}
	order := []string{}
	scanIdx := func(q string, markUnused bool) {
		rows, err := conn.QueryContext(ctx, q, shardRegex, topN)
		if err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s index query failed: %s", dbName, err.Error()))
			return
		}
		defer rows.Close()
		for rows.Next() {
			var schema, logical string
			var shard, size, idxScan sql.NullInt64
			if rows.Scan(&schema, &logical, &shard, &size, &idxScan) != nil {
				continue
			}
			key := schema + "." + logical
			if r, ok := idxMap[key]; ok {
				if markUnused {
					u := true
					r.IsUnused = &u
				}
				continue
			}
			u := markUnused
			r := &store.SpaceRow{
				ObjectType: "index", DbName: dbName, SchemaName: schema, ObjectName: logical,
				ShardCount: valOr1(shard), SizeBytes: nullableInt64(size),
				IdxScan: nullableInt64(idxScan), IsUnused: &u,
			}
			idxMap[key] = r
			order = append(order, key)
		}
	}
	scanIdx(pgSpaceIndexesSQL, false)
	scanIdx(pgSpaceUnusedIndexesSQL, true)
	for _, k := range order {
		r := idxMap[k]
		res.Indexes = append(res.Indexes, r)
		if r.IsUnused != nil && *r.IsUnused {
			res.UnusedIndexes = append(res.UnusedIndexes, r)
		}
	}
}

func (s *Service) selectScanDbs(opts analyzeOptions, dbList []dbInfo, res *analysis) []string {
	switch opts.scanMode {
	case scanNone:
		return nil
	case scanOne:
		name := strings.TrimSpace(opts.specificDb)
		if name == "" {
			return nil
		}
		return []string{name}
	case scanAll:
		maxDbs := s.maxDatabases()
		minSize := s.minDbSizeBytes()
		out := make([]string, 0, len(dbList))
		for _, d := range dbList {
			if minSize > 0 && d.Size < minSize {
				continue
			}
			out = append(out, d.Name)
			if maxDbs > 0 && len(out) >= maxDbs {
				break
			}
		}
		if len(out) == 0 {
			res.Warnings = append(res.Warnings, "no connectable database to scan")
		}
		return out
	}
	return nil
}

func analysisRows(a *analysis, t connTarget, collectedAt time.Time) []*store.SpaceRow {
	if a == nil {
		return nil
	}
	all := make([]*store.SpaceRow, 0, len(a.Tablespaces)+len(a.Databases)+len(a.Tables)+len(a.Indexes)+len(a.Sequences))
	appendAll := func(rows []*store.SpaceRow) {
		for _, r := range rows {
			r.CollectedAt = collectedAt
			r.InstanceID = t.inst.ID
			r.InstanceName = t.inst.Name
			r.Host = t.host
			r.Port = t.port
			all = append(all, r)
		}
	}
	appendAll(a.Tablespaces)
	appendAll(a.Databases)
	appendAll(a.Tables)
	appendAll(a.Indexes)
	appendAll(a.Sequences)
	return all
}

func (s *Service) runCollectTargets(ctx context.Context, physicals []physicalInstance, reporter *collectReporter) (instancesProcessed, rowsWritten int, err error) {
	scanAllFlag := s.scanAllDatabases()
	topN := s.topN()
	collectedAt := time.Now().UTC()

	total := len(physicals)
	for i, p := range physicals {
		if e := ctx.Err(); e != nil {
			return instancesProcessed, rowsWritten, e
		}
		t := p.target
		reporter.instanceStart(ctx, i, total, t.host)

		mode := scanNone
		if scanAllFlag {
			mode = scanAll
		}
		opts := analyzeOptions{scanMode: mode, topN: topN}
		if reporter != nil {
			opts.onProgress = reporter.progressFn(ctx)
		}

		lk := pgSpaceLockFor(connectionKey(t.host, t.port))
		lk.Lock()
		a := s.analyzeOne(ctx, t, opts)
		lk.Unlock()

		rows := analysisRows(a, t, collectedAt)
		if len(rows) > 0 {
			n, be := s.Store.BatchInsertSpaceSnapshots(ctx, rows)
			if be != nil {
				// continue other instances
			} else {
				rowsWritten += n
				reporter.addRows(ctx, n)
				s.persistAdviceFromRows(ctx, rows, t.inst.ID, t.inst.Name, t.host, t.port, collectedAt)
			}
		}
		instancesProcessed++
		reporter.instanceDone(ctx, instancesProcessed)

		if e := ctx.Err(); e != nil {
			return instancesProcessed, rowsWritten, e
		}
	}
	return instancesProcessed, rowsWritten, nil
}

// ---------- 读 API ----------

// ListInstances 物理实例下拉（按 host:port 去重）。
func (s *Service) ListInstances(ctx context.Context, q string) ([]*InstanceItem, error) {
	physicals, err := s.enumeratePhysicalPostgresInstances()
	if err != nil {
		return nil, err
	}
	kw := strings.ToLower(strings.TrimSpace(q))
	out := make([]*InstanceItem, 0, len(physicals))
	for _, p := range physicals {
		item := &InstanceItem{
			InstanceID: p.target.inst.ID,
			Name:       p.target.inst.Name,
			Host:       p.target.host,
			Port:       p.target.port,
			IDC:        p.target.inst.IDC,
		}
		if kw != "" {
			if !strings.Contains(strings.ToLower(item.Name), kw) && !strings.Contains(strings.ToLower(item.Host), kw) {
				continue
			}
		}
		out = append(out, item)
	}
	return out, nil
}

// Realtime 实时直连分析。
func (s *Service) Realtime(ctx context.Context, instanceID, host string, port int64, dbName string, topN int) (*RealtimeData, error) {
	target, err := s.resolveTarget(instanceID, host, port)
	if err != nil {
		return nil, err
	}

	lk := pgSpaceLockFor(connectionKey(target.host, target.port))
	lk.Lock()
	defer lk.Unlock()

	if topN <= 0 {
		topN = 20
	}
	if topN > 200 {
		topN = 200
	}
	opts := analyzeOptions{topN: topN}
	switch strings.TrimSpace(dbName) {
	case "":
		opts.scanMode = scanNone
	case "__all__":
		opts.scanMode = scanAll
	default:
		opts.scanMode = scanOne
		opts.specificDb = strings.TrimSpace(dbName)
	}

	a := s.analyzeOne(ctx, target, opts)
	return &RealtimeData{
		InstanceID:    target.inst.ID,
		InstanceName:  target.inst.Name,
		Host:          target.host,
		Port:          target.port,
		CollectedAt:   time.Now().UTC(),
		Tablespaces:   rowsToDTO(a.Tablespaces),
		Databases:     rowsToDTO(a.Databases),
		Tables:        rowsToDTO(a.Tables),
		Indexes:       rowsToDTO(a.Indexes),
		UnusedIndexes: rowsToDTO(a.UnusedIndexes),
		Sequences:     rowsToDTO(a.Sequences),
		ScannedDbs:    a.ScannedDbs,
		Warnings:      a.Warnings,
	}, nil
}

// Databases 候选库列表。
func (s *Service) Databases(ctx context.Context, instanceID, host string, port int64) (*DatabasesData, error) {
	rows, _, err := s.Store.ListSpaceSnapshots(ctx, store.SpaceListFilter{
		InstanceID: instanceID, Host: host, Port: port,
		ObjectType: "database", OnlyLatest: true, Limit: 500,
	})
	if err != nil {
		return nil, err
	}
	data := &DatabasesData{Databases: []*DatabaseItem{}}
	if len(rows) > 0 {
		data.Source = "snapshot"
		t := rows[0].CollectedAt
		data.CollectedAt = &t
		data.Databases = rowsToDatabaseItems(rows)
		return data, nil
	}

	target, terr := s.resolveTarget(instanceID, host, port)
	if terr != nil {
		return nil, terr
	}
	liveRows, lerr := s.fetchDatabasesLive(ctx, target)
	if lerr != nil {
		return nil, lerr
	}
	data.Source = "realtime"
	now := time.Now().UTC()
	data.CollectedAt = &now
	data.Databases = rowsToDatabaseItems(liveRows)
	return data, nil
}

func (s *Service) fetchDatabasesLive(ctx context.Context, t connTarget) ([]*store.SpaceRow, error) {
	pass, err := s.decryptPassword(t.inst)
	if err != nil {
		return nil, err
	}
	user := strings.TrimSpace(t.inst.Username)
	if user == "" || pass == "" {
		return nil, fmt.Errorf("credential empty")
	}
	conn, err := openPgSpaceConn(ctx, t.host, t.port, user, pass, defaultDB, s.statementTimeoutMs())
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	rows, err := conn.QueryContext(ctx, pgSpaceDatabaseSQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*store.SpaceRow
	for rows.Next() {
		var name, tsName string
		var size, datAge sql.NullInt64
		if rows.Scan(&name, &size, &tsName, &datAge) != nil {
			continue
		}
		out = append(out, &store.SpaceRow{
			ObjectType: "database", ObjectName: name, DbName: name, TablespaceName: tsName,
			ShardCount: 1, SizeBytes: nullableInt64(size), DatFrozenXIDAge: nullableInt64(datAge),
		})
	}
	return out, rows.Err()
}

// Snapshots 历史快照分页。
func (s *Service) Snapshots(ctx context.Context, instanceID, host string, port int64, objectType, dbName string, onlyLatest bool, current, pageSize int) (*SnapshotPage, error) {
	if current < 1 {
		current = 1
	}
	if pageSize <= 0 {
		pageSize = 50
	}
	rows, total, err := s.Store.ListSpaceSnapshots(ctx, store.SpaceListFilter{
		InstanceID: instanceID, Host: host, Port: port,
		ObjectType: objectType, DbName: dbName, OnlyLatest: onlyLatest,
		Offset: (current - 1) * pageSize, Limit: pageSize,
	})
	if err != nil {
		return nil, err
	}
	list := make([]*SnapshotItem, 0, len(rows))
	for _, r := range rows {
		list = append(list, &SnapshotItem{
			ID: r.ID, CollectedAt: r.CollectedAt, Object: *rowToObject(r),
			InstanceID: r.InstanceID, InstanceName: r.InstanceName, Host: r.Host, Port: r.Port,
		})
	}
	return &SnapshotPage{List: list, Total: total, Current: current, PageSize: pageSize}, nil
}

// Trend 增长趋势。
func (s *Service) Trend(ctx context.Context, instanceID, host string, port int64, objectType, dbName, objectName, startTime, endTime string, capacityBytes *int64) (*TrendData, error) {
	filter := store.SpaceTrendFilter{
		InstanceID: instanceID, Host: host, Port: port,
		ObjectType: strings.TrimSpace(objectType), DbName: dbName, ObjectName: strings.TrimSpace(objectName),
	}
	if t, ok := parseTime(startTime); ok {
		filter.StartTime = &t
	}
	if t, ok := parseTime(endTime); ok {
		filter.EndTime = &t
	}
	points, err := s.Store.SpaceTrend(ctx, filter)
	if err != nil {
		return nil, err
	}
	data := &TrendData{
		ObjectType: filter.ObjectType, DbName: dbName, ObjectName: filter.ObjectName,
		Points: make([]*TrendPoint, 0, len(points)),
	}
	for _, p := range points {
		data.Points = append(data.Points, &TrendPoint{CollectedAt: p.CollectedAt, SizeBytes: p.SizeBytes})
	}
	computeGrowth(data, points, capacityBytes)
	return data, nil
}

func computeGrowth(data *TrendData, points []*store.SpaceTrendPoint, capacityBytes *int64) {
	var first, last *store.SpaceTrendPoint
	for _, p := range points {
		if p.SizeBytes == nil {
			continue
		}
		if first == nil {
			first = p
		}
		last = p
	}
	if first == nil || last == nil {
		return
	}
	fv, lv := *first.SizeBytes, *last.SizeBytes
	data.FirstSizeBytes = &fv
	data.LastSizeBytes = &lv
	growth := lv - fv
	data.GrowthBytes = &growth
	days := last.CollectedAt.Sub(first.CollectedAt).Hours() / 24
	if days <= 0 {
		return
	}
	daily := float64(growth) / days
	data.DailyGrowthBytes = &daily
	if capacityBytes != nil && *capacityBytes > 0 && daily > 0 {
		remain := float64(*capacityBytes - lv)
		if remain < 0 {
			remain = 0
		}
		dtf := remain / daily
		data.DaysToFull = &dtf
	}
}

// Summary 可视汇总。
func (s *Service) Summary(ctx context.Context, instanceID, host string, port int64, topN int) (*SummaryData, error) {
	if topN <= 0 {
		topN = 10
	}
	if topN > 50 {
		topN = 50
	}
	latest, ok, err := s.Store.LatestSpaceTime(ctx, instanceID, host, port)
	if err != nil {
		return nil, err
	}
	data := &SummaryData{
		Tablespaces: []*Object{}, Databases: []*Object{},
		TopTables: []*Object{}, TopIndexes: []*Object{},
		TopBloat: []*Object{}, UnusedIndexes: []*Object{},
		TopSequences: []*Object{}, TopSeqScan: []*Object{},
		TopCacheMiss: []*Object{}, TopHotTables: []*Object{},
		TopDeadTuples: []*Object{}, TopXIDAge: []*Object{},
	}
	if !ok {
		return data, nil
	}
	data.CollectedAt = &latest

	fetch := func(objType string, limit int) []*store.SpaceRow {
		rows, _, e := s.Store.ListSpaceSnapshots(ctx, store.SpaceListFilter{
			InstanceID: instanceID, Host: host, Port: port,
			ObjectType: objType, OnlyLatest: true, Limit: limit,
		})
		if e != nil {
			return nil
		}
		return rows
	}

	data.Tablespaces = rowsToDTO(fetch("tablespace", 200))
	dbRows := fetch("database", 500)
	data.Databases = rowsToDTO(dbRows)
	for _, r := range dbRows {
		if r.SizeBytes != nil {
			data.TotalDatabaseBytes += *r.SizeBytes
		}
	}
	data.TopTables = rowsToDTO(fetch("table", topN))

	idxRows := fetch("index", 1000)
	data.TopIndexes = rowsToDTO(head(idxRows, topN))
	var unused []*store.SpaceRow
	for _, r := range idxRows {
		if r.IsUnused != nil && *r.IsUnused {
			unused = append(unused, r)
			if r.SizeBytes != nil {
				data.UnusedIndexBytes += *r.SizeBytes
			}
		}
	}
	data.UnusedIndexes = rowsToDTO(head(unused, topN))

	tblRows := fetch("table", 1000)
	sort.SliceStable(tblRows, func(i, j int) bool {
		return valInt64(tblRows[i].BloatBytes) > valInt64(tblRows[j].BloatBytes)
	})
	data.TopBloat = rowsToDTO(head(tblRows, topN))

	deadSorted := append([]*store.SpaceRow(nil), tblRows...)
	sort.SliceStable(deadSorted, func(i, j int) bool {
		return valInt64(deadSorted[i].NDeadTup) > valInt64(deadSorted[j].NDeadTup)
	})
	data.TopDeadTuples = rowsToDTO(head(deadSorted, topN))

	seqScanSorted := append([]*store.SpaceRow(nil), tblRows...)
	sort.SliceStable(seqScanSorted, func(i, j int) bool {
		return valInt64(seqScanSorted[i].SeqScan) > valInt64(seqScanSorted[j].SeqScan)
	})
	data.TopSeqScan = rowsToDTO(head(seqScanSorted, topN))

	cacheSorted := append([]*store.SpaceRow(nil), tblRows...)
	sort.SliceStable(cacheSorted, func(i, j int) bool {
		return valFloat64(cacheSorted[i].HeapCacheHitRatio) < valFloat64(cacheSorted[j].HeapCacheHitRatio)
	})
	data.TopCacheMiss = rowsToDTO(head(cacheSorted, topN))

	hotSorted := append([]*store.SpaceRow(nil), tblRows...)
	sort.SliceStable(hotSorted, func(i, j int) bool {
		return nTupDML(hotSorted[i]) > nTupDML(hotSorted[j])
	})
	data.TopHotTables = rowsToDTO(head(hotSorted, topN))

	xidSorted := append([]*store.SpaceRow(nil), tblRows...)
	sort.SliceStable(xidSorted, func(i, j int) bool {
		return valInt64(xidSorted[i].RelFrozenXIDAge) > valInt64(xidSorted[j].RelFrozenXIDAge)
	})
	data.TopXIDAge = rowsToDTO(head(xidSorted, topN))

	seqRows := fetch("sequence", 1000)
	sort.SliceStable(seqRows, func(i, j int) bool {
		return valFloat64(seqRows[i].SeqUsedRatio) > valFloat64(seqRows[j].SeqUsedRatio)
	})
	data.TopSequences = rowsToDTO(head(seqRows, topN))
	return data, nil
}

// ---------- helpers ----------

func fillTableDerived(r *store.SpaceRow) {
	var hit, read int64
	if r.HeapBlksHit != nil {
		hit = *r.HeapBlksHit
	}
	if r.HeapBlksRead != nil {
		read = *r.HeapBlksRead
	}
	if denom := hit + read; denom > 0 {
		ratio := float64(hit) / float64(denom)
		r.HeapCacheHitRatio = &ratio
	}
}

func fillBloat(r *store.SpaceRow) {
	var live, dead int64
	if r.NLiveTup != nil {
		live = *r.NLiveTup
	}
	if r.NDeadTup != nil {
		dead = *r.NDeadTup
	}
	denom := live + dead
	if denom <= 0 {
		return
	}
	ratio := float64(dead) / float64(denom)
	r.DeadRatio = &ratio
	r.BloatRatio = &ratio
	if r.TableBytes != nil {
		b := int64(ratio * float64(*r.TableBytes))
		r.BloatBytes = &b
	}
}

func rowToObject(r *store.SpaceRow) *Object {
	return &Object{
		ObjectType: r.ObjectType, DbName: r.DbName, SchemaName: r.SchemaName,
		ObjectName: r.ObjectName, TablespaceName: r.TablespaceName, ShardCount: r.ShardCount,
		SizeBytes: r.SizeBytes, TableBytes: r.TableBytes, IndexBytes: r.IndexBytes, ToastBytes: r.ToastBytes,
		NLiveTup: r.NLiveTup, NDeadTup: r.NDeadTup, DeadRatio: r.DeadRatio,
		BloatBytes: r.BloatBytes, BloatRatio: r.BloatRatio, IdxScan: r.IdxScan, IsUnused: r.IsUnused,
		SeqScan: r.SeqScan, TblIdxScan: r.TblIdxScan,
		HeapBlksRead: r.HeapBlksRead, HeapBlksHit: r.HeapBlksHit, HeapCacheHitRatio: r.HeapCacheHitRatio,
		NTupIns: r.NTupIns, NTupUpd: r.NTupUpd, NTupDel: r.NTupDel,
		LastVacuum: r.LastVacuum, LastAutovacuum: r.LastAutovacuum,
		LastAnalyze: r.LastAnalyze, LastAutoanalyze: r.LastAutoanalyze,
		RelFrozenXIDAge: r.RelFrozenXIDAge, DatFrozenXIDAge: r.DatFrozenXIDAge,
		SeqLastValue: r.SeqLastValue, SeqMaxValue: r.SeqMaxValue, SeqMinValue: r.SeqMinValue,
		SeqUsedRatio: r.SeqUsedRatio, SeqRemaining: r.SeqRemaining, SeqCycle: r.SeqCycle,
	}
}

func rowsToDTO(rows []*store.SpaceRow) []*Object {
	out := make([]*Object, 0, len(rows))
	for _, r := range rows {
		out = append(out, rowToObject(r))
	}
	return out
}

func rowsToDatabaseItems(rows []*store.SpaceRow) []*DatabaseItem {
	out := make([]*DatabaseItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, &DatabaseItem{DbName: r.DbName, SizeBytes: r.SizeBytes, TablespaceName: r.TablespaceName})
	}
	return out
}

func head(rows []*store.SpaceRow, n int) []*store.SpaceRow {
	if n <= 0 || len(rows) <= n {
		return rows
	}
	return rows[:n]
}

func parseTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	if t, err := time.Parse("2006-01-02 15:04:05", s); err == nil {
		return t.UTC(), true
	}
	return time.Time{}, false
}

func nullableInt64(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	x := v.Int64
	return &x
}

func nullableFloat64(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	x := v.Float64
	return &x
}

func timePtrNull(v sql.NullTime) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time
	return &t
}

func valOr1(v sql.NullInt64) int64 {
	if v.Valid && v.Int64 > 0 {
		return v.Int64
	}
	return 1
}

func valInt64(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

func valFloat64(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

func nTupDML(r *store.SpaceRow) int64 {
	if r == nil {
		return 0
	}
	return valInt64(r.NTupIns) + valInt64(r.NTupUpd) + valInt64(r.NTupDel)
}
