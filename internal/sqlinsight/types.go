package sqlinsight

import (
	"encoding/json"
	"time"
)

// Pagination 列表分页。
type Pagination struct {
	Total    int `json:"total"`
	Current  int `json:"current"`
	PageSize int `json:"page_size"`
}

// ---- Catalog ----

type BuiltinField struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Source      string `json:"source"` // awr / pg_activity
	Unit        string `json:"unit"`
	DefaultStat string `json:"default_stat"`
	Description string `json:"description"`
}

// ---- Metric ----

type MetricCreateReq struct {
	MetricKey    string `json:"metric_key" binding:"required"`
	Name         string `json:"name" binding:"required"`
	Description  string `json:"description"`
	Source       string `json:"source" binding:"required"`
	ExprType     string `json:"expr_type"`
	BuiltinField string `json:"builtin_field" binding:"required"`
	Unit         string `json:"unit"`
	Stat         string `json:"stat"`
	ScopeHost    string `json:"scope_host"`
	ScopePort    int    `json:"scope_port"`
	Enabled      *bool  `json:"enabled"`
}

type MetricUpdateReq struct {
	Name         *string `json:"name"`
	Description  *string `json:"description"`
	Source       *string `json:"source"`
	ExprType     *string `json:"expr_type"`
	BuiltinField *string `json:"builtin_field"`
	Unit         *string `json:"unit"`
	Stat         *string `json:"stat"`
	ScopeHost    *string `json:"scope_host"`
	ScopePort    *int    `json:"scope_port"`
	Enabled      *bool   `json:"enabled"`
}

type MetricQuery struct {
	Source   string `form:"source"`
	Enabled  *bool  `form:"enabled"`
	Current  int    `form:"current"`
	PageSize int    `form:"page_size"`
}

type MetricItem struct {
	ID           string     `json:"id"`
	MetricKey    string     `json:"metric_key"`
	Name         string     `json:"name"`
	Description  string     `json:"description"`
	Source       string     `json:"source"`
	ExprType     string     `json:"expr_type"`
	BuiltinField string     `json:"builtin_field"`
	Unit         string     `json:"unit"`
	Stat         string     `json:"stat"`
	ScopeHost    string     `json:"scope_host"`
	ScopePort    int        `json:"scope_port"`
	Enabled      bool       `json:"enabled"`
	System       bool       `json:"system"`
	CreatedBy    string     `json:"created_by"`
	UpdatedBy    string     `json:"updated_by"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    *time.Time `json:"updated_at,omitempty"`
}

type MetricListData struct {
	List       []*MetricItem `json:"list"`
	Pagination Pagination    `json:"pagination"`
}

// ---- Rule ----

type RuleCreateReq struct {
	Name           string   `json:"name" binding:"required"`
	Description    string   `json:"description"`
	MetricKey      string   `json:"metric_key" binding:"required"`
	RuleKind       string   `json:"rule_kind" binding:"required"`
	Direction      string   `json:"direction"`
	WarnThreshold  *float64 `json:"warn_threshold"`
	CritThreshold  *float64 `json:"crit_threshold"`
	BaselineWindow string   `json:"baseline_window"`
	ZScoreK        *float64 `json:"zscore_k"`
	Enabled        *bool    `json:"enabled"`
}

type RuleUpdateReq struct {
	Name           *string  `json:"name"`
	Description    *string  `json:"description"`
	MetricKey      *string  `json:"metric_key"`
	RuleKind       *string  `json:"rule_kind"`
	Direction      *string  `json:"direction"`
	WarnThreshold  *float64 `json:"warn_threshold"`
	CritThreshold  *float64 `json:"crit_threshold"`
	BaselineWindow *string  `json:"baseline_window"`
	ZScoreK        *float64 `json:"zscore_k"`
	Enabled        *bool    `json:"enabled"`
}

type RuleQuery struct {
	MetricKey string `form:"metric_key"`
	Enabled   *bool  `form:"enabled"`
	Current   int    `form:"current"`
	PageSize  int    `form:"page_size"`
}

type RuleItem struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Description    string     `json:"description"`
	MetricKey      string     `json:"metric_key"`
	RuleKind       string     `json:"rule_kind"`
	Direction      string     `json:"direction"`
	WarnThreshold  *float64   `json:"warn_threshold,omitempty"`
	CritThreshold  *float64   `json:"crit_threshold,omitempty"`
	BaselineWindow string     `json:"baseline_window"`
	ZScoreK        float64    `json:"zscore_k"`
	Enabled        bool       `json:"enabled"`
	System         bool       `json:"system"`
	CreatedBy      string     `json:"created_by"`
	UpdatedBy      string     `json:"updated_by"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      *time.Time `json:"updated_at,omitempty"`
}

type RuleListData struct {
	List       []*RuleItem `json:"list"`
	Pagination Pagination  `json:"pagination"`
}

// ---- Run / Finding ----

type RunCreateReq struct {
	Date         string   `json:"date"`
	Host         string   `json:"host"`
	Port         int      `json:"port"`
	UserScenario string   `json:"user_scenario"`
	ScenarioTags []string `json:"scenario_tags"`
}

