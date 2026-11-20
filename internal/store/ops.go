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
	} else {
		q += ` AND status<>?`
		args = append(args, StatusSuperseded)
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

// ListCDCByParentID 列出某 init 下的子任务（可选按 task_type 过滤）。
func (d *DB) ListCDCByParentID(parentID, taskType string) ([]CDCTask, error) {
	parentID = strings.TrimSpace(parentID)
	if parentID == "" {
		return nil, nil
	}
	q := `SELECT ` + cdcTaskCols + ` FROM tbl_cdc_tasks WHERE parent_id=?`
	args := []any{parentID}
	if tt := strings.TrimSpace(taskType); tt != "" {
		q += ` AND task_type=?`
		args = append(args, tt)
	}
	q += ` ORDER BY created_at ASC`
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

func (d *DB) UpdateCDCIssueID(id, issueID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`UPDATE tbl_cdc_tasks SET issue_id=?, updated_at=? WHERE id=?`, strings.TrimSpace(issueID), now, id)
	return err
}

func (d *DB) UpdateCDCTargetID(id, targetID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`UPDATE tbl_cdc_tasks SET target_instance_id=?, updated_at=? WHERE id=?`, strings.TrimSpace(targetID), now, id)
	return err
}

func (d *DB) UpdateCDCSourceID(id, sourceID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`UPDATE tbl_cdc_tasks SET source_instance_id=?, updated_at=? WHERE id=?`, strings.TrimSpace(sourceID), now, id)
	return err
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

// syncSlotCols 列表和按键读取共用。重启必须带上采集补齐的 connector，否则本地没有 CDC 任务的槽会全部跳过。
const syncSlotCols = `id, host, port, slot_name, database_name, active, COALESCE(active_pid,0), restart_lsn, COALESCE(confirmed_flush_lsn,''), COALESCE(lag_bytes,0), plugin, updated_at, COALESCE(connect_base,''), COALESCE(connector_name,''), COALESCE(task_name,''), COALESCE(topic,'')`

func scanSyncSlot(row interface{ Scan(...any) error }) (*SyncSlot, error) {
	s := &SyncSlot{}
	var active int
	if err := row.Scan(&s.ID, &s.Host, &s.Port, &s.SlotName, &s.Database, &active, &s.ActivePID, &s.RestartLSN, &s.ConfirmedFlushLSN, &s.LagBytes, &s.Plugin, &s.UpdatedAt, &s.ConnectMetaBase, &s.ConnectMetaName, &s.ConnectMetaTask, &s.ConnectMetaTopic); err != nil {
		return nil, err
	}
	s.Active = active == 1
	return s, nil
}

