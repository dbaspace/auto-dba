package cdc

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"auto-dba/internal/store"
)

const shardFanoutArgKey = "sync_shard_fanout_on_approve"

// shardFanoutEnabledValue 空值或读不到时默认打开，与种子和 Hub 一致。
func shardFanoutEnabledValue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "false", "0", "no", "off":
		return false
	default:
		return true
	}
}

func wantShardFanout(taskType, status string, extra map[string]string, enabled bool) bool {
	if !enabled || extra == nil {
		return false
	}
	tt := strings.TrimSpace(taskType)
	if tt != "" && !strings.EqualFold(tt, "init") {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(status), store.StatusSuperseded) {
		return false
	}
	if strings.TrimSpace(extra["cdc_fanout_from"]) != "" {
		return false
	}
	return ShardModeActive(extra)
}

func (o *Orchestrator) shardFanoutEnabled() bool {
	if o == nil || o.Store == nil {
		return true
	}
	v, err := o.Store.GetGlobalArg(shardFanoutArgKey)
	if err != nil {
		return true
	}
	return shardFanoutEnabledValue(v)
}

func (o *Orchestrator) shouldShardFanout(t *store.CDCTask, extra map[string]string) bool {
	if o == nil || t == nil {
		return false
	}
	return wantShardFanout(t.TaskType, t.Status, extra, o.shardFanoutEnabled())
}

// childInitExtra 单库子任务的元数据：本库的 publication/slot/connector，共享 topic_prefix。
func childInitExtra(parent map[string]string, parentID string, b ShardDbBinding, tables []string) map[string]string {
	child := map[string]string{}
	for k, v := range parent {
		child[k] = v
	}
	for _, k := range []string{"shard_db_bindings", "source_db_names", "source_db_prefix", "source_scope_mode", "representative_db", "error_step"} {
		delete(child, k)
	}
	child["shard_db"] = "false"
	child["publication_name"] = b.PublicationName
	child["slot_name"] = b.SlotName
	child["connector_name"] = b.ConnectorName
	if tp := strings.TrimSpace(parent["topic_prefix"]); tp != "" {
		child["topic_prefix"] = tp
	}
	child["cdc_fanout_from"] = strings.TrimSpace(parentID)
	child["tables"] = strings.Join(cleanStringSlice(tables), ",")
	maps := ParseMappingsJSON(parent["mappings"])
	var kept []TableMapping
	for _, m := range maps {
		if !MappingAppliesToDB(m, b.DbName) {
			continue
		}
		m.SourceDbNames = nil
		m.SourceDbScope = ""
		kept = append(kept, m)
	}
	if len(kept) == 0 {
		delete(child, "mappings")
	} else {
		raw, err := json.Marshal(kept)
		if err == nil {
			child["mappings"] = string(raw)
		}
	}
	return child
}

func (o *Orchestrator) fanoutChildren(parentID string) ([]store.CDCTask, error) {
	if o == nil || o.Store == nil || strings.TrimSpace(parentID) == "" {
		return nil, nil
	}
	list, err := o.Store.ListCDC("", "init")
	if err != nil {
		return nil, err
	}
	var out []store.CDCTask
	for i := range list {
		if list[i].ID == parentID {
			continue
		}
		if parseTaskExtra(list[i].Description)["cdc_fanout_from"] == parentID {
			out = append(out, list[i])
		}
	}
	return out, nil
}

