package pgspace

import "time"

// Object 表空间/存储分析对象（各列表复用）。
type Object struct {
	ObjectType     string   `json:"object_type"`
	DbName         string   `json:"db_name,omitempty"`
	SchemaName     string   `json:"schema_name,omitempty"`
	ObjectName     string   `json:"object_name"`
	TablespaceName string   `json:"tablespace_name,omitempty"`
	ShardCount     int64    `json:"shard_count"`
	SizeBytes      *int64   `json:"size_bytes"`
	TableBytes     *int64   `json:"table_bytes,omitempty"`
	IndexBytes     *int64   `json:"index_bytes,omitempty"`
	ToastBytes     *int64   `json:"toast_bytes,omitempty"`
	NLiveTup       *int64   `json:"n_live_tup,omitempty"`
	NDeadTup       *int64   `json:"n_dead_tup,omitempty"`
	DeadRatio      *float64 `json:"dead_ratio,omitempty"`
	BloatBytes     *int64   `json:"bloat_bytes,omitempty"`
	BloatRatio     *float64 `json:"bloat_ratio,omitempty"`
	IdxScan        *int64   `json:"idx_scan,omitempty"`
	IsUnused       *bool    `json:"is_unused,omitempty"`

	SeqScan           *int64     `json:"seq_scan,omitempty"`
	TblIdxScan        *int64     `json:"tbl_idx_scan,omitempty"`
	HeapBlksRead      *int64     `json:"heap_blks_read,omitempty"`
	HeapBlksHit       *int64     `json:"heap_blks_hit,omitempty"`
	HeapCacheHitRatio *float64   `json:"heap_cache_hit_ratio,omitempty"`
	NTupIns           *int64     `json:"n_tup_ins,omitempty"`
	NTupUpd           *int64     `json:"n_tup_upd,omitempty"`
	NTupDel           *int64     `json:"n_tup_del,omitempty"`
	LastVacuum        *time.Time `json:"last_vacuum,omitempty"`
	LastAutovacuum    *time.Time `json:"last_autovacuum,omitempty"`
	LastAnalyze       *time.Time `json:"last_analyze,omitempty"`
	LastAutoanalyze   *time.Time `json:"last_autoanalyze,omitempty"`
	RelFrozenXIDAge   *int64     `json:"relfrozenxid_age,omitempty"`
	DatFrozenXIDAge   *int64     `json:"datfrozenxid_age,omitempty"`
	SeqLastValue      *int64     `json:"seq_last_value,omitempty"`
	SeqMaxValue       *int64     `json:"seq_max_value,omitempty"`
	SeqMinValue       *int64     `json:"seq_min_value,omitempty"`
	SeqUsedRatio      *float64   `json:"seq_used_ratio,omitempty"`
	SeqRemaining      *int64     `json:"seq_remaining,omitempty"`
	SeqCycle          *bool      `json:"seq_cycle,omitempty"`
}

// InstanceItem 物理实例下拉项。
type InstanceItem struct {
	InstanceID string `json:"instance_id"`
	Name       string `json:"name"`
	Host       string `json:"host"`
	Port       int64  `json:"port"`
	IDC        string `json:"idc,omitempty"`
}

// RealtimeData 实时分析结果。
type RealtimeData struct {
	InstanceID    string    `json:"instance_id"`
	InstanceName  string    `json:"instance_name"`
	Host          string    `json:"host"`
	Port          int64     `json:"port"`
	CollectedAt   time.Time `json:"collected_at"`
	Tablespaces   []*Object `json:"tablespaces"`
	Databases     []*Object `json:"databases"`
	Tables        []*Object `json:"tables"`
	Indexes       []*Object `json:"indexes"`
	UnusedIndexes []*Object `json:"unused_indexes"`
	Sequences     []*Object `json:"sequences"`
	ScannedDbs    []string  `json:"scanned_dbs"`
	Warnings      []string  `json:"warnings"`
}

// DatabaseItem 候选库。
type DatabaseItem struct {
	DbName         string `json:"db_name"`
	SizeBytes      *int64 `json:"size_bytes"`
	TablespaceName string `json:"tablespace_name,omitempty"`
}

// DatabasesData 候选库列表。
type DatabasesData struct {
	Source      string          `json:"source"`
	CollectedAt *time.Time      `json:"collected_at"`
	Databases   []*DatabaseItem `json:"databases"`
}

// SnapshotItem 历史快照。
type SnapshotItem struct {
	ID          int64     `json:"id"`
	CollectedAt time.Time `json:"collected_at"`
	Object
	InstanceID   string `json:"instance_id"`
	InstanceName string `json:"instance_name"`
	Host         string `json:"host"`
	Port         int64  `json:"port"`
}

// SnapshotPage 快照分页。
type SnapshotPage struct {
	List     []*SnapshotItem `json:"list"`
	Total    int             `json:"total"`
	Current  int             `json:"current"`
	PageSize int             `json:"page_size"`
}

// TrendPoint 趋势点。
type TrendPoint struct {
	CollectedAt time.Time `json:"collected_at"`
	SizeBytes   *int64    `json:"size_bytes"`
}

