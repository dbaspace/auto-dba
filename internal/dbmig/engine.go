package dbmig

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"auto-dba/internal/store"
)

type Engine struct {
	Store  *store.DB
	Secret string
	mu     sync.Mutex
	cancel map[string]context.CancelFunc
	paused map[string]bool
}

func New(st *store.DB, secret string) *Engine {
	return &Engine{Store: st, Secret: secret, cancel: map[string]context.CancelFunc{}, paused: map[string]bool{}}
}

func (e *Engine) Start(taskID string) error {
	return e.start(taskID, false)
}

func (e *Engine) Resume(taskID string) error {
	return e.start(taskID, true)
}

func (e *Engine) start(taskID string, resume bool) error {
	t, err := e.Store.GetMigration(taskID)
	if err != nil || t == nil {
		return fmt.Errorf("任务不存在")
	}
	if t.Status == store.StatusRunning || t.Status == store.StatusPreChecking {
		return fmt.Errorf("任务已在执行")
	}
	if !store.MigrationApproved(t) {
		return fmt.Errorf("审批未通过，不能启动")
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.mu.Lock()
	if old, ok := e.cancel[taskID]; ok {
		old()
	}
	e.cancel[taskID] = cancel
	delete(e.paused, taskID)
	e.mu.Unlock()
	go e.run(ctx, taskID, resume)
	return nil
}

func (e *Engine) Cancel(taskID string) {
	e.mu.Lock()
	if c, ok := e.cancel[taskID]; ok {
		c()
		delete(e.cancel, taskID)
	}
	e.mu.Unlock()
}

func (e *Engine) Pause(taskID string) error {
	e.mu.Lock()
	e.paused[taskID] = true
	e.mu.Unlock()
	e.Cancel(taskID)
	t, err := e.Store.GetMigration(taskID)
	if err != nil || t == nil {
		return fmt.Errorf("任务不存在")
	}
	t.Status = store.StatusPaused
	t.ErrorMessage = "已暂停"
	return e.Store.UpdateMigration(t)
}

func (e *Engine) logf(taskID, level, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	_ = e.Store.AddMigrationLog(taskID, level, msg)
}

func (e *Engine) run(ctx context.Context, taskID string, resume bool) {
	t, err := e.Store.GetMigration(taskID)
	if err != nil || t == nil {
		return
	}
	if !resume {
		t.CompletedObjects = 0
		t.CompletedRows = 0
		for i := range t.Objects {
			t.Objects[i].Status = store.StatusPending
			t.Objects[i].CompletedRows = 0
			t.Objects[i].ErrorMessage = ""
			_ = e.Store.UpdateMigrationObject(&t.Objects[i])
		}
	}
	t.Status = store.StatusPreChecking
	t.ErrorMessage = ""
	if !resume || t.StartedAt == "" {
		t.StartedAt = time.Now().UTC().Format(time.RFC3339)
	}
	t.FinishedAt = ""
	_ = e.Store.UpdateMigration(t)
	e.logf(taskID, "INFO", "开始预检")

	items := Precheck(ctx, e.Store, e.Secret, t)
	_ = e.Store.ReplaceMigrationPrechecks(taskID, items)
	if !PrecheckPassed(items) {
		t.Status = store.StatusPreCheckFailed
		t.ErrorMessage = "预检未通过"
		t.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		_ = e.Store.UpdateMigration(t)
		e.logf(taskID, "ERROR", "预检未通过")
		return
	}
	e.logf(taskID, "INFO", "预检通过，开始迁移")
	t.Status = store.StatusRunning
	_ = e.Store.UpdateMigration(t)

	if err := e.execute(ctx, t, resume); err != nil {
		e.mu.Lock()
		wasPaused := e.paused[taskID]
		delete(e.paused, taskID)
		e.mu.Unlock()
		if wasPaused {
			t.Status = store.StatusPaused
			t.ErrorMessage = "已暂停"
			e.logf(taskID, "WARN", "任务已暂停")
		} else if ctx.Err() != nil {
			t.Status = store.StatusCanceled
			t.ErrorMessage = "已取消"
			e.logf(taskID, "WARN", "任务已取消")
		} else {
			t.Status = store.StatusFailed
			t.ErrorMessage = err.Error()
			e.logf(taskID, "ERROR", "%s", err.Error())
			log.Printf("migration %s failed: %v", taskID, err)
		}
		t.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		_ = e.Store.UpdateMigration(t)
		return
	}
	t.Status = store.StatusCompleted
	t.ErrorMessage = ""
	t.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	_ = e.Store.UpdateMigration(t)
	e.logf(taskID, "INFO", "迁移完成，对象 %d，行 %d", t.CompletedObjects, t.CompletedRows)
}

type dbPair struct {
	src *sql.DB
	tgt *sql.DB
}

func (e *Engine) execute(ctx context.Context, t *store.MigrationTask, resume bool) error {
	engine := store.NormalizeDBType(t.DBType)
	srcInst, err := e.Store.GetInstance(t.SourceID)
	if err != nil || srcInst == nil {
		return fmt.Errorf("源实例不存在")
	}
	tgtInst, err := e.Store.GetInstance(t.TargetID)
	if err != nil || tgtInst == nil {
		return fmt.Errorf("目标实例不存在")
	}

	objs := t.Objects
	if len(objs) == 0 {
		return fmt.Errorf("未选择迁移对象")
	}
	t.TotalObjects = len(objs)
	_ = e.Store.UpdateMigration(t)

	perf := e.Store.GetMigrationSettings().Performance
	parallelism := perf.TableParallelism
	if parallelism <= 0 {
		parallelism = 4
	}
	if parallelism > 16 {
		parallelism = 16
	}
	tableTimeout := time.Duration(perf.SingleTableTimeoutMin) * time.Minute
	if tableTimeout <= 0 {
		tableTimeout = 30 * time.Minute
	}

	var pairMu sync.Mutex
	pairs := map[string]*dbPair{}
	defer func() {
		for _, p := range pairs {
			if p.src != nil {
				p.src.Close()
			}
			if p.tgt != nil {
				p.tgt.Close()
			}
		}
	}()

	getPair := func(srcDB, tgtDB string) (*dbPair, error) {
		key := srcDB + "\x00" + tgtDB
		pairMu.Lock()
		if p, ok := pairs[key]; ok {
			pairMu.Unlock()
			return p, nil
		}
		pairMu.Unlock()
		src, err := OpenInstance(ctx, srcInst, e.Secret, firstNonEmpty(srcDB, defaultDB(engine)))
		if err != nil {
			return nil, fmt.Errorf("连接源库 %s: %w", srcDB, err)
		}
		src.SetMaxOpenConns(parallelism + 2)
		tgt, err := OpenInstance(ctx, tgtInst, e.Secret, firstNonEmpty(tgtDB, srcDB, defaultDB(engine)))
		if err != nil {
			src.Close()
			return nil, fmt.Errorf("连接目标库 %s: %w", tgtDB, err)
		}
		tgt.SetMaxOpenConns(parallelism + 2)
		p := &dbPair{src: src, tgt: tgt}
		pairMu.Lock()
		if exist, ok := pairs[key]; ok {
			pairMu.Unlock()
			src.Close()
			tgt.Close()
			return exist, nil
		}
		pairs[key] = p
		pairMu.Unlock()
		return p, nil
	}

	pairsNames := tablePairsFromObjects(objs)
	if engine == "postgres" && t.SchemaStrategy != "no_init" {
		seenDB := map[string]struct{}{}
		for _, o := range objs {
			key := o.SourceDB + "\x00" + firstNonEmpty(o.TargetDB, o.SourceDB)
			if _, ok := seenDB[key]; ok {
				continue
			}
			seenDB[key] = struct{}{}
			pair, err := getPair(o.SourceDB, firstNonEmpty(o.TargetDB, o.SourceDB))
			if err != nil {
				return err
			}
			if err := preparePGDatabase(ctx, pair.src, pair.tgt); err != nil {
				return err
			}
		}
	}

	var (
		progressMu sync.Mutex
		firstErr   error
		errOnce    sync.Once
		pendMu     sync.Mutex
		pendFKs    []deferredStmt
		pendTrigs  []deferredStmt
	)
	setErr := func(err error) {
		errOnce.Do(func() { firstErr = err })
	}

	sem := make(chan struct{}, parallelism)
	var wg sync.WaitGroup
	workCtx, cancelWork := context.WithCancel(ctx)
	defer cancelWork()

	for i := range objs {
		o := &objs[i]
		if resume && o.Status == store.StatusCompleted {
			continue
		}
		if err := workCtx.Err(); err != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(o *store.MigrationObject) {
			defer wg.Done()
			defer func() { <-sem }()
			if workCtx.Err() != nil {
				return
			}
			pair, err := getPair(o.SourceDB, firstNonEmpty(o.TargetDB, o.SourceDB))
			if err != nil {
				o.Status = store.StatusFailed
				o.ErrorMessage = err.Error()
				_ = e.Store.UpdateMigrationObject(o)
				setErr(err)
				cancelWork()
				return
			}

			o.Status = store.StatusRunning
			_ = e.Store.UpdateMigrationObject(o)
			e.logf(t.ID, "INFO", "处理表 %s.%s", o.SourceDB, o.SourceTable)

			tableCtx, tableCancel := context.WithTimeout(workCtx, tableTimeout)
			defer tableCancel()

			var deferred *schemaDeferred
			if t.SchemaStrategy != "no_init" && !(resume && o.CompletedRows > 0) {
				var err error
				deferred, err = applySchema(tableCtx, engine, pair.src, pair.tgt, o, t.ConflictStrategy, pairsNames)
				if err != nil {
					o.Status = store.StatusFailed
					o.ErrorMessage = err.Error()
					_ = e.Store.UpdateMigrationObject(o)
					setErr(fmt.Errorf("结构迁移 %s: %w", o.SourceTable, err))
					cancelWork()
					return
				}
			} else if t.MigrationMode == "full_data" && t.ConflictStrategy == "truncate" && o.CompletedRows == 0 {
				if err := truncateTable(tableCtx, pair.tgt, engine, o); err != nil {
					o.Status = store.StatusFailed
					o.ErrorMessage = err.Error()
					_ = e.Store.UpdateMigrationObject(o)
					setErr(err)
					cancelWork()
					return
				}
			}

			if t.MigrationMode != "schema_only" {
				n, err := copyTable(tableCtx, engine, pair.src, pair.tgt, o, t.BatchSize, t.SpeedLimit, func(delta int64) error {
					progressMu.Lock()
					o.CompletedRows += delta
					t.CompletedRows += delta
					progressMu.Unlock()
					_ = e.Store.UpdateMigrationObject(o)
					_ = e.Store.UpdateMigration(t)
					return tableCtx.Err()
				})
				if err != nil {
					o.Status = store.StatusFailed
					o.ErrorMessage = err.Error()
					_ = e.Store.UpdateMigrationObject(o)
					setErr(fmt.Errorf("数据迁移 %s: %w", o.SourceTable, err))
					cancelWork()
					return
				}
				progressMu.Lock()
				o.TotalRows = n
				o.CompletedRows = n
				progressMu.Unlock()
				if engine == "postgres" {
					ss := firstNonEmpty(o.SourceSchema, "public")
					ts := firstNonEmpty(o.TargetSchema, ss)
					seqCols, serr := listSequenceColumns(tableCtx, pair.src, ss, o.SourceTable)
					if serr != nil {
						o.Status = store.StatusFailed
						o.ErrorMessage = serr.Error()
						_ = e.Store.UpdateMigrationObject(o)
						setErr(fmt.Errorf("序列收集 %s: %w", o.SourceTable, serr))
						cancelWork()
						return
					}
					if err := syncPGSequencesAfterData(tableCtx, pair.tgt, ts, o.TargetTable, seqCols); err != nil {
						o.Status = store.StatusFailed
						o.ErrorMessage = err.Error()
						_ = e.Store.UpdateMigrationObject(o)
						setErr(fmt.Errorf("序列同步 %s: %w", o.SourceTable, err))
						cancelWork()
						return
					}
				} else if err := syncMySQLAutoIncrement(tableCtx, pair.tgt, o.TargetTable); err != nil {
					o.Status = store.StatusFailed
					o.ErrorMessage = err.Error()
					_ = e.Store.UpdateMigrationObject(o)
					setErr(fmt.Errorf("更新 AUTO_INCREMENT %s: %w", o.SourceTable, err))
					cancelWork()
					return
				}
			} else if engine == "postgres" && deferred != nil && deferred.Created {
				ts := firstNonEmpty(o.TargetSchema, firstNonEmpty(o.SourceSchema, "public"))
				if err := syncPGSequencesFromSource(tableCtx, pair.tgt, retargetSeqSync(deferred.SeqSyncs, ts, o.TargetTable)); err != nil {
					o.Status = store.StatusFailed
					o.ErrorMessage = err.Error()
					_ = e.Store.UpdateMigrationObject(o)
					setErr(fmt.Errorf("序列同步 %s: %w", o.SourceTable, err))
					cancelWork()
					return
				}
			}
			if deferred != nil && deferred.Created {
				ts := firstNonEmpty(o.TargetSchema, firstNonEmpty(o.SourceSchema, "public"))
				key := strings.ToLower(ts + "." + o.TargetTable)
				pendMu.Lock()
				for _, sqlText := range deferred.FKs {
					pendFKs = append(pendFKs, deferredStmt{DB: pair.tgt, SQL: sqlText, Table: key})
				}
				for _, sqlText := range deferred.Triggers {
					pendTrigs = append(pendTrigs, deferredStmt{DB: pair.tgt, SQL: sqlText, Table: key})
				}
				pendMu.Unlock()
			}
			o.Status = store.StatusCompleted
			o.ErrorMessage = ""
			_ = e.Store.UpdateMigrationObject(o)
			progressMu.Lock()
			t.CompletedObjects++
			progressMu.Unlock()
			_ = e.Store.UpdateMigration(t)
		}(o)
	}
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	if err := workCtx.Err(); err != nil {
		return err
	}
	allowed := map[string]struct{}{}
	for _, p := range pairsNames {
		allowed[strings.ToLower(p.TgtSchema+"."+p.TgtTable)] = struct{}{}
	}
	keptFKs, _ := filterDeferredByTables(pendFKs, allowed)
	for _, stmt := range keptFKs {
		if err := execSQL(ctx, stmt.DB, "外键创建", stmt.SQL); err != nil {
			return err
		}
	}
	keptTrigs, skipped := filterDeferredByTables(pendTrigs, allowed)
	if len(skipped) > 0 {
		e.logf(t.ID, "WARN", "跳过 %d 个触发器（所属表未纳入本次迁移）", len(skipped))
	}
	byDB := map[*sql.DB][]string{}
	var dbOrder []*sql.DB
	for _, stmt := range keptTrigs {
		if _, ok := byDB[stmt.DB]; !ok {
			dbOrder = append(dbOrder, stmt.DB)
		}
		byDB[stmt.DB] = append(byDB[stmt.DB], stmt.SQL)
	}
	for _, db := range dbOrder {
		if engine == "postgres" {
			if err := applyDeferredTriggers(ctx, db, byDB[db]); err != nil {
				return err
			}
			continue
		}
		for _, stmt := range byDB[db] {
			if err := execSQL(ctx, db, "触发器迁移", stmt); err != nil {
				return err
			}
		}
	}
	return nil
}

func retargetSeqSync(in []pgSeqSync, schema, table string) []pgSeqSync {
	out := make([]pgSeqSync, len(in))
	for i, s := range in {
		s.Schema = schema
		s.Table = table
		out[i] = s
	}
	return out
}

// StartScheduler 已废弃：由 jobreg migration.due_scanner 接管。
//
// Deprecated: 使用 jobboot.RegisterPlatformJobs。
func (e *Engine) StartScheduler(ctx context.Context) {
	_ = ctx
	log.Printf("DEPRECATED: dbmig.StartScheduler ignored; scheduling is owned by jobreg (migration.due_scanner)")
}

// ScanDue 扫描到期迁移任务（供 jobreg 调用）。
func (e *Engine) ScanDue() {
	if e == nil {
		return
	}
	list, err := e.Store.ListDueScheduledMigrations()
	if err != nil {
		return
	}
	for _, t := range list {
		if !store.MigrationApproved(&t) {
			continue
		}
		if err := e.Start(t.ID); err != nil {
			e.logf(t.ID, "WARN", "定时启动失败: %s", err.Error())
		} else {
			e.logf(t.ID, "INFO", "定时任务已触发")
		}
	}
}

func BuildObjects(engine, sourceDB, targetDB string, tables []string) []store.MigrationObject {
	engine = store.NormalizeDBType(engine)
	srcDB := strings.TrimSpace(sourceDB)
	tgtDB := strings.TrimSpace(targetDB)
	if tgtDB == "" {
		tgtDB = srcDB
	}
	var out []store.MigrationObject
	for _, raw := range tables {
		schema, table := splitTable(engine, raw)
		if table == "" {
			continue
		}
		ts := schema
		out = append(out, store.MigrationObject{
			SourceDB: srcDB, SourceSchema: schema, SourceTable: table,
			TargetDB: tgtDB, TargetSchema: ts, TargetTable: table, ObjectType: "table",
		})
	}
	return out
}

type ObjectSel struct {
	SourceDB    string `json:"source_db_name"`
	SourceTable string `json:"source_table_name"`
	TargetDB    string `json:"target_db_name"`
	TargetTable string `json:"target_table_name"`
	TotalBytes  int64  `json:"total_bytes"`
}

func BuildObjectsFromSels(engine string, items []ObjectSel) []store.MigrationObject {
	engine = store.NormalizeDBType(engine)
	var out []store.MigrationObject
	for _, it := range items {
		srcDB := strings.TrimSpace(it.SourceDB)
		srcTable := strings.TrimSpace(it.SourceTable)
		if srcDB == "" || srcTable == "" {
			continue
		}
		schema, table := splitTable(engine, srcTable)
		if table == "" {
			table = srcTable
		}
		tgtDB := strings.TrimSpace(it.TargetDB)
		if tgtDB == "" {
			tgtDB = srcDB
		}
		tgtTable := strings.TrimSpace(it.TargetTable)
		if tgtTable == "" {
			tgtTable = table
		}
		_, tt := splitTable(engine, tgtTable)
		if tt != "" {
			tgtTable = tt
		}
		out = append(out, store.MigrationObject{
			SourceDB: srcDB, SourceSchema: schema, SourceTable: table,
			TargetDB: tgtDB, TargetSchema: schema, TargetTable: tgtTable,
			ObjectType: "table", TotalBytes: it.TotalBytes,
		})
	}
	return out
}
