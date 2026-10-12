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

	"auto-dba/internal/aiops"
	"auto-dba/internal/store"
)

// ChatImage 附图（Cursor prompt.images / OpenAI vision）。
type ChatImage struct {
	Data     string `json:"data"`      // base64，可带 data:image/...;base64, 前缀
	MimeType string `json:"mime_type"` // image/png|jpeg|gif|webp
}

// ChatRequest 助手请求。
type ChatRequest struct {
	Message      string      `json:"message"`
	AlertID      string      `json:"alert_id,omitempty"`
	Model        string      `json:"model,omitempty"` // 可选，须在列表内；空则默认；Cursor 可用 auto
	Images       []ChatImage `json:"images,omitempty"`
	SessionID    string      `json:"session_id,omitempty"`    // 多轮会话；空则新建
	ForceRefresh bool        `json:"force_refresh,omitempty"` // true 时跳过问答缓存
	TenantID     string      `json:"-"`
}

// ChatResponse 助手响应。
type ChatResponse struct {
	Mode      string           `json:"mode"` // offline | rag | llm | cache
	Answer    string           `json:"answer"`
	Model     string           `json:"model,omitempty"`
	Provider  string           `json:"provider,omitempty"`
	SessionID string           `json:"session_id,omitempty"`
	Cached    bool             `json:"cached,omitempty"`
	Learned   bool             `json:"learned,omitempty"`   // 已确认学习入库
	Learnable bool             `json:"learnable,omitempty"` // 知识库未强命中，可点「学习」入库
	Question  string           `json:"question,omitempty"`  // 便于前端学习按钮带回原问
	Sources   []string         `json:"sources,omitempty"`
	Cases     []aiops.CaseHint `json:"cases,omitempty"`
	RAG       []aiops.RAGHit   `json:"rag,omitempty"`
}

// ModelsInfo GET /copilot/models。
type ModelsInfo struct {
	Enabled  bool          `json:"enabled"`
	Provider string        `json:"provider"`
	Default  string        `json:"default"`
	Models   []ModelOption `json:"models"`
}

// Service Copilot。
type Service struct {
	Store *store.DB
	AIOps *aiops.Service
}

func New(db *store.DB) *Service {
	return &Service{Store: db, AIOps: aiops.New(db)}
}

func (s *Service) Models(ctx context.Context) ModelsInfo {
	cfg := LoadConfig(s.Store)
	info := ModelsInfo{
		Enabled:  cfg.Enabled(),
		Provider: cfg.Provider,
		Default:  cfg.DefaultModel(),
		Models:   cfg.Models,
	}
	if cfg.Enabled() && cfg.Provider == ProviderCursor {
		if remote, err := cursorListModels(ctx, cfg); err == nil && len(remote) > 0 {
			// 与本地配置取交：若本地配置了显式列表，优先保留配置顺序并过滤远程
			if len(cfg.Models) > 0 && !(len(cfg.Models) == 1 && (cfg.Models[0].Value == "auto" || cfg.Models[0].Value == "gpt-4o-mini")) {
				want := map[string]bool{}
				for _, m := range cfg.Models {
					want[m.Value] = true
				}
				var filtered []ModelOption
				for _, m := range remote {
					if want[m.Value] || m.Value == "auto" {
						filtered = append(filtered, m)
					}
				}
				if len(filtered) > 0 {
					info.Models = filtered
				} else {
					info.Models = remote
				}
			} else {
				info.Models = remote
			}
			info.Default = info.Models[0].Value
		}
	}
	return info
}

