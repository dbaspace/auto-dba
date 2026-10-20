package cdc

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	"auto-dba/internal/dbmig"
	"auto-dba/internal/store"
)

// replicationSlotsSQL 只读 pg_replication_slots。当前 WAL 位置只取一次，不查 pg_stat_activity / pg_stat_replication。
const replicationSlotsSQL = `
SELECT s.slot_name,
       COALESCE(s.database, ''),
       s.active,
       COALESCE(s.active_pid, 0),
       COALESCE(s.restart_lsn::text, ''),
       COALESCE(s.confirmed_flush_lsn::text, ''),
       COALESCE(s.plugin, ''),
       COALESCE(pg_wal_lsn_diff(cur.lsn, COALESCE(s.confirmed_flush_lsn, s.restart_lsn))::bigint, 0)
FROM pg_replication_slots s
CROSS JOIN (SELECT pg_current_wal_lsn() AS lsn) cur`

// replicationSlotsSQLFallback 在主查询失败时仍只读目录，滞后改用 restart_lsn。
const replicationSlotsSQLFallback = `
SELECT s.slot_name,
       COALESCE(s.database, ''),
       s.active,
       COALESCE(s.active_pid, 0),
       COALESCE(s.restart_lsn::text, ''),
       ''::text,
       COALESCE(s.plugin, ''),
       COALESCE(pg_wal_lsn_diff(cur.lsn, s.restart_lsn)::bigint, 0)
FROM pg_replication_slots s
CROSS JOIN (SELECT pg_current_wal_lsn() AS lsn) cur`

// SlotRow 是同步列表的一行：复制槽字段来自采集，CDC 字段按槽名左连接补齐。
type SlotRow struct {
	store.SyncSlot
	Connector     string `json:"connector,omitempty"`
	ConnectorName string `json:"connector_name,omitempty"`
	ConnectBase   string `json:"connect_base,omitempty"`
	TaskName      string `json:"task_name,omitempty"`
	TaskID        string `json:"task_id,omitempty"`
	Topic         string `json:"topic,omitempty"`
	Heartbeat     string `json:"heartbeat,omitempty"`
}

// CollectInstanceSlots 对单个实例只建一条连接，读完即关。dbNames 是 postgres 库连不上时的备选库，仍只查复制槽目录。
func CollectInstanceSlots(ctx context.Context, inst *store.Instance, secret string, dbNames []string) ([]store.SyncSlot, error) {
	if inst == nil {
		return nil, fmt.Errorf("实例不存在")
	}
	var last error
	for i, name := range slotQueryDatabases(dbNames) {
		ictx, cancel := context.WithTimeout(ctx, 8*time.Second)
		db, err := dbmig.OpenInstance(ictx, inst, secret, name)
		if err != nil {
			cancel()
			last = err
			if i == 0 && !retryOtherDatabase(err) {
				return nil, err
			}
			continue
		}
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(0)
		slots, qerr := QueryReplicationSlots(ictx, db)
		db.Close()
		cancel()
		if qerr != nil {
			last = qerr
			return nil, qerr
		}
		return slots, nil
	}
	if last == nil {
		last = fmt.Errorf("无法连接")
	}
	return nil, last
}

// slotQueryDatabases 优先 postgres。连不上该库时再试调用方给出的业务库，最多 3 个，避免对实例反复建连。
func slotQueryDatabases(extra []string) []string {
	out := []string{"postgres"}
	seen := map[string]struct{}{"postgres": {}}
	for _, name := range extra {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, name)
		if len(out) >= 3 {
			break
		}
	}
	return out
}

func retryOtherDatabase(err error) bool {
	if err == nil {
		return false
	}
	low := strings.ToLower(err.Error())
	return strings.Contains(low, "does not exist") || strings.Contains(low, "3d000") || strings.Contains(low, "permission denied") || strings.Contains(low, "not permitted")
}

