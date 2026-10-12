package copilot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	cursorAgentTimeout = 180 * time.Second
	cursorPollInterval = 3 * time.Second
	cursorReqTimeout   = 45 * time.Second
)

// cursorAnswer 调用 Cursor Cloud Agents API（no-repo），轮询 run.result。
// 文档：POST /v1/agents，omit repos/env；GET /v1/agents/{id}/runs/{runId}。
//
// 注意：省略 model 时 Cursor 会用账号默认模型；若默认是 gpt-4o-mini 且区域不可用会 400。
// 因此 Auto 会从 GET /v1/models 显式挑选可用模型，并在 invalid_model 时换下一个。
type cursorAnswerResult struct {
	ChatResponse
	agentID string
}

func (s *Service) cursorAnswer(ctx context.Context, cfg Config, model string, promptText string, images []ChatImage, reuseAgentID string) (*cursorAnswerResult, error) {
	client := &http.Client{Timeout: cursorReqTimeout}
	candidates := s.cursorModelCandidates(ctx, cfg, model)
	var (
		agentID, runID, usedModel string
		err                       error
		fallbackNote              string
	)

	reuseAgentID = strings.TrimSpace(reuseAgentID)
	if reuseAgentID != "" {
		// 多轮：在已有 agent 上创建 run，复用对话记忆
		usedModel = "composer-2"
		if len(candidates) > 0 {
			usedModel = candidates[0]
		}
		if m := strings.TrimSpace(model); m != "" && !strings.EqualFold(m, "auto") {
			usedModel = m
		}
		runID, err = s.cursorCreateRun(ctx, client, cfg, reuseAgentID, usedModel, promptText, images)
		if err == nil {
			agentID = reuseAgentID
		}
		// agent 失效则下面走新建
	}

	if agentID == "" {
		for i, cand := range candidates {
			agentID, runID, err = s.cursorCreateAgent(ctx, client, cfg, cand, promptText, images)
			if err == nil {
				usedModel = cand
				if i > 0 {
					fallbackNote = fmt.Sprintf("（原模型不可用，已改用 %s）\n\n", cand)
				}
				break
			}
			if !isCursorInvalidModel(err) {
				return nil, err
			}
		}
		if err != nil {
			return nil, fmt.Errorf("%s；请在下拉框选择区域可用模型（见 https://cursor.com/docs/account/regions）tried=%v",
				friendlyCursorModelErr(err), candidates)
		}
	}

	text, err := s.cursorWaitRun(ctx, client, cfg, agentID, runID, usedModel)
	if err != nil {
		return nil, err
	}
	return &cursorAnswerResult{
		ChatResponse: ChatResponse{
			Answer:  fallbackNote + text,
			Model:   usedModel,
			Sources: []string{"cursor_agent", "alert_context_bundle"},
		},
		agentID: agentID,
	}, nil
}

func (s *Service) cursorWaitRun(ctx context.Context, client *http.Client, cfg Config, agentID, runID, usedModel string) (string, error) {
	deadline := time.Now().Add(cursorAgentTimeout)
	var lastTransient string
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if time.Now().After(deadline) {
			msg := fmt.Sprintf("cursor agent timeout agent=%s run=%s model=%s", agentID, runID, usedModel)
			if lastTransient != "" {
				msg += " last=" + lastTransient
			}
			return "", fmt.Errorf("%s", msg)
		}
		run, err := s.cursorGetRun(ctx, client, cfg, agentID, runID)
		if err != nil {
			if isTransientCursorErr(err) {
				lastTransient = err.Error()
				select {
				case <-ctx.Done():
					return "", ctx.Err()
				case <-time.After(cursorPollInterval):
				}
				continue
			}
			return "", err
		}
		st := strings.ToUpper(strings.TrimSpace(run.Status))
		switch st {
		case "FINISHED":
			text := strings.TrimSpace(run.Result)
			if text == "" {
				return "", fmt.Errorf("cursor run finished but empty result agent=%s run=%s", agentID, runID)
			}
			return text, nil
		case "ERROR", "CANCELLED", "EXPIRED":
			return "", fmt.Errorf("cursor run status=%s agent=%s run=%s", st, agentID, runID)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(cursorPollInterval):
		}
	}
}

