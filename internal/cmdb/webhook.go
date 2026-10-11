package cmdb

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"
)

const ArgCmdbWebhookURL = "cmdb_webhook_url"
const ArgCmdbSyncAutoApply = "cmdb_sync_auto_apply"

// WebhookEvent CI/关系变更事件（可选外发）。
type WebhookEvent struct {
	Event     string `json:"event"`
	CIType    string `json:"ci_type,omitempty"`
	CIID      string `json:"ci_id,omitempty"`
	Actor     string `json:"actor,omitempty"`
	At        string `json:"at"`
	Detail    any    `json:"detail,omitempty"`
}

// EmitWebhook 若配置了 cmdb_webhook_url 则异步 POST JSON。
func (s *Service) EmitWebhook(event string, ciType, ciID, actor string, detail any) {
	if s == nil || s.Store == nil {
		return
	}
	url, err := s.Store.GetGlobalArg(ArgCmdbWebhookURL)
	if err != nil || strings.TrimSpace(url) == "" {
		return
	}
	payload := WebhookEvent{
		Event: event, CIType: ciType, CIID: ciID, Actor: actor,
		At: time.Now().UTC().Format(time.RFC3339), Detail: detail,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSpace(url), bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Printf("cmdb webhook: %v", err)
			return
		}
		_ = resp.Body.Close()
	}()
}
