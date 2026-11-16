package cloudsvc

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"auto-dba/internal/cloud"
	"auto-dba/internal/config"
	"auto-dba/internal/slowbus"
	"auto-dba/internal/store"
)

// SlowlogView 给页面和排障看消费积压、死信、发送失败和限频次数。
func (s *Service) SlowlogView() map[string]int64 {
	if s == nil {
		return nil
	}
	s.slowMu.Lock()
	bus := s.bus
	s.slowMu.Unlock()
	var lag, dlq, produceErr int64
	if bus != nil {
		lag, dlq, produceErr = bus.Stats()
	}
	return map[string]int64{
		"lag": lag, "dlq": dlq, "produce_errors": produceErr, "rate_limit": s.rateHits.Load(),
	}
}

// StartSlowlog 启动消费，并在配置变更后换总线。拉取由定时任务触发。
func (s *Service) StartSlowlog(ctx context.Context) {
	if s == nil {
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	s.slowMu.Lock()
	s.pullCancel = cancel
	s.slowMu.Unlock()
	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				cfg := config.Snapshot().SlowlogBus
				if strings.TrimSpace(cfg.Brokers) == "" {
					s.drainBus()
					continue
				}
				s.slowMu.Lock()
				s.drainSince = time.Time{}
				s.slowMu.Unlock()
				_, _ = s.ensureBus(cfg)
			}
		}
	}()
}

// StopSlowlog 先停新的拉取，等正在进行的窗口结束，最多 30 秒，然后刷出生产者并停消费。
func (s *Service) StopSlowlog() {
	if s == nil {
		return
	}
	s.slowMu.Lock()
	s.stopping = true
	cancel := s.pullCancel
	s.slowMu.Unlock()
	if cancel != nil {
		cancel()
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		s.slowMu.Lock()
		n := s.pulling
		s.slowMu.Unlock()
		if n == 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	s.closeBus()
}

// SlowlogAll 给定时任务用。别的副本正在拉时直接返回，不记失败。
func (s *Service) SlowlogAll(ctx context.Context, onlyCloudID string) error {
	_, err := s.runSlow(ctx, onlyCloudID)
	return err
}

func (s *Service) runSlow(ctx context.Context, only string) (int, error) {
	s.slowMu.Lock()
	if s.stopping {
		s.slowMu.Unlock()
		return 0, fmt.Errorf("服务正在停止")
	}
	s.pulling++
	s.slowMu.Unlock()
	defer func() {
		s.slowMu.Lock()
		s.pulling--
		s.slowMu.Unlock()
	}()
	release, ok, err := s.DB.TryAdvisoryLock(ctx, "cloud.pg.slowlog")
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, nil
	}
	defer release()

	cfg := config.Snapshot().SlowlogBus
	if strings.TrimSpace(cfg.Brokers) != "" {
		if err := slowbus.Probe(ctx, cfg.Brokers); err != nil {
			s.closeBus()
			s.noteKafkaDown(err)
			return 0, err
		}
	}
	roundCtx, stopRound := context.WithCancel(ctx)
	defer stopRound()
	halt := &slowHalt{stop: stopRound}

	accounts, err := s.DB.ListCloudAccounts("", true)
	if err != nil {
		return 0, err
	}
	var first error
	total := 0
	for i := range accounts {
		if !cloud.Supported(accounts[i].Vendor, cloud.EnginePostgres) {
			continue
		}
		if !s.tryAccount(accounts[i].ID + ":slow") {
			if first == nil {
				first = fmt.Errorf("排队中")
			}
			continue
		}
		n, err := s.slowAccount(roundCtx, &accounts[i], only, halt)
		s.doneAccount(accounts[i].ID + ":slow")
		total += n
		if err != nil && !errors.Is(err, cloud.ErrNoKey) && !errors.Is(err, context.Canceled) && first == nil {
			first = err
		}
		if roundCtx.Err() != nil {
			break
		}
	}
	s.purgeSlowDaily()
	return total, first
}

type slowHalt struct {
	once sync.Once
	stop context.CancelFunc
}

