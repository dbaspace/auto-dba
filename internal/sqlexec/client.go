package sqlexec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ErrJobNotFound 表示执行器进程里没有这个任务（重启、过期或任务号不对）。
var ErrJobNotFound = errors.New("执行器上没有这个任务")

// Probe 访问执行器健康检查，并把失败原因写成页面能直接展示的话。
func Probe(ctx context.Context, baseURL, user, pass string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/healthz", nil)
	if err != nil {
		return fmt.Errorf("地址不通: %s", err.Error())
	}
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("地址不通: %s", err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("账号密码不对")
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("地址不通: HTTP %d", resp.StatusCode)
	}
	return nil
}

// Submit 下发任务，立刻返回任务号。语句在执行器当地跑。
func Submit(ctx context.Context, baseURL, user, pass string, job JobRequest) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	if err := call(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/v1/sql/jobs", user, pass, job, &out); err != nil {
		return "", err
	}
	if strings.TrimSpace(out.ID) == "" {
		return "", fmt.Errorf("执行器没有返回任务号")
	}
	return out.ID, nil
}

// Get 读取执行器上的进度。after 是已经写入工单的库数，响应只带其后的库和语句。
func Get(ctx context.Context, baseURL, user, pass, id string, after int) (JobView, error) {
	u := strings.TrimRight(baseURL, "/") + "/v1/sql/jobs/" + id
	if after > 0 {
		u += "?after=" + strconv.Itoa(after)
	}
	var out JobView
	err := call(ctx, http.MethodGet, u, user, pass, nil, &out)
	return out, err
}

// StopJob 让执行器取消任务并终止当前会话。
func StopJob(ctx context.Context, baseURL, user, pass, id string) error {
	return call(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/v1/sql/jobs/"+id+"/stop", user, pass, map[string]any{}, nil)
}

func call(ctx context.Context, method, url, user, pass string, body any, dest any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return fmt.Errorf("地址不通: %s", err.Error())
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("地址不通: %s", err.Error())
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("账号密码不对")
	}
	if resp.StatusCode == http.StatusNotFound {
		msg := strings.TrimSpace(string(raw))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("%w：%s", ErrJobNotFound, msg)
	}
	if resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(raw))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("执行器返回 %s", msg)
	}
	if dest != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, dest); err != nil {
			return err
		}
	}
	return nil
}
