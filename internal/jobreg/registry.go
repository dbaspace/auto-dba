package jobreg

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"auto-dba/internal/store"
)

type registered struct {
	desc   JobDesc
	runner Runner
}

// Registry 平台定时任务注册中心。
type Registry struct {
	Store     *store.DB
	OwnerID   string
	OwnerHost string
	OwnerPID  int
	LeaseTTL  time.Duration
	Heartbeat time.Duration

	mu   sync.RWMutex
	jobs map[string]*registered
}

func New(db *store.DB) *Registry {
	host, _ := os.Hostname()
	if host == "" {
		host = "unknown"
	}
	return &Registry{
		Store:     db,
		OwnerID:   uuid.NewString(),
		OwnerHost: host,
		OwnerPID:  os.Getpid(),
		LeaseTTL:  DefaultLeaseTTL,
		Heartbeat: DefaultHeartbeat,
		jobs:      map[string]*registered{},
	}
}

func (r *Registry) Register(desc JobDesc, runner Runner) {
	if r == nil || desc.Key == "" || runner == nil {
		return
	}
	if desc.MisfirePolicy == "" {
		if desc.ScheduleKind == ScheduleCron6 {
			desc.MisfirePolicy = MisfireCatchUpOnce
		} else {
			desc.MisfirePolicy = MisfireSkip
		}
	}
	if desc.MisfireGrace <= 0 {
		desc.MisfireGrace = 24 * time.Hour
	}
	if desc.RiskLevel == "" {
		desc.RiskLevel = RiskLow
	}
	r.mu.Lock()
	r.jobs[desc.Key] = &registered{desc: desc, runner: runner}
	r.mu.Unlock()

	enabled := true
	spec := desc.DefaultSpec
	if r.Store != nil {
		if desc.SyncEnabledArg != "" {
			if v, err := r.Store.GetGlobalArg(desc.SyncEnabledArg); err == nil && strings.TrimSpace(v) != "" {
				enabled = parseBoolArg(v)
			}
		}
		if desc.SyncGlobalArg != "" {
			if v, err := r.Store.GetGlobalArg(desc.SyncGlobalArg); err == nil && strings.TrimSpace(v) != "" {
				spec = strings.TrimSpace(v)
			}
		}
		_ = r.Store.EnsureJobSetting(desc.Key, spec, enabled)
	}
}

func parseBoolArg(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "on", "yes":
		return true
	default:
		return false
	}
}

func (r *Registry) get(key string) *registered {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.jobs[key]
}

func (r *Registry) Keys() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.jobs))
	for k := range r.jobs {
		out = append(out, k)
	}
	return out
}

func (r *Registry) ttlSec() int {
	d := r.LeaseTTL
	if d <= 0 {
		d = DefaultLeaseTTL
	}
	return int(d.Seconds())
}

// Start 启动所有已注册任务的轮询循环，并做 stuck 恢复。
func (r *Registry) Start(ctx context.Context) {
	if r == nil || r.Store == nil {
		return
	}
	if n, err := r.Store.RecoverStuckJobRuns(2 * time.Minute); err != nil {
		log.Printf("jobreg: recover stuck: %v", err)
	} else if n > 0 {
		log.Printf("jobreg: recovered %d stuck run(s)", n)
	}
	r.mu.RLock()
	list := make([]*registered, 0, len(r.jobs))
	for _, j := range r.jobs {
		list = append(list, j)
	}
	r.mu.RUnlock()
	for _, j := range list {
		j := j
		go r.loopJob(ctx, j)
	}
	go r.maintenanceLoop(ctx)
	log.Printf("jobreg: started owner=%s host=%s jobs=%d", r.OwnerID, r.OwnerHost, len(list))
}

func (r *Registry) maintenanceLoop(ctx context.Context) {
	t := time.NewTicker(6 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := r.Store.PurgeOldJobFireLedger(time.Now().AddDate(0, 0, -7)); err != nil {
				log.Printf("jobreg: purge ledger: %v", err)
			}
			if n, err := r.Store.RecoverStuckJobRuns(2 * time.Minute); err == nil && n > 0 {
				log.Printf("jobreg: recovered %d stuck run(s)", n)
			}
		}
	}
}

func (r *Registry) loopJob(ctx context.Context, j *registered) {
	// 启动抖动，降低齐刷
	select {
	case <-ctx.Done():
		return
	case <-time.After(time.Duration(rand.Intn(2000)) * time.Millisecond):
	}
	// misfire catch-up once at start
	r.maybeMisfire(ctx, j)

	poll := DefaultPollInterval
	if j.desc.ScheduleKind == ScheduleInterval {
		if d, err := parseDurationSpec(j.desc.DefaultSpec); err == nil && d < poll {
			poll = d
			if poll < time.Second {
				poll = time.Second
			}
		}
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.tickOnce(ctx, j)
		}
	}
}

func (r *Registry) resolveSpec(j *registered) (enabled bool, spec string) {
	enabled = true
	spec = j.desc.DefaultSpec
	st, err := r.Store.GetJobSetting(j.desc.Key)
	if err == nil && st != nil {
		enabled = st.Enabled
		if strings.TrimSpace(st.ScheduleSpec) != "" {
			spec = strings.TrimSpace(st.ScheduleSpec)
		}
	}
	return enabled, spec
}

