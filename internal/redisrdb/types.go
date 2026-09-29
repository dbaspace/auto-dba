package redisrdb

import "time"

const (
	JobPending    = "pending"
	JobRunning    = "running"
	JobSucceeded  = "succeeded"
	JobFailed     = "failed"
	MaxUploadSize = 512 << 20 // 512MB
	DefaultTopN   = 50
	MaxTopN       = 200
)

// Job 一次 RDB 分析任务元数据。
type Job struct {
	ID           string    `json:"id"`
	InstanceID   string    `json:"instance_id,omitempty"`
	InstanceName string    `json:"instance_name,omitempty"`
	FileName     string    `json:"file_name"`
	FileSize     int64     `json:"file_size"`
	Status       string    `json:"status"`
	Error        string    `json:"error,omitempty"`
	Note         string    `json:"note,omitempty"`
	Source       string    `json:"source,omitempty"` // upload | agent
	AgentID      string    `json:"agent_id,omitempty"`
	AgentName    string    `json:"agent_name,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	StartedAt    time.Time `json:"started_at,omitempty"`
	FinishedAt   time.Time `json:"finished_at,omitempty"`
	CreatedBy    string    `json:"created_by,omitempty"`
}

// AgentItem 可选平台 Agent（公共主机）。
type AgentItem struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	BaseURL        string `json:"base_url"`
	HostInstance   string `json:"host_instance_id,omitempty"`
	Remark         string `json:"remark,omitempty"`
	EnabledGroups  string `json:"enabled_groups,omitempty"`
	Capabilities   string `json:"capabilities,omitempty"`
	RDBReady       bool   `json:"rdb_ready"`
}

// KeyItem 大 key / 明细行。
type KeyItem struct {
	DB         int    `json:"db"`
	Key        string `json:"key"`
	Type       string `json:"type"`
	Encoding   string `json:"encoding,omitempty"`
	Bytes      int64  `json:"bytes"`
	ElemCount  int    `json:"elem_count"`
	HasTTL     bool   `json:"has_ttl"`
	ExpireAt   string `json:"expire_at,omitempty"`
	TTLSeconds int64  `json:"ttl_seconds,omitempty"`
}

// TypeStat 按类型汇总。
type TypeStat struct {
	Type  string `json:"type"`
	Count int64  `json:"count"`
	Bytes int64  `json:"bytes"`
}

// DBStat 按 DB 汇总。
type DBStat struct {
	DB    int   `json:"db"`
	Count int64 `json:"count"`
	Bytes int64 `json:"bytes"`
}

// PrefixStat 按 key 前缀汇总（默认按 : 分段）。
type PrefixStat struct {
	Prefix string `json:"prefix"`
	Count  int64  `json:"count"`
	Bytes  int64  `json:"bytes"`
}

// TTLBucket TTL 分布桶。
type TTLBucket struct {
	Bucket string `json:"bucket"`
	Count  int64  `json:"count"`
	Bytes  int64  `json:"bytes"`
}

// AdviceItem 诊断建议。
type AdviceItem struct {
	Level   string `json:"level"` // info | warn | critical
	Code    string `json:"code"`
	Title   string `json:"title"`
	Detail  string `json:"detail"`
	Suggest string `json:"suggest,omitempty"`
}

// Report RDB 分析完整报告。
type Report struct {
	JobID          string       `json:"job_id"`
	GeneratedAt    time.Time    `json:"generated_at"`
	DurationMS     int64        `json:"duration_ms"`
	RDBVersion     string       `json:"rdb_version,omitempty"`
	RedisVersion   string       `json:"redis_version,omitempty"`
	TotalKeys      int64        `json:"total_keys"`
	TotalBytes     int64        `json:"total_bytes"`
	KeysWithTTL    int64        `json:"keys_with_ttl"`
	KeysPersistent int64        `json:"keys_persistent"`
	ByType         []TypeStat   `json:"by_type"`
	ByDB           []DBStat     `json:"by_db"`
	ByPrefix       []PrefixStat `json:"by_prefix"`
	TTLBuckets     []TTLBucket  `json:"ttl_buckets"`
	TopKeys        []KeyItem    `json:"top_keys"`
	Advice         []AdviceItem `json:"advice"`
}

// OnlineSummary 在线 INFO 内存快照（不触发 BGSAVE）。
type OnlineSummary struct {
	InstanceID   string            `json:"instance_id"`
	InstanceName string            `json:"instance_name"`
	CollectedAt  time.Time         `json:"collected_at"`
	Memory       map[string]string `json:"memory"`
	Keyspace     map[string]string `json:"keyspace"`
	Stats        map[string]string `json:"stats,omitempty"`
	Parsed       OnlineParsed      `json:"parsed"`
	Advice       []AdviceItem      `json:"advice"`
}

// OnlineParsed 从 INFO 解析的常用指标。
type OnlineParsed struct {
	UsedMemoryBytes      int64   `json:"used_memory_bytes"`
	UsedMemoryHuman      string  `json:"used_memory_human"`
	MaxMemoryBytes       int64   `json:"maxmemory_bytes"`
	MaxMemoryPolicy      string  `json:"maxmemory_policy"`
	MemFragmentation     float64 `json:"mem_fragmentation_ratio"`
	TotalKeys            int64   `json:"total_keys"`
	TotalExpires         int64   `json:"total_expires"`
	EvictedKeys          int64   `json:"evicted_keys"`
	ExpiredKeys          int64   `json:"expired_keys"`
	UsedMemoryPctOfMax   float64 `json:"used_memory_pct_of_max,omitempty"`
}

// InstanceItem Redis 实例列表项。
type InstanceItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Host        string `json:"host"`
	Port        int64  `json:"port"`
	Environment string `json:"environment,omitempty"`
	Region      string `json:"region,omitempty"`
}