func (h *slowHalt) on(s *Service, err error) {
	if h == nil || !slowbus.IsDialTimeout(err) {
		return
	}
	h.once.Do(func() {
		s.closeBus()
		if h.stop != nil {
			h.stop()
		}
	})
}

func (s *Service) slowAccount(ctx context.Context, account *store.CloudAccount, only string, halt *slowHalt) (int, error) {
	reader, err := cloud.GetReader(account.Vendor, cloud.EnginePostgres)
	if err != nil {
		return 0, err
	}
	cred, err := s.Cred(account, cloud.RoleReadonly, "")
	if err != nil {
		return 0, err
	}
	list, err := s.DB.ListCloudResources(account.TenantID, cloud.EnginePostgres, "")
	if err != nil {
		return 0, err
	}
	now := time.Now()
	byRegion := map[string][]store.CloudInstance{}
	for _, inst := range list {
		if inst.AccountID != account.ID {
			continue
		}
		if only != "" && inst.CloudID != only && inst.InstanceID != only {
			continue
		}
		byRegion[inst.Region] = append(byRegion[inst.Region], inst)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var first error
	total := 0
	for region, items := range byRegion {
		sem := make(chan struct{}, 2)
		for _, inst := range items {
			wg.Add(1)
			go func(region string, inst store.CloudInstance) {
				defer wg.Done()
				select {
				case <-ctx.Done():
					return
				case sem <- struct{}{}:
				}
				defer func() { <-sem }()
				if ctx.Err() != nil {
					return
				}
				c := cred
				if region != "" {
					c.Region = region
				}
				n, err := s.slowOne(ctx, reader, c, account, inst, now)
				halt.on(s, err)
				mu.Lock()
				total += n
				if err != nil && !errors.Is(err, context.Canceled) && !slowCloudTimeout(err) && first == nil {
					first = err
				}
				mu.Unlock()
			}(region, inst)
		}
	}
	wg.Wait()
	return total, first
}

func (s *Service) slowOne(ctx context.Context, reader cloud.Reader, cred cloud.Cred, account *store.CloudAccount, inst store.CloudInstance, now time.Time) (int, error) {
	cur, err := s.DB.GetCloudCursor(account.Vendor, "postgres", account.ID, inst.CloudID, "slow_log")
	if err != nil {
		return 0, err
	}
	last := time.Time{}
	if cur != nil && strings.TrimSpace(cur.CursorAt) != "" {
		last, _ = time.Parse(time.RFC3339, cur.CursorAt)
	}
	if cur != nil && cur.Status == "empty" {
		n, e := s.DB.CountCloudSlowLogsSince(inst.CloudID, now.Add(-cloud.SlowLogAPILookback))
		if e == nil && n == 0 {
			last = time.Time{}
		}
	}
	plan := cloud.PlanSlowPull(last, now)
	row := &store.CloudCursor{
		TenantID: account.TenantID, Vendor: account.Vendor, Engine: "postgres", AccountID: account.ID,
		CloudID: inst.CloudID, DataType: "slow_log",
	}
	if cur != nil {
		row.CursorAt = cur.CursorAt
		row.FailCount = cur.FailCount
		row.LastCount = cur.LastCount
	}
	if plan.Gap {
		row.Status = "gap"
		row.GapFrom = plan.GapFrom.UTC().Format(time.RFC3339)
		row.GapTo = plan.GapTo.UTC().Format(time.RFC3339)
		row.Error = fmt.Sprintf("云上已无 %s 至 %s 的慢日志", row.GapFrom, row.GapTo)
		if err := s.DB.SaveCloudCursor(row); err != nil {
			return 0, err
		}
		s.upsertSlowAlert("slowlog-gap-"+inst.CloudID, "云慢日志有无法补回的缺口", row.Error, true)
	}
	if len(plan.Windows) == 0 {
		s.upsertSlowAlert("slowlog-lag-"+inst.CloudID, "云慢日志游标落后", "", false)
		return 0, nil
	}
	cfg := config.Snapshot().SlowlogBus
	useKafka := cfg.Allows(inst.CloudID)
	var publish func(context.Context, []slowbus.Message) error
	if useKafka {
		bus, err := s.ensureBus(cfg)
		if err != nil {
			return 0, s.failSlow(row, cur, err, true)
		}
		publish = bus.Publish
	}
	advanced := last
	if plan.Gap {
		advanced = plan.GapTo
	}
	resetDone := false
	resetIngested := func() {
		if resetDone {
			return
		}
		resetDone = true
		_ = s.DB.ResetSlowIngested(account.Vendor, "postgres", account.ID, inst.CloudID)
	}
	total := 0
	for _, seg := range plan.Windows {
		if err := ctx.Err(); err != nil {
			if errors.Is(err, context.Canceled) {
				return total, err
			}
			return total, s.failSlow(row, cur, err, useKafka)
		}
		n, err := s.pullSlow(ctx, reader, cred, account, inst, seg, useKafka, publish, resetIngested)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return total, err
			}
			var send kafkaSendError
			return total, s.failSlow(row, cur, err, errors.As(err, &send))
		}
		total += n
		advanced = cloud.AdvanceCursor(advanced, seg, true)
		row.CursorAt = advanced.UTC().Format(time.RFC3339)
		row.Status = "ok"
		if total == 0 {
			row.Status = "none"
		}
		row.Error = ""
		row.FailCount = 0
		at := time.Now().UTC().Format(time.RFC3339)
		row.ProducedAt = at
		if total > 0 {
			row.LastCount = total
			if !useKafka {
				row.IngestedAt = at
				row.IngestedCount = total
			}
		}
		if err := s.DB.SaveCloudCursor(row); err != nil {
			return total, err
		}
		if total > 0 && !useKafka {
			if err := s.DB.SetSlowIngested(account.Vendor, "postgres", account.ID, inst.CloudID, total, at); err != nil {
				return total, err
			}
		}
		cur = row
	}
	s.noteKafkaUp()
	behind := now.Add(-cloud.SlowSafetyLag).Sub(advanced)
	s.upsertSlowAlert("slowlog-lag-"+inst.CloudID, "云慢日志游标落后", advanced.UTC().Format(time.RFC3339), behind > 30*time.Minute)
	return total, nil
}

