package store

type User struct {
	ID               string   `json:"id"`
	Username         string   `json:"username"`
	DisplayName      string   `json:"display_name"`
	Role             string   `json:"role"`
	RoleID           string   `json:"role_id"`
	RoleIDs          []string `json:"role_ids,omitempty"`
	PasswordHash     string   `json:"-"`
	Source           string   `json:"source,omitempty"`
	CreatedAt        string   `json:"created_at"`
	LoginDisabled    bool     `json:"login_disabled"`
	FailedLoginCount int      `json:"failed_login_count"`
	LockedAt         string   `json:"locked_at,omitempty"`
	Status           string   `json:"status"`
	Avatar           string   `json:"avatar,omitempty"`
	Gender           string   `json:"gender,omitempty"`
	Email            string   `json:"email,omitempty"`
	Phone            string   `json:"phone,omitempty"`
	IMType           string   `json:"im_type,omitempty"`
	IMAccount        string   `json:"im_account,omitempty"`
}

type Role struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Code        string `json:"code"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at,omitempty"`
	MemberCount int    `json:"member_count"`
}

type Menu struct {
	ID             string `json:"id"`
	ParentID       string `json:"parent_id,omitempty"`
	Title          string `json:"title"`
	URN            string `json:"urn,omitempty"`
	Path           string `json:"path,omitempty"`
	PermissionDesc string `json:"permission_desc,omitempty"`
	Icon           string `json:"icon,omitempty"`
	SortOrder      int    `json:"sort_order"`
	Enabled        bool   `json:"enabled"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

type MenuNode struct {
	Menu
	ParentTitle string     `json:"parent_title,omitempty"`
	Children    []MenuNode `json:"children,omitempty"`
}

type Region struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type Environment struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type Instance struct {
	ID              string   `json:"id"`
	TenantID        string   `json:"tenant_id,omitempty"`
	TenantName      string   `json:"tenant_name,omitempty"`
	Name            string   `json:"name"`
	DomainName      string   `json:"domain_name"`
	Domains         []string `json:"domains"`
	Port            int      `json:"port"`
	DBType          string   `json:"db_type"`
	IDC             string   `json:"idc"`
	RegionID        string   `json:"region_id,omitempty"`
	EnvID           string   `json:"env_id,omitempty"`
	Host            string   `json:"host"`
	Username        string   `json:"username,omitempty"`
	Password        string   `json:"password,omitempty"`
	PasswordSet     bool     `json:"password_set,omitempty"`
	Approvers       []string `json:"approvers"`
	Tags            []string `json:"tags"`
	CloudVendor     string   `json:"cloud_vendor,omitempty"`
	CloudInstanceID string   `json:"cloud_instance_id,omitempty"`
	CloudRegion     string   `json:"cloud_region,omitempty"`
	Visibility      string   `json:"visibility,omitempty"`
	AccessUsers     []string `json:"access_users,omitempty"`
	RoleIDs         []string `json:"role_ids,omitempty"`
	Description     string   `json:"description,omitempty"`
	CreatedAt       string   `json:"created_at"`
	UpdatedAt       string   `json:"updated_at"`
}

type Database struct {
	ID           string   `json:"id"`
	TenantID     string   `json:"tenant_id,omitempty"`
	TenantName   string   `json:"tenant_name,omitempty"`
	InstanceID   string   `json:"instance_id"`
	InstanceName string   `json:"instance_name,omitempty"`
	DBName       string   `json:"db_name"`
	Domains      []string `json:"domains"`
	Approvers    []string `json:"approvers"`
	Visibility   string   `json:"visibility"`
	AccessUsers  []string `json:"access_users"`
	RoleIDs      []string `json:"role_ids"`
	Tags         []string `json:"tags"`
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
}

type GlobalArg struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Description string `json:"description"`
	UpdatedAt   string `json:"updated_at"`
}

type Connector struct {
	ID                string `json:"id"`
	TenantID          string `json:"tenant_id,omitempty"`
	DBZHost           string `json:"dbz_host"`
	Env               string `json:"env"`
	KafkaHost         string `json:"kafka_host"`
	MessageSchemaMode string `json:"message_schema_mode"`
	Remark            string `json:"remark,omitempty"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
}

