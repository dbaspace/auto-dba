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

// RestartSyncSlotItem 同步列表单条重启：连业务库写 heartbeat，再 POST .../tasks/0/restart。
// 不走 CDC 编排，也不调用 includeTasks 全量重启。
func RestartSyncSlotItem(ctx context.Context, inst *store.Instance, secret, dbName, connectBase, connectorName string) error {
	dbName = strings.TrimSpace(dbName)
	connectorName = strings.TrimSpace(connectorName)
	if inst == nil {
		return fmt.Errorf("实例不存在")
	}
	if dbName == "" {
		return fmt.Errorf("dbname 必填")
	}
	if connectorName == "" {
		return fmt.Errorf("connector 名称为空")
	}
	base := NormalizeConnectBaseURL(connectBase)
	if base == "" {
		return fmt.Errorf("connect 地址为空")
	}

	if err := updateSyncSlotHeartbeat(ctx, inst, secret, dbName); err != nil {
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

// FindInstanceForSlot 按槽的 host:port 在实例列表中匹配。
func FindInstanceForSlot(slot store.SyncSlot, insts []store.Instance) *store.Instance {
	for i := range insts {
		inst := &insts[i]
		if SlotHostMatches(slot.Host, slot.Port, inst) {
			return inst
		}
	}
	return nil
}
