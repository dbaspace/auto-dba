package cdc

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"auto-dba/internal/dbmig"
	"auto-dba/internal/store"
)

// 对齐 Hub sync_slot_restart：重启前写业务库心跳，再只重启 Connect task 0。
const heartbeatUpdateSQL = `UPDATE tbl_heartbeat_table SET updatetime = now(), dbname = current_database()`

const (
	heartbeatSQLTimeout     = 20 * time.Second
	connectTask0HTTPTimeout = 30 * time.Second
)

// FillSlotBoundHosts 按已登记库给每条槽填展示用的绑定域名。对不上或库没有绑定时留空，不回退连接主机和实例域名列表。
func FillSlotBoundHosts(rows []SlotRow, insts []store.Instance, dbs []store.Database) []SlotRow {
	byInst := map[string]map[string]string{}
	for _, db := range dbs {
		host := databaseBoundHost(db)
		instID := strings.TrimSpace(db.InstanceID)
		name := strings.ToLower(strings.TrimSpace(db.DBName))
		if host == "" || instID == "" || name == "" {
			continue
		}
		m := byInst[instID]
		if m == nil {
			m = map[string]string{}
			byInst[instID] = m
		}
		if _, ok := m[name]; !ok {
			m[name] = host
		}
	}
	for i := range rows {
		rows[i].BoundHost = ""
		inst := FindInstanceForSlot(rows[i].SyncSlot, insts)
		if inst == nil {
			continue
		}
		rows[i].BoundHost = byInst[inst.ID][strings.ToLower(strings.TrimSpace(rows[i].Database))]
	}
	return rows
}

// databaseBoundHost 只取该库自己的绑定域名，多个用逗号隔开。
// 有 domains 时不用标签，也不读实例连接主机和实例域名全集。
func databaseBoundHost(db store.Database) string {
	names := uniqueBoundNames(db.Domains)
	if len(names) == 0 {
		var tagged []string
		for _, raw := range db.Tags {
			k, v, ok := splitInstanceTag(raw)
			if ok && strings.EqualFold(k, "domain") {
				tagged = append(tagged, v)
			}
		}
		names = uniqueBoundNames(tagged)
	}
	return strings.Join(names, ", ")
}

func uniqueBoundNames(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		key := strings.ToLower(s)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, s)
	}
	return out
}

// RestartSyncSlotItem 同步列表单条重启：按连接主机连业务库写 heartbeat，再 POST .../tasks/0/restart。
// 不走 CDC 编排，也不调用 includeTasks 全量重启，不改实例连接主机和 CDC 任务记录。
func RestartSyncSlotItem(ctx context.Context, inst *store.Instance, secret, dbName, connectBase, connectorName, dialHost string) error {
	dbName = strings.TrimSpace(dbName)
	connectorName = strings.TrimSpace(connectorName)
	dialHost = strings.TrimSpace(dialHost)
	if inst == nil {
		return fmt.Errorf("实例不存在")
	}
	if dbName == "" {
		return fmt.Errorf("dbname 必填")
	}
	if connectorName == "" {
		return fmt.Errorf("connector 名称为空")
	}
	if dialHost == "" {
		return fmt.Errorf("未配置连接主机")
	}
	base := NormalizeConnectBaseURL(connectBase)
	if base == "" {
		return fmt.Errorf("connect 地址为空")
	}

	dial := dbmig.WithDialHost(inst, dialHost)
	if err := updateSyncSlotHeartbeat(ctx, dial, secret, dbName); err != nil {
		return err
	}
	return RestartDebeziumTask0(ctx, base, connectorName)
}

func updateSyncSlotHeartbeat(ctx context.Context, inst *store.Instance, secret, dbName string) error {
	ictx, cancel := context.WithTimeout(ctx, heartbeatSQLTimeout)
	defer cancel()
	db, err := dbmig.OpenInstance(ictx, inst, secret, dbName)
	if err != nil {
		return fmt.Errorf("heartbeat 连库: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(0)
	if _, err := db.ExecContext(ictx, heartbeatUpdateSQL); err != nil {
		return fmt.Errorf("heartbeat update sql: %w", err)
	}
	return nil
}

// RestartDebeziumTask0 仅重启 Connect 上 connector 的 task 0（Hub 同步列表路径）。
func RestartDebeziumTask0(ctx context.Context, base, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("connector 名称为空")
	}
	base = NormalizeConnectBaseURL(base)
	if base == "" {
		return fmt.Errorf("connect 地址为空")
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, connectTask0HTTPTimeout)
		defer cancel()
	}
	path := "/connectors/" + url.PathEscape(name) + "/tasks/0/restart"
	_, err := connectPostRetry(ctx, base, path)
	if err != nil {
		return fmt.Errorf("connector task0 restart: %w", err)
	}
	return nil
}

// FindInstanceForSlot 按槽的 host:port 在实例列表中匹配。连接主机优先；对不上时再看实例域名和标签 domain=，便于还记在域名上的旧槽找到凭证。
func FindInstanceForSlot(slot store.SyncSlot, insts []store.Instance) *store.Instance {
	for i := range insts {
		inst := &insts[i]
		if SlotHostMatches(slot.Host, slot.Port, inst) {
			return inst
		}
	}
	for i := range insts {
		inst := &insts[i]
		if inst.Port != slot.Port {
			continue
		}
		if instanceListedDomain(inst, slot.Host) {
			return inst
		}
	}
	return nil
}

func instanceListedDomain(inst *store.Instance, host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if inst == nil || host == "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(inst.DomainName), host) {
		return true
	}
	for _, d := range inst.Domains {
		if strings.EqualFold(strings.TrimSpace(d), host) {
			return true
		}
	}
	for _, raw := range inst.Tags {
		k, v, ok := splitInstanceTag(raw)
		if ok && strings.EqualFold(k, "domain") && strings.EqualFold(strings.TrimSpace(v), host) {
			return true
		}
	}
	return false
}
