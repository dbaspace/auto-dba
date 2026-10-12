package copilot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCursorAnswerNoRepoPoll(t *testing.T) {
	var createHits, getHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer crsr_") {
			http.Error(w, "unauthorized", 401)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{{"id": "composer-2"}, {"id": "gpt-4o-mini"}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/agents":
			createHits++
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if _, ok := body["repos"]; ok {
				t.Fatalf("no-repo agent must omit repos")
			}
			mm, _ := body["model"].(map[string]any)
			if str(mm["id"]) != "composer-2" {
				t.Fatalf("want composer-2 first, got %#v", body["model"])
			}
			prompt, _ := body["prompt"].(map[string]any)
			if strings.TrimSpace(str(prompt["text"])) == "" {
				t.Fatalf("missing prompt.text")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"agent": map[string]any{"id": "bc-test"},
				"run":   map[string]any{"id": "run-test", "status": "CREATING"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/agents/bc-test/runs/run-test":
			getHits++
			if getHits == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"error":"Service Unavailable","statusCode":503}`))
				return
			}
			status := "RUNNING"
			result := ""
			if getHits >= 3 {
				status = "FINISHED"
				result = "1. 查 max_connections\n2. 查空闲会话"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "run-test", "agentId": "bc-test", "status": status, "result": result,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	svc := &Service{}
	cfg := Config{BaseURL: srv.URL, APIKey: "crsr_test", Provider: ProviderCursor}
	resp, err := svc.cursorAnswer(context.Background(), cfg, "auto", "请排查 PG 连接打满", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if createHits != 1 || getHits < 3 {
		t.Fatalf("create=%d get=%d", createHits, getHits)
	}
	if !strings.Contains(resp.Answer, "max_connections") {
		t.Fatalf("answer=%s", resp.Answer)
	}
	if resp.Model != "composer-2" {
		t.Fatalf("model=%s", resp.Model)
	}
}

func TestCursorInvalidModelFallsBackToNext(t *testing.T) {
	var models []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{{"id": "gpt-5.5"}, {"id": "composer-2"}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/agents":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			mm, _ := body["model"].(map[string]any)
			id := str(mm["id"])
			models = append(models, id)
			if id == "gpt-5.5" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"code":"invalid_model","message":"not supported in your region"}}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"agent": map[string]any{"id": "bc-1"},
				"run":   map[string]any{"id": "run-1", "status": "CREATING"},
			})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/runs/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "run-1", "agentId": "bc-1", "status": "FINISHED", "result": "ok-fallback",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	svc := &Service{}
	cfg := Config{BaseURL: srv.URL, APIKey: "crsr_x", Provider: ProviderCursor}
	resp, err := svc.cursorAnswer(context.Background(), cfg, "gpt-5.5", "q", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) < 2 || models[0] != "gpt-5.5" || models[1] != "composer-2" {
		t.Fatalf("models=%v", models)
	}
	if !strings.Contains(resp.Answer, "ok-fallback") || resp.Model != "composer-2" {
		t.Fatalf("answer=%s model=%s", resp.Answer, resp.Model)
	}
}

func TestPreferCursorNativeModels(t *testing.T) {
	got := preferCursorNativeModels([]string{"gpt-4o-mini", "composer-2", "gpt-5.5", "claude-4-sonnet"})
	if got[0] != "composer-2" || got[1] != "claude-4-sonnet" {
		t.Fatalf("%v", got)
	}
}

func str(v any) string {
	s, _ := v.(string)
	return s
}
