package cloud

import (
	"context"
	"errors"
	"time"
)

const VendorTencent = "tencent"

var (
	ErrUnsupported = errors.New("暂未支持")
	ErrRateLimit   = errors.New("云厂商限流")
	ErrNoApproval  = errors.New("没有审核记录，拒绝调用变更接口")
	ErrNoAccount   = errors.New("未绑定云账号")
	ErrNoKey       = errors.New("云账号缺少对应密钥")
	ErrNotBound    = errors.New("当前用户未绑定该云账号")
)

// Cred 是一套账号上的一把密钥。Secret 不写入日志。
type Cred struct {
	AccountID string
	Vendor    string
	Region    string
	SecretID  string
	SecretKey string
	Role      string
}

type Instance struct {
	CloudID  string
	Name     string
	Region   string
	Status   string
	CPU      int
	MemoryMB int
	DiskGB   int
	DiskUsed int
	Version  string
	Zone     string
	PayType  string
	ExpireAt string
	Host     string
	Port     int
	Extras   map[string]any
}

type Spec struct {
	CPU      int    `json:"cpu"`
	MemoryMB int    `json:"memory_mb"`
	Label    string `json:"label"`
}

type Quote struct {
	Amount float64 `json:"amount"`
	Detail string  `json:"detail"`
}

type SlowLog struct {
	Time       time.Time
	DBName     string
	User       string
	Client     string
	DurationMS int
	SQL        string
	Raw        string
}

type Param struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Event struct {
	Type    string
	Message string
	At      time.Time
}

type Task struct {
	ID     string
	Status string // running | success | failed
	Detail string
}

type CreateInput struct {
	Region   string
	Zone     string
	CPU      int
	MemoryMB int
	DiskGB   int
	Name     string
	Admin    string
	Password string
	Period   int
}

type SpecInput struct {
	CloudID      string
	CPU          int
	MemoryMB     int
	DiskGB       int
	InstanceType string // 主机变配机型，如 S5.MEDIUM4
}

// Reader 只包含查询。同步任务的参数类型是 Reader，编译期调不到变更方法。
type Reader interface {
	Vendor() string
	Caps() Caps
	ListInstances(ctx context.Context, cred Cred) ([]Instance, error)
	ListSpecs(ctx context.Context, cred Cred, zone string) ([]Spec, error)
	QuoteCreate(ctx context.Context, cred Cred, in CreateInput) (Quote, error)
	QuoteUpgrade(ctx context.Context, cred Cred, in SpecInput) (Quote, error)
	ListSlowLogs(ctx context.Context, cred Cred, cloudID string, from, to time.Time, offset, limit int) ([]SlowLog, int, error)
	ListParameters(ctx context.Context, cred Cred, cloudID string) ([]Param, error)
	ListEvents(ctx context.Context, cred Cred, cloudID string, from, to time.Time) ([]Event, error)
	DescribeTask(ctx context.Context, cred Cred, cloudID, taskID string) (Task, error)
}

// Writer 只给工单执行器。不包含隔离、销毁、备份恢复、账号口令和外网开关。
type Writer interface {
	Vendor() string
	Caps() Caps
	Create(ctx context.Context, cred Cred, in CreateInput) (Task, error)
	ModifySpec(ctx context.Context, cred Cred, in SpecInput) (Task, error)
	Restart(ctx context.Context, cred Cred, cloudID string) (Task, error)
	Switchover(ctx context.Context, cred Cred, cloudID string) (Task, error)
	ModifyParameters(ctx context.Context, cred Cred, cloudID string, params []Param) (Task, error)
}

type Caps struct {
	ScaleDiskDown bool `json:"scale_disk_down"`
	Switchover    bool `json:"switchover"`
	Parameters    bool `json:"parameters"`
	Restart       bool `json:"restart"`
	Create        bool `json:"create"`
	SlowLog       bool `json:"slow_log"`
}
