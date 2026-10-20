package jobreg

import (
	"context"
	"time"
)

const (
	ScheduleInterval = "interval"
	ScheduleCron6    = "cron6" // 秒 分 时 * * *
	ScheduleCron5    = "cron5" // 分 时 日 月 周

	MisfireCatchUpOnce = "catch_up_once"
	MisfireSkip        = "skip"

	TriggerCron    = "cron"
	TriggerManual  = "manual"
	TriggerMisfire = "misfire"

	RiskLow    = "low"
	RiskMedium = "medium"
	RiskHigh   = "high"

	DefaultLeaseTTL     = 30 * time.Second
	DefaultHeartbeat    = 10 * time.Second
	DefaultPollInterval = 5 * time.Second
)

// Runner 执行业务逻辑；ctx 在 fencing 失败时会被取消。
type Runner func(ctx context.Context) error

type JobDesc struct {
	Key            string
	Name           string
	Group          string
	Description    string
	ScheduleKind   string
	DefaultSpec    string // interval: "300s"；cron6/cron5: 表达式
	Mutable        bool
	Triggerable    bool
	Pauseable      bool
	RiskLevel      string
	MisfirePolicy  string
	MisfireGrace   time.Duration
	SyncGlobalArg  string // 非空时写 settings 同步回写该 global arg
	SyncEnabledArg string // 开关类 global arg（true/false）
}

type JobView struct {
	Key            string `json:"key"`
	Name           string `json:"name"`
	Group          string `json:"group"`
	Description    string `json:"description"`
	ScheduleKind   string `json:"schedule_kind"`
	ScheduleSpec   string `json:"schedule_spec"`
	ScheduleHuman  string `json:"schedule_human"`
	Enabled        bool   `json:"enabled"`
	Mutable        bool   `json:"mutable"`
	Triggerable    bool   `json:"triggerable"`
	Pauseable      bool   `json:"pauseable"`
	RiskLevel      string `json:"risk_level"`
	MisfirePolicy  string `json:"misfire_policy"`
	Version        int64  `json:"version"`
	NextFireAt     string `json:"next_fire_at,omitempty"`
	LastStatus     string `json:"last_status,omitempty"`
	LastStartedAt  string `json:"last_started_at,omitempty"`
	LastEndedAt    string `json:"last_ended_at,omitempty"`
	LastError      string `json:"last_error,omitempty"`
	LastDurationMs int64  `json:"last_duration_ms,omitempty"`
	RunCount24h    int    `json:"run_count_24h"`
	FailCount24h   int    `json:"fail_count_24h"`
	LeaseOwnerID   string `json:"lease_owner_id,omitempty"`
	LeaseOwnerHost string `json:"lease_owner_host,omitempty"`
	LeaseExpiresAt string `json:"lease_expires_at,omitempty"`
	LeaseHeld      bool   `json:"lease_held"`
	FencingToken   int64  `json:"fencing_token,omitempty"`
	LocalOwner     bool   `json:"local_owner"`
}

type BusinessSchedule struct {
	Kind         string `json:"kind"`
	ID           string `json:"id"`
	Name         string `json:"name"`
	ScheduleExpr string `json:"schedule_expr,omitempty"`
	Enabled      bool   `json:"enabled"`
	Status       string `json:"status,omitempty"`
	LastRunAt    string `json:"last_run_at,omitempty"`
	Link         string `json:"link"`
	TenantID     string `json:"tenant_id,omitempty"`
	Extra        string `json:"extra,omitempty"`
}

type TriggerResult struct {
	Skipped  bool   `json:"skipped"`
	Reason   string `json:"reason,omitempty"`
	RunID    string `json:"run_id,omitempty"`
	FireKey  string `json:"fire_key,omitempty"`
	Holder   string `json:"holder,omitempty"`
	Expires  string `json:"expires_at,omitempty"`
	Conflict bool   `json:"conflict,omitempty"` // lease held by other
}