func (s *Service) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	imgs, err := normalizeChatImages(req.Images)
	if err != nil {
		return nil, err
	}
	req.Images = imgs
	if strings.TrimSpace(req.Message) == "" && len(req.Images) == 0 {
		return nil, fmt.Errorf("请输入问题或粘贴/上传图片")
	}
	if strings.TrimSpace(req.Message) == "" && len(req.Images) > 0 {
		req.Message = "请识别图中监控/告警/日志信息，结合 DBA 知识给出排查建议"
	}
	cfg := LoadConfig(s.Store)
	model, err := cfg.ResolveModel(req.Model)
	if err != nil {
		return nil, err
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		sessionID = newSessionID()
	}
	req.SessionID = sessionID

	offline, err := s.offlineAnswer(ctx, req)
	if err != nil {
		return nil, err
	}
	offline.SessionID = sessionID

	// 高分命中知识库优先于问答缓存与大模型（ForceRefresh / 附图除外）
	ragHits := aiops.FilterRelevantRAGHits(req.Message, offline.RAG)
	if !req.ForceRefresh && len(req.Images) == 0 && aiops.StrongRAGHit(ragHits) {
		ragAns := &ChatResponse{
			Mode: "rag", Answer: aiops.FormatRAGAnswer(req.Message, ragHits),
			SessionID: sessionID, Sources: append([]string{"knowledge_rag"}, offline.Sources...),
			Cases: offline.Cases, RAG: ragHits,
		}
		if s.Store != nil {
			s.persistSessionTurns(sessionID, req, "builtin", "knowledge_rag", req.Message, ragAns.Answer)
			fp := store.CopilotCacheFingerprint(req.TenantID, req.AlertID, req.Message)
			_ = s.Store.UpsertCopilotCache(&store.CopilotCacheRow{
				Fingerprint: fp, TenantID: req.TenantID, AlertID: req.AlertID,
				Question: req.Message, Answer: ragAns.Answer, Model: "knowledge_rag", Provider: "builtin",
			})
		}
		return ragAns, nil
	}

	// 无附图时查问答缓存，避免重复调大模型
	if !req.ForceRefresh && len(req.Images) == 0 && s.Store != nil {
		fp := store.CopilotCacheFingerprint(req.TenantID, req.AlertID, req.Message)
		if row, err := s.Store.GetCopilotCache(fp); err == nil && row != nil && strings.TrimSpace(row.Answer) != "" {
			_ = s.Store.TouchCopilotCache(fp)
			s.persistSessionTurns(sessionID, req, row.Provider, row.Model, req.Message, row.Answer)
			return &ChatResponse{
				Mode: "cache", Answer: row.Answer, Model: row.Model, Provider: row.Provider,
				SessionID: sessionID, Cached: true,
				Sources: []string{"copilot_cache"},
			}, nil
		}
	}

	if !cfg.Enabled() {
		offline.Mode = "offline"
		if len(req.Images) > 0 {
			offline.Answer += "\n\n（附图需启用 LLM/Cursor Agent 才能识图分析）"
		}
		s.persistSessionTurns(sessionID, req, "offline", "", req.Message, offline.Answer)
		return offline, nil
	}
	llmAns, err := s.llmAnswer(ctx, cfg, model, req, offline)
	if err != nil {
		offline.Mode = "offline"
		offline.SessionID = sessionID
		offline.Answer = offline.Answer + "\n\n（LLM 调用失败，已回退离线摘要：" + err.Error() + "）"
		s.persistSessionTurns(sessionID, req, "offline", model, req.Message, offline.Answer)
		return offline, nil
	}
	llmAns.Mode = "llm"
	if strings.TrimSpace(llmAns.Model) == "" {
		llmAns.Model = model
	}
	llmAns.Provider = cfg.Provider
	llmAns.SessionID = sessionID
	llmAns.Sources = append(llmAns.Sources, offline.Sources...)
	llmAns.Cases = offline.Cases
	llmAns.RAG = offline.RAG
	llmAns.Question = req.Message
	// 大模型成功回答且无附图：展示「学习」供人工确认入库。
	// 强制刷新时即使知识库已有强命中也允许学习（用模型答案更新/补充库内案例）。
	llmAns.Learnable = len(req.Images) == 0 && (req.ForceRefresh || !aiops.StrongRAGHit(ragHits))

	s.persistSessionTurns(sessionID, req, cfg.Provider, llmAns.Model, req.Message, llmAns.Answer)
	if len(req.Images) == 0 && s.Store != nil {
		fp := store.CopilotCacheFingerprint(req.TenantID, req.AlertID, req.Message)
		_ = s.Store.UpsertCopilotCache(&store.CopilotCacheRow{
			Fingerprint: fp, TenantID: req.TenantID, AlertID: req.AlertID,
			Question: req.Message, Answer: llmAns.Answer, Model: llmAns.Model, Provider: cfg.Provider,
		})
	}
	return llmAns, nil
}

