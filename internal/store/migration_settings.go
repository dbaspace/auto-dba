package store

import (
	"strconv"
	"strings"
)

const (
	ArgMigrationApprovalEnabled    = "migration_approval_enabled"
	ArgMigrationApproverUserIDs    = "migration_approver_user_ids"
	ArgMigrationOSSEndpoint        = "migration_oss_endpoint"
	ArgMigrationOSSBucket          = "migration_oss_bucket"
	ArgMigrationOSSPrefix          = "migration_oss_prefix"
	ArgMigrationOSSAccessKey       = "migration_oss_access_key"
	ArgMigrationOSSSecretKey       = "migration_oss_secret_key"
	ArgMigrationOSSRetentionDays   = "migration_oss_retention_days"
	ArgMigrationOSSChunkSizeMB     = "migration_oss_chunk_size_mb"
	ArgMigrationMaxConcurrentTasks = "migration_max_concurrent_tasks"
	ArgMigrationTableParallelism   = "migration_table_parallelism"
	ArgMigrationBatchSize          = "migration_batch_size"
	ArgMigrationSingleTableTimeout = "migration_single_table_timeout_min"
	ArgMigrationDefaultSpeedLimit  = "migration_default_speed_limit"
	ArgMigrationScheduleTolerance  = "migration_schedule_tolerance_min"
)

type MigrationOSSSettings struct {
	Endpoint      string `json:"endpoint"`
	Bucket        string `json:"bucket"`
	Prefix        string `json:"prefix"`
	AccessKey     string `json:"access_key"`
	SecretKey     string `json:"secret_key"`
	RetentionDays int    `json:"retention_days"`
	ChunkSizeMB   int    `json:"chunk_size_mb"`
	Configured    bool   `json:"configured"`
}

type MigrationPerfSettings struct {
	MaxConcurrentTasks    int `json:"max_concurrent_tasks"`
	TableParallelism      int `json:"table_parallelism"`
	BatchSize             int `json:"batch_size"`
	SingleTableTimeoutMin int `json:"single_table_timeout_min"`
	DefaultSpeedLimit     int `json:"default_speed_limit"`
	ScheduleToleranceMin  int `json:"schedule_tolerance_min"`
}

type MigrationSettings struct {
	ApprovalEnabled bool                  `json:"approval_enabled"`
	ApproverUserIDs string                `json:"approver_user_ids"`
	OSS             MigrationOSSSettings  `json:"oss"`
	Performance     MigrationPerfSettings `json:"performance"`
}

func migrationSettingDefaults() []GlobalArg {
	return []GlobalArg{
		{Key: ArgMigrationApprovalEnabled, Value: "true", Description: "数据迁移是否走审批"},
		{Key: ArgMigrationApproverUserIDs, Value: "admin", Description: "数据迁移审核人登录名，逗号分隔"},
		{Key: ArgMigrationOSSEndpoint, Value: "", Description: "迁移 OSS 访问地址"},
		{Key: ArgMigrationOSSBucket, Value: "", Description: "迁移 OSS 存储桶名称"},
		{Key: ArgMigrationOSSPrefix, Value: "migration", Description: "迁移 OSS 文件路径前缀"},
		{Key: ArgMigrationOSSAccessKey, Value: "", Description: "迁移 OSS AccessKey ID"},
		{Key: ArgMigrationOSSSecretKey, Value: "", Description: "迁移 OSS AccessKey Secret"},
		{Key: ArgMigrationOSSRetentionDays, Value: "7", Description: "迁移 OSS 临时文件保留天数"},
		{Key: ArgMigrationOSSChunkSizeMB, Value: "100", Description: "迁移 OSS 单文件分片大小(MB)"},
		{Key: ArgMigrationMaxConcurrentTasks, Value: "3", Description: "迁移最大并发任务数"},
		{Key: ArgMigrationTableParallelism, Value: "4", Description: "迁移默认多表并行度"},
		{Key: ArgMigrationBatchSize, Value: "10000", Description: "迁移默认批次大小"},
		{Key: ArgMigrationSingleTableTimeout, Value: "30", Description: "单表迁移超时(分钟)"},
		{Key: ArgMigrationDefaultSpeedLimit, Value: "0", Description: "迁移默认限速(行/秒)，0 表示不限速"},
		{Key: ArgMigrationScheduleTolerance, Value: "10", Description: "定时触发容忍窗口(分钟)"},
	}
}