type Issue struct {
	ID           string `json:"id"`
	TenantID     string `json:"tenant_id,omitempty"`
	TicketNo     string `json:"ticket_no"`
	IssueType    string `json:"issue_type"`
	Type         string `json:"type"`
	InstanceID   string `json:"instance_id"`
	Domain       string `json:"domain,omitempty"`
	DatabaseID   string `json:"database_id,omitempty"`
	DBName       string `json:"db_name"`
	SQLContent   string `json:"sql_content"`
	Description  string `json:"description"`
	Status       string `json:"status"`
	Submitter    string `json:"submitter_name"`
	AssignedTo   string `json:"assigned_to"`
	RejectReason string `json:"reject_reason,omitempty"`
	NeedRollback string `json:"need_rollback,omitempty"`
	// AssigneeTransferHistory JSON 字符串数组，如 ["a->b by=a at=..."]
	AssigneeTransferHistory string `json:"assignee_transfer_history,omitempty"`
	ScheduledAt             string `json:"scheduled_at,omitempty"`
	ExecuteResult           string `json:"-"`
	Exec                    any    `json:"exec,omitempty"`
	CreatedAt               string `json:"created_at"`
	UpdatedAt               string `json:"updated_at"`
}

type CDCConnectorConfigVersion struct {
	ID             string `json:"id"`
	TaskID         string `json:"task_id"`
	VersionNo      int    `json:"version_no"`
	Version        int    `json:"version"`
	ConfigJSON     string `json:"-"`
	ChangeSource   string `json:"change_source"`
	OperationID    string `json:"operation_id"`
	Operator       string `json:"operator"`
	OperatorIP     string `json:"operator_ip"`
	ConfigKeyCount int    `json:"config_key_count"`
	CreatedAt      string `json:"created_at"`
}

// CDCSyncLog 对齐 hub tbl_sync_logs，独立于任务 extra 保存编排操作日志。
type CDCSyncLog struct {
	ID        int64  `json:"id"`
	TaskID    string `json:"task_id"`
	MappingID string `json:"mapping_id,omitempty"`
	Level     string `json:"level"`
	Step      string `json:"step"`
	Message   string `json:"message"`
	Detail    string `json:"detail,omitempty"`
	Operator  string `json:"operator,omitempty"`
	CreatedAt string `json:"created_at"`
}

type CDCTask struct {
	ID             string `json:"id"`
	TenantID       string `json:"tenant_id,omitempty"`
	Name           string `json:"name"`
	TaskType       string `json:"task_type"`
	Status         string `json:"status"`
	SourceID       string `json:"source_instance_id"`
	TargetID       string `json:"target_instance_id"`
	SourceDB       string `json:"source_db"`
	TargetDB       string `json:"target_db"`
	ParentID       string `json:"parent_id,omitempty"`
	ApprovalStatus string `json:"approval_status"`
	IssueID        string `json:"issue_id,omitempty"`
	Description    string `json:"description,omitempty"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	// 展示用（非表字段）：Connector 在 Connect 上 NOT_FOUND 且状态允许时可重新初始化
	CanReinit        bool `json:"can_reinit,omitempty"`
	ConnectorMissing bool `json:"connector_missing,omitempty"`
}

type SyncSlot struct {
	ID         string `json:"id"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	SlotName   string `json:"slot_name"`
	Database   string `json:"database"`
	Active     bool   `json:"active"`
	RestartLSN string `json:"restart_lsn,omitempty"`
	Plugin     string `json:"plugin,omitempty"`
	UpdatedAt  string `json:"updated_at"`
}

