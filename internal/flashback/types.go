package flashback

import (
	"time"

	"github.com/google/uuid"
)

const (
	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"

	KindUndo = "undo"
	KindRedo = "redo"

	OutputFlashback = "flashback"
	OutputOriginal  = "original"
)

func NewID() string { return uuid.NewString() }

type TaskRow struct {
	ID             string
	InstanceID     string
	MDMInstanceID  string
	Host           string
	Port           int
	DatabaseName   string
	Tables         string
	TargetTime     time.Time
	EndTime        *time.Time
	StartXID       int64
	StopXID        int64
	StartFile      string
	StartPos       uint32
	StopFile       string
	StopPos        uint32
	SQLType        string
	OutputKind     string
	Status         string
	ErrorMessage   string
	Warning        string
	WorkDir        string
	WALBytes       int64
	WALFiles       int
	ChangeCount    int
	LogTotal       int
	LogDone        int
	ParseTotal     int
	ParseDone      int
	DMLTicketID    string
	IssueID        string
	ApprovalStatus string
	CreatedBy      string
	Reviewer       string
	CreatedAt      time.Time
	UpdatedAt      *time.Time
	StartedAt      *time.Time
	FinishedAt     *time.Time
}

type LogRow struct {
	ID        int64
	TaskID    string
	Level     string
	Message   string
	CreatedAt time.Time
}

type SQLRow struct {
	ID         int64
	TaskID     string
	Seq        int
	Kind       string
	SchemaName string
	TableName  string
	Op         string
	XID        int64
	TS         *time.Time
	Statement  string
	Risk       string
}

type ResourceDbsInfo struct {
	Address  string
	Version  string
	Name     string
	RegionID string
	Zone     *ResourceZone
	DbType   string
	Tags     map[string]interface{}
	Region   *ResourceRegion
}

type ResourceZone struct {
	Name string
	Zone string
}

type ResourceRegion struct {
	Name   string
	Region string
}

type DomainInstance struct {
	ID         string
	InstanceID string
	MainIP     string
	DomainName string
	DbType     string
	Port       int
}

type FlashbackTaskReq struct {
	InstanceID      string   `json:"instance_id"`
	Database        string   `json:"database"`
	DBName          string   `json:"db_name"`
	Tables          []string `json:"tables"`
	TargetTime      string   `json:"target_time"`
	StartTime       string   `json:"start_time"`
	EndTime         string   `json:"end_time"`
	StartXID        int64    `json:"start_xid"`
	StopXID         int64    `json:"stop_xid"`
	StartFile       string   `json:"start_file"`
	StartPos        uint32   `json:"start_pos"`
	StopFile        string   `json:"stop_file"`
	StopPos         uint32   `json:"stop_pos"`
	SQLType         string   `json:"sql_type"`
	SQLTypes        []string `json:"sql_types"`
	OutputKind      string   `json:"output_kind"`
	Reviewer        string   `json:"reviewer"`
	Approver        string   `json:"approver"`
	AssignedTo      string   `json:"assigned_to"`
	DictTaskID      string   `json:"dict_task_id"`
	CloudInstanceID string   `json:"cloud_instance_id"`
	CloudRegion     string   `json:"cloud_region"`
}

func (r *FlashbackTaskReq) normalize() {
	if r.Database == "" {
		r.Database = r.DBName
	}
	if r.TargetTime == "" {
		r.TargetTime = r.StartTime
	}
	if r.SQLType == "" && len(r.SQLTypes) > 0 {
		r.SQLType = joinCSV(r.SQLTypes)
	}
}

type FlashbackCheckItem struct {
	Code    string `json:"code"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
	OK      bool   `json:"ok"`
}

type FlashbackPrecheckResult struct {
	OK            bool                 `json:"ok"`
	Items         []FlashbackCheckItem `json:"items"`
	Host          string               `json:"host,omitempty"`
	Port          int                  `json:"port,omitempty"`
	ServerVersion string               `json:"server_version,omitempty"`
	WALLevel      string               `json:"wal_level,omitempty"`
	ArchiveMode   string               `json:"archive_mode,omitempty"`
	WALFiles      int                  `json:"wal_files"`
	WALBytes      int64                `json:"wal_bytes"`
	WALFrom       *time.Time           `json:"wal_from,omitempty"`
	WALTo         *time.Time           `json:"wal_to,omitempty"`
	Covered       bool                 `json:"covered"`
	ParseMode     string               `json:"parse_mode,omitempty"`
	Message       string               `json:"message,omitempty"`
}

type FlashbackTaskProgress struct {
	Phase        string `json:"phase"`
	Unit         string `json:"unit,omitempty"`
	FetchDone    int    `json:"fetch_done"`
	FetchTotal   int    `json:"fetch_total"`
	FetchRemain  int    `json:"fetch_remain"`
	FetchPercent int    `json:"fetch_percent"`
	ParseDone    int    `json:"parse_done"`
	ParseTotal   int    `json:"parse_total"`
	ParseRemain  int    `json:"parse_remain"`
	ParsePercent int    `json:"parse_percent"`
}

type FlashbackTaskDTO struct {
	ID           string                 `json:"id"`
	InstanceID   string                 `json:"instance_id"`
	Host         string                 `json:"host"`
	Port         int                    `json:"port"`
	Database     string                 `json:"database"`
	DBName       string                 `json:"db_name"`
	Tables       []string               `json:"tables"`
	TargetTime   time.Time              `json:"target_time"`
	StartTime    string                 `json:"start_time,omitempty"`
	EndTime      *time.Time             `json:"end_time,omitempty"`
	SQLType      string                 `json:"sql_type,omitempty"`
	OutputKind   string                 `json:"output_kind"`
	Status       string                 `json:"status"`
	ErrorMessage string                 `json:"error_message,omitempty"`
	Warning      string                 `json:"warning,omitempty"`
	ChangeCount  int                    `json:"change_count"`
	WALBytes     int64                  `json:"wal_bytes"`
	WALFiles     int                    `json:"wal_files"`
	Progress     *FlashbackTaskProgress `json:"progress,omitempty"`
	DMLTicketID  string                 `json:"dml_ticket_id,omitempty"`
	Reviewer     string                 `json:"reviewer,omitempty"`
	CreatedBy    string                 `json:"created_by,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
}

func joinCSV(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += ","
		}
		out += s
	}
	return out
}
