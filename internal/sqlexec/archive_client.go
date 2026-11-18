package sqlexec

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const archiveOldAgent = "执行器版本过旧，请到「配置 → 执行器」用本版包重新下发后再试"

// ArchiveDB 是执行器要在当地连上的库。口令只出现在下发请求里。
type ArchiveDB struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
	DBName   string `json:"db_name"`
	DBType   string `json:"db_type"`
}

// ArchiveRequest 一次归档任务。循环在执行器上按批自动提交。
type ArchiveRequest struct {
	Strategy     string     `json:"strategy"`
	DBType       string     `json:"db_type"`
	Table        string     `json:"table"`
	Condition    string     `json:"condition"`
	PGDeleteMode string     `json:"pg_delete_mode,omitempty"`
	ScriptType   string     `json:"script_type,omitempty"`
	OSSBucket    string     `json:"oss_bucket,omitempty"`
	TargetTable  string     `json:"target_table,omitempty"`
	CleanupAfter bool       `json:"cleanup_after,omitempty"`
	Source       ArchiveDB  `json:"source"`
	Target       *ArchiveDB `json:"target,omitempty"`
}

// ArchiveView 是轮询看到的进度，不含数据库口令。
type ArchiveView struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	Rounds      int    `json:"rounds"`
	DeletedRows int64  `json:"deleted_rows"`
	Message     string `json:"message,omitempty"`
	SQLPreview  string `json:"sql_preview,omitempty"`
	Error       string `json:"error,omitempty"`
}

func SubmitArchive(ctx context.Context, baseURL, user, pass string, req ArchiveRequest) (string, error) {
	var out ArchiveView
	err := call(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/v1/archive/jobs", user, pass, req, &out)
	if err != nil {
		if errors.Is(err, ErrJobNotFound) {
			return "", fmt.Errorf("%s", archiveOldAgent)
		}
		return "", err
	}
	if strings.TrimSpace(out.ID) == "" {
		return "", fmt.Errorf("执行器没有返回任务号")
	}
	return out.ID, nil
}

func GetArchive(ctx context.Context, baseURL, user, pass, id string) (ArchiveView, error) {
	var out ArchiveView
	err := call(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/v1/archive/jobs/"+id, user, pass, nil, &out)
	return out, err
}

func StopArchive(ctx context.Context, baseURL, user, pass, id string) error {
	err := callWithTimeout(ctx, 8*time.Second, http.MethodPost, strings.TrimRight(baseURL, "/")+"/v1/archive/jobs/"+id+"/stop", user, pass, map[string]any{}, nil)
	if errors.Is(err, ErrJobNotFound) {
		return nil
	}
	return err
}