func newSessionID() string {
	return fmt.Sprintf("cps-%d", time.Now().UnixNano())
}

// persistSessionTurns 落库会话元数据 + 本轮问答，供刷新后历史恢复。
func (s *Service) persistSessionTurns(sessionID string, req ChatRequest, provider, model, userMsg, assistantMsg string) {
	if s == nil || s.Store == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	_ = s.Store.EnsureCopilotSession(sessionID, req.TenantID, provider, model, userMsg)
	_ = s.Store.AddCopilotTurn(sessionID, "user", userMsg)
	_ = s.Store.AddCopilotTurn(sessionID, "assistant", assistantMsg)
}

func (s *Service) offlineAnswer(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	resp, ctxParts := s.buildDiagContext(ctx, req)
	resp.Mode = "offline"
	var parts []string
	parts = append(parts, "【离线 DBA 助手】基于平台 Context Bundle / 知识库（未调用大模型）。")
	parts = append(parts, ctxParts...)
	parts = append(parts, "说明：请在 Nacos/本地 YAML 配置 copilot：Cursor 用 base_url=https://api.cursor.com 与 api_key（crsr_…）；通义等兼容模型用 openai 网关 /v1 + provider: openai，勿与 Cursor 混用。")
	resp.Answer = strings.Join(parts, "\n")
	return resp, nil
}

// buildDiagContext 收集告警 Bundle + RAG，供离线展示与 LLM prompt（不含配置说明）。
func (s *Service) buildDiagContext(ctx context.Context, req ChatRequest) (*ChatResponse, []string) {
	resp := &ChatResponse{}
	var parts []string
	parts = append(parts, "用户问题："+strings.TrimSpace(req.Message))

	if id := strings.TrimSpace(req.AlertID); id != "" && s.AIOps != nil {
		b, err := s.AIOps.BuildAlertContextBundle(ctx, id, req.TenantID)
		if err == nil && b != nil {
			parts = append(parts, "诊断摘要："+b.Summary)
			if len(b.Cases) > 0 {
				parts = append(parts, "对齐案例：")
				for i, c := range b.Cases {
					if i >= 3 {
						break
					}
					parts = append(parts, fmt.Sprintf("- %s：%s → %s", c.Title, trim(c.RootCauseTemplate, 120), trim(c.ActionTemplate, 120)))
				}
				resp.Cases = b.Cases
			}
			resp.Sources = append(resp.Sources, b.Sources...)
			resp.Sources = append(resp.Sources, "alert_context_bundle")
		}
	}

	if s.AIOps != nil {
		hits, _ := s.AIOps.RAGSearch(ctx, req.Message, 5)
		if len(hits) > 0 {
			parts = append(parts, "知识库检索（请引用下列案例标题）：")
			for _, h := range hits {
				parts = append(parts, fmt.Sprintf("- [%s] %s（score=%.1f）%s", h.Source, h.Title, h.Score, h.Snippet))
			}
			resp.RAG = hits
			resp.Sources = append(resp.Sources, "rag")
		}
	}
	return resp, parts
}

