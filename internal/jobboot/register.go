package jobboot

import (
	"context"
	"log"

	"auto-dba/internal/alert"
	"auto-dba/internal/archive"
	"auto-dba/internal/awr"
	"auto-dba/internal/backup"
	"auto-dba/internal/cmdb"
	"auto-dba/internal/dbmig"
	"auto-dba/internal/handler"
	"auto-dba/internal/jobreg"
	"auto-dba/internal/pgdiag"
	"auto-dba/internal/pgspace"
	"auto-dba/internal/sessionstore"
	"auto-dba/internal/sqlinsight"
	"auto-dba/internal/store"
)

// Deps 注册平台任务所需依赖。
type Deps struct {
	DB         *store.DB
	Sessions   *sessionstore.Store
	PgSpace    *pgspace.Service
	AWR        *awr.Service
	SQLInsight *sqlinsight.Service
	Migration  *dbmig.Engine
	Archive    *archive.Engine
	Backup     *backup.Engine
	Ticket     *handler.TicketHandler
	CDC        *handler.CDCHandler
	Slot       *handler.SlotHandler
	AlertSecret string
	PgSecret    *string
	Cmdb        *cmdb.Service
}

// RegisterPlatformJobs 注册全部平台系统任务（不含启动循环）。
func RegisterPlatformJobs(r *jobreg.Registry, d Deps) {
	if r == nil || d.DB == nil {
		return
	}

	r.Register(jobreg.JobDesc{
		Key: "session.reconcile", Name: "会话对账", Group: "session",
		Description: "元库 online 会话与 Redis/ZSET 一致性修复",
		ScheduleKind: jobreg.ScheduleInterval, DefaultSpec: "5m",
		Mutable: true, Triggerable: true, Pauseable: true, RiskLevel: jobreg.RiskLow,
		MisfirePolicy: jobreg.MisfireSkip,
	}, func(ctx context.Context) error {
		if d.Sessions == nil {
			return nil
		}
		_, err := sessionstore.ReconcileOnce(ctx, d.Sessions, d.DB)
		return err
	})

	r.Register(jobreg.JobDesc{
		Key: "pgdiag.activity_snapshot", Name: "PG 活动快照采集", Group: "collect",
		Description: "定时采集 PostgreSQL 活动快照并按保留天数清理",
		ScheduleKind: jobreg.ScheduleInterval, DefaultSpec: "300s",
		Mutable: true, Triggerable: true, Pauseable: true, RiskLevel: jobreg.RiskLow,
		MisfirePolicy: jobreg.MisfireSkip,
	}, func(ctx context.Context) error {
		return pgdiag.RunOnce(ctx, d.DB, deref(d.PgSecret))
	})

	if d.PgSpace != nil {
		d.PgSpace.MarkJobsInterrupted(context.Background())
	}
	r.Register(jobreg.JobDesc{
		Key: "pgspace.daily", Name: "表空间每日采集", Group: "collect",
		Description: "每日定点全量表空间采集与过期快照清理",
		ScheduleKind: jobreg.ScheduleCron6, DefaultSpec: "0 0 3 * * *",
		Mutable: true, Triggerable: true, Pauseable: true, RiskLevel: jobreg.RiskMedium,
		MisfirePolicy: jobreg.MisfireCatchUpOnce, SyncGlobalArg: "pg_space_daily_cron", SyncEnabledArg: "pg_space_enabled",
	}, func(ctx context.Context) error {
		if d.PgSpace == nil {
			return nil
		}
		return d.PgSpace.RunDailyCron(ctx)
	})

	if d.AWR != nil {
		d.AWR.MarkJobsInterrupted(context.Background())
	}
	r.Register(jobreg.JobDesc{
		Key: "awr.daily", Name: "AWR 每日报告", Group: "collect",
		Description: "每日生成昨日 AWR 报告并清理过期数据",
		ScheduleKind: jobreg.ScheduleCron6, DefaultSpec: "0 30 1 * * *",
		Mutable: true, Triggerable: true, Pauseable: true, RiskLevel: jobreg.RiskMedium,
		MisfirePolicy: jobreg.MisfireCatchUpOnce, SyncGlobalArg: "awr_daily_cron", SyncEnabledArg: "awr_enabled",
	}, func(ctx context.Context) error {
		if d.AWR == nil {
			return nil
		}
		return d.AWR.RunDailyCron(ctx)
	})

	if d.SQLInsight != nil {
		if err := d.SQLInsight.SeedSystemCases(context.Background()); err != nil {
			log.Printf("jobreg: SeedSystemCases: %v", err)
		}
		if err := d.SQLInsight.SeedSystemRules(context.Background()); err != nil {
			log.Printf("jobreg: SeedSystemRules: %v", err)
		}
	}
	r.Register(jobreg.JobDesc{
		Key: "sqlinsight.daily", Name: "SQL Insight 日分析", Group: "collect",
		Description: "对前一日执行智能分析",
		ScheduleKind: jobreg.ScheduleCron6, DefaultSpec: "0 0 3 * * *",
		Mutable: true, Triggerable: true, Pauseable: true, RiskLevel: jobreg.RiskMedium,
		MisfirePolicy: jobreg.MisfireCatchUpOnce,
		SyncGlobalArg: "sql_insight_analysis_cron", SyncEnabledArg: "sql_insight_analysis_cron_enabled",
	}, func(ctx context.Context) error {
		if d.SQLInsight == nil {
			return nil
		}
		return d.SQLInsight.RunDailyCron(ctx)
	})

	r.Register(jobreg.JobDesc{
		Key: "migration.due_scanner", Name: "迁移到期扫描", Group: "scanner",
		Description: "扫描 status=scheduled 且已到点的迁移任务并启动",
		ScheduleKind: jobreg.ScheduleInterval, DefaultSpec: "30s",
		Mutable: true, Triggerable: true, Pauseable: true, RiskLevel: jobreg.RiskHigh,
		MisfirePolicy: jobreg.MisfireSkip,
	}, func(ctx context.Context) error {
		if d.Migration == nil {
			return nil
		}
		d.Migration.ScanDue()
		return nil
	})

	r.Register(jobreg.JobDesc{
		Key: "archive.due_scanner", Name: "归档到期扫描", Group: "scanner",
		Description: "扫描已审批且定时模式的归档任务并触发",
		ScheduleKind: jobreg.ScheduleInterval, DefaultSpec: "60s",
		Mutable: true, Triggerable: true, Pauseable: true, RiskLevel: jobreg.RiskHigh,
		MisfirePolicy: jobreg.MisfireSkip,
	}, func(ctx context.Context) error {
		if d.Archive == nil {
			return nil
		}
		d.Archive.ScanDue()
		return nil
	})

	r.Register(jobreg.JobDesc{
		Key: "backup.due_scanner", Name: "备份策略扫描", Group: "scanner",
		Description: "扫描启用的备份策略并按 cron 触发",
		ScheduleKind: jobreg.ScheduleInterval, DefaultSpec: "60s",
		Mutable: true, Triggerable: true, Pauseable: true, RiskLevel: jobreg.RiskHigh,
		MisfirePolicy: jobreg.MisfireSkip,
	}, func(ctx context.Context) error {
		if d.Backup == nil {
			return nil
		}
		d.Backup.ScanDue()
		return nil
	})

	r.Register(jobreg.JobDesc{
		Key: "ticket.auto_cancel", Name: "工单待审超时取消", Group: "ticket",
		Description: "超过 8 小时仍待审批的工单系统取消",
		ScheduleKind: jobreg.ScheduleInterval, DefaultSpec: "5m",
		Mutable: true, Triggerable: true, Pauseable: true, RiskLevel: jobreg.RiskMedium,
		MisfirePolicy: jobreg.MisfireSkip,
	}, func(ctx context.Context) error {
		if d.Ticket == nil {
			return nil
		}
		return handler.RunIssueAutoCancelOnce(ctx, d.Ticket)
	})

	r.Register(jobreg.JobDesc{
		Key: "ticket.await_exec_notify", Name: "待执行卡住提醒", Group: "ticket",
		Description: "长时间停在已通过/待执行的工单邮件提醒提交人",
		ScheduleKind: jobreg.ScheduleInterval, DefaultSpec: "12m",
		Mutable: true, Triggerable: true, Pauseable: true, RiskLevel: jobreg.RiskLow,
		MisfirePolicy: jobreg.MisfireSkip,
	}, func(ctx context.Context) error {
		if d.Ticket == nil {
			return nil
		}
		_, err := handler.RunAwaitExecStuckNotifyOnce(ctx, d.Ticket)
		return err
	})

	r.Register(jobreg.JobDesc{
		Key: "cdc.schema_drift", Name: "CDC 结构漂移扫描", Group: "cdc",
		Description: "周期扫描 init CDC 任务结构漂移并写缓存",
		ScheduleKind: jobreg.ScheduleInterval, DefaultSpec: "300s",
		Mutable: true, Triggerable: true, Pauseable: true, RiskLevel: jobreg.RiskLow,
		MisfirePolicy: jobreg.MisfireSkip,
	}, func(ctx context.Context) error {
		if d.CDC == nil {
			return nil
		}
		handler.RunSchemaDriftScanOnce(ctx, d.CDC)
		return nil
	})

	_ = d.DB.EnsureGlobalArgDefault(
		store.ArgSlotMonitorCollectIntervalSec,
		"60",
		"复制槽监控采集间隔（秒）：支持 30、60 或自定义；定时任务 sync.slots.collect 按此间隔采集",
	)
	r.Register(jobreg.JobDesc{
		Key: "sync.slots.collect", Name: "复制槽状态采集", Group: "collect",
		Description: "周期采集 PostgreSQL 复制槽（pid、LSN、滞后），供同步列表展示",
		ScheduleKind: jobreg.ScheduleInterval, DefaultSpec: "60",
		Mutable: true, Triggerable: true, Pauseable: true, RiskLevel: jobreg.RiskLow,
		MisfirePolicy: jobreg.MisfireSkip,
		SyncGlobalArg: store.ArgSlotMonitorCollectIntervalSec,
	}, func(ctx context.Context) error {
		if d.Slot == nil {
			return nil
		}
		return handler.RunSlotCollectOnce(ctx, d.Slot)
	})

	ruler := &alert.Ruler{Store: d.DB, Secret: d.AlertSecret, Interval: alert.DefaultInterval}
	r.Register(jobreg.JobDesc{
		Key: "alert.ruler", Name: "告警规则评估", Group: "alert",
		Description: "Prometheus 评估告警规则、silence/inhibit/notify",
		ScheduleKind: jobreg.ScheduleInterval, DefaultSpec: "30s",
		Mutable: true, Triggerable: true, Pauseable: true, RiskLevel: jobreg.RiskHigh,
		MisfirePolicy: jobreg.MisfireSkip,
	}, func(ctx context.Context) error {
		ruler.RunOnce(ctx)
		return nil
	})

	r.Register(jobreg.JobDesc{
		Key: "alert.rule_compile", Name: "告警规则编译刷新", Group: "alert",
		Description: "周期性刷新告警规则编译缓存",
		ScheduleKind: jobreg.ScheduleInterval, DefaultSpec: "5m",
		Mutable: true, Triggerable: true, Pauseable: true, RiskLevel: jobreg.RiskLow,
		MisfirePolicy: jobreg.MisfireSkip,
	}, func(ctx context.Context) error {
		_, err := d.DB.RefreshAllAlertRuleCompiled()
		return err
	})

	r.Register(jobreg.JobDesc{
		Key: "cmdb.sync_scanner", Name: "CMDB 同步调度", Group: "scanner",
		Description: "按各同步任务 cron 表达式触发 CMDB 同步",
		ScheduleKind: jobreg.ScheduleInterval, DefaultSpec: "60s",
		Mutable: true, Triggerable: true, Pauseable: true, RiskLevel: jobreg.RiskMedium,
		MisfirePolicy: jobreg.MisfireSkip,
	}, func(ctx context.Context) error {
		if d.Cmdb == nil {
			d.Cmdb = cmdb.New(d.DB)
		}
		return d.Cmdb.ScanDueCron(ctx)
	})

	r.Register(jobreg.JobDesc{
		Key: "ticket.scheduled_exec", Name: "工单预约到期执行", Group: "ticket",
		Description: "扫描已通过且 scheduled_at 已到期的工单并后台自动执行",
		ScheduleKind: jobreg.ScheduleInterval, DefaultSpec: "30s",
		Mutable: true, Triggerable: true, Pauseable: true, RiskLevel: jobreg.RiskHigh,
		MisfirePolicy: jobreg.MisfireSkip,
	}, func(ctx context.Context) error {
		if d.Ticket == nil {
			return nil
		}
		return handler.RunDueScheduledIssuesOnce(ctx, d.Ticket)
	})
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}