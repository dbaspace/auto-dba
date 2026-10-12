package copilot

import (
	"context"
	"strings"
	"testing"

	"auto-dba/internal/aiops"
)

func TestOfflineAnswerUsesRAG(t *testing.T) {
	svc := &Service{AIOps: &aiops.Service{}}
	// 弱相关问题、且无 LLM 配置 → offline
	resp, err := svc.Chat(context.Background(), ChatRequest{Message: "今天天气如何与数据库无关xyz"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Mode != "offline" && resp.Mode != "rag" {
		t.Fatalf("mode=%s", resp.Mode)
	}
}

func TestPreferBuiltinRAGOverLLM(t *testing.T) {
	svc := &Service{AIOps: &aiops.Service{}}
	resp, err := svc.Chat(context.Background(), ChatRequest{Message: "PostgreSQL 连接打满怎么排查？"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Mode != "rag" {
		t.Fatalf("want mode=rag, got %s answer=%s", resp.Mode, resp.Answer)
	}
	if !strings.Contains(resp.Answer, "知识库直答") {
		t.Fatalf("answer=%s", resp.Answer)
	}
	if len(resp.RAG) == 0 {
		t.Fatal("expected rag hits")
	}
}

func TestBuildDiagContextForLLMOmitsConfigTip(t *testing.T) {
	svc := &Service{AIOps: &aiops.Service{}}
	resp, parts := svc.buildDiagContext(context.Background(), ChatRequest{Message: "PostgreSQL 连接打满怎么排查？"})
	joined := strings.Join(parts, "\n")
	if strings.Contains(joined, "copilot_base_url") || strings.Contains(joined, "离线 DBA") {
		t.Fatalf("llm context should not contain config tip: %s", joined)
	}
	if !strings.Contains(joined, "用户问题") {
		t.Fatalf("missing question: %s", joined)
	}
	if len(resp.RAG) == 0 {
		t.Fatal("expected RAG hits for 连接打满")
	}
}
