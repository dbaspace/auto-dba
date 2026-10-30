package sqlexec

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// SubmitExport 下发导出任务，立刻返回任务号。
func SubmitExport(ctx context.Context, baseURL, user, pass string, req ExportRequest) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	if err := call(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/v1/sql/export-jobs", user, pass, req, &out); err != nil {
		return "", err
	}
	if strings.TrimSpace(out.ID) == "" {
		return "", fmt.Errorf("执行器没有返回导出任务号")
	}
	return out.ID, nil
}

// GetExport 读取导出任务状态。
func GetExport(ctx context.Context, baseURL, user, pass, id string) (ExportView, error) {
	var out ExportView
	err := call(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/v1/sql/export-jobs/"+id, user, pass, nil, &out)
	return out, err
}

// StopExport 取消导出任务。
func StopExport(ctx context.Context, baseURL, user, pass, id string) error {
	return call(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/v1/sql/export-jobs/"+id+"/stop", user, pass, map[string]any{}, nil)
}

// FetchExportFile 将执行器上的导出文件流式写入 destPath。
func FetchExportFile(ctx context.Context, baseURL, user, pass, id, destPath string) (int64, error) {
	url := strings.TrimRight(baseURL, "/") + "/v1/sql/export-jobs/" + id + "/file"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("地址不通: %s", err.Error())
	}
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	client := &http.Client{Timeout: 15 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("地址不通: %s", err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return 0, fmt.Errorf("账号密码不对")
	}
	if resp.StatusCode == http.StatusNotFound {
		return 0, fmt.Errorf("%w：导出文件不存在", ErrJobNotFound)
	}
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		msg := strings.TrimSpace(string(raw))
		if msg == "" {
			msg = resp.Status
		}
		var wrapped struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &wrapped)
		if wrapped.Message != "" {
			msg = wrapped.Message
		} else if wrapped.Error != "" {
			msg = wrapped.Error
		}
		return 0, fmt.Errorf("执行器返回 %s", msg)
	}
	f, err := os.Create(destPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	n, err := io.Copy(f, resp.Body)
	if err != nil {
		_ = os.Remove(destPath)
		return n, err
	}
	return n, nil
}