type RunQuery struct {
	Host     string `form:"host"`
	Port     int    `form:"port"`
	Current  int    `form:"current"`
	PageSize int    `form:"page_size"`
}

type Narrative struct {
	Headline        string         `json:"headline"`
	OverallSeverity string         `json:"overall_severity"`
	RootCauses      []string       `json:"root_causes"`
	Actions         []NarrativeAct `json:"actions"`
	Confidence      string         `json:"confidence"`
	Source          string         `json:"source"`
}

type NarrativeAct struct {
	Priority       int      `json:"priority"`
	Action         string   `json:"action"`
	RelatedMetrics []string `json:"related_metrics"`
}

type RunItem struct {
	ID              string     `json:"id"`
	AnalysisDate    string     `json:"analysis_date"`
	Host            string     `json:"host"`
	Port            int        `json:"port"`
	Status          string     `json:"status"`
	Severity        string     `json:"severity"`
	FindingCount    int        `json:"finding_count"`
	CriticalCount   int        `json:"critical_count"`
	WarningCount    int        `json:"warning_count"`
	InfoCount       int        `json:"info_count"`
	ErrorMessage    string     `json:"error_message,omitempty"`
	UserScenario    string     `json:"user_scenario,omitempty"`
	ScenarioTags    []string   `json:"scenario_tags,omitempty"`
	NarrativeStatus string     `json:"narrative_status"`
	Narrative       *Narrative `json:"narrative,omitempty"`
	NarrativeText   string     `json:"narrative_text,omitempty"`
	NarrativeSource string     `json:"narrative_source,omitempty"`
	TriggerType     string     `json:"trigger_type"`
	CreatedBy       string     `json:"created_by"`
	StartedAt       time.Time  `json:"started_at"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

type DeepAnalysis struct {
	Summary         string               `json:"summary"`
	Observations    []DeepObservation    `json:"observations,omitempty"`
	Diagnoses       []DeepDiagnosis      `json:"diagnoses,omitempty"`
	Recommendations []DeepRecommendation `json:"recommendations,omitempty"`
	RelatedMetrics  []string             `json:"related_metrics,omitempty"`
	ScenarioApplied *ScenarioApplied     `json:"scenario_applied,omitempty"`
}

type DeepObservation struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Value string `json:"value"`
	Note  string `json:"note,omitempty"`
}

type DeepDiagnosis struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
}

type DeepRecommendation struct {
	Category string `json:"category"`
	Priority int    `json:"priority"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
}

type ScenarioApplied struct {
	Tags     []string `json:"tags,omitempty"`
	UserText string   `json:"user_text,omitempty"`
	Effects  []string `json:"effects,omitempty"`
}

type RunListData struct {
	List       []*RunItem `json:"list"`
	Pagination Pagination `json:"pagination"`
}

type FindingQuery struct {
	RunID    string `form:"run_id"`
	Host     string `form:"host"`
	Port     int    `form:"port"`
	Severity string `form:"severity"`
	Current  int    `form:"current"`
	PageSize int    `form:"page_size"`
}

type FindingItem struct {
	ID            string          `json:"id"`
	RunID         string          `json:"run_id"`
	AnalysisDate  string          `json:"analysis_date"`
	Host          string          `json:"host"`
	Port          int             `json:"port"`
	InstanceID    string          `json:"instance_id"`
	InstanceName  string          `json:"instance_name"`
	MetricKey     string          `json:"metric_key"`
	RuleID        string          `json:"rule_id"`
	RuleKind      string          `json:"rule_kind"`
	Severity      string          `json:"severity"`
	Title         string          `json:"title"`
	Message       string          `json:"message"`
	Value         *float64        `json:"value,omitempty"`
	Threshold     *float64        `json:"threshold,omitempty"`
	BaselineValue *float64        `json:"baseline_value,omitempty"`
	Unit          string          `json:"unit"`
	Evidence      json.RawMessage `json:"evidence"`
	DeepAnalysis  *DeepAnalysis   `json:"deep_analysis,omitempty"`
	Suggestion    string          `json:"suggestion"`
	Fingerprint   string          `json:"fingerprint"`
	Feedback      string          `json:"feedback,omitempty"`
	FeedbackNote  string          `json:"feedback_note,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
}

type FindingListData struct {
	List       []*FindingItem `json:"list"`
	Pagination Pagination     `json:"pagination"`
}

type FeedbackReq struct {
	Feedback string `json:"feedback" binding:"required"`
	Note     string `json:"note"`
}

type SummaryQuery struct {
	RunID string `form:"run_id"`
	Date  string `form:"date"`
	Host  string `form:"host"`
	Port  int    `form:"port"`
}

type SummaryData struct {
	RunID         string `json:"run_id,omitempty"`
	AnalysisDate  string `json:"analysis_date,omitempty"`
	CriticalCount int    `json:"critical_count"`
	WarningCount  int    `json:"warning_count"`
	InfoCount     int    `json:"info_count"`
	FindingTotal  int    `json:"finding_total"`
	Severity      string `json:"severity"`
}

// AWRReportView 分析引擎消费的报告视图。
type AWRReportView struct {
	Host         string
	Port         int64
	InstanceID   string
	InstanceName string
	Sections     json.RawMessage
	Anomalies    json.RawMessage
}