// QueryReplicationSlots 在已有连接上只读复制槽。滞后优先用 confirmed_flush_lsn，否则用 restart_lsn。
func QueryReplicationSlots(ctx context.Context, db *sql.DB) ([]store.SyncSlot, error) {
	if db == nil {
		return nil, fmt.Errorf("数据库连接为空")
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_, _ = conn.ExecContext(ctx, `SET statement_timeout = '3s'`)
	_, _ = conn.ExecContext(ctx, `SET lock_timeout = '1s'`)
	rows, err := conn.QueryContext(ctx, replicationSlotsSQL)
	if err != nil {
		rows, err = conn.QueryContext(ctx, replicationSlotsSQLFallback)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.SyncSlot
	for rows.Next() {
		var s store.SyncSlot
		var active sql.NullBool
		if err := rows.Scan(&s.SlotName, &s.Database, &active, &s.ActivePID, &s.RestartLSN, &s.ConfirmedFlushLSN, &s.Plugin, &s.LagBytes); err != nil {
			return nil, err
		}
		s.Active = active.Bool
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// RestartDebeziumConnector 只重启 Connect 上的连接器及其 Task，不走 CDC 任务编排。
func RestartDebeziumConnector(ctx context.Context, base, name string) error {
	return connectRestartConnectorAndTasks(ctx, base, name)
}

// SlotDisplayHost 同步列表展示的连接主机：优先实例主机，取逗号分隔的第一段。
func SlotDisplayHost(inst *store.Instance) string {
	if inst == nil {
		return ""
	}
	h := strings.TrimSpace(inst.Host)
	if h == "" {
		h = strings.TrimSpace(inst.DomainName)
	}
	if i := strings.Index(h, ","); i >= 0 {
		h = strings.TrimSpace(h[:i])
	}
	return h
}

// InstanceHostAliases 返回实例可能被记成槽主机的全部写法。
func InstanceHostAliases(inst *store.Instance) []string {
	if inst == nil {
		return nil
	}
	var out []string
	seen := map[string]struct{}{}
	add := func(raw string) {
		for _, p := range strings.Split(raw, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			key := strings.ToLower(p)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, p)
		}
	}
	add(inst.Host)
	add(inst.DomainName)
	return out
}

type slotTaskCand struct {
	task  *store.CDCTask
	inst  *store.Instance
	extra map[string]string
}

// AttachSlotCDC 按主机、端口、槽名和库名把 CDC 任务补到复制槽上。对不上的任务字段留空。
// 先按 slot_name 建索引，每个任务只 ParseTaskExtra 一次，避免 O(槽×任务) 全表扫描。
func AttachSlotCDC(slots []store.SyncSlot, tasks []store.CDCTask, insts []store.Instance) []SlotRow {
	byID := make(map[string]*store.Instance, len(insts))
	for i := range insts {
		byID[insts[i].ID] = &insts[i]
	}
	bySlot := make(map[string][]slotTaskCand)
	addCand := func(slotName string, c slotTaskCand) {
		key := strings.ToLower(strings.TrimSpace(slotName))
		if key == "" {
			return
		}
		bySlot[key] = append(bySlot[key], c)
	}
	for i := range tasks {
		task := &tasks[i]
		inst := byID[task.SourceID]
		if inst == nil {
			continue
		}
		extra := ParseTaskExtra(task.Description)
		c := slotTaskCand{task: task, inst: inst, extra: extra}
		for _, b := range ParseShardBindings(extra) {
			addCand(b.SlotName, c)
		}
		addCand(extra["slot_name"], c)
	}
	out := make([]SlotRow, 0, len(slots))
	for _, slot := range slots {
		row := SlotRow{SyncSlot: slot}
		best := 0
		cands := bySlot[strings.ToLower(strings.TrimSpace(slot.SlotName))]
		for _, c := range cands {
			if !slotHostMatches(slot.Host, slot.Port, c.inst) {
				continue
			}
			name, base, topic, score := matchTaskSlot(slot, c.task, c.extra)
			if score <= best || name == "" {
				continue
			}
			best = score
			row.TaskID = c.task.ID
			row.TaskName = c.task.Name
			row.ConnectorName = name
			row.ConnectBase = base
			row.Connector = connectorLabel(base)
			row.Topic = topic
		}
		out = append(out, row)
	}
	return out
}

// SlotHostMatches 判断槽上记录的主机端口是否属于这台实例。
func SlotHostMatches(host string, port int, inst *store.Instance) bool {
	return slotHostMatches(host, port, inst)
}

func slotHostMatches(host string, port int, inst *store.Instance) bool {
	if inst == nil || inst.Port != port {
		return false
	}
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	for _, alias := range InstanceHostAliases(inst) {
		if strings.ToLower(alias) == host {
			return true
		}
	}
	return false
}

func matchTaskSlot(slot store.SyncSlot, task *store.CDCTask, extra map[string]string) (name, base, topic string, score int) {
	bindings := ParseShardBindings(extra)
	for _, b := range bindings {
		if !strings.EqualFold(strings.TrimSpace(b.SlotName), strings.TrimSpace(slot.SlotName)) {
			continue
		}
		if slot.Database != "" && b.DbName != "" && !strings.EqualFold(b.DbName, slot.Database) {
			continue
		}
		name = strings.TrimSpace(b.ConnectorName)
		if name == "" {
			name = strings.TrimSpace(extra["connector_name"])
		}
		score = 2
		if strings.EqualFold(b.DbName, slot.Database) && slot.Database != "" {
			score = 3
		}
		break
	}
	if score == 0 && strings.EqualFold(strings.TrimSpace(extra["slot_name"]), strings.TrimSpace(slot.SlotName)) {
		if slot.Database != "" && task.SourceDB != "" && !strings.EqualFold(task.SourceDB, slot.Database) && !extraListsDB(extra, slot.Database) {
			return "", "", "", 0
		}
		name = strings.TrimSpace(extra["connector_name"])
		score = 2
		if slot.Database != "" && strings.EqualFold(task.SourceDB, slot.Database) {
			score = 3
		}
	}
	if name == "" {
		return "", "", "", 0
	}
	base = strings.TrimSpace(extra["connect_base_url"])
	if base == "" {
		base = strings.TrimSpace(extra["worker_url"])
	}
	topic = strings.TrimSpace(extra["topic_prefix"])
	return name, base, topic, score
}

func extraListsDB(extra map[string]string, dbName string) bool {
	for _, name := range splitCSV(extra["source_db_names"]) {
		if strings.EqualFold(name, dbName) {
			return true
		}
	}
	return false
}

func connectorLabel(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		raw = strings.TrimPrefix(raw, "http://")
		raw = strings.TrimPrefix(raw, "https://")
		if i := strings.Index(raw, "/"); i >= 0 {
			raw = raw[:i]
		}
		return raw
	}
	return u.Host
}