type MigrationTask struct {
	ID                    string              `json:"id"`
	Name                  string              `json:"name"`
	Status                string              `json:"status"`
	SourceID              string              `json:"source_instance_id"`
	TargetID              string              `json:"target_instance_id"`
	SourceDB              string              `json:"source_db"`
	TargetDB              string              `json:"target_db"`
	Description           string              `json:"description,omitempty"`
	CreatedBy             string              `json:"created_by,omitempty"`
	DBType                string              `json:"db_type,omitempty"`
	Env                   string              `json:"env,omitempty"`
	SourceHost            string              `json:"source_host,omitempty"`
	SourcePort            int                 `json:"source_port,omitempty"`
	TargetHost            string              `json:"target_host,omitempty"`
	TargetPort            int                 `json:"target_port,omitempty"`
	MigrationMode         string              `json:"migration_mode,omitempty"`
	SchemaStrategy        string              `json:"schema_strategy,omitempty"`
	ConflictStrategy      string              `json:"conflict_strategy,omitempty"`
	TransferMode          string              `json:"transfer_mode,omitempty"`
	StartMode             string              `json:"start_mode,omitempty"`
	ScheduledAt           string              `json:"scheduled_at,omitempty"`
	ApprovalStatus        string              `json:"approval_status,omitempty"`
	AssignedApprover      string              `json:"assigned_approver,omitempty"`
	ApprovedBy            string              `json:"approved_by,omitempty"`
	ApprovalComment       string              `json:"approval_comment,omitempty"`
	IssueID               string              `json:"issue_id,omitempty"`
	BatchSize             int                 `json:"batch_size,omitempty"`
	SpeedLimit            int                 `json:"speed_limit,omitempty"`
	TotalObjects          int                 `json:"total_objects"`
	CompletedObjects      int                 `json:"completed_objects"`
	TotalRows             int64               `json:"total_rows"`
	CompletedRows         int64               `json:"completed_rows"`
	TotalBytes            int64               `json:"total_bytes"`
	CompletedBytes        int64               `json:"completed_bytes"`
	ErrorMessage          string              `json:"error_message,omitempty"`
	StartedAt             string              `json:"started_at,omitempty"`
	FinishedAt            string              `json:"finished_at,omitempty"`
	CreatedAt             string              `json:"created_at"`
	UpdatedAt             string              `json:"updated_at"`
	Objects               []MigrationObject   `json:"objects,omitempty"`
	Prechecks             []MigrationPrecheck `json:"prechecks,omitempty"`
	TaskName              string              `json:"task_name,omitempty"`
	AssignedApproverLogin string              `json:"assigned_approver_login,omitempty"`
	SubmittedAt           string              `json:"submitted_at,omitempty"`
	ApprovedAt            string              `json:"approved_at,omitempty"`
	RejectedAt            string              `json:"rejected_at,omitempty"`
	SecondsUntilScheduled *int64              `json:"seconds_until_scheduled,omitempty"`
	Tags                  map[string]any      `json:"tags,omitempty"`
}

