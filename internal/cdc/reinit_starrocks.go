package cdc

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"auto-dba/internal/dbmig"
	"auto-dba/internal/store"
)

func srManualMode(t *store.CDCTask, extra map[string]string) bool {
	return extra["auto_create_table"] == "false" || t == nil || strings.TrimSpace(t.TargetID) == ""
}

func (o *Orchestrator) openTargetDB(ctx context.Context, t *store.CDCTask) (*sql.DB, error) {
	inst, err := o.Store.GetInstance(t.TargetID)
	if err != nil || inst == nil {
		return nil, fmt.Errorf("目标实例不存在")
	}
	db, err := dbmig.OpenInstance(ctx, inst, o.Secret, t.TargetDB)
	if err != nil {
		return nil, fmt.Errorf("连接目标端失败: %w", err)
	}
	return db, nil
}

func srTableExists(ctx context.Context, db *sql.DB, targetDB, table string) (bool, error) {
	esc := strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(table)
	rows, err := db.QueryContext(ctx, fmt.Sprintf("SHOW TABLES FROM `%s` LIKE '%s'", strings.ReplaceAll(targetDB, "`", "``"), esc))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if rows.Scan(&name) == nil && strings.EqualFold(strings.TrimSpace(name), table) {
			return true, nil
		}
	}
	return false, rows.Err()
}

func routineLoadStateOf(ctx context.Context, db *sql.DB, targetDB, table, taskType, env, sourceDB, topicPrefix string) string {
	states := ListRoutineLoadStates(ctx, db, targetDB, []string{table}, taskType, env, sourceDB, topicPrefix)
	if len(states) == 0 {
		return "UNKNOWN"
	}
	return strings.ToUpper(strings.TrimSpace(states[0].State))
}

func execSRStatement(ctx context.Context, db *sql.DB, stmt string) error {
	stmt = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(stmt), ";"))
	if stmt == "" {
		return nil
	}
	_, err := db.ExecContext(ctx, stmt)
	return err
}

func (o *Orchestrator) previewStarRocks(ctx context.Context, t *store.CDCTask, extra map[string]string, resume bool, res *ReinitPreviewResult) {
	pending := InitIncomplete(extra)
	targets := collectTargetTables(ActiveMappings(ParseMappingsJSON(extra["mappings"])), splitCSV(extra["tables"]))
	if srManualMode(t, extra) {
		if !pending {
			return
		}
		for _, tb := range targets {
			res.add(ReinitItem{Resource: "mapping", Name: tb, Action: ReinitActionUpdate, Message: "初始化未完成：手动建表模式，仅回填 target_sql 等元数据，不连 StarRocks 建表"})
		}
		return
	}
	db, err := o.openTargetDB(ctx, t)
	if err != nil {
		for _, tb := range targets {
			rlName := RoutineLoadNameForTask(t.TaskType, extra["env"], t.SourceDB, extra["topic_prefix"], tb)
			res.add(ReinitItem{Resource: "routine_load", Name: rlName, Action: ReinitActionUnknown, Message: err.Error()})
		}
		res.warn("probe_failed", "StarRocks 探测失败："+err.Error())
		return
	}
	defer db.Close()

	if pending {
		items, perr := PreviewTargetSQLItems(ctx, o.Store, o.Secret, t)
		if perr != nil {
			res.warn("probe_failed", "生成目标 SQL 失败："+perr.Error())
			return
		}
		for _, it := range items {
			if it.Skipped {
				continue
			}
			exists, _ := srTableExists(ctx, db, t.TargetDB, it.Table)
			state := routineLoadStateOf(ctx, db, t.TargetDB, it.Table, t.TaskType, extra["env"], t.SourceDB, extra["topic_prefix"])
			rlExists := state != "NOT_FOUND" && state != "UNKNOWN"
			rlName := RoutineLoadNameForTask(t.TaskType, extra["env"], t.SourceDB, extra["topic_prefix"], it.Table)
			item := ReinitItem{Resource: "routine_load", Name: rlName, Exists: exists && rlExists}
			switch {
			case exists && rlExists:
				item.Action, item.Message = ReinitActionSkip, fmt.Sprintf("目标表 %s 与 Routine Load 已存在（%s），跳过", it.Table, state)
			case exists:
				item.Action, item.Message = ReinitActionCreate, fmt.Sprintf("初始化未完成：目标表 %s 已存在，按原配置补建 Routine Load", it.Table)
			default:
				item.Action, item.Message = ReinitActionCreate, fmt.Sprintf("初始化未完成，按原配置补建目标表 %s 与 Routine Load", it.Table)
			}
			res.add(item)
		}
		return
	}

	for _, rl := range ListRoutineLoadStates(ctx, db, t.TargetDB, targets, t.TaskType, extra["env"], t.SourceDB, extra["topic_prefix"]) {
		state := strings.ToUpper(strings.TrimSpace(rl.State))
		item := ReinitItem{Resource: "routine_load", Name: rl.Name, Exists: state != "NOT_FOUND"}
		switch state {
		case "PAUSED":
			if resume {
				item.Action, item.Message = ReinitActionResume, "已暂停，将执行 RESUME"
			} else {
				item.Action, item.Message = ReinitActionSkip, "已暂停（未勾选恢复），跳过"
			}
		case "RUNNING", "NEED_SCHEDULE":
			item.Action, item.Message = ReinitActionSkip, "运行中，跳过"
		case "NOT_FOUND":
			item.Action, item.Message = ReinitActionWarn, "Routine Load 不存在，不会新建，需人工处理"
			res.warn("routine_load_missing", fmt.Sprintf("表 %s 的 Routine Load %s 缺失，重新初始化不会新建", rl.Table, rl.Name))
		default:
			item.Action = ReinitActionWarn
			item.Message = "状态 " + defaultStr(state, "UNKNOWN") + "，需人工处理"
			if rl.Reason != "" {
				item.Message += "：" + rl.Reason
			}
		}
		res.add(item)
	}
}