// cursorModelCandidates 解析要尝试的模型 id 列表（显式 id，避免依赖 Cursor 账号默认）。
func (s *Service) cursorModelCandidates(ctx context.Context, cfg Config, want string) []string {
	want = strings.TrimSpace(want)
	remote, _ := cursorListModels(ctx, cfg)
	var ids []string
	for _, m := range remote {
		if m.Value == "" || strings.EqualFold(m.Value, "auto") {
			continue
		}
		ids = append(ids, m.Value)
	}
	ids = preferCursorNativeModels(ids)

	if want != "" && !strings.EqualFold(want, "auto") {
		out := []string{want}
		for _, id := range ids {
			if id != want {
				out = append(out, id)
			}
		}
		return out
	}
	if len(ids) > 0 {
		return ids
	}
	// 列表拉不到时的保底：Cursor 原生常用 id（仍可能因账号权限失败）
	return []string{"composer-2", "composer-2.5", "claude-4-sonnet"}
}

func preferCursorNativeModels(ids []string) []string {
	var native, other []string
	for _, id := range ids {
		low := strings.ToLower(id)
		// 区域内容易不可用的 OpenAI 系放到后面
		if strings.HasPrefix(low, "gpt-") || strings.Contains(low, "o1") || strings.Contains(low, "o3") {
			other = append(other, id)
			continue
		}
		native = append(native, id)
	}
	return append(native, other...)
}

func (s *Service) cursorCreateRun(ctx context.Context, client *http.Client, cfg Config, agentID, model, promptText string, images []ChatImage) (runID string, err error) {
	prompt := map[string]any{"text": promptText}
	if len(images) > 0 {
		var imgs []map[string]string
		for _, im := range images {
			imgs = append(imgs, map[string]string{"data": im.Data, "mimeType": im.MimeType})
		}
		prompt["images"] = imgs
	}
	body := map[string]any{"prompt": prompt}
	if m := strings.TrimSpace(model); m != "" && !strings.EqualFold(m, "auto") {
		body["model"] = map[string]any{"id": m}
	}
	raw, _ := json.Marshal(body)
	url := strings.TrimRight(cfg.BaseURL, "/") + "/v1/agents/" + agentID + "/runs"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	setCursorAuth(req, cfg.APIKey)
	res, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("cursor create run: %w", err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		msg := fmt.Sprintf("cursor create run status=%d %s", res.StatusCode, trim(string(b), 300))
		if res.StatusCode == 401 {
			msg += authFailureHint(ProviderCursor)
		}
		return "", fmt.Errorf("%s", msg)
	}
	var out struct {
		Run struct {
			ID string `json:"id"`
		} `json:"run"`
		ID string `json:"id"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return "", fmt.Errorf("cursor create run parse: %w", err)
	}
	id := strings.TrimSpace(out.Run.ID)
	if id == "" {
		id = strings.TrimSpace(out.ID)
	}
	if id == "" {
		return "", fmt.Errorf("cursor create run missing id: %s", trim(string(b), 200))
	}
	return id, nil
}

func (s *Service) cursorCreateAgent(ctx context.Context, client *http.Client, cfg Config, model, promptText string, images []ChatImage) (agentID, runID string, err error) {
	base := strings.TrimRight(cfg.BaseURL, "/")
	prompt := map[string]any{"text": promptText}
	if len(images) > 0 {
		var imgs []map[string]string
		for _, im := range images {
			imgs = append(imgs, map[string]string{
				"data":     im.Data,
				"mimeType": im.MimeType,
			})
		}
		prompt["images"] = imgs
	}
	createBody := map[string]any{
		"prompt": prompt,
		"name":   "KeePlus DBA",
	}
	// 始终显式传 model.id，避免省略后落到账号默认 gpt-4o-mini（区域常不可用）
	if m := strings.TrimSpace(model); m != "" && !strings.EqualFold(m, "auto") {
		createBody["model"] = map[string]any{"id": m}
	} else {
		return "", "", fmt.Errorf("cursor create requires explicit model id (got %q)", model)
	}
	raw, _ := json.Marshal(createBody)
	url := base + "/v1/agents"

	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", "", ctx.Err()
			case <-time.After(time.Duration(attempt) * 2 * time.Second):
			}
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
		if err != nil {
			return "", "", err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		setCursorAuth(httpReq, cfg.APIKey)

		res, err := client.Do(httpReq)
		if err != nil {
			lastErr = fmt.Errorf("cursor create agent: %w", err)
			continue
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode == 429 || res.StatusCode == 503 {
			lastErr = fmt.Errorf("cursor create status=%d %s", res.StatusCode, trim(string(b), 200))
			continue
		}
		if res.StatusCode >= 300 {
			msg := fmt.Sprintf("cursor create status=%d url=%s %s", res.StatusCode, url, trim(string(b), 400))
			if res.StatusCode == 401 {
				msg += authFailureHint(ProviderCursor)
			}
			return "", "", fmt.Errorf("%s", msg)
		}
		var created struct {
			Agent struct {
				ID string `json:"id"`
			} `json:"agent"`
			Run struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"run"`
		}
		if err := json.Unmarshal(b, &created); err != nil {
			return "", "", fmt.Errorf("cursor create parse: %w body=%s", err, trim(string(b), 200))
		}
		agentID = strings.TrimSpace(created.Agent.ID)
		runID = strings.TrimSpace(created.Run.ID)
		if agentID == "" || runID == "" {
			return "", "", fmt.Errorf("cursor create missing agent/run id: %s", trim(string(b), 200))
		}
		return agentID, runID, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("cursor create failed")
	}
	return "", "", lastErr
}