// fanOutShardInit 把分库汇总任务拆成每个有表的物理库一条单库 init，汇总任务标 superseded。
func (o *Orchestrator) fanOutShardInit(ctx context.Context, t *store.CDCTask, extra map[string]string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if t == nil || o == nil || o.Store == nil {
		return fmt.Errorf("任务为空")
	}
	pub := extra["publication_name"]
	slot := extra["slot_name"]
	connector := extra["connector_name"]
	bindings := ResolveOrBuildShardBindings(extra, t.SourceDB, pub, slot, connector)
	PersistShardBindings(extra, bindings)
	o.saveExtra(t, extra)

	existing, err := o.fanoutChildren(t.ID)
	if err != nil {
		return err
	}
	have := map[string]struct{}{}
	for _, ch := range existing {
		have[strings.ToLower(strings.TrimSpace(ch.SourceDB))] = struct{}{}
	}
	maps := ParseMappingsJSON(extra["mappings"])
	tables := splitCSV(extra["tables"])
	var skipped []string
	var created []string
	for _, b := range bindings {
		dbTables := TablesForShardDB(maps, tables, b.DbName)
		if len(dbTables) == 0 {
			skipped = append(skipped, b.DbName)
			o.appendLog(t, "INFO", "orchestrate", "分库 "+b.DbName+" 无命中表，跳过拆分")
			continue
		}
		if _, ok := have[strings.ToLower(b.DbName)]; ok {
			continue
		}
		childExtra := childInitExtra(extra, t.ID, b, dbTables)
		raw, mErr := json.Marshal(childExtra)
		if mErr != nil {
			return mErr
		}
		text, _ := splitCDCDesc(t.Description)
		child := &store.CDCTask{
			Name:           fmt.Sprintf("%s · %s", t.Name, b.DbName),
			TaskType:       "init",
			Status:         store.StatusInit,
			SourceID:       t.SourceID,
			TargetID:       t.TargetID,
			SourceDB:       b.DbName,
			TargetDB:       t.TargetDB,
			ApprovalStatus: t.ApprovalStatus,
			TenantID:       t.TenantID,
			Description:    strings.TrimSpace(text + "\n-- auto-dba-cdc:" + string(raw)),
		}
		if strings.TrimSpace(child.TenantID) == "" && o.Store != nil {
			child.TenantID = o.Store.RowTenantID("tbl_cdc_tasks", t.ID)
		}
		if err := o.Store.CreateCDC(child); err != nil {
			o.appendLog(t, "ERROR", "orchestrate", fmt.Sprintf("分库 %s 子任务创建失败：%s", b.DbName, err.Error()))
			return fmt.Errorf("分库 %s 子任务创建失败: %w", b.DbName, err)
		}
		created = append(created, fmt.Sprintf("%s(%d 表)", b.DbName, len(dbTables)))
		o.appendLog(t, "INFO", "orchestrate", fmt.Sprintf("分库 %s → 单库子任务 %s（publication=%s slot=%s connector=%s）", b.DbName, child.ID, b.PublicationName, b.SlotName, b.ConnectorName))
		have[strings.ToLower(b.DbName)] = struct{}{}
	}
	children, err := o.fanoutChildren(t.ID)
	if err != nil {
		return err
	}
	if len(children) == 0 {
		_ = o.Store.UpdateCDCStatus(t.ID, store.StatusFailed, t.ApprovalStatus)
		msg := "分库拆分未生成任何子任务（所有分库均无命中表）"
		o.appendLog(t, "ERROR", "orchestrate", msg)
		return fmt.Errorf("%s", msg)
	}
	summary := fmt.Sprintf("分库拆分完成：%d 个单库子任务", len(children))
	if len(created) > 0 {
		summary += " [" + strings.Join(created, ", ") + "]"
	}
	if len(skipped) > 0 {
		summary += "；跳过无表分库 [" + strings.Join(skipped, ", ") + "]"
	}
	o.appendLog(t, "INFO", "orchestrate", summary+"；共享 topic_prefix="+extra["topic_prefix"])
	if err := o.Store.UpdateCDCStatus(t.ID, store.StatusSuperseded, t.ApprovalStatus); err != nil {
		return err
	}
	t.Status = store.StatusSuperseded
	ids := make([]string, 0, len(children))
	for _, ch := range children {
		ids = append(ids, ch.ID)
	}
	go o.orchestrateFanoutChildren(ids)
	return nil
}

func (o *Orchestrator) orchestrateFanoutChildren(ids []string) {
	if o == nil || len(ids) == 0 {
		return
	}
	dur := time.Duration(len(ids)) * 15 * time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), dur)
	defer cancel()
	for _, id := range ids {
		child, err := o.Store.GetCDC(id)
		if err != nil || child == nil {
			continue
		}
		if child.Status != store.StatusInit && child.Status != store.StatusFailed {
			continue
		}
		if err := o.Apply(ctx, id, ""); err != nil {
			o.appendLog(child, "ERROR", "orchestrate", "分库子任务编排失败: "+err.Error())
		}
	}
}