type MigrationObject struct {
	ID             string `json:"id"`
	TaskID         string `json:"task_id"`
	SourceDB       string `json:"source_db_name"`
	SourceSchema   string `json:"source_schema"`
	SourceTable    string `json:"source_table_name"`
	TargetDB       string `json:"target_db_name"`
	TargetSchema   string `json:"target_schema"`
	TargetTable    string `json:"target_table_name"`
	ObjectType     string `json:"object_type"`
	Status         string `json:"status"`
	TotalRows      int64  `json:"total_rows"`
	CompletedRows  int64  `json:"completed_rows"`
	TotalBytes     int64  `json:"total_bytes"`
	CompletedBytes int64  `json:"completed_bytes"`
	ErrorMessage   string `json:"error_message,omitempty"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

type MigrationLog struct {
	ID        int64  `json:"id"`
	TaskID    string `json:"task_id"`
	Level     string `json:"log_level"`
	LogType   string `json:"log_type,omitempty"`
	Message   string `json:"message"`
	Detail    string `json:"detail,omitempty"`
	CreatedAt string `json:"created_at"`
}

type MigrationPrecheck struct {
	ID      int64  `json:"id"`
	TaskID  string `json:"task_id"`
	Item    string `json:"check_item"`
	Level   string `json:"check_level"`
	Status  string `json:"check_status"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

type FlashbackTask struct {
	ID             string   `json:"id"`
	Name           string   `json:"name,omitempty"`
	Status         string   `json:"status"`
	InstanceID     string   `json:"instance_id"`
	DBName         string   `json:"db_name"`
	Database       string   `json:"database,omitempty"`
	Host           string   `json:"host,omitempty"`
	Port           int      `json:"port,omitempty"`
	Tables         string   `json:"-"`
	TableList      []string `json:"tables,omitempty"`
	StartTime      string   `json:"start_time,omitempty"`
	TargetTime     string   `json:"target_time,omitempty"`
	EndTime        string   `json:"end_time,omitempty"`
	StartXID       int64    `json:"start_xid,omitempty"`
	StopXID        int64    `json:"stop_xid,omitempty"`
	StartFile      string   `json:"start_file,omitempty"`
	StartPos       uint32   `json:"start_pos,omitempty"`
	StopFile       string   `json:"stop_file,omitempty"`
	StopPos        uint32   `json:"stop_pos,omitempty"`
	SQLType        string   `json:"sql_type,omitempty"`
	OutputKind     string   `json:"output_kind,omitempty"`
	Reviewer       string   `json:"reviewer,omitempty"`
	ErrorMessage   string   `json:"error_message,omitempty"`
	Warning        string   `json:"warning,omitempty"`
	WorkDir        string   `json:"work_dir,omitempty"`
	WALBytes       int64    `json:"wal_bytes"`
	WALFiles       int      `json:"wal_files"`
	ChangeCount    int      `json:"change_count"`
	LogTotal       int      `json:"log_total"`
	LogDone        int      `json:"log_done"`
	ParseTotal     int      `json:"parse_total"`
	ParseDone      int      `json:"parse_done"`
	DMLTicketID    string   `json:"dml_ticket_id,omitempty"`
	IssueID        string   `json:"issue_id,omitempty"`
	ApprovalStatus string   `json:"approval_status,omitempty"`
	CreatedBy      string   `json:"created_by,omitempty"`
	SQLPreview     string   `json:"sql_preview,omitempty"`
	StartedAt      string   `json:"started_at,omitempty"`
	FinishedAt     string   `json:"finished_at,omitempty"`
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
}

type FlashbackLog struct {
	ID        int64  `json:"id"`
	TaskID    string `json:"task_id"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	CreatedAt string `json:"created_at"`
}

type FlashbackSQL struct {
	ID         int64  `json:"id"`
	TaskID     string `json:"task_id"`
	Seq        int    `json:"seq"`
	Kind       string `json:"kind"`
	SchemaName string `json:"schema_name"`
	TableName  string `json:"table_name"`
	Op         string `json:"op"`
	XID        int64  `json:"xid"`
	TS         string `json:"ts,omitempty"`
	Statement  string `json:"statement"`
	Risk       string `json:"risk,omitempty"`
}

type ArchiveTask struct {
	ID             string `json:"id"`
	TenantID       string `json:"tenant_id,omitempty"`
	Name           string `json:"name"`
	Status         string `json:"status"`
	SourceID       string `json:"source_instance_id"`
	SourceDB       string `json:"source_db"`
	TableName      string `json:"table_name"`
	Condition      string `json:"condition,omitempty"`
	IssueID        string `json:"issue_id,omitempty"`
	ApprovalStatus string `json:"approval_status,omitempty"`
	AssignedTo     string `json:"assigned_to,omitempty"`
	ScheduleExpr   string `json:"schedule_expr,omitempty"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

type ArchiveRun struct {
	ID          string `json:"id"`
	TaskID      string `json:"task_id"`
	TaskName    string `json:"task_name,omitempty"`
	Status      string `json:"status"`
	Strategy    string `json:"strategy,omitempty"`
	DeletedRows int64  `json:"deleted_rows"`
	Rounds      int    `json:"rounds"`
	Message     string `json:"message,omitempty"`
	SQLPreview  string `json:"sql_preview,omitempty"`
	Volume      string `json:"volume,omitempty"`
	Duration    string `json:"duration,omitempty"`
	StartedAt   string `json:"started_at,omitempty"`
	FinishedAt  string `json:"finished_at,omitempty"`
	CreatedAt   string `json:"created_at"`
}

type KafkaOrder struct {
	ID          string `json:"id"`
	TicketNo    string `json:"ticket_no"`
	Cluster     string `json:"cluster"`
	Topic       string `json:"topic"`
	Action      string `json:"action"`
	Status      string `json:"status"`
	Submitter   string `json:"submitter"`
	AssignedTo  string `json:"assigned_to"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
}

type MultilangTask struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	InstanceID string `json:"instance_id"`
	Template   string `json:"template"`
	CreatedAt  string `json:"created_at"`
}

type InsightJob struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	InstanceID string `json:"instance_id"`
	DBName     string `json:"db_name"`
	Status     string `json:"status"`
	Summary    string `json:"summary"`
	CreatedAt  string `json:"created_at"`
}

// ReviewRule 是「配置 → 审核规则」中的引擎参数（MySQL / PostgreSQL / StarRocks）。
type ReviewRule struct {
	ID        string `json:"id"`
	Engine    string `json:"engine"`
	Category  string `json:"category,omitempty"`
	Name      string `json:"name"`
	Label     string `json:"label"`
	Value     string `json:"value"`
	SortOrder int    `json:"sort_order"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// DBAccount 是「配置 → 账号管理」中的业务库账号，密码以 enc:v1: 密文落库。
type DBAccount struct {
	ID           string   `json:"id"`
	TenantID     string   `json:"tenant_id,omitempty"`
	TenantName   string   `json:"tenant_name,omitempty"`
	InstanceID   string   `json:"instance_id"`
	DatabaseID   string   `json:"database_id,omitempty"`
	DBName       string   `json:"db_name"`
	DBType       string   `json:"db_type"`
	IDC          string   `json:"idc"`
	Username     string   `json:"username"`
	Password     string   `json:"password,omitempty"`
	PasswordSet  bool     `json:"password_set,omitempty"`
	Permissions  []string `json:"permissions"`
	Description  string   `json:"description,omitempty"`
	TicketID     string   `json:"ticket_id,omitempty"`
	CreatedBy    string   `json:"created_by,omitempty"`
	UpdatedBy    string   `json:"updated_by,omitempty"`
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
	InstanceName string   `json:"instance_name,omitempty"`
	Host         string   `json:"host,omitempty"`
	Port         int      `json:"port,omitempty"`
	DomainName   string   `json:"domain_name,omitempty"`
}

// LoginLog 是一次登录尝试（成功或失败）的审计记录。
type LoginLog struct {
	ID          string `json:"id"`
	UserID      string `json:"user_id,omitempty"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name,omitempty"`
	Success     bool   `json:"success"`
	Message     string `json:"message,omitempty"`
	IP          string `json:"ip"`
	UserAgent   string `json:"user_agent,omitempty"`
	LoggedAt    string `json:"logged_at"`
}

type SessionRow struct {
	PID      int64  `json:"pid"`
	User     string `json:"user"`
	Database string `json:"database"`
	State    string `json:"state"`
	Query    string `json:"query"`
	Host     string `json:"host"`
}

// PGActivitySnapshot 对齐 Hub tbl_pg_activity_snapshots。
type PGActivitySnapshot struct {
	ID              string   `json:"id"`
	CollectedAt     string   `json:"collected_at"`
	InstanceID      string   `json:"instance_id"`
	InstanceName    string   `json:"instance_name"`
	Host            string   `json:"host"`
	Port            int      `json:"port"`
	PID             *int64   `json:"pid"`
	PUser           string   `json:"p_user"`
	PHost           string   `json:"p_host"`
	PDb             string   `json:"p_db"`
	Command         string   `json:"command"`
	XactDuration    *float64 `json:"xact_duration"`
	State           string   `json:"state"`
	Info            string   `json:"info"`
	QueryNormalized string   `json:"query_normalized"`
	QueryHash       string   `json:"query_hash"`
	CreatedAt       string   `json:"created_at"`
}

type PGActivitySnapshotFilter struct {
	InstanceID string
	StartTime  string
	EndTime    string
	Offset     int
	Limit      int
}

type PGActivityStat struct {
	QueryHash         string   `json:"query_hash"`
	QueryNormalized   string   `json:"query_normalized"`
	SampleInfo        string   `json:"sample_info"`
	Count             int64    `json:"count"`
	DistinctInstances int64    `json:"distinct_instances"`
	DistinctDbs       int64    `json:"distinct_dbs"`
	AvgXactDuration   *float64 `json:"avg_xact_duration"`
	MaxXactDuration   *float64 `json:"max_xact_duration"`
	FirstSeen         string   `json:"first_seen"`
	LastSeen          string   `json:"last_seen"`
}

type PGActivityStatFilter struct {
	InstanceID string
	StartTime  string
	EndTime    string
	PDb        string
	PUser      string
	OrderBy    string
	Offset     int
	Limit      int
}

// PGActivitySummaryFilter 可视汇总查询条件。
type PGActivitySummaryFilter struct {
	InstanceID string
	StartTime  string
	EndTime    string
	PDb        string
	PUser      string
	TopN       int
}

type PGActivityKV struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

type PGActivityDurationBucket struct {
	Label string   `json:"label"`
	Min   *float64 `json:"min"`
	Max   *float64 `json:"max"`
	Count int64    `json:"count"`
}

type PGActivityTrendPoint struct {
	Time  string `json:"time"`
	Count int64  `json:"count"`
}

type PGActivitySummary struct {
	StateDistribution []PGActivityKV             `json:"state_distribution"`
	DurationBuckets   []PGActivityDurationBucket `json:"duration_buckets"`
	TopDatabases      []PGActivityKV             `json:"top_databases"`
	TopUsers          []PGActivityKV             `json:"top_users"`
	TopInstances      []PGActivityKV             `json:"top_instances"`
	Trend             []PGActivityTrendPoint     `json:"trend"`
}

type PGSession struct {
	PID             int64  `json:"pid"`
	Datname         string `json:"datname"`
	Usename         string `json:"usename"`
	State           string `json:"state"`
	Query           string `json:"query"`
	ApplicationName string `json:"application_name"`
	ClientAddr      string `json:"client_addr"`
	BackendStart    string `json:"backend_start"`
	Duration        string `json:"duration"`
	WaitEventType   string `json:"wait_event_type"`
	WaitEvent       string `json:"wait_event"`
}

type PGLock struct {
	LockType string `json:"locktype"`
	Database string `json:"database"`
	PID      int64  `json:"pid"`
	Mode     string `json:"mode"`
	Relation string `json:"relation"`
}

// QueryAudit 是一次交互式查询（库 / Kafka / Redis）的审计记录。
type QueryAudit struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	UserID       string `json:"user_id,omitempty"`
	Username     string `json:"username"`
	DisplayName  string `json:"display_name,omitempty"`
	IP           string `json:"ip"`
	UserAgent    string `json:"user_agent,omitempty"`
	InstanceID   string `json:"instance_id,omitempty"`
	InstanceName string `json:"instance_name,omitempty"`
	DBType       string `json:"db_type,omitempty"`
	Target       string `json:"target,omitempty"`
	Statement    string `json:"statement"`
	Success      bool   `json:"success"`
	Message      string `json:"message,omitempty"`
	RowCount     int    `json:"row_count"`
	ElapsedMS    int64  `json:"elapsed_ms"`
	CreatedAt    string `json:"created_at"`
}

// HACluster 高可用集群登记（独立于 Instance，不改动实例表语义）
type HACluster struct {
	ID                   string     `json:"id"`
	TenantID             string     `json:"tenant_id,omitempty"`
	Name                 string     `json:"name"`
	Engine               string     `json:"engine"` // mysql | postgres | redis
	EnvID                string     `json:"env_id"`
	Mode                 string     `json:"mode"` // async_repl | patroni | sentinel | manual
	ControllerID         string     `json:"controller_id,omitempty"`
	StandbyControllerIDs []string   `json:"standby_controller_ids,omitempty"`
	MaintStatus          string     `json:"maint_status"` // normal | maintaining
	VIP                  string     `json:"vip,omitempty"`
	Remark               string     `json:"remark,omitempty"`
	CreatedBy            string     `json:"created_by,omitempty"`
	UpdatedBy            string     `json:"updated_by,omitempty"`
	CreatedAt            string     `json:"created_at"`
	UpdatedAt            string     `json:"updated_at"`
	Members              []HAMember `json:"members,omitempty"`
}

type HAMember struct {
	ID         string `json:"id"`
	ClusterID  string `json:"cluster_id"`
	InstanceID string `json:"instance_id"`
	Role       string `json:"role"` // primary | replica | sentinel | unknown
	Priority   int    `json:"priority"`
	Enabled    bool   `json:"enabled"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
	// 展示用（非表字段）
	InstanceName string `json:"instance_name,omitempty"`
	Host         string `json:"host,omitempty"`
	Port         int    `json:"port,omitempty"`
	DBType       string `json:"db_type,omitempty"`
}

type HAController struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Engine      string `json:"engine"` // mysql | postgres | redis
	Kind        string `json:"kind"`   // orchestrator | patroni | sentinel | mha
	EnvID       string `json:"env_id"`
	BaseURL     string `json:"base_url"`
	Username    string `json:"username,omitempty"`
	PasswordEnc string `json:"-"`
	PasswordSet bool   `json:"password_set"`
	AgentID     string `json:"agent_id,omitempty"` // 关联配置中心平台 Agent
	Remark      string `json:"remark,omitempty"`
	CreatedBy   string `json:"created_by,omitempty"`
	UpdatedBy   string `json:"updated_by,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
	// 展示用：被多少集群引用为主控 / 备控
	RefPrimary int `json:"ref_primary,omitempty"`
	RefStandby int `json:"ref_standby,omitempty"`
}

type HATask struct {
	ID               string `json:"id"`
	ClusterID        string `json:"cluster_id"`
	IssueID          string `json:"issue_id,omitempty"`
	Action           string `json:"action"` // switchover | enter_maint | exit_maint
	Status           string `json:"status"`
	TargetInstanceID string `json:"target_instance_id,omitempty"`
	PrecheckJSON     string `json:"precheck_json,omitempty"`
	StepsJSON        string `json:"steps_json,omitempty"`
	ResultJSON       string `json:"result_json,omitempty"`
	Operator         string `json:"operator,omitempty"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
	ClusterName      string `json:"cluster_name,omitempty"`
	Engine           string `json:"engine,omitempty"`
}

const (
	IssueTypeDDL           = "ddl"
	IssueTypeDML           = "dml"
	IssueTypeDCL           = "dcl"
	IssueTypeDQL           = "dql"
	IssueTypeKafka         = "kafka"
	IssueTypeCDC           = "cdc_sync"
	IssueTypeArchive       = "archive"
	IssueTypeFlashback     = "flashback"
	IssueTypeMigration     = "migration"
	IssueTypeMullang       = "mullang"
	IssueTypeCreateDB      = "createdb"
	IssueTypeRedis         = "redis"
	IssueTypeHA            = "ha"
	IssueTypeBackupRestore = "backup_restore"
	IssueTypeSrCluster     = "sr_cluster"
	IssueTypeDbDeploy      = "db_deploy"

	StatusPending        = "pending"
	StatusApproved       = "approved"
	StatusRejected       = "rejected"
	StatusCanceled       = "canceled"
	StatusDeleted        = "deleted"
	StatusExecuted       = "executed"
	StatusFailed         = "failed"
	StatusRunning        = "running"
	StatusPaused         = "paused"
	StatusInit           = "init"
	StatusScheduled      = "scheduled"
	StatusCompleted      = "completed"
	StatusPreChecking    = "pre_checking"
	StatusPreCheckFailed = "pre_check_failed"
	StatusSucceeded      = "succeeded"
	StatusReinitializing = "reinitializing"
)