type cursorRun struct {
	ID       string `json:"id"`
	AgentID  string `json:"agentId"`
	Status   string `json:"status"`
	Result   string `json:"result"`
	Duration int    `json:"durationMs"`
}

type transientCursorErr struct{ msg string }

func (e *transientCursorErr) Error() string { return e.msg }

func isTransientCursorErr(err error) bool {
	_, ok := err.(*transientCursorErr)
	return ok
}

func isCursorInvalidModel(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "invalid_model") ||
		strings.Contains(s, "not supported in your region") ||
		strings.Contains(s, "model provider is not supported")
}

func friendlyCursorModelErr(err error) string {
	s := err.Error()
	if isCursorInvalidModel(err) {
		return "Cursor 模型在当前区域不可用（invalid_model）"
	}
	return s
}

func setCursorAuth(req *http.Request, apiKey string) {
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
}

func (s *Service) cursorGetRun(ctx context.Context, client *http.Client, cfg Config, agentID, runID string) (*cursorRun, error) {
	url := strings.TrimRight(cfg.BaseURL, "/") + "/v1/agents/" + agentID + "/runs/" + runID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	setCursorAuth(req, cfg.APIKey)
	res, err := client.Do(req)
	if err != nil {
		return nil, &transientCursorErr{msg: fmt.Sprintf("cursor get run network: %v", err)}
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode == 429 || res.StatusCode == 503 || res.StatusCode == 502 {
		return nil, &transientCursorErr{msg: fmt.Sprintf("cursor get run status=%d %s", res.StatusCode, trim(string(b), 120))}
	}
	if res.StatusCode >= 300 {
		msg := fmt.Sprintf("cursor get run status=%d %s", res.StatusCode, trim(string(b), 200))
		if res.StatusCode == 401 {
			msg += authFailureHint(ProviderCursor)
		}
		return nil, fmt.Errorf("%s", msg)
	}
	var run cursorRun
	if err := json.Unmarshal(b, &run); err != nil {
		return nil, fmt.Errorf("cursor get run parse: %w body=%s", err, trim(string(b), 200))
	}
	return &run, nil
}

// cursorListModels GET /v1/models。
func cursorListModels(ctx context.Context, cfg Config) ([]ModelOption, error) {
	url := strings.TrimRight(cfg.BaseURL, "/") + "/v1/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	setCursorAuth(req, cfg.APIKey)
	client := &http.Client{Timeout: 20 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		msg := fmt.Sprintf("cursor models status=%d %s", res.StatusCode, trim(string(b), 200))
		if res.StatusCode == 401 {
			msg += authFailureHint(ProviderCursor)
		}
		return nil, fmt.Errorf("%s", msg)
	}
	var wrap struct {
		Models []json.RawMessage `json:"models"`
		Items  []json.RawMessage `json:"items"`
	}
	_ = json.Unmarshal(b, &wrap)
	rawList := wrap.Models
	if len(rawList) == 0 {
		rawList = wrap.Items
	}
	if len(rawList) == 0 {
		var arr []json.RawMessage
		if err := json.Unmarshal(b, &arr); err == nil {
			rawList = arr
		}
	}
	var out []ModelOption
	for _, raw := range rawList {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			var id string
			if err2 := json.Unmarshal(raw, &id); err2 == nil && id != "" {
				out = append(out, ModelOption{Value: id, Label: id})
			}
			continue
		}
		id := firstString(m, "id", "value", "model")
		if id == "" {
			continue
		}
		label := firstString(m, "displayName", "name", "label")
		if label == "" {
			label = id
		}
		out = append(out, ModelOption{Value: id, Label: label})
	}
	out = dedupeModels(out)
	if len(out) == 0 {
		return nil, fmt.Errorf("cursor models empty")
	}
	return append([]ModelOption{{Value: "auto", Label: "Auto（Cursor 默认）"}}, out...), nil
}