func (r *Registry) tickOnce(ctx context.Context, j *registered) {
	enabled, spec := r.resolveSpec(j)
	if !enabled {
		return
	}
	now := time.Now()
	fireKey, due := dueNow(j.desc.ScheduleKind, spec, now)
	if !due {
		return
	}
	if has, err := r.Store.HasJobFireLedger(j.desc.Key, fireKey); err == nil && has {
		return
	}
	_, _ = r.TryFire(ctx, j.desc.Key, TriggerCron, fireKey)
}

func (r *Registry) maybeMisfire(ctx context.Context, j *registered) {
	if j.desc.MisfirePolicy != MisfireCatchUpOnce {
		return
	}
	enabled, spec := r.resolveSpec(j)
	if !enabled {
		return
	}
	fk, ok := missedFireKey(j.desc.ScheduleKind, spec, time.Now(), j.desc.MisfireGrace)
	if !ok {
		return
	}
	has, err := r.Store.HasJobFireLedger(j.desc.Key, fk)
	if err != nil || has {
		return
	}
	log.Printf("jobreg: misfire catch-up job=%s fire_key=%s", j.desc.Key, fk)
	_, _ = r.TryFire(ctx, j.desc.Key, TriggerMisfire, fk)
}

// TryFire 统一触发流水线：lease → ledger → run → release。
func (r *Registry) TryFire(ctx context.Context, key, trigger, fireKey string) (*TriggerResult, error) {
	j := r.get(key)
	if j == nil {
		return nil, fmt.Errorf("unknown job %s", key)
	}
	enabled, spec := r.resolveSpec(j)
	if !enabled && trigger != TriggerManual {
		return &TriggerResult{Skipped: true, Reason: "disabled"}, nil
	}
	if fireKey == "" {
		fk, due := dueNow(j.desc.ScheduleKind, spec, time.Now())
		if !due && trigger == TriggerCron {
			return &TriggerResult{Skipped: true, Reason: "not_due"}, nil
		}
		if fk == "" {
			// manual：用时间戳火次，允许随时跑
			fireKey = "m:" + time.Now().UTC().Format("20060102150405.000")
		} else {
			fireKey = fk
		}
	}
	if trigger == TriggerManual {
		fireKey = "m:" + time.Now().UTC().Format("20060102150405.000")
	}

	token, ok, err := r.Store.AcquireJobLease(key, r.OwnerID, r.OwnerHost, r.OwnerPID, r.ttlSec())
	if err != nil {
		return nil, err
	}
	if !ok {
		lease, _ := r.Store.GetJobLease(key)
		res := &TriggerResult{Skipped: true, Reason: "lease_held", Conflict: true}
		if lease != nil {
			res.Holder = lease.OwnerHost
			if lease.OwnerHost == "" {
				res.Holder = lease.OwnerID
			}
			res.Expires = lease.ExpiresAt
		}
		return res, nil
	}
	defer func() { _ = r.Store.ReleaseJobLease(key, r.OwnerID, token) }()

	runID := uuid.NewString()
	inserted, err := r.Store.InsertJobFireLedger(key, fireKey, token, runID)
	if err != nil {
		return nil, err
	}
	if !inserted && trigger != TriggerManual {
		return &TriggerResult{Skipped: true, Reason: "already_fired", FireKey: fireKey}, nil
	}
	if !inserted && trigger == TriggerManual {
		// manual 仍允许跑，但换 fire_key
		fireKey = "m:" + uuid.NewString()
		inserted, err = r.Store.InsertJobFireLedger(key, fireKey, token, runID)
		if err != nil {
			return nil, err
		}
		if !inserted {
			return &TriggerResult{Skipped: true, Reason: "ledger_conflict"}, nil
		}
	}

	started := time.Now()
	run := &store.JobRun{
		ID: runID, JobKey: key, Trigger: trigger, Status: "running",
		FireKey: fireKey, OwnerID: r.OwnerID, OwnerHost: r.OwnerHost,
		FencingToken: token, StartedAt: started.UTC().Format(time.RFC3339),
	}
	if err := r.Store.InsertJobRun(run); err != nil {
		return nil, err
	}
	_ = r.Store.BindJobLeaseRun(key, r.OwnerID, token, runID)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	hbDone := make(chan struct{})
	go func() {
		defer close(hbDone)
		t := time.NewTicker(r.Heartbeat)
		defer t.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-t.C:
				ok, err := r.Store.HeartbeatJobLease(key, r.OwnerID, token, r.ttlSec())
				if err != nil || !ok {
					log.Printf("jobreg: fencing lost job=%s: ok=%v err=%v", key, ok, err)
					cancel()
					return
				}
			}
		}
	}()

	var runErr error
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				runErr = fmt.Errorf("panic: %v", rec)
			}
		}()
		runErr = j.runner(runCtx)
	}()
	cancel()
	<-hbDone

	status := "success"
	errMsg := ""
	if runErr != nil {
		status = "failed"
		errMsg = runErr.Error()
		log.Printf("jobreg: job=%s failed: %v", key, runErr)
		// 失败可观测：管理页 24h 失败计数；后续可接 notify 邮件/飞书
		log.Printf("jobreg_notify: event=job_failed job=%s run_id=%s err=%s", key, runID, errMsg)
	}
	dur := time.Since(started).Milliseconds()
	_ = r.Store.FinishJobRun(runID, status, errMsg, dur)
	return &TriggerResult{RunID: runID, FireKey: fireKey}, nil
}