// TrendData 趋势结果。
type TrendData struct {
	ObjectType       string        `json:"object_type"`
	DbName           string        `json:"db_name,omitempty"`
	ObjectName       string        `json:"object_name"`
	Points           []*TrendPoint `json:"points"`
	FirstSizeBytes   *int64        `json:"first_size_bytes"`
	LastSizeBytes    *int64        `json:"last_size_bytes"`
	GrowthBytes      *int64        `json:"growth_bytes"`
	DailyGrowthBytes *float64      `json:"daily_growth_bytes"`
	DaysToFull       *float64      `json:"days_to_full,omitempty"`
}

// SummaryData 可视汇总。
type SummaryData struct {
	CollectedAt        *time.Time `json:"collected_at"`
	Tablespaces        []*Object  `json:"tablespaces"`
	Databases          []*Object  `json:"databases"`
	TopTables          []*Object  `json:"top_tables"`
	TopIndexes         []*Object  `json:"top_indexes"`
	TopBloat           []*Object  `json:"top_bloat"`
	UnusedIndexes      []*Object  `json:"unused_indexes"`
	TopSequences       []*Object  `json:"top_sequences"`
	TopSeqScan         []*Object  `json:"top_seq_scan"`
	TopCacheMiss       []*Object  `json:"top_cache_miss"`
	TopHotTables       []*Object  `json:"top_hot_tables"`
	TopDeadTuples      []*Object  `json:"top_dead_tuples"`
	TopXIDAge          []*Object  `json:"top_xid_age"`
	UnusedIndexBytes   int64      `json:"unused_index_bytes"`
	TotalDatabaseBytes int64      `json:"total_database_bytes"`
}

// Advice 单条建议。
type Advice struct {
	Key       string    `json:"key"`
	Title     string    `json:"title"`
	Severity  string    `json:"severity"`
	Action    string    `json:"action"`
	Message   string    `json:"message"`
	Value     *float64  `json:"value,omitempty"`
	Threshold *float64  `json:"threshold,omitempty"`
	Unit      string    `json:"unit,omitempty"`
	Evidence  []*Object `json:"evidence,omitempty"`
}

// AdviceData 实例级建议报告。
type AdviceData struct {
	InstanceID        string     `json:"instance_id"`
	InstanceName      string     `json:"instance_name"`
	Host              string     `json:"host"`
	Port              int64      `json:"port"`
	CollectedAt       *time.Time `json:"collected_at,omitempty"`
	Severity          string     `json:"severity"`
	AdviceCount       int        `json:"advice_count"`
	ReclaimableBytes  int64      `json:"reclaimable_bytes"`
	XIDRiskCount      int        `json:"xid_risk_count"`
	SequenceRiskCount int        `json:"sequence_risk_count"`
	VacuumRiskCount   int        `json:"vacuum_risk_count"`
	AccessRiskCount   int        `json:"access_risk_count"`
	CacheRiskCount    int        `json:"cache_risk_count"`
	HotTableCount     int        `json:"hot_table_count"`
	Message           string     `json:"message,omitempty"`
	Advices           []*Advice  `json:"advices"`
	Summary           *SummaryData `json:"summary,omitempty"`
}

// AdviceTypeStat 建议类型统计。
type AdviceTypeStat struct {
	Key           string `json:"key"`
	Title         string `json:"title"`
	Action        string `json:"action"`
	Count         int    `json:"count"`
	CriticalCount int    `json:"critical_count"`
	WarningCount  int    `json:"warning_count"`
}

// AdviceInstanceBrief 汇总中的问题实例。
type AdviceInstanceBrief struct {
	InstanceID       string    `json:"instance_id"`
	InstanceName     string    `json:"instance_name"`
	Host             string    `json:"host"`
	Port             int64     `json:"port"`
	Severity         string    `json:"severity"`
	AdviceCount      int       `json:"advice_count"`
	ReclaimableBytes int64     `json:"reclaimable_bytes"`
	TopAdvices       []*Advice `json:"top_advices"`
}

// AdviceFleetData 跨实例汇总。
type AdviceFleetData struct {
	TotalInstances        int                    `json:"total_instances"`
	CriticalCount         int                    `json:"critical_count"`
	WarningCount          int                    `json:"warning_count"`
	NoneCount             int                    `json:"none_count"`
	IssueInstanceCount    int                    `json:"issue_instance_count"`
	AdviceTotal           int                    `json:"advice_total"`
	ReclaimableBytesTotal int64                  `json:"reclaimable_bytes_total"`
	AdviceTypeStats       []*AdviceTypeStat      `json:"advice_type_stats"`
	Instances             []*AdviceInstanceBrief `json:"instances"`
}

// JobStartData 启动采集返回。
type JobStartData struct {
	JobID  string `json:"job_id"`
	State  string `json:"state"`
	Reused bool   `json:"reused"`
}

// JobControlData 暂停/继续/终止返回。
type JobControlData struct {
	JobID string `json:"job_id"`
	State string `json:"state"`
}
