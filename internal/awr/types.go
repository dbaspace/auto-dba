package awr

import (
	"encoding/json"
	"time"
)

// ReportQuery 每日 AWR 报告列表查询。
type ReportQuery struct {
	ReportDate string `form:"report_date"`
	StartDate  string `form:"start_date"`
	EndDate    string `form:"end_date"`
	InstanceID string `form:"instance_id"`
	Host       string `form:"host"`
	Port       int64  `form:"port"`
	Status     string `form:"status"`
	Current    int    `form:"current"`
	PageSize   int    `form:"page_size"`
}

// ReportDetailQuery 报告详情查询。
type ReportDetailQuery struct {
	ID         int64  `form:"id"`
	ReportDate string `form:"report_date"`
	Host       string `form:"host"`
	Port       int64  `form:"port"`
}

// GenerateReq 手动生成报告请求。
type GenerateReq struct {
	Date       string `json:"date"`
	InstanceID string `json:"instance_id"`
	Host       string `json:"host"`
	Port       int64  `json:"port"`
}

// ReportItem 报告列表项（不含 sections）。
type ReportItem struct {
	ID           int64        `json:"id"`
	ReportDate   string       `json:"report_date"`
	InstanceID   string       `json:"instance_id"`
	InstanceName string       `json:"instance_name"`
	Host         string       `json:"host"`
	Port         int64        `json:"port"`
	Source       string       `json:"source"`
	PeriodStart  time.Time    `json:"period_start"`
	PeriodEnd    time.Time    `json:"period_end"`
	Status       string       `json:"status"`
	Severity     string       `json:"severity"`
	AnomalyCount int          `json:"anomaly_count"`
	Anomalies    []AwrAnomaly `json:"anomalies,omitempty"`
	ErrorMessage string       `json:"error_message,omitempty"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    *time.Time   `json:"updated_at,omitempty"`
}

// AwrAnomaly 单条异常明细。
type AwrAnomaly struct {
	Key       string   `json:"key"`
	Title     string   `json:"title"`
	Section   string   `json:"section"`
	Severity  string   `json:"severity"`
	Kind      string   `json:"kind"`
	Value     *float64 `json:"value,omitempty"`
	Threshold *float64 `json:"threshold,omitempty"`
	Baseline  *float64 `json:"baseline,omitempty"`
	DeltaPct  *float64 `json:"delta_pct,omitempty"`
	Unit      string   `json:"unit,omitempty"`
	Message   string   `json:"message"`
}

// ReportDetail 报告详情（含 sections）。
type ReportDetail struct {
	ReportItem
	Sections json.RawMessage `json:"sections"`
}

// ReportListData 报告列表分页。
type ReportListData struct {
	List     []*ReportItem `json:"list"`
	Total    int           `json:"total"`
	Current  int           `json:"current"`
	PageSize int           `json:"page_size"`
}

// GenerateData 同步生成统计（兼容保留）。
type GenerateData struct {
	Date               string `json:"date"`
	InstancesProcessed int    `json:"instances_processed"`
	ReportsWritten     int    `json:"reports_written"`
	Failed             int    `json:"failed"`
}

// GenerateJobQuery 生成任务查询。
type GenerateJobQuery struct {
	JobID string `form:"job_id"`
	Limit int    `form:"limit"`
}

// GenerateJobItem 报告生成任务进度。
type GenerateJobItem struct {
	ID              string     `json:"id"`
	Scope           string     `json:"scope"`
	ReportDate      string     `json:"report_date"`
	InstanceID      string     `json:"instance_id,omitempty"`
	Host            string     `json:"host,omitempty"`
	Port            int64      `json:"port,omitempty"`
	State           string     `json:"state"`
	InstancesTotal  int        `json:"instances_total"`
	InstancesDone   int        `json:"instances_done"`
	CurrentInstance string     `json:"current_instance,omitempty"`
	CurrentStage    string     `json:"current_stage,omitempty"`
	ReportsWritten  int        `json:"reports_written"`
	FailedCount     int        `json:"failed_count"`
	Error           string     `json:"error,omitempty"`
	CreatedBy       string     `json:"created_by,omitempty"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	UpdatedAt       *time.Time `json:"updated_at,omitempty"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

// JobStartData 启动生成任务返回。
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

// AnomalyQuery 异常汇总查询。
type AnomalyQuery struct {
	ReportDate string `form:"report_date"`
	StartDate  string `form:"start_date"`
	EndDate    string `form:"end_date"`
	Severity   string `form:"severity"`
	Limit      int    `form:"limit"`
}

// AnomalySummary 异常汇总。
type AnomalySummary struct {
	Total         int           `json:"total"`
	CriticalCount int           `json:"critical_count"`
	WarningCount  int           `json:"warning_count"`
	NoneCount     int           `json:"none_count"`
	AnomalyTotal  int           `json:"anomaly_total"`
	TopInstances  []*ReportItem `json:"top_instances"`
}

// SummaryQuery 全实例异常汇总查询。
type SummaryQuery struct {
	ReportDate string `form:"report_date"`
}

// SummaryDomainItem 汇总中单个实例的异常摘要。
type SummaryDomainItem struct {
	InstanceID   string       `json:"instance_id"`
	InstanceName string       `json:"instance_name"`
	Host         string       `json:"host"`
	Port         int64        `json:"port"`
	Source       string       `json:"source"`
	Severity     string       `json:"severity"`
	Status       string       `json:"status"`
	ErrorMessage string       `json:"error_message,omitempty"`
	AnomalyCount int          `json:"anomaly_count"`
	Anomalies    []AwrAnomaly `json:"anomalies"`
}

// AnomalyTypeStat 按指标类型聚合的异常统计。
type AnomalyTypeStat struct {
	Key           string `json:"key"`
	Title         string `json:"title"`
	Section       string `json:"section"`
	Unit          string `json:"unit,omitempty"`
	Count         int    `json:"count"`
	CriticalCount int    `json:"critical_count"`
	WarningCount  int    `json:"warning_count"`
}

// SummaryData 全实例异常汇总数据。
type SummaryData struct {
	ReportDate         string               `json:"report_date"`
	Total              int                  `json:"total"`
	CriticalCount      int                  `json:"critical_count"`
	WarningCount       int                  `json:"warning_count"`
	NoneCount          int                  `json:"none_count"`
	FailedCount        int                  `json:"failed_count"`
	AnomalyDomainCount int                  `json:"anomaly_domain_count"`
	IssueDomainCount   int                  `json:"issue_domain_count"`
	AnomalyTotal       int                  `json:"anomaly_total"`
	AnomalyTypeStats   []*AnomalyTypeStat   `json:"anomaly_type_stats"`
	Domains            []*SummaryDomainItem `json:"domains"`
}

// AwrSeriesPoint 降采样时间序列点。
type AwrSeriesPoint struct {
	T int64   `json:"t"`
	V float64 `json:"v"`
}

// AwrMetric 单个指标聚合结果。
type AwrMetric struct {
	Key    string           `json:"key"`
	Title  string           `json:"title"`
	Unit   string           `json:"unit,omitempty"`
	Avg    *float64         `json:"avg,omitempty"`
	Max    *float64         `json:"max,omitempty"`
	Min    *float64         `json:"min,omitempty"`
	Last   *float64         `json:"last,omitempty"`
	Series []AwrSeriesPoint `json:"series,omitempty"`
	Error  string           `json:"error,omitempty"`
}

// AwrSection 报告板块。
type AwrSection struct {
	Name    string       `json:"name"`
	Title   string       `json:"title"`
	Metrics []*AwrMetric `json:"metrics"`
}

// AwrTopSQL Top SQL / 长事务条目。
type AwrTopSQL struct {
	QueryHash       string   `json:"query_hash"`
	QueryNormalized string   `json:"query_normalized"`
	SampleInfo      string   `json:"sample_info"`
	Count           int64    `json:"count"`
	AvgXactDuration *float64 `json:"avg_xact_duration,omitempty"`
	MaxXactDuration *float64 `json:"max_xact_duration,omitempty"`
}

// AwrSections 报告全部板块（供 sqlinsight 等复用）。
type AwrSections struct {
	GeneratedAt time.Time     `json:"generated_at"`
	Sections    []*AwrSection `json:"sections"`
	TopSQL      []*AwrTopSQL  `json:"top_sql,omitempty"`
	LongTxn     []*AwrTopSQL  `json:"long_txn,omitempty"`
	Warnings    []string      `json:"warnings,omitempty"`
}