// reinitStarRocks 初始化未完成时按表补齐（手动模式只回填 target_sql）；已完成初始化的只 RESUME 已暂停的 RL，缺失仅告警。
func (o *Orchestrator) reinitStarRocks(ctx context.Context, t *store.CDCTask, extra map[string]string, resume bool, op string) error {
	pending := extra["init_starrocks_pending"] == "true"
	if srManualMode(t, extra) {
		if !pending {
			return nil
		}
		sqlText, err := PreviewTargetSQL(ctx, o.Store, o.Secret, t)
		if err != nil {
			o.reinitLog(t, "WARN", "手动建表模式：生成目标 SQL 失败："+err.Error(), op)
			return nil
		}
		extra["target_sql"] = sqlText
		o.reinitLog(t, "INFO", "手动建表模式：已回填 target_sql，未连接 StarRocks 建表", op)
		return nil
	}
	if pending {
		return o.provisionStarRocks(ctx, t, extra, op)
	}
	db, err := o.openTargetDB(ctx, t)
	if err != nil {
		o.reinitLog(t, "WARN", "StarRocks 不可达，跳过 Routine Load 检查："+err.Error(), op)
		return nil
	}
	defer db.Close()
	targets := collectTargetTables(ActiveMappings(ParseMappingsJSON(extra["mappings"])), splitCSV(extra["tables"]))
	for _, rl := range ListRoutineLoadStates(ctx, db, t.TargetDB, targets, t.TaskType, extra["env"], t.SourceDB, extra["topic_prefix"]) {
		switch strings.ToUpper(strings.TrimSpace(rl.State)) {
		case "PAUSED":
			if !resume {
				o.reinitLog(t, "INFO", "Routine Load "+rl.Name+" 已暂停（未勾选恢复），跳过", op)
				continue
			}
			stmt := fmt.Sprintf("RESUME ROUTINE LOAD FOR `%s`.`%s`", t.TargetDB, rl.Name)
			if err := execSRStatement(ctx, db, stmt); err != nil {
				o.reinitLog(t, "WARN", stmt+" 失败："+err.Error(), op)
			} else {
				o.reinitLog(t, "INFO", "Routine Load "+rl.Name+" 已 RESUME", op)
			}
		case "RUNNING", "NEED_SCHEDULE":
		case "NOT_FOUND":
			o.reinitLog(t, "WARN", "Routine Load "+rl.Name+" 不存在，重新初始化不会新建，请人工处理", op)
		default:
			o.reinitLog(t, "WARN", fmt.Sprintf("Routine Load %s 状态 %s，需人工处理 %s", rl.Name, rl.State, rl.Reason), op)
		}
	}
	return nil
}

func (o *Orchestrator) provisionStarRocks(ctx context.Context, t *store.CDCTask, extra map[string]string, op string) error {
	items, err := PreviewTargetSQLItems(ctx, o.Store, o.Secret, t)
	if err != nil {
		return fmt.Errorf("生成目标 SQL 失败: %w", err)
	}
	if sqlText := JoinTargetSQL(items); sqlText != "" {
		extra["target_sql"] = sqlText
	}
	db, err := o.openTargetDB(ctx, t)
	if err != nil {
		return err
	}
	defer db.Close()
	for _, it := range items {
		if it.Skipped {
			o.reinitLog(t, "INFO", fmt.Sprintf("表 %s 跳过：%s", it.Table, it.Reason), op)
			continue
		}
		exists, err := srTableExists(ctx, db, t.TargetDB, it.Table)
		if err != nil {
			return fmt.Errorf("查询目标表 %s 失败: %w", it.Table, err)
		}
		if !exists {
			if err := execSRStatement(ctx, db, it.DDL); err != nil {
				return fmt.Errorf("创建目标表 %s 失败: %w", it.Table, err)
			}
			o.reinitLog(t, "INFO", "已按原配置创建目标表 "+it.Table, op)
		}
		rlName := RoutineLoadNameForTask(t.TaskType, extra["env"], t.SourceDB, extra["topic_prefix"], it.Table)
		state := routineLoadStateOf(ctx, db, t.TargetDB, it.Table, t.TaskType, extra["env"], t.SourceDB, extra["topic_prefix"])
		if state != "NOT_FOUND" && state != "UNKNOWN" {
			o.reinitLog(t, "INFO", fmt.Sprintf("Routine Load %s 已存在（%s），跳过", rlName, state), op)
			continue
		}
		if err := execSRStatement(ctx, db, it.Load); err != nil {
			return fmt.Errorf("创建 Routine Load %s 失败: %w", rlName, err)
		}
		o.reinitLog(t, "INFO", fmt.Sprintf("已按原配置创建 Routine Load %s", rlName), op)
	}
	return nil
}