func (s *Service) pullSlow(ctx context.Context, reader cloud.Reader, cred cloud.Cred, account *store.CloudAccount, inst store.CloudInstance, seg cloud.Window, useKafka bool, publish func(context.Context, []slowbus.Message) error, resetIngested func()) (int, error) {
	offset := 0
	n := 0
	for {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		if err := slowAPI.Wait(ctx, account.ID+"|"+inst.Region); err != nil {
			return n, err
		}
		var page []cloud.SlowLog
		var total int
		err := slowRetry(ctx, func() error {
			var e error
			page, total, e = reader.ListSlowLogs(ctx, cred, inst.CloudID, seg.From, seg.To, offset, 100)
			return e
		})
		if err != nil {
			if cloud.IsRateLimit(err) {
				s.rateHits.Add(1)
			}
			return n, err
		}
		if len(page) == 0 && total > offset {
			return n, fmt.Errorf("慢日志返回 %d 条但没有明细", total)
		}
		msgs := make([]slowbus.Message, 0, len(page))
		direct := make([]store.CloudSlowLog, 0, len(page))
		for _, row := range page {
			if row.Time.IsZero() {
				log.Printf("slowlog skip unparsed time cloud=%s", inst.CloudID)
				continue
			}
			if row.Time.Before(seg.From) || row.Time.After(seg.To) {
				continue
			}
			raw := row.Raw
			if raw == "" {
				raw = row.SQL
			}
			msg := slowbus.Message{
				Region: inst.Region, AccountID: account.ID, TenantID: account.TenantID, Vendor: account.Vendor,
				CloudID: inst.CloudID, WindowFrom: seg.From.UTC().Format(time.RFC3339), WindowTo: seg.To.UTC().Format(time.RFC3339),
				LogTime: row.Time.UTC().Format(time.RFC3339Nano), DBName: row.DBName, DBUser: row.User, Client: row.Client,
				Duration: row.DurationMS, SQL: row.SQL, Fingerprint: cloud.Fingerprint(row.SQL), Raw: raw,
				Dedupe: slowbus.DedupeKey(raw, row.SQL),
			}
			if useKafka {
				msgs = append(msgs, msg)
			} else {
				prepared := slowbus.Prepare(msg)
				direct = append(direct, store.CloudSlowLog{
					TenantID: prepared.TenantID, Vendor: prepared.Vendor, Engine: "postgres", AccountID: prepared.AccountID,
					CloudID: prepared.CloudID, LogTime: prepared.LogTime, DBName: prepared.DBName, DBUser: prepared.DBUser,
					Client: prepared.Client, Duration: prepared.Duration, SQL: prepared.SQL, Fingerprint: prepared.Fingerprint,
					Raw: prepared.Raw, Dedupe: prepared.Dedupe,
				})
			}
			n++
		}
		if useKafka {
			if len(msgs) > 0 && resetIngested != nil {
				resetIngested()
			}
			if err := publish(ctx, msgs); err != nil {
				return n, kafkaSendError{err}
			}
		} else if len(direct) > 0 {
			if resetIngested != nil {
				resetIngested()
			}
			if err := s.DB.InsertCloudSlowLogs(direct); err != nil {
				return n, err
			}
		}
		offset += len(page)
		if len(page) == 0 || offset >= total {
			break
		}
	}
	return n, nil
}

