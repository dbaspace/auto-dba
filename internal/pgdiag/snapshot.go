package pgdiag

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

// CollectSQL 对齐 Hub 活动快照采集：非 idle、事务已开始、排除自身连接。
const CollectSQL = `SELECT pid,
         usename AS p_user,
         client_addr AS p_host,
         datname AS p_db,
         state AS command,
         EXTRACT(EPOCH FROM (now()-xact_start)) AS xact_duration,
         state,
         substr(query, 0, 15000) AS info
  FROM pg_stat_activity
  WHERE state != 'idle'
    AND EXTRACT(EPOCH FROM (now()-xact_start)) >= 0
    AND pid <> pg_backend_pid()`

type CollectResult struct {
	InstancesProcessed int
	RowsWritten        int
}

func decryptPassword(appSecret string, inst *store.Instance) (string, error) {
	if inst == nil {
		return "", fmt.Errorf("实例不存在")
	}
	if inst.Password == "" {
		return "", fmt.Errorf("实例未配置密码")
	}
	return secret.Decrypt(appSecret, inst.Password)
}

func CollectOne(ctx context.Context, meta *store.DB, appSecret string, inst *store.Instance, collectedAt time.Time) (int, error) {
	plain, err := decryptPassword(appSecret, inst)
	if err != nil {
		return 0, err
	}
	db, qctx, cancel, err := pingQuery(ctx, inst, plain)
	if err != nil {
		return 0, err
	}
	defer cancel()
	defer db.Close()

	rows, err := db.QueryContext(qctx, CollectSQL)
	if err != nil {
		return 0, fmt.Errorf("查询 pg_stat_activity: %w", err)
	}
	defer rows.Close()

	host := InstanceHost(inst)
	now := time.Now().UTC().Format(time.RFC3339)
	collected := collectedAt.UTC().Format(time.RFC3339)
	var snaps []store.PGActivitySnapshot
	for rows.Next() {
		var (
			pid   sql.NullInt64
			pUser sql.NullString
			pHost sql.NullString
			pDb   sql.NullString
			cmd   sql.NullString
			xact  sql.NullFloat64
			state sql.NullString
			info  sql.NullString
		)
		if err := rows.Scan(&pid, &pUser, &pHost, &pDb, &cmd, &xact, &state, &info); err != nil {
			return 0, err
		}
		norm, hash := NormalizeQuery(info.String)
		s := store.PGActivitySnapshot{
			ID:              uuid.NewString(),
			CollectedAt:     collected,
			InstanceID:      inst.ID,
			InstanceName:    inst.Name,
			Host:            host,
			Port:            inst.Port,
			PUser:           pUser.String,
			PHost:           pHost.String,
			PDb:             pDb.String,
			Command:         cmd.String,
			State:           state.String,
			Info:            info.String,
			QueryNormalized: norm,
			QueryHash:       hash,
			CreatedAt:       now,
		}
		if pid.Valid {
			v := pid.Int64
			s.PID = &v
		}
		if xact.Valid {
			v := xact.Float64
			s.XactDuration = &v
		}
		snaps = append(snaps, s)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if err := meta.InsertPGActivitySnapshots(snaps); err != nil {
		return 0, err
	}
	return len(snaps), nil
}

func Collect(ctx context.Context, meta *store.DB, appSecret, instanceID string) (CollectResult, []string, error) {
	var result CollectResult
	var warns []string
	var insts []store.Instance
	if instanceID != "" {
		it, err := meta.GetInstance(instanceID)
		if err != nil {
			return result, nil, err
		}
		if it == nil {
			return result, nil, fmt.Errorf("实例不存在")
		}
		if !IsPostgres(it.DBType) {
			return result, nil, fmt.Errorf("仅支持 PostgreSQL 实例")
		}
		insts = []store.Instance{*it}
	} else {
		list, err := ListPostgresInstances(meta)
		if err != nil {
			return result, nil, err
		}
		insts = list
	}
	collectedAt := time.Now().UTC()
	for i := range insts {
		inst := &insts[i]
		n, err := CollectOne(ctx, meta, appSecret, inst, collectedAt)
		if err != nil {
			warns = append(warns, fmt.Sprintf("%s (%s:%d): %v", inst.Name, InstanceHost(inst), inst.Port, err))
			continue
		}
		result.InstancesProcessed++
		result.RowsWritten += n
	}
	return result, warns, nil
}
