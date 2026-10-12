package aiops

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestMergeLearnedAction(t *testing.T) {
	old := "查 pg_stat_activity；核对连接池。"
	llm := "1) 确认是否真打满\n2) 看谁占连接\n3) 区分空闲与阻塞\n4) 核对连接池与应用"
	got := mergeLearnedAction(old, llm)
	if !strings.Contains(got, "【助手补充】") {
		t.Fatalf("want marker: %s", got)
	}
	got2 := mergeLearnedAction(got, "更新补充内容ABC")
	if strings.Count(got2, "【助手补充】") != 1 {
		t.Fatalf("want single marker: %s", got2)
	}
	if !strings.Contains(got2, "更新补充内容ABC") {
		t.Fatalf("want updated: %s", got2)
	}
}

func TestPickLearnTargetMHANotSlowQuery(t *testing.T) {
	rag := []RAGHit{{
		Fingerprint: "slow_query_cnt|warning|spike", Title: "PG 慢查询环比飙升",
		MetricKey: "slow_query_cnt", AlertNames: "PGSlowQuery", Score: 5,
	}}
	fp, metric, an, title, merge := pickLearnTarget("mha部署方案", "MHA 部署步骤……", rag)
	if strings.Contains(title, "慢查询") {
		t.Fatalf("title must not be slow query: %s", title)
	}
	if !strings.Contains(strings.ToLower(title), "mha") && !strings.Contains(title, "部署") {
		t.Fatalf("title should reflect question: %s", title)
	}
	if fp != "mysql_mha|info|manual" {
		t.Fatalf("fp=%s metric=%s an=%s merge=%v", fp, metric, an, merge)
	}
	if metric != "mysql_ha" || an != "MySQLHA" {
		t.Fatalf("metric/an=%s/%s", metric, an)
	}
}

func TestPickLearnTargetConnStillWorks(t *testing.T) {
	fp, metric, an, title, _ := pickLearnTarget("PostgreSQL 连接打满怎么排查？", "", nil)
	if fp != "conn_usage_ratio|warning|above" || metric != "conn_usage_ratio" || an != "PGConnHigh" {
		t.Fatalf("%s %s %s %s", fp, metric, an, title)
	}
}

func TestNormalizeLearnedTextBreaksLongLine(t *testing.T) {
	in := "根因是复制槽滞后；归档也可能失败。1) 查槽 2) 查归档 3) 扩容"
	out := normalizeLearnedText(in)
	if !strings.Contains(out, "\n") {
		t.Fatalf("want newlines: %q", out)
	}
}

func TestTrimSnippetUTF8Safe(t *testing.T) {
	s := strings.Repeat("参数深度解析", 200)
	out := trimSnippet(s, 50)
	if !utf8.ValidString(out) {
		t.Fatalf("invalid utf8 after trim: %q", out)
	}
	bad := "正常" + string([]byte{0xe6, 0xe2, 0x80}) + "结尾"
	cleaned := strings.ToValidUTF8(bad, "")
	if !utf8.ValidString(cleaned) {
		t.Fatal("ToValidUTF8 failed")
	}
}

func TestLearnKeepsFullAnswer(t *testing.T) {
	long := strings.Repeat("这是一段完整的 PostgreSQL 参数解析正文。", 80)
	got := normalizeLearnedText(strings.ToValidUTF8(long, ""))
	if len([]rune(got)) < len([]rune(long))-5 {
		t.Fatalf("learn body should not truncate, got %d runes want ~%d", len([]rune(got)), len([]rune(long)))
	}
	title := learnTitle(strings.Repeat("问", 100))
	if !strings.HasPrefix(title, "学习补充：") || !strings.Contains(title, strings.Repeat("问", 100)) {
		t.Fatalf("title should keep full question: %s", title)
	}
}

func TestMergeSkipsMisalignedAnswer(t *testing.T) {
	old := "原有动作"
	got := mergeLearnedAction(old, "知识库案例与 MHA 主题不对齐，下面改讲 MHA")
	if got != old {
		t.Fatalf("should not merge misaligned: %s", got)
	}
}
