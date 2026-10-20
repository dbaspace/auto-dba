package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const cdcTaskCols = `id, name, task_type, status, COALESCE(source_instance_id,''), COALESCE(target_instance_id,''), COALESCE(source_db,''), COALESCE(target_db,''), COALESCE(parent_id,''), COALESCE(approval_status,''), COALESCE(issue_id,''), COALESCE(description,''), created_at, updated_at`

func (d *DB) CreateCDC(t *CDCTask) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	if t.Status == "" {
		t.Status = StatusInit
	}
	t.CreatedAt, t.UpdatedAt = now, now
	_, err := d.Exec(
		`INSERT INTO tbl_cdc_tasks(id, name, task_type, status, source_instance_id, target_instance_id, source_db, target_db, parent_id, approval_status, issue_id, description, created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.Name, t.TaskType, t.Status, t.SourceID, t.TargetID, t.SourceDB, t.TargetDB, t.ParentID, t.ApprovalStatus, t.IssueID, t.Description, t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return err
	}
	if t.TenantID == "" {
		t.TenantID = d.inheritTenantFromInstance(t.SourceID)
	}
	return d.AssignTenantID("tbl_cdc_tasks", t.ID, t.TenantID)
}

func (d *DB) GetCDCByIssueID(issueID string) (*CDCTask, error) {
	list, err := d.ListCDCByIssueID(issueID)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	t := list[0]
	return &t, nil
}

func (d *DB) ListCDCByIssueID(issueID string) ([]CDCTask, error) {
	if strings.TrimSpace(issueID) == "" {
		return nil, nil
	}
	rows, err := d.Query(
		`SELECT `+cdcTaskCols+` FROM tbl_cdc_tasks WHERE issue_id=? ORDER BY created_at ASC`, issueID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CDCTask
	for rows.Next() {
		var t CDCTask
		if err := rows.Scan(&t.ID, &t.Name, &t.TaskType, &t.Status, &t.SourceID, &t.TargetID, &t.SourceDB, &t.TargetDB, &t.ParentID, &t.ApprovalStatus, &t.IssueID, &t.Description, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (d *DB) GetCDC(id string) (*CDCTask, error) {
	t := &CDCTask{}
	err := d.QueryRow(
		`SELECT `+cdcTaskCols+` FROM tbl_cdc_tasks WHERE id=?`, id,
	).Scan(&t.ID, &t.Name, &t.TaskType, &t.Status, &t.SourceID, &t.TargetID, &t.SourceDB, &t.TargetDB, &t.ParentID, &t.ApprovalStatus, &t.IssueID, &t.Description, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

func (d *DB) ListCDC(status, taskType string) ([]CDCTask, error) {
	return d.ListCDCInTenant("", status, taskType)
}

func (d *DB) ListCDCInTenant(tenantID, status, taskType string) ([]CDCTask, error) {
	q := `SELECT ` + cdcTaskCols + ` FROM tbl_cdc_tasks WHERE 1=1`
	var args []any
	q, args = appendTenantFilter(q, args, tenantID)
	if status != "" {
		q += ` AND status=?`
		args = append(args, status)
	}
	if taskType != "" {
		q += ` AND task_type=?`
		args = append(args, taskType)
	}
	q += ` ORDER BY created_at DESC`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CDCTask
	for rows.Next() {
		var t CDCTask
		if err := rows.Scan(&t.ID, &t.Name, &t.TaskType, &t.Status, &t.SourceID, &t.TargetID, &t.SourceDB, &t.TargetDB, &t.ParentID, &t.ApprovalStatus, &t.IssueID, &t.Description, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (d *DB) UpdateCDCStatus(id, status, approval string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`UPDATE tbl_cdc_tasks SET status=?, approval_status=?, updated_at=? WHERE id=?`, status, approval, now, id)
	return err
}

func (d *DB) DeleteCDC(id string) error {
	_ = d.DeleteCDCLogsByTask(id)
	_, err := d.Exec(`DELETE FROM tbl_cdc_tasks WHERE id=?`, id)
	return err
}

func (d *DB) ListSlots() ([]SyncSlot, error) {
	return d.ListSlotsInTenant("")
}

func (d *DB) ListSlotsInTenant(tenantID string) ([]SyncSlot, error) {
	q := `SELECT id, host, port, slot_name, database_name, active, COALESCE(active_pid,0), restart_lsn, COALESCE(confirmed_flush_lsn,''), COALESCE(lag_bytes,0), plugin, updated_at FROM tbl_sync_slots WHERE 1=1`
	args := []any{}
	q, args = appendTenantFilter(q, args, tenantID)
	q += ` ORDER BY host, slot_name`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SyncSlot
	for rows.Next() {
		var s SyncSlot
		var active int
		if err := rows.Scan(&s.ID, &s.Host, &s.Port, &s.SlotName, &s.Database, &active, &s.ActivePID, &s.RestartLSN, &s.ConfirmedFlushLSN, &s.LagBytes, &s.Plugin, &s.UpdatedAt); err != nil {
			return nil, err
		}
		s.Active = active == 1
		out = append(out, s)
	}
	return out, rows.Err()
}

// UpsertSlotMeta 只登记槽身份。已有 pid、LSN、滞后时不覆盖，避免建任务把采集结果清掉。
func (d *DB) UpsertSlotMeta(s *SyncSlot) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if s.ID == "" {
		s.ID = uuid.NewString()
	}
	active := 0
	if s.Active {
		active = 1
	}
	var exists string
	err := d.QueryRow(`SELECT id FROM tbl_sync_slots WHERE host=? AND port=? AND slot_name=?`, s.Host, s.Port, s.SlotName).Scan(&exists)
	if err == nil && exists != "" {
		_, err = d.Exec(`UPDATE tbl_sync_slots SET database_name=?, active=?, plugin=?, updated_at=? WHERE id=?`,
			s.Database, active, s.Plugin, now, exists)
		return err
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = d.Exec(
		`INSERT INTO tbl_sync_slots(id, host, port, slot_name, database_name, active, active_pid, restart_lsn, confirmed_flush_lsn, lag_bytes, plugin, updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		s.ID, s.Host, s.Port, s.SlotName, s.Database, active, s.ActivePID, s.RestartLSN, s.ConfirmedFlushLSN, s.LagBytes, s.Plugin, now,
	)
	if err != nil {
		return err
	}
	if tid := d.inheritTenantFromHostPort(s.Host, s.Port); tid != "" {
		return d.AssignTenantID("tbl_sync_slots", s.ID, tid)
	}
	return nil
}