func (d *DB) EnsureMigrationSettings() error {
	for _, a := range migrationSettingDefaults() {
		var exists int
		_ = d.QueryRow(`SELECT COUNT(1) FROM tbl_global_args WHERE key=?`, a.Key).Scan(&exists)
		if exists > 0 {
			continue
		}
		if err := d.SetGlobalArg(a.Key, a.Value, a.Description); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) argOr(key, def string) string {
	v, err := d.GetGlobalArg(key)
	if err != nil || strings.TrimSpace(v) == "" {
		return def
	}
	return strings.TrimSpace(v)
}

func (d *DB) intArgClamped(key string, def, min, max int) int {
	v, err := d.GetGlobalArg(key)
	if err != nil || strings.TrimSpace(v) == "" {
		return def
	}
	n, perr := strconv.Atoi(strings.TrimSpace(v))
	if perr != nil {
		return def
	}
	if min < max {
		if n < min {
			n = min
		}
		if n > max {
			n = max
		}
	}
	return n
}

func (d *DB) GetMigrationSettings() MigrationSettings {
	_ = d.EnsureMigrationSettings()
	oss := MigrationOSSSettings{
		Endpoint:      d.argOr(ArgMigrationOSSEndpoint, ""),
		Bucket:        d.argOr(ArgMigrationOSSBucket, ""),
		Prefix:        d.argOr(ArgMigrationOSSPrefix, "migration"),
		AccessKey:     d.argOr(ArgMigrationOSSAccessKey, ""),
		SecretKey:     d.argOr(ArgMigrationOSSSecretKey, ""),
		RetentionDays: d.intArgClamped(ArgMigrationOSSRetentionDays, 7, 1, 3650),
		ChunkSizeMB:   d.intArgClamped(ArgMigrationOSSChunkSizeMB, 100, 1, 10240),
	}
	oss.Configured = oss.Endpoint != "" && oss.Bucket != "" && oss.AccessKey != "" && oss.SecretKey != ""
	return MigrationSettings{
		ApprovalEnabled: d.BoolArg(ArgMigrationApprovalEnabled, true),
		ApproverUserIDs: d.argOr(ArgMigrationApproverUserIDs, ""),
		OSS:             oss,
		Performance: MigrationPerfSettings{
			MaxConcurrentTasks:    d.intArgClamped(ArgMigrationMaxConcurrentTasks, 3, 1, 10),
			TableParallelism:      d.intArgClamped(ArgMigrationTableParallelism, 4, 1, 16),
			BatchSize:             d.intArgClamped(ArgMigrationBatchSize, 10000, 100, 100000),
			SingleTableTimeoutMin: d.intArgClamped(ArgMigrationSingleTableTimeout, 30, 1, 1440),
			DefaultSpeedLimit:     d.intArgClamped(ArgMigrationDefaultSpeedLimit, 0, 0, 1000000),
			ScheduleToleranceMin:  d.intArgClamped(ArgMigrationScheduleTolerance, 10, 1, 60),
		},
	}
}

func (d *DB) SaveMigrationOSS(s MigrationOSSSettings) error {
	pairs := []GlobalArg{
		{Key: ArgMigrationOSSEndpoint, Value: strings.TrimSpace(s.Endpoint), Description: "迁移 OSS 访问地址"},
		{Key: ArgMigrationOSSBucket, Value: strings.TrimSpace(s.Bucket), Description: "迁移 OSS 存储桶名称"},
		{Key: ArgMigrationOSSPrefix, Value: strings.TrimSpace(s.Prefix), Description: "迁移 OSS 文件路径前缀"},
		{Key: ArgMigrationOSSAccessKey, Value: strings.TrimSpace(s.AccessKey), Description: "迁移 OSS AccessKey ID"},
		{Key: ArgMigrationOSSSecretKey, Value: strings.TrimSpace(s.SecretKey), Description: "迁移 OSS AccessKey Secret"},
		{Key: ArgMigrationOSSRetentionDays, Value: strconv.Itoa(clampInt(s.RetentionDays, 1, 3650, 7)), Description: "迁移 OSS 临时文件保留天数"},
		{Key: ArgMigrationOSSChunkSizeMB, Value: strconv.Itoa(clampInt(s.ChunkSizeMB, 1, 10240, 100)), Description: "迁移 OSS 单文件分片大小(MB)"},
	}
	for _, a := range pairs {
		if (a.Key == ArgMigrationOSSAccessKey || a.Key == ArgMigrationOSSSecretKey) && isMaskedSecret(a.Value) {
			continue
		}
		if err := d.SetGlobalArg(a.Key, a.Value, a.Description); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) SaveMigrationPerf(s MigrationPerfSettings) error {
	pairs := []struct {
		key, desc          string
		val, min, max, def int
	}{
		{ArgMigrationMaxConcurrentTasks, "迁移最大并发任务数", s.MaxConcurrentTasks, 1, 10, 3},
		{ArgMigrationTableParallelism, "迁移默认多表并行度", s.TableParallelism, 1, 16, 4},
		{ArgMigrationBatchSize, "迁移默认批次大小", s.BatchSize, 100, 100000, 10000},
		{ArgMigrationSingleTableTimeout, "单表迁移超时(分钟)", s.SingleTableTimeoutMin, 1, 1440, 30},
		{ArgMigrationDefaultSpeedLimit, "迁移默认限速(行/秒)，0 表示不限速", s.DefaultSpeedLimit, 0, 1000000, 0},
		{ArgMigrationScheduleTolerance, "定时触发容忍窗口(分钟)", s.ScheduleToleranceMin, 1, 60, 10},
	}
	for _, a := range pairs {
		if err := d.SetGlobalArg(a.key, strconv.Itoa(clampInt(a.val, a.min, a.max, a.def)), a.desc); err != nil {
			return err
		}
	}
	return nil
}

func clampInt(n, min, max, def int) int {
	if n == 0 && def != 0 && min > 0 {
		return def
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}

func isMaskedSecret(v string) bool {
	s := strings.TrimSpace(v)
	if s == "" {
		return true
	}
	for _, c := range s {
		if c != '*' && c != '•' && c != '·' {
			return false
		}
	}
	return len(s) >= 4
}
