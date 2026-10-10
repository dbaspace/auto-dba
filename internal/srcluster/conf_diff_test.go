package srcluster

import (
	"reflect"
	"strings"
	"testing"
)

func TestConfKeyDiff(t *testing.T) {
	old := "# comment\nhttp_port = 8030\nquery_port=9030\n\nsys_log_level = INFO\n"
	neu := "sys_log_level = WARN\nhttp_port = 8030\n# another comment\nmem_limit = 80%\n"
	got := ConfKeyDiff(old, neu)
	want := []ConfKeyChange{
		{Key: "mem_limit", Op: "add", New: "80%"},
		{Key: "query_port", Op: "del", Old: "9030"},
		{Key: "sys_log_level", Op: "change", Old: "INFO", New: "WARN"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	if d := ConfKeyDiff("a = 1\n# x\n", "# y\na=1"); len(d) != 0 {
		t.Fatalf("仅注释 / 空白 / 顺序不同应无差异: %+v", d)
	}
}

func TestConfLineDiff(t *testing.T) {
	old := "a\nb\nc\nd\n"
	neu := "a\nc\nd  \nx\n"
	lines := ConfLineDiff(old, neu)
	var ops []string
	for _, l := range lines {
		ops = append(ops, l.Op+":"+l.Text)
	}
	want := "equal:a del:b equal:c equal:d   add:x"
	if strings.Join(ops, " ") != want {
		t.Fatalf("got %q", strings.Join(ops, " "))
	}
	if lines[1].OldNo != 2 || lines[1].NewNo != 0 || lines[4].NewNo != 4 || lines[4].OldNo != 0 {
		t.Fatalf("行号错误: %+v", lines)
	}
	if add, del := ConfLineStats(lines); add != 1 || del != 1 {
		t.Fatalf("stats add=%d del=%d", add, del)
	}
}

func TestConfKeyPatch(t *testing.T) {
	target := "# head\nsys_log_level = INFO\nJAVA_OPTS=\"-Xmx4g\"\nqe_slow_log_ms = 5000\nmax_conn = 1\nmax_conn = 2\nfrontend_address = 10.0.0.2\n"
	changes := []ConfKeyChange{
		{Key: "sys_log_level", Op: "change", Old: "INFO", New: "WARN"},
		{Key: "JAVA_OPTS", Op: "change", New: "\"-Xmx8g\""},
		{Key: "qe_slow_log_ms", Op: "del", Old: "5000"},
		{Key: "max_conn", Op: "change", New: "9"},
		{Key: "mem_limit", Op: "add", New: "80%"},
	}
	got := ConfKeyPatch(target, changes)
	want := "# head\nsys_log_level = WARN\nJAVA_OPTS=\"-Xmx8g\"\nmax_conn = 1\nmax_conn = 9\nfrontend_address = 10.0.0.2\nmem_limit = 80%\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if ConfKeyPatch(target, nil) != target {
		t.Fatal("无改动应原样返回")
	}
}

func TestSplitNodeLocalChanges(t *testing.T) {
	common, local := SplitNodeLocalChanges(ConfKeyDiff(
		"frontend_address = a\npriority_networks = x\nsys_log_level = INFO\n",
		"frontend_address = b\npriority_networks = y\nsys_log_level = WARN\n",
	))
	if len(common) != 1 || common[0].Key != "sys_log_level" || len(local) != 2 {
		t.Fatalf("common=%+v local=%+v", common, local)
	}
}

func TestConfLineDiffEdges(t *testing.T) {
	if l := ConfLineDiff("", ""); len(l) != 0 {
		t.Fatalf("空对空: %+v", l)
	}
	l := ConfLineDiff("", "a\nb")
	if len(l) != 2 || l[0].Op != "add" || l[1].NewNo != 2 {
		t.Fatalf("全新增: %+v", l)
	}
	l = ConfLineDiff("a\r\nb\r\n", "a\nb\n")
	if add, del := ConfLineStats(l); add != 0 || del != 0 {
		t.Fatalf("CRLF 应视为相同: %+v", l)
	}
	l = ConfLineDiff("x\ny\n", "y\nx\n")
	if add, del := ConfLineStats(l); add != 1 || del != 1 {
		t.Fatalf("交换两行: %+v", l)
	}
}