func (d *DB) UpsertSlot(s *SyncSlot) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if s.ID == "" {
		s.ID = uuid.NewString()
	}
	active := 0
	if s.Active {
		active = 1
	}
	var exists string
	err := d.QueryRow(`SELECT id FROM tbl_sync_slots WHERE host=? AND port=? AND slot_name=?`, s.Host, s.Port, s.SlotName).Scan(&exists)
	if err == nil && exists != "" {
		_, err = d.Exec(`UPDATE tbl_sync_slots SET database_name=?, active=?, active_pid=?, restart_lsn=?, confirmed_flush_lsn=?, lag_bytes=?, plugin=?, updated_at=? WHERE id=?`,
			s.Database, active, s.ActivePID, s.RestartLSN, s.ConfirmedFlushLSN, s.LagBytes, s.Plugin, now, exists)
		return err
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = d.Exec(
		`INSERT INTO tbl_sync_slots(id, host, port, slot_name, database_name, active, active_pid, restart_lsn, confirmed_flush_lsn, lag_bytes, plugin, updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		s.ID, s.Host, s.Port, s.SlotName, s.Database, active, s.ActivePID, s.RestartLSN, s.ConfirmedFlushLSN, s.LagBytes, s.Plugin, now,
	)
	if err != nil {
		return err
	}
	if tid := d.inheritTenantFromHostPort(s.Host, s.Port); tid != "" {
		return d.AssignTenantID("tbl_sync_slots", s.ID, tid)
	}
	return nil
}

func (d *DB) DeleteSlot(id string) error {
	_, err := d.Exec(`DELETE FROM tbl_sync_slots WHERE id=?`, id)
	return err
}

func (d *DB) GetSyncSlot(id string) (*SyncSlot, error) {
	s := &SyncSlot{}
	var active int
	err := d.QueryRow(`SELECT id, host, port, slot_name, database_name, active, COALESCE(active_pid,0), restart_lsn, COALESCE(confirmed_flush_lsn,''), COALESCE(lag_bytes,0), plugin, updated_at FROM tbl_sync_slots WHERE id=?`, id).
		Scan(&s.ID, &s.Host, &s.Port, &s.SlotName, &s.Database, &active, &s.ActivePID, &s.RestartLSN, &s.ConfirmedFlushLSN, &s.LagBytes, &s.Plugin, &s.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.Active = active == 1
	return s, nil
}

func (d *DB) DeleteSlotsByName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	_, err := d.Exec(`DELETE FROM tbl_sync_slots WHERE slot_name=?`, name)
	return err
}

// ReplaceHostSlots 用本次采集结果更新同一实例主机（含域名/多地址）上的槽：
// 按 (host|alias, port, slot_name) 原地 UPDATE 保留 id；仅删除本次未再出现的槽。
func (d *DB) ReplaceHostSlots(displayHost string, port int, aliases []string, slots []SyncSlot) error {
	hosts := make([]string, 0, 1+len(aliases))
	seenHost := map[string]struct{}{}
	addHost := func(h string) {
		h = strings.TrimSpace(h)
		if h == "" {
			return
		}
		key := strings.ToLower(h)
		if _, ok := seenHost[key]; ok {
			return
		}
		seenHost[key] = struct{}{}
		hosts = append(hosts, h)
	}
	addHost(displayHost)
	for _, h := range aliases {
		addHost(h)
	}
	if len(hosts) == 0 {
		return nil
	}

	keep := map[string]struct{}{}
	now := time.Now().UTC().Format(time.RFC3339)
	for i := range slots {
		s := &slots[i]
		s.Host = displayHost
		s.Port = port
		name := strings.TrimSpace(s.SlotName)
		if name == "" {
			continue
		}
		keep[strings.ToLower(name)] = struct{}{}
		active := 0
		if s.Active {
			active = 1
		}
		existID := d.findSlotIDOnHosts(hosts, port, name)
		if existID != "" {
			_, err := d.Exec(`UPDATE tbl_sync_slots SET host=?, port=?, database_name=?, active=?, active_pid=?, restart_lsn=?, confirmed_flush_lsn=?, lag_bytes=?, plugin=?, updated_at=? WHERE id=?`,
				displayHost, port, s.Database, active, s.ActivePID, s.RestartLSN, s.ConfirmedFlushLSN, s.LagBytes, s.Plugin, now, existID)
			if err != nil {
				return err
			}
			s.ID = existID
			continue
		}
		s.ID = ""
		if err := d.UpsertSlot(s); err != nil {
			return err
		}
	}

	// 删除同实例主机上本次未采集到的槽
	for _, h := range hosts {
		rows, err := d.Query(`SELECT id, slot_name FROM tbl_sync_slots WHERE host=? AND port=?`, h, port)
		if err != nil {
			return err
		}
		var drop []string
		for rows.Next() {
			var id, sn string
			if err := rows.Scan(&id, &sn); err != nil {
				rows.Close()
				return err
			}
			if _, ok := keep[strings.ToLower(strings.TrimSpace(sn))]; !ok {
				drop = append(drop, id)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, id := range drop {
			if err := d.DeleteSlot(id); err != nil {
				return err
			}
		}
	}
	return nil
}

func (d *DB) findSlotIDOnHosts(hosts []string, port int, slotName string) string {
	slotName = strings.TrimSpace(slotName)
	if slotName == "" || port <= 0 {
		return ""
	}
	for _, h := range hosts {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		var id string
		err := d.QueryRow(`SELECT id FROM tbl_sync_slots WHERE host=? AND port=? AND slot_name=?`, h, port, slotName).Scan(&id)
		if err == nil && strings.TrimSpace(id) != "" {
			return id
		}
	}
	return ""
}

// GetSyncSlotByKey 按主机+端口+槽名取槽（稳定键，不受采集换 id 影响）。
func (d *DB) GetSyncSlotByKey(host string, port int, slotName string) (*SyncSlot, error) {
	host = strings.TrimSpace(host)
	slotName = strings.TrimSpace(slotName)
	if host == "" || port <= 0 || slotName == "" {
		return nil, nil
	}
	s := &SyncSlot{}
	var active int
	err := d.QueryRow(`SELECT id, host, port, slot_name, database_name, active, COALESCE(active_pid,0), restart_lsn, COALESCE(confirmed_flush_lsn,''), COALESCE(lag_bytes,0), plugin, updated_at FROM tbl_sync_slots WHERE host=? AND port=? AND slot_name=?`,
		host, port, slotName).
		Scan(&s.ID, &s.Host, &s.Port, &s.SlotName, &s.Database, &active, &s.ActivePID, &s.RestartLSN, &s.ConfirmedFlushLSN, &s.LagBytes, &s.Plugin, &s.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.Active = active == 1
	return s, nil
}

func (d *DB) CreateArchive(t *ArchiveTask) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	if t.Status == "" {
		t.Status = StatusInit
	}
	t.CreatedAt, t.UpdatedAt = now, now
	_, err := d.Exec(
		`INSERT INTO tbl_archive_tasks(id, name, status, source_instance_id, source_db, table_name, condition_sql, issue_id, approval_status, assigned_to, schedule_expr, created_at, updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.Name, t.Status, t.SourceID, t.SourceDB, t.TableName, t.Condition, t.IssueID, t.ApprovalStatus, t.AssignedTo, t.ScheduleExpr, t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return err
	}
	if t.TenantID == "" {
		t.TenantID = d.inheritTenantFromInstance(t.SourceID)
	}
	return d.AssignTenantID("tbl_archive_tasks", t.ID, t.TenantID)
}

func archiveTaskSelect() string {
	return `id, name, status, source_instance_id, source_db, table_name, condition_sql, issue_id, approval_status, assigned_to, schedule_expr, created_at, updated_at`
}

func scanArchiveTask(t *ArchiveTask, scan func(dest ...any) error) error {
	return scan(&t.ID, &t.Name, &t.Status, &t.SourceID, &t.SourceDB, &t.TableName, &t.Condition, &t.IssueID, &t.ApprovalStatus, &t.AssignedTo, &t.ScheduleExpr, &t.CreatedAt, &t.UpdatedAt)
}

func (d *DB) GetArchive(id string) (*ArchiveTask, error) {
	t := &ArchiveTask{}
	err := scanArchiveTask(t, d.QueryRow(`SELECT `+archiveTaskSelect()+` FROM tbl_archive_tasks WHERE id=?`, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

func (d *DB) GetArchiveByIssueID(issueID string) (*ArchiveTask, error) {
	if strings.TrimSpace(issueID) == "" {
		return nil, nil
	}
	t := &ArchiveTask{}
	err := scanArchiveTask(t, d.QueryRow(`SELECT `+archiveTaskSelect()+` FROM tbl_archive_tasks WHERE issue_id=? ORDER BY created_at DESC LIMIT 1`, issueID).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

func (d *DB) ListArchives() ([]ArchiveTask, error) {
	return d.ListArchivesInTenant("")
}

func (d *DB) ListArchivesInTenant(tenantID string) ([]ArchiveTask, error) {
	q := `SELECT ` + archiveTaskSelect() + ` FROM tbl_archive_tasks WHERE 1=1`
	args := []any{}
	q, args = appendTenantFilter(q, args, tenantID)
	q += ` ORDER BY created_at DESC`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ArchiveTask
	for rows.Next() {
		var t ArchiveTask
		if err := scanArchiveTask(&t, rows.Scan); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func archiveMetaParts(t *ArchiveTask) []string {
	if t == nil {
		return nil
	}
	raw := strings.TrimSpace(t.Condition)
	if !strings.HasPrefix(raw, "-- auto-dba-archive:") {
		return nil
	}
	line, _, _ := strings.Cut(raw, "\n")
	return strings.Split(strings.TrimPrefix(line, "-- auto-dba-archive:"), ",")
}

func ArchiveStrategy(t *ArchiveTask) string {
	parts := archiveMetaParts(t)
	if len(parts) >= 1 {
		s := strings.TrimSpace(parts[0])
		if s != "" {
			return s
		}
	}
	return "backup_target"
}

func ArchiveRunMode(t *ArchiveTask) string {
	if t == nil {
		return "immediate"
	}
	if strings.TrimSpace(t.ScheduleExpr) != "" {
		return "scheduled"
	}
	parts := archiveMetaParts(t)
	if len(parts) >= 2 {
		mode := strings.TrimSpace(parts[1])
		if mode == "scheduled" || mode == "daily_incremental" || mode == "daily" {
			return "scheduled"
		}
	}
	return "immediate"
}

func (d *DB) ArchiveStats() (map[string]any, error) {
	return d.ArchiveStatsInTenant("")
}

func (d *DB) ArchiveStatsInTenant(tenantID string) (map[string]any, error) {
	list, err := d.ListArchivesInTenant(tenantID)
	if err != nil {
		return nil, err
	}
	total := len(list)
	running, failed, done := 0, 0, 0
	var recent []map[string]any
	for _, t := range list {
		switch t.Status {
		case StatusRunning:
			running++
		case StatusFailed:
			failed++
		case StatusExecuted, "success":
			done++
		}
		recent = append(recent, map[string]any{
			"id": t.ID, "task_id": t.ID, "task_name": t.Name, "name": t.Name,
			"status": t.Status, "source_db": t.SourceDB, "table_name": t.TableName,
			"condition": t.Condition, "created_at": t.CreatedAt, "started_at": t.UpdatedAt,
			"volume": "-", "duration": "-",
		})
		if len(recent) >= 20 {
			break
		}
	}
	rate := "-"
	if total > 0 {
		rate = fmt.Sprintf("%.0f%%", float64(done)/float64(total)*100)
	}
	waitApproval, scheduleQueue := 0, 0
	for _, t := range list {
		if t.ApprovalStatus == StatusPending || t.Status == StatusPending {
			waitApproval++
		}
		if ArchiveRunMode(&t) == "scheduled" && t.ApprovalStatus == StatusApproved && t.Status != StatusRunning {
			scheduleQueue++
		}
	}
	return map[string]any{
		"total_tasks": total, "archived_volume": "-", "success_rate": rate,
		"pending_alerts": failed, "abnormal_count": failed,
		"storage_usage": 0, "db_connection": "ok",
		"pending_approval": waitApproval, "schedule_queue": scheduleQueue,
		"recent_runs": recent,
		"running":     running, "completed_today": done, "failed_today": failed,
	}, nil
}

func (d *DB) UpdateArchiveStatus(id, status string) error {
	return d.UpdateArchiveState(id, status, "")
}

func (d *DB) UpdateArchiveState(id, status, approval string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if strings.TrimSpace(approval) == "" {
		_, err := d.Exec(`UPDATE tbl_archive_tasks SET status=?, updated_at=? WHERE id=?`, status, now, id)
		return err
	}
	_, err := d.Exec(`UPDATE tbl_archive_tasks SET status=?, approval_status=?, updated_at=? WHERE id=?`, status, approval, now, id)
	return err
}

func (d *DB) UpdateArchiveFields(t *ArchiveTask) error {
	if t == nil {
		return errors.New("task is nil")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	t.UpdatedAt = now
	_, err := d.Exec(
		`UPDATE tbl_archive_tasks SET name=?, source_db=?, table_name=?, condition_sql=?, assigned_to=?, schedule_expr=?, updated_at=? WHERE id=?`,
		t.Name, t.SourceDB, t.TableName, t.Condition, t.AssignedTo, t.ScheduleExpr, t.UpdatedAt, t.ID,
	)
	return err
}

func (d *DB) CreateKafka(t *KafkaOrder) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	t.CreatedAt = now
	if t.Status == "" {
		t.Status = StatusPending
	}
	_, err := d.Exec(
		`INSERT INTO tbl_kafka_orders(id, ticket_no, cluster, topic, action, status, submitter, assigned_to, description, created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.TicketNo, t.Cluster, t.Topic, t.Action, t.Status, t.Submitter, t.AssignedTo, t.Description, t.CreatedAt,
	)
	return err
}

func (d *DB) ListKafka() ([]KafkaOrder, error) {
	return d.ListKafkaInTenant("")
}

func (d *DB) ListKafkaInTenant(tenantID string) ([]KafkaOrder, error) {
	q, args := appendTenantFilter(`SELECT id, ticket_no, cluster, topic, action, status, submitter, assigned_to, description, created_at FROM tbl_kafka_orders WHERE 1=1`, nil, tenantID)
	rows, err := d.Query(q+` ORDER BY created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []KafkaOrder
	for rows.Next() {
		var t KafkaOrder
		if err := rows.Scan(&t.ID, &t.TicketNo, &t.Cluster, &t.Topic, &t.Action, &t.Status, &t.Submitter, &t.AssignedTo, &t.Description, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (d *DB) CreateMultilang(t *MultilangTask) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	t.CreatedAt = now
	if t.Status == "" {
		t.Status = StatusInit
	}
	_, err := d.Exec(`INSERT INTO tbl_multilang_tasks(id, name, status, instance_id, template, created_at) VALUES(?,?,?,?,?,?)`,
		t.ID, t.Name, t.Status, t.InstanceID, t.Template, t.CreatedAt)
	return err
}

func (d *DB) ListMultilang() ([]MultilangTask, error) {
	rows, err := d.Query(`SELECT id, name, status, instance_id, template, created_at FROM tbl_multilang_tasks ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MultilangTask
	for rows.Next() {
		var t MultilangTask
		if err := rows.Scan(&t.ID, &t.Name, &t.Status, &t.InstanceID, &t.Template, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (d *DB) CreateInsight(t *InsightJob) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	t.CreatedAt = now
	if t.Status == "" {
		t.Status = StatusInit
	}
	_, err := d.Exec(`INSERT INTO tbl_insight_jobs(id, kind, instance_id, db_name, status, summary, created_at) VALUES(?,?,?,?,?,?,?)`,
		t.ID, t.Kind, t.InstanceID, t.DBName, t.Status, t.Summary, t.CreatedAt)
	return err
}

func (d *DB) ListInsight(kind string) ([]InsightJob, error) {
	q := `SELECT id, kind, instance_id, db_name, status, summary, created_at FROM tbl_insight_jobs WHERE 1=1`
	var args []any
	if kind != "" {
		q += ` AND kind=?`
		args = append(args, kind)
	}
	q += ` ORDER BY created_at DESC`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []InsightJob
	for rows.Next() {
		var t InsightJob
		if err := rows.Scan(&t.ID, &t.Kind, &t.InstanceID, &t.DBName, &t.Status, &t.Summary, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
