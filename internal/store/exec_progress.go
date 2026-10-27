package store

import (
	"encoding/json"
	"strings"
)

// ExecProgress 工单执行中的小进度，列表和详情轮询用，不放整份执行结果。
type ExecProgress struct {
	Total           int    `json:"total"`
	Done            int    `json:"done"`
	Failed          int    `json:"failed"`
	CurrentInstance string `json:"current_instance,omitempty"`
	CurrentDB       string `json:"current_db,omitempty"`
	InstanceID      string `json:"instance_id,omitempty"`
	Host            string `json:"host,omitempty"`
	DBType          string `json:"db_type,omitempty"`
	StmtIndex       int    `json:"stmt_index"`
	StmtTotal       int    `json:"stmt_total"`
	SessionID       int64  `json:"session_id,omitempty"`
	Engine          string `json:"engine,omitempty"`
	Phase           string `json:"phase,omitempty"`
	Env             string `json:"env,omitempty"`
	Executor        string `json:"executor,omitempty"`
	// 远程执行器诊断。密码不下发到进度里。
	JobID       string `json:"job_id,omitempty"`
	ExecutorURL string `json:"executor_url,omitempty"`
	TargetUser  string `json:"target_user,omitempty"`
	LastGetAt   string `json:"last_get_at,omitempty"`
	GetFailures int    `json:"get_failures,omitempty"`
	LastError   string `json:"last_error,omitempty"`
}

// Fraction 进度条比例。preparing 返回不确定。单库按语句，多库按已结束库加当前库的语句比例。
func (p ExecProgress) Fraction() (float64, bool) {
	if p.Phase == "preparing" {
		return 0, false
	}
	if p.Total <= 1 {
		if p.StmtTotal <= 0 {
			return 0, true
		}
		v := float64(p.StmtIndex) / float64(p.StmtTotal)
		if v < 0 {
			v = 0
		}
		if v > 1 {
			v = 1
		}
		return v, true
	}
	frac := float64(p.Done)
	if p.StmtTotal > 0 && p.StmtIndex > 0 && p.Done < p.Total {
		frac += float64(p.StmtIndex) / float64(p.StmtTotal)
	}
	v := frac / float64(p.Total)
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	return v, true
}

func ParseExecProgress(raw string) *ExecProgress {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var p ExecProgress
	if json.Unmarshal([]byte(raw), &p) != nil {
		return nil
	}
	return &p
}