func (d *DB) ListSlotsInTenant(tenantID string) ([]SyncSlot, error) {
	q := `SELECT ` + syncSlotCols + ` FROM tbl_sync_slots WHERE 1=1`
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
		s, err := scanSyncSlot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
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
	exists, err := d.findSlotIDByName(s.SlotName)
	if err != nil {
		return err
	}
	if exists == "" {
		err = d.QueryRow(`SELECT id FROM tbl_sync_slots WHERE host=? AND port=? AND slot_name=?`, s.Host, s.Port, s.SlotName).Scan(&exists)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if errors.Is(err, sql.ErrNoRows) {
			exists = ""
			err = nil
		}
	}
	if exists != "" {
		_, err = d.Exec(`UPDATE tbl_sync_slots SET database_name=?, active=?, plugin=?, updated_at=? WHERE id=?`,
			s.Database, active, s.Plugin, now, exists)
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

func (d *DB) UpdateSlotConnectMeta(id, base, connectorName, topic string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	_, err := d.Exec(`UPDATE tbl_sync_slots SET connect_base=?, connector_name=?, task_name=?, topic=? WHERE id=?`,
		strings.TrimSpace(base), strings.TrimSpace(connectorName), strings.TrimSpace(connectorName), strings.TrimSpace(topic), id)
	return err
}

func (d *DB) ClearSlotConnectMeta(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	_, err := d.Exec(`UPDATE tbl_sync_slots SET connect_base='', connector_name='', task_name='', topic='' WHERE id=?`, id)
	return err
}

func (d *DB) DeleteSlot(id string) error {
	_, err := d.Exec(`DELETE FROM tbl_sync_slots WHERE id=?`, id)
	return err
}

func (d *DB) GetSyncSlot(id string) (*SyncSlot, error) {
	s, err := scanSyncSlot(d.QueryRow(`SELECT `+syncSlotCols+` FROM tbl_sync_slots WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return s, err
}

func (d *DB) DeleteSlotsByName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	_, err := d.Exec(`DELETE FROM tbl_sync_slots WHERE slot_name=?`, name)
	return err
}

// ReplaceHostSlots 用本次采集结果更新槽。槽名全局唯一：按 slot_name 覆盖同一行的
// pid、LSN、滞后，并删掉同名重复行。仅删除本实例主机上本次未再出现的槽。
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
		hits, err := d.listSlotsByName(name)
		if err != nil {
			return err
		}
		if len(hits) > 0 {
			keepID := hits[0].ID
			for _, h := range hits {
				if hostInList(h.Host, hosts) {
					keepID = h.ID
					break
				}
			}
			_, err = d.Exec(`UPDATE tbl_sync_slots SET host=?, port=?, database_name=?, active=?, active_pid=?, restart_lsn=?, confirmed_flush_lsn=?, lag_bytes=?, plugin=?, updated_at=? WHERE id=?`,
				displayHost, port, s.Database, active, s.ActivePID, s.RestartLSN, s.ConfirmedFlushLSN, s.LagBytes, s.Plugin, now, keepID)
			if err != nil {
				return err
			}
			for _, h := range hits {
				if h.ID == keepID {
					continue
				}
				if err := d.DeleteSlot(h.ID); err != nil {
					return err
				}
			}
			s.ID = keepID
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

type namedSlotHit struct {
	ID   string
	Host string
}

func hostInList(host string, hosts []string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	for _, h := range hosts {
		if strings.ToLower(strings.TrimSpace(h)) == host {
			return true
		}
	}
	return false
}

// listSlotsByName 按槽名取全部行（槽名全局唯一，主机写法不同视为同一槽）。
func (d *DB) listSlotsByName(slotName string) ([]namedSlotHit, error) {
	slotName = strings.TrimSpace(slotName)
	if slotName == "" {
		return nil, nil
	}
	rows, err := d.Query(`SELECT id, host FROM tbl_sync_slots WHERE slot_name=? ORDER BY updated_at ASC, id ASC`, slotName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []namedSlotHit
	for rows.Next() {
		var h namedSlotHit
		if err := rows.Scan(&h.ID, &h.Host); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (d *DB) findSlotIDByName(slotName string) (string, error) {
	hits, err := d.listSlotsByName(slotName)
	if err != nil || len(hits) == 0 {
		return "", err
	}
	return hits[0].ID, nil
}

// GetSyncSlotByKey 按主机+端口+槽名取槽（稳定键，不受采集换 id 影响）。
func (d *DB) GetSyncSlotByKey(host string, port int, slotName string) (*SyncSlot, error) {
	host = strings.TrimSpace(host)
	slotName = strings.TrimSpace(slotName)
	if host == "" || port <= 0 || slotName == "" {
		return nil, nil
	}
	s, err := scanSyncSlot(d.QueryRow(`SELECT `+syncSlotCols+` FROM tbl_sync_slots WHERE host=? AND port=? AND slot_name=?`, host, port, slotName))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return s, err
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
	running, taskFailed := 0, 0
	var recent []map[string]any
	for _, t := range list {
		switch t.Status {
		case StatusRunning:
			running++
		case StatusFailed:
			taskFailed++
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
	outcomes, err := d.listArchiveRunOutcomes(tenantID)
	if err != nil {
		return nil, err
	}
	rate, doneToday, failedToday := summarizeArchiveRuns(outcomes, time.Now())
	archivedRows, err := d.sumArchiveDeletedRows(tenantID)
	if err != nil {
		return nil, err
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
		"total_tasks": total, "archived_volume": formatArchivedVolume(archivedRows), "success_rate": rate,
		"pending_alerts": taskFailed, "abnormal_count": taskFailed,
		"storage_usage": 0, "db_connection": "ok",
		"pending_approval": waitApproval, "schedule_queue": scheduleQueue,
		"recent_runs": recent,
		"running":     running, "completed_today": doneToday, "failed_today": failedToday,
	}, nil
}

type archiveRunOutcome struct {
	Status string
	At     string
}

func formatArchivedVolume(rows int64) string {
	return fmt.Sprintf("%d 行", rows)
}

func (d *DB) sumArchiveDeletedRows(tenantID string) (int64, error) {
	q := `SELECT COALESCE(SUM(r.deleted_rows), 0)
		FROM tbl_archive_runs r
		JOIN tbl_archive_tasks t ON t.id = r.task_id
		WHERE 1=1`
	var args []any
	if tid := strings.TrimSpace(tenantID); tid != "" {
		q += ` AND COALESCE(t.tenant_id,'')=?`
		args = append(args, tid)
	}
	var n int64
	err := d.QueryRow(q, args...).Scan(&n)
	return n, err
}

func (d *DB) listArchiveRunOutcomes(tenantID string) ([]archiveRunOutcome, error) {
	q := `SELECT r.status, COALESCE(NULLIF(r.finished_at,''), NULLIF(r.started_at,''), r.created_at)
		FROM tbl_archive_runs r
		JOIN tbl_archive_tasks t ON t.id = r.task_id
		WHERE 1=1`
	var args []any
	if tid := strings.TrimSpace(tenantID); tid != "" {
		q += ` AND COALESCE(t.tenant_id,'')=?`
		args = append(args, tid)
	}
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []archiveRunOutcome
	for rows.Next() {
		var item archiveRunOutcome
		if err := rows.Scan(&item.Status, &item.At); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func archiveShanghaiDayStart(now time.Time) time.Time {
	loc := time.FixedZone("CST", 8*3600)
	if loaded, err := time.LoadLocation("Asia/Shanghai"); err == nil {
		loc = loaded
	}
	t := now.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

func archiveRunKind(status string) string {
	switch strings.TrimSpace(status) {
	case StatusExecuted, "success":
		return "success"
	case StatusFailed:
		return "failed"
	default:
		return ""
	}
}

// summarizeArchiveRuns 用执行记录算成功率。删除 0 行的成功也计入。取消和仍在运行的不进分母。
// 今日完成、今日失败按结束时间（没有则用开始时间）落在 Asia/Shanghai 当天。
func summarizeArchiveRuns(rows []archiveRunOutcome, now time.Time) (rate string, completedToday, failedToday int) {
	success, failed := 0, 0
	start := archiveShanghaiDayStart(now)
	for _, row := range rows {
		kind := archiveRunKind(row.Status)
		if kind == "" {
			continue
		}
		if kind == "success" {
			success++
		} else {
			failed++
		}
		at, err := time.Parse(time.RFC3339, strings.TrimSpace(row.At))
		if err != nil || at.Before(start) {
			continue
		}
		if kind == "success" {
			completedToday++
		} else {
			failedToday++
		}
	}
	rate = "-"
	if success+failed > 0 {
		rate = fmt.Sprintf("%.0f%%", float64(success)/float64(success+failed)*100)
	}
	return rate, completedToday, failedToday
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