var slowAPI = cloud.NewLimiter(func() int { return cloud.SlowAPIQPS })

type kafkaSendError struct{ error }

func (e kafkaSendError) Unwrap() error { return e.error }

func slowCloudTimeout(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Client.Timeout") || strings.Contains(msg, "while reading body")
}

func slowRetry(ctx context.Context, fn func() error) error {
	delay := time.Second
	var err error
	timeouts := 0
	for i := 0; i < 3; i++ {
		err = fn()
		if err == nil {
			return nil
		}
		retry := cloud.IsRateLimit(err)
		if slowCloudTimeout(err) {
			timeouts++
			retry = timeouts <= 1
		}
		if !retry || i == 2 {
			return err
		}
		t := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
		delay *= 2
	}
	return err
}

func (s *Service) failSlow(row *store.CloudCursor, cur *store.CloudCursor, err error, kafka bool) error {
	row.Status = "error"
	row.Error = err.Error()
	if cur != nil {
		row.FailCount = cur.FailCount + 1
		if row.CursorAt == "" {
			row.CursorAt = cur.CursorAt
		}
	}
	_ = s.DB.SaveCloudCursor(row)
	if kafka {
		s.noteKafkaDown(err)
	}
	return err
}

func (s *Service) noteKafkaDown(err error) {
	s.slowMu.Lock()
	if s.kafkaDown.IsZero() {
		s.kafkaDown = time.Now()
	}
	since := s.kafkaDown
	s.slowMu.Unlock()
	if time.Since(since) >= 30*time.Minute {
		s.upsertSlowAlert("slowlog-kafka", "云慢日志 Kafka 不可用", err.Error(), true)
	}
}

func (s *Service) noteKafkaUp() {
	s.slowMu.Lock()
	s.kafkaDown = time.Time{}
	s.slowMu.Unlock()
	s.upsertSlowAlert("slowlog-kafka", "云慢日志 Kafka 不可用", "", false)
}

func (s *Service) upsertSlowAlert(id, title, detail string, firing bool) {
	status := store.AlertStatusResolved
	handle := store.AlertHandleResolved
	if firing {
		status = store.AlertStatusFiring
		handle = store.AlertHandleUnclaimed
	}
	a := &store.Alert{
		ID: id, Fingerprint: id, Severity: store.AlertSeverityP2, Title: title, Service: "cloud",
		Status: status, Handle: handle,
		Annotations: map[string]string{"error": detail},
	}
	if firing {
		a.StartedAt = time.Now().UTC().Format(time.RFC3339)
	} else {
		a.ResolvedAt = time.Now().UTC().Format(time.RFC3339)
	}
	_ = s.DB.UpsertAlert(a)
}