func (r *Registry) UpdateSetting(key string, enabled *bool, scheduleSpec *string, updatedBy string) (*store.JobSetting, error) {
	j := r.get(key)
	if j == nil {
		return nil, fmt.Errorf("unknown job %s", key)
	}
	cur, err := r.Store.GetJobSetting(key)
	if err != nil {
		return nil, err
	}
	en := true
	spec := j.desc.DefaultSpec
	if cur != nil {
		en = cur.Enabled
		if cur.ScheduleSpec != "" {
			spec = cur.ScheduleSpec
		}
	}
	if enabled != nil {
		if !j.desc.Pauseable && !*enabled {
			return nil, fmt.Errorf("job %s cannot be paused", key)
		}
		en = *enabled
	}
	if scheduleSpec != nil {
		if !j.desc.Mutable {
			return nil, fmt.Errorf("job %s schedule is not mutable", key)
		}
		spec = strings.TrimSpace(*scheduleSpec)
		if j.desc.ScheduleKind == ScheduleInterval {
			if _, err := parseDurationSpec(spec); err != nil {
				return nil, err
			}
		}
		if j.desc.ScheduleKind == ScheduleCron6 {
			if _, _, ok := parseCron6HMS(spec); !ok {
				return nil, fmt.Errorf("invalid cron6 spec")
			}
		}
	}
	st, err := r.Store.UpsertJobSetting(key, en, spec, updatedBy)
	if err != nil {
		return nil, err
	}
	if j.desc.SyncGlobalArg != "" {
		_ = r.Store.SetGlobalArg(j.desc.SyncGlobalArg, spec, "")
	}
	if j.desc.SyncEnabledArg != "" {
		v := "false"
		if en {
			v = "true"
		}
		_ = r.Store.SetGlobalArg(j.desc.SyncEnabledArg, v, "")
	}
	return st, nil
}

func (r *Registry) ListViews() ([]JobView, error) {
	r.mu.RLock()
	list := make([]*registered, 0, len(r.jobs))
	for _, j := range r.jobs {
		list = append(list, j)
	}
	r.mu.RUnlock()
	since := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)
	out := make([]JobView, 0, len(list))
	for _, j := range list {
		v, err := r.buildView(j, since)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, nil
}

func (r *Registry) GetView(key string) (*JobView, error) {
	j := r.get(key)
	if j == nil {
		return nil, fmt.Errorf("unknown job %s", key)
	}
	since := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)
	return r.buildView(j, since)
}

func (r *Registry) buildView(j *registered, since string) (*JobView, error) {
	enabled, spec := r.resolveSpec(j)
	st, _ := r.Store.GetJobSetting(j.desc.Key)
	ver := int64(0)
	if st != nil {
		ver = st.Version
	}
	v := &JobView{
		Key: j.desc.Key, Name: j.desc.Name, Group: j.desc.Group,
		Description: j.desc.Description, ScheduleKind: j.desc.ScheduleKind,
		ScheduleSpec: spec, ScheduleHuman: humanSchedule(j.desc.ScheduleKind, spec),
		Enabled: enabled, Mutable: j.desc.Mutable, Triggerable: j.desc.Triggerable,
		Pauseable: j.desc.Pauseable, RiskLevel: j.desc.RiskLevel,
		MisfirePolicy: j.desc.MisfirePolicy, Version: ver,
	}
	if nf := nextFireAt(j.desc.ScheduleKind, spec, time.Now()); !nf.IsZero() {
		v.NextFireAt = nf.UTC().Format(time.RFC3339)
	}
	if last, err := r.Store.LastJobRun(j.desc.Key); err == nil && last != nil {
		v.LastStatus = last.Status
		v.LastStartedAt = last.StartedAt
		v.LastEndedAt = last.EndedAt
		v.LastError = last.Error
		v.LastDurationMs = last.DurationMs
	}
	if n, err := r.Store.CountJobRunsSince(j.desc.Key, since, ""); err == nil {
		v.RunCount24h = n
	}
	if n, err := r.Store.CountJobRunsSince(j.desc.Key, since, "failed"); err == nil {
		v.FailCount24h = n
	}
	if lease, err := r.Store.GetJobLease(j.desc.Key); err == nil && lease != nil {
		v.LeaseOwnerID = lease.OwnerID
		v.LeaseOwnerHost = lease.OwnerHost
		v.LeaseExpiresAt = lease.ExpiresAt
		v.LeaseHeld = lease.Held
		v.FencingToken = lease.FencingToken
		v.LocalOwner = lease.Held && lease.OwnerID == r.OwnerID
	}
	return v, nil
}
