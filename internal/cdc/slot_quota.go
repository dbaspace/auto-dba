package cdc

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

func ReplicationSlotCapacity(ctx context.Context, db *sql.DB) (used, max int, err error) {
	if err = db.QueryRowContext(ctx, `SELECT COUNT(1) FROM pg_replication_slots`).Scan(&used); err != nil {
		return 0, 0, fmt.Errorf("查询复制槽数量失败: %w", err)
	}
	var maxStr string
	if err = db.QueryRowContext(ctx, `SHOW max_replication_slots`).Scan(&maxStr); err != nil {
		return used, 0, fmt.Errorf("查询 max_replication_slots 失败: %w", err)
	}
	max, err = strconv.Atoi(strings.TrimSpace(maxStr))
	if err != nil {
		return used, 0, fmt.Errorf("解析 max_replication_slots %q 失败: %w", maxStr, err)
	}
	return used, max, nil
}

func ReplicationSlotQuotaFull(used, max int) bool {
	return max > 0 && used >= max
}

func ReplicationSlotQuotaAlmostFull(used, max int) bool {
	return max > 0 && used == max-1
}

func replicationSlotQuotaExhausted(err error) bool {
	return err != nil && strings.Contains(err.Error(), "replication slots are in use")
}

func FormatCreateReplicationSlotError(ctx context.Context, db *sql.DB, err error) error {
	if err == nil || slotAlreadyExists(err) {
		return nil
	}
	if !replicationSlotQuotaExhausted(err) {
		return fmt.Errorf("创建 replication slot 失败: %w", err)
	}
	if db == nil {
		return fmt.Errorf("源库逻辑复制槽已满：%w；请清理 pg_replication_slots 中无用逻辑槽或提高 max_replication_slots 后重试", err)
	}
	used, max, cerr := ReplicationSlotCapacity(ctx, db)
	if cerr != nil {
		return fmt.Errorf("源库逻辑复制槽已满：%w；请清理 pg_replication_slots 中无用逻辑槽或提高 max_replication_slots 后重试", err)
	}
	return fmt.Errorf("源库逻辑复制槽已满（used/max=%d/%d）：%w；请清理 pg_replication_slots 中无用槽或提高 max_replication_slots 后重试", used, max, err)
}