func (s *Service) ensureBus(cfg config.SlowlogBusConfig) (*slowbus.Bus, error) {
	key := cfg.Key()
	s.busOpen.Lock()
	defer s.busOpen.Unlock()
	s.slowMu.Lock()
	if s.bus != nil && s.busKey == key {
		bus := s.bus
		s.slowMu.Unlock()
		return bus, nil
	}
	old := s.bus
	s.bus = nil
	s.busKey = ""
	s.slowMu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	bus, err := slowbus.Open(cfg)
	if err != nil {
		s.noteKafkaDown(err)
		return nil, err
	}
	bus.SetAlerter(func(kind, detail string, firing bool) {
		title := "云慢日志消费积压"
		id := "slowlog-backlog"
		if kind == "dlq" {
			title = "云慢日志死信"
			id = "slowlog-dlq"
		}
		s.upsertSlowAlert(id, title, detail, firing)
	})
	s.slowMu.Lock()
	s.bus = bus
	s.busKey = key
	s.slowMu.Unlock()
	go bus.Run(context.Background(), s.insertSlowMessages)
	return bus, nil
}

// drainBus 在地址被清空后继续把已经进 Kafka 的消息写完，积压回到 0 并保持两分钟再停。
func (s *Service) drainBus() {
	s.slowMu.Lock()
	bus := s.bus
	if bus == nil {
		s.drainSince = time.Time{}
		s.slowMu.Unlock()
		return
	}
	lag, _, _ := bus.Stats()
	if lag > 0 {
		s.drainSince = time.Time{}
		s.slowMu.Unlock()
		return
	}
	if s.drainSince.IsZero() {
		s.drainSince = time.Now()
	}
	ready := time.Since(s.drainSince) >= 2*time.Minute
	s.slowMu.Unlock()
	if ready {
		s.closeBus()
	}
}

func (s *Service) closeBus() {
	s.busOpen.Lock()
	defer s.busOpen.Unlock()
	s.slowMu.Lock()
	bus := s.bus
	s.bus = nil
	s.busKey = ""
	s.drainSince = time.Time{}
	s.slowMu.Unlock()
	if bus != nil {
		_ = bus.Close()
	}
}

func (s *Service) insertSlowMessages(ctx context.Context, msgs []slowbus.Message) error {
	rows := make([]store.CloudSlowLog, 0, len(msgs))
	seen := map[string]slowbus.Message{}
	for _, m := range msgs {
		rows = append(rows, store.CloudSlowLog{
			TenantID: m.TenantID, Vendor: m.Vendor, Engine: "postgres", AccountID: m.AccountID,
			CloudID: m.CloudID, LogTime: m.LogTime, DBName: m.DBName, DBUser: m.DBUser, Client: m.Client,
			Duration: m.Duration, SQL: m.SQL, Fingerprint: m.Fingerprint, Raw: m.Raw, Dedupe: m.Dedupe,
		})
		seen[m.Vendor+"\x00"+m.AccountID+"\x00"+m.CloudID] = m
	}
	if err := s.DB.InsertCloudSlowLogs(rows); err != nil {
		return err
	}
	at := time.Now().UTC().Format(time.RFC3339)
	counts := map[string]int{}
	for _, m := range msgs {
		key := m.Vendor + "\x00" + m.AccountID + "\x00" + m.CloudID
		counts[key]++
		seen[key] = m
	}
	for key, m := range seen {
		_ = s.DB.AddSlowIngested(m.Vendor, "postgres", m.AccountID, m.CloudID, counts[key], at)
	}
	return nil
}

func (s *Service) purgeSlowDaily() {
	s.slowMu.Lock()
	if !s.lastPurge.IsZero() && time.Since(s.lastPurge) < 24*time.Hour {
		s.slowMu.Unlock()
		return
	}
	s.lastPurge = time.Now()
	s.slowMu.Unlock()
	raw, _ := s.DB.GetGlobalArg(store.ArgCloudSlowlogRetention)
	days := atoi(raw)
	if days <= 0 {
		days = 15
	}
	before := time.Now().AddDate(0, 0, -days).UTC().Format(time.RFC3339)
	for {
		n, err := s.DB.PurgeCloudSlowLogsBatch(before, 5000)
		if err != nil {
			log.Printf("slowlog purge: %v", err)
			return
		}
		if n == 0 {
			return
		}
	}
}