func (s *Service) llmAnswer(ctx context.Context, cfg Config, model string, req ChatRequest, offline *ChatResponse) (*ChatResponse, error) {
	// 重新拼干净上下文，勿把「离线助手 / 配置说明」喂给模型
	_, ctxParts := s.buildDiagContext(ctx, req)
	sys := "你是 KeePlus DBA Copilot，面向 PostgreSQL/MySQL 运维问答。" +
		"优先依据下方「知识库检索 / 对齐案例 / 诊断摘要」作答；有案例时必须点名引用案例标题。" +
		"若附带截图，请先识别图中指标/告警/日志文字，再结合知识库给出排查建议。" +
		"若缺少实例实时指标，仍给出通用排查步骤，并明确标注「需在目标库核实」；禁止臆造具体数值。" +
		"不要建议无审批的写库、杀主库进程或危险操作。"
	user := strings.Join(ctxParts, "\n") + "\n\n请用中文给出简洁排查步骤（不超过 8 条）。"
	if len(req.Images) > 0 {
		user += fmt.Sprintf("\n（已附 %d 张截图，请结合图片内容分析。）", len(req.Images))
	}
	prompt := sys + "\n\n" + user

	if cfg.Provider == ProviderCursor {
		var reuseAgent string
		if s.Store != nil && strings.TrimSpace(req.SessionID) != "" {
			if sess, err := s.Store.GetCopilotSession(req.SessionID); err == nil && sess != nil {
				reuseAgent = sess.CursorAgentID
			}
		}
		ans, err := s.cursorAnswer(ctx, cfg, model, prompt, req.Images, reuseAgent)
		if err != nil {
			return nil, err
		}
		if s.Store != nil && strings.TrimSpace(req.SessionID) != "" && strings.TrimSpace(ans.agentID) != "" {
			_ = s.Store.SaveCopilotSession(&store.CopilotSession{
				ID: req.SessionID, TenantID: req.TenantID, Provider: ProviderCursor,
				CursorAgentID: ans.agentID, Model: ans.Model,
			})
		}
		out := ans.ChatResponse
		return &out, nil
	}
	// OpenAI：附带近期会话轮次作记忆
	if s.Store != nil && strings.TrimSpace(req.SessionID) != "" {
		if turns, err := s.Store.ListCopilotTurns(req.SessionID, 12); err == nil && len(turns) > 0 {
			var hist []string
			for _, t := range turns {
				hist = append(hist, t.Role+": "+trim(t.Content, 800))
			}
			user = "近期对话：\n" + strings.Join(hist, "\n") + "\n\n" + user
		}
	}
	return s.openaiAnswer(ctx, cfg, model, sys, user, req.Images)
}

func (s *Service) openaiAnswer(ctx context.Context, cfg Config, model, sys, user string, images []ChatImage) (*ChatResponse, error) {
	userContent := any(user)
	if len(images) > 0 {
		parts := []map[string]any{{"type": "text", "text": user}}
		for _, im := range images {
			parts = append(parts, map[string]any{
				"type": "image_url",
				"image_url": map[string]string{
					"url": "data:" + im.MimeType + ";base64," + im.Data,
				},
			})
		}
		userContent = parts
	}
	body := map[string]any{
		"model": model,
		"messages": []any{
			map[string]string{"role": "system", "content": sys},
			map[string]any{"role": "user", "content": userContent},
		},
		"temperature": 0.2,
	}
	raw, _ := json.Marshal(body)
	url := cfg.BaseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	client := &http.Client{Timeout: 45 * time.Second}
	res, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		msg := fmt.Sprintf("llm status=%d url=%s %s", res.StatusCode, url, trim(string(b), 200))
		if res.StatusCode == 401 {
			msg += authFailureHint(ProviderOpenAI)
		}
		return nil, fmt.Errorf("%s", msg)
	}
	if len(b) > 0 && b[0] == '<' {
		return nil, fmt.Errorf("上游返回 HTML（请检查 copilot.base_url；Cursor 应为 https://api.cursor.com，勿填 dashboard）。url=%s body=%s", url, trim(string(b), 120))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(b, &parsed); err != nil {
		return nil, fmt.Errorf("llm json: %w url=%s body=%s", err, url, trim(string(b), 120))
	}
	if len(parsed.Choices) == 0 || strings.TrimSpace(parsed.Choices[0].Message.Content) == "" {
		return nil, fmt.Errorf("llm empty response")
	}
	return &ChatResponse{Answer: parsed.Choices[0].Message.Content, Sources: []string{"llm", "alert_context_bundle"}}, nil
}

func trim(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
