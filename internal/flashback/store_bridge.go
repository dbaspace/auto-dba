package flashback

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"auto-dba/internal/store"
)

type Store struct{}

func (Store) InsertTask(_ context.Context, r *TaskRow) error {
	if runtime == nil || runtime.Store == nil {
		return errString("flashback runtime 未初始化")
	}
	t := taskFromRow(r)
	return runtime.Store.CreateFlashback(t)
}

func (Store) GetTask(_ context.Context, id string) (*TaskRow, error) {
	if runtime == nil || runtime.Store == nil {
		return nil, errString("flashback runtime 未初始化")
	}
	t, err := runtime.Store.GetFlashback(id)
	if err != nil || t == nil {
		return nil, err
	}
	return rowFromTask(t), nil
}

func (Store) UpdateStatus(_ context.Context, id, status, errMsg, warning string) error {
	return runtime.Store.UpdateFlashbackStatus(id, status, errMsg, warning)
}

func (Store) UpdateProgress(_ context.Context, id, workDir string, walBytes int64, walFiles, changeCount int) error {
	return runtime.Store.UpdateFlashbackProgress(id, workDir, walBytes, walFiles, changeCount)
}

func (Store) UpdateStageProgress(_ context.Context, id string, logDone, logTotal, parseDone, parseTotal int) error {
	return runtime.Store.UpdateFlashbackStage(id, logDone, logTotal, parseDone, parseTotal)
}

func (Store) SetDMLTicketID(_ context.Context, id, ticketID string) error {
	return runtime.Store.SetFlashbackDMLTicket(id, ticketID)
}

func (Store) InsertLog(_ context.Context, taskID, level, message string) error {
	return runtime.Store.AddFlashbackLog(taskID, level, message)
}

func (Store) ListLogs(_ context.Context, taskID string, limit int) ([]*LogRow, error) {
	list, err := runtime.Store.ListFlashbackLogs(taskID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]*LogRow, 0, len(list))
	for _, l := range list {
		out = append(out, &LogRow{ID: l.ID, TaskID: l.TaskID, Level: l.Level, Message: l.Message, CreatedAt: parseTime(l.CreatedAt)})
	}
	return out, nil
}

func (Store) InsertSQLs(_ context.Context, rows []*SQLRow) error {
	list := make([]store.FlashbackSQL, 0, len(rows))
	for _, r := range rows {
		ts := ""
		if r.TS != nil {
			ts = r.TS.UTC().Format(time.RFC3339)
		}
		list = append(list, store.FlashbackSQL{
			TaskID: r.TaskID, Seq: r.Seq, Kind: r.Kind, SchemaName: r.SchemaName, TableName: r.TableName,
			Op: r.Op, XID: r.XID, TS: ts, Statement: r.Statement, Risk: r.Risk,
		})
	}
	return runtime.Store.InsertFlashbackSQLs(list)
}

func (Store) ListAllSQLStatements(_ context.Context, taskID, kind string, ops []string) ([]string, error) {
	return runtime.Store.ListFlashbackSQLStatements(taskID, kind, ops)
}

func (Store) FailStuckRunning(_ context.Context, msg string) (int64, error) {
	return runtime.Store.FailStuckFlashbacks(msg)
}

func taskFromRow(r *TaskRow) *store.FlashbackTask {
	end := ""
	if r.EndTime != nil {
		end = r.EndTime.UTC().Format(time.RFC3339)
	}
	return &store.FlashbackTask{
		ID: r.ID, Name: "flashback-" + r.ID[:8], Status: r.Status, InstanceID: r.InstanceID,
		DBName: r.DatabaseName, Host: r.Host, Port: r.Port, Tables: r.Tables,
		StartTime: r.TargetTime.UTC().Format(time.RFC3339), EndTime: end,
		StartXID: r.StartXID, StopXID: r.StopXID, StartFile: r.StartFile, StartPos: r.StartPos,
		StopFile: r.StopFile, StopPos: r.StopPos, SQLType: r.SQLType, OutputKind: r.OutputKind,
		Reviewer: r.Reviewer, ErrorMessage: r.ErrorMessage, Warning: r.Warning, WorkDir: r.WorkDir,
		WALBytes: r.WALBytes, WALFiles: r.WALFiles, ChangeCount: r.ChangeCount,
		LogTotal: r.LogTotal, LogDone: r.LogDone, ParseTotal: r.ParseTotal, ParseDone: r.ParseDone,
		DMLTicketID: r.DMLTicketID, IssueID: r.IssueID, ApprovalStatus: r.ApprovalStatus, CreatedBy: r.CreatedBy,
	}
}

func rowFromTask(t *store.FlashbackTask) *TaskRow {
	target := parseTime(firstNonEmpty(t.StartTime, t.TargetTime))
	var endPtr *time.Time
	if strings.TrimSpace(t.EndTime) != "" {
		et := parseTime(t.EndTime)
		endPtr = &et
	}
	tables := t.Tables
	if tables == "" && len(t.TableList) > 0 {
		b, _ := json.Marshal(t.TableList)
		tables = string(b)
	}
	return &TaskRow{
		ID: t.ID, InstanceID: t.InstanceID, Host: t.Host, Port: t.Port, DatabaseName: t.DBName,
		Tables: tables, TargetTime: target, EndTime: endPtr,
		StartXID: t.StartXID, StopXID: t.StopXID, StartFile: t.StartFile, StartPos: t.StartPos,
		StopFile: t.StopFile, StopPos: t.StopPos, SQLType: t.SQLType, OutputKind: t.OutputKind,
		Status: t.Status, ErrorMessage: t.ErrorMessage, Warning: t.Warning, WorkDir: t.WorkDir,
		WALBytes: t.WALBytes, WALFiles: t.WALFiles, ChangeCount: t.ChangeCount,
		LogTotal: t.LogTotal, LogDone: t.LogDone, ParseTotal: t.ParseTotal, ParseDone: t.ParseDone,
		DMLTicketID: t.DMLTicketID, IssueID: t.IssueID, ApprovalStatus: t.ApprovalStatus,
		CreatedBy: t.CreatedBy, Reviewer: t.Reviewer,
		CreatedAt: parseTime(t.CreatedAt),
	}
}

func parseTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339, time.RFC3339Nano, "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t
		}
	}
	return time.Time{}
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
