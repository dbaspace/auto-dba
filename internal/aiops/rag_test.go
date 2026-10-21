package aiops

import (
	"strings"
	"testing"
)

func TestRAGSearchBuiltin(t *testing.T) {
	svc := &Service{}
	hits, err := svc.RAGSearch(nil, "连接 打满 PGConnHigh", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("expected hits")
	}
}

func TestRAGSearchConnFullNaturalLanguage(t *testing.T) {
	svc := &Service{}
	hits, err := svc.RAGSearch(nil, "PostgreSQL 连接打满怎么排查？", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("expected hits for 连接打满")
	}
	if !StrongRAGHit(hits) {
		t.Fatalf("want strong hit, top=%+v", hits[0])
	}
	found := false
	for _, h := range hits {
		if strings.Contains(h.Title, "连接") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("want 连接 case, got %+v", hits)
	}
	ans := FormatRAGAnswer("PostgreSQL 连接打满怎么排查？", hits)
	if !strings.Contains(ans, "知识库直答") || !strings.Contains(ans, "连接") {
		t.Fatalf("bad format: %s", ans)
	}
}

func TestTokenizeCJKBigrams(t *testing.T) {
	toks := tokenize("PostgreSQL 连接打满怎么排查")
	joined := strings.Join(toks, ",")
	need := []string{"连接", "打满", "排查", "postgresql"}
	for _, n := range need {
		ok := false
		for _, t := range toks {
			if t == n || strings.Contains(t, n) {
				ok = true
				break
			}
		}
		if !ok {
			t.Fatalf("missing token %q in %s", n, joined)
		}
	}
	// 同义词扩展
	hasConn := false
	for _, t := range toks {
		if t == "连接使用率" || t == "pgconnhigh" {
			hasConn = true
		}
	}
	if !hasConn {
		t.Fatalf("expected synonym expansion, got %s", joined)
	}
}

func TestTokenize(t *testing.T) {
	toks := tokenize("PG 连接 cache_hit")
	if len(toks) < 2 {
		t.Fatalf("%v", toks)
	}
}

func TestRAGSearchMultiEngine(t *testing.T) {
	svc := &Service{}
	cases := []struct {
		q, want string
	}{
		{"Redis 大key 导致主从切换", "大 Key"},
		{"MySQL Too many connections 怎么排查", "连接"},
		{"StarRocks BE 宕机怎么处理", "BE"},
	}
	for _, tc := range cases {
		hits, err := svc.RAGSearch(nil, tc.q, 5)
		if err != nil {
			t.Fatal(err)
		}
		hits = FilterRelevantRAGHits(tc.q, hits)
		if len(hits) == 0 {
			t.Fatalf("%s: no hits", tc.q)
		}
		found := false
		for _, h := range hits {
			if strings.Contains(h.Title, tc.want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("%s: want title containing %q, got %+v", tc.q, tc.want, hits)
		}
	}
}

func TestInstanceStatusNotBuiltin(t *testing.T) {
	svc := &Service{}
	q := "分析下dorisdb.bj.restosuite.cn 实例最近1小时运行情况"
	u := ParseUtterance(q)
	if u.Symptom != "" || u.Object == "" || u.Time == "" {
		t.Fatalf("parse=%+v meaning=%s", u, u.Meaning())
	}
	if !strings.Contains(u.Meaning(), "查看") || !strings.Contains(u.Meaning(), u.Object) || !strings.Contains(u.Meaning(), "运行情况") {
		t.Fatalf("meaning=%s", u.Meaning())
	}
	hits, err := svc.RAGSearch(nil, q, 10)
	if err != nil {
		t.Fatal(err)
	}
	hits = FilterRelevantRAGHits(q, hits)
	if StrongRAGHit(hits) {
		t.Fatalf("status question must not direct-answer, hits=%+v", hits)
	}
	ans := FormatRAGAnswer(q, hits)
	for _, bad := range []string{"NotReady", "慢查询"} {
		if strings.Contains(ans, bad) {
			t.Fatalf("unrelated case leaked: %s", ans)
		}
	}
}

func TestRAGSearchWALNotCPU(t *testing.T) {
	svc := &Service{}
	q := "postgresql 数据库wal大的原因"
	hits, err := svc.RAGSearch(nil, q, 10)
	if err != nil {
		t.Fatal(err)
	}
	hits = FilterRelevantRAGHits(q, hits)
	if len(hits) == 0 {
		t.Fatal("expected WAL-related hits")
	}
	if !StrongRAGHit(hits) {
		t.Fatalf("want strong WAL hit, top=%+v", hits[0])
	}
	top := strings.ToLower(hits[0].Title)
	if strings.Contains(top, "cpu") || strings.Contains(top, "cpu") {
		t.Fatalf("CPU should not rank first: %s", hits[0].Title)
	}
	if !strings.Contains(top, "wal") && !strings.Contains(top, "槽") {
		t.Fatalf("want WAL/slot first, got %s score=%.2f", hits[0].Title, hits[0].Score)
	}
	for _, h := range hits[:min(3, len(hits))] {
		tl := strings.ToLower(h.Title)
		if strings.Contains(tl, "cpu") {
			t.Fatalf("CPU must not appear in top hits: %+v", hits)
		}
	}
}
