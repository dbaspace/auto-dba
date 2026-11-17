package srcluster

import "testing"

func TestInferFeSoftwareBaseFromInternalLog(t *testing.T) {
	got := InferFeSoftwareBaseFromInternalLog("/data1/dorisdb_base/fe/log")
	if got != "/data1/dorisdb_base" {
		t.Fatalf("got %q", got)
	}
	if InferFeSoftwareBaseFromInternalLog("") != "" {
		t.Fatal("empty")
	}
}

func TestInferFeSoftwareBaseFromPath(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"/home/dorisdb_base/fe/meta", "/home/dorisdb_base"},
		{"/home/dorisdb_base/fe/log", "/home/dorisdb_base"},
		{"/home/dorisdb_base/fe", "/home/dorisdb_base"},
		{"/home/dorisdb_base/fe/log/fe.out", "/home/dorisdb_base"},
		{"/home/log", "/home/log"},
		{"/home/fe/log", "/home"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := InferFeSoftwareBaseFromPath(tc.in); got != tc.want {
			t.Fatalf("InferFeSoftwareBaseFromPath(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestPickDeeperSoftwareBase(t *testing.T) {
	if got := pickDeeperSoftwareBase("/home", "/home/dorisdb_base"); got != "/home/dorisdb_base" {
		t.Fatalf("got %q", got)
	}
	if got := pickDeeperSoftwareBase("/home/dorisdb_base", "/home"); got != "/home/dorisdb_base" {
		t.Fatalf("got %q", got)
	}
	if got := pickDeeperSoftwareBase("", "/home/dorisdb_base"); got != "/home/dorisdb_base" {
		t.Fatalf("empty a got %q", got)
	}
	if got := pickDeeperSoftwareBase("/home/dorisdb_base", ""); got != "/home/dorisdb_base" {
		t.Fatalf("empty b got %q", got)
	}
	if got := pickDeeperSoftwareBase("/home/dorisdb_base", "/resto/dorisdb_base"); got != "/home/dorisdb_base" {
		t.Fatalf("longer sibling won: %q", got)
	}
}

func TestAlignFeSoftwareBaseWithBE(t *testing.T) {
	got, did := alignFeSoftwareBaseWithBE("/home", "/home/dorisdb_base")
	if !did || got != "/home/dorisdb_base" {
		t.Fatalf("got=%q did=%v", got, did)
	}
	got, did = alignFeSoftwareBaseWithBE("/home/dorisdb_base", "/home/dorisdb_base")
	if did || got != "/home/dorisdb_base" {
		t.Fatalf("same got=%q did=%v", got, did)
	}
	got, did = alignFeSoftwareBaseWithBE("/opt/fe_root", "/opt/be_root")
	if did || got != "/opt/fe_root" {
		t.Fatalf("unrelated got=%q did=%v", got, did)
	}
	got, did = alignFeSoftwareBaseWithBE("/home/fe/log", "/home/dorisdb_base")
	// Infer already stripped; this tests raw align only — /home/fe/log is not prefix of BE
	if did {
		t.Fatalf("unexpected align got=%q", got)
	}
	got, did = alignFeSoftwareBaseWithBE("/home/dorisdb_base", "/home/dorisdb_base/extra")
	if did || got != "/home/dorisdb_base" {
		t.Fatalf("dorisdb_base should stay got=%q did=%v", got, did)
	}
}

func TestInferBeSoftwareBaseFromMinidump(t *testing.T) {
	got := InferBeSoftwareBaseFromMinidump("/data1/dorisdb_base/be")
	if got != "/data1/dorisdb_base" {
		t.Fatalf("got %q", got)
	}
	got = InferBeSoftwareBaseFromMinidump("/data1/dorisdb_base/be/minidump")
	if got != "/data1/dorisdb_base" {
		t.Fatalf("nested got %q", got)
	}
}

func TestParseBEVarz(t *testing.T) {
	body := "foo=1\nsys_minidump_dir=/data1/dorisdb_base/be\nstorage_root_path=/data1/dorisdb/be/storage\nbar=2\n"
	md, st := ParseBEVarz(body)
	if md != "/data1/dorisdb_base/be" || st != "/data1/dorisdb/be/storage" {
		t.Fatalf("md=%q st=%q", md, st)
	}
	_, st2 := ParseBEVarz("storage_root_path=/a/storage,medium:hdd;/b/storage\n")
	if st2 != "/a/storage" {
		t.Fatalf("multi disk got %q", st2)
	}
}

func TestResolveStorageRoot(t *testing.T) {
	cases := []string{"/data", "/data1/dorisdb/be/storeage", "/data1/dorisdb/be/storage", "/data/dorisdb"}
	for _, in := range cases {
		if got := resolveStorageRoot(in); got != in {
			t.Fatalf("resolveStorageRoot(%q)=%q", in, got)
		}
	}
	if got := resolveStorageRoot("  /data1/dorisdb/be/storeage/  "); got != "/data1/dorisdb/be/storeage" {
		t.Fatalf("trim got %q", got)
	}
	if got := resolveStorageRoot(""); got != "/data" {
		t.Fatalf("empty got %q", got)
	}
}

func TestChooseFeSoftwareBasePrefersMetaAndDefault(t *testing.T) {
	meta, metaOK := inferRoleSoftwareBase("/home/dorisdb_base/fe/meta", "fe")
	logBase, logOK := inferRoleSoftwareBase("/resto/dorisdb_base/fe/log", "fe")
	got, uncertain := chooseFeSoftwareBase(meta, logBase, metaOK, logOK)
	if uncertain || got != "/home/dorisdb_base" {
		t.Fatalf("got=%q uncertain=%v metaOK=%v logOK=%v", got, uncertain, metaOK, logOK)
	}
	got, uncertain = chooseFeSoftwareBase("/resto/dorisdb_base", "/home/dorisdb_base", true, true)
	if uncertain || got != "/home/dorisdb_base" {
		t.Fatalf("sibling default got=%q uncertain=%v", got, uncertain)
	}
	got, uncertain = chooseFeSoftwareBase("/data1/dorisdb_base", "", true, false)
	if uncertain || got != "/data1/dorisdb_base" {
		t.Fatalf("single custom got=%q uncertain=%v", got, uncertain)
	}
	got, uncertain = chooseFeSoftwareBase("/opt/a", "/opt/b", false, false)
	if !uncertain || got != "" {
		t.Fatalf("uncertain got=%q uncertain=%v", got, uncertain)
	}
	got, uncertain = chooseFeSoftwareBase("/opt/fe-root", "/data1/dorisdb_base", true, true)
	if uncertain || got != "/opt/fe-root" {
		t.Fatalf("meta wins got=%q uncertain=%v", got, uncertain)
	}
}

func TestAssignBEProbeSummaryPerNode(t *testing.T) {
	out := &LayoutProbeResult{}
	out.BENodes = []LayoutBENode{
		layoutBENodeFromVarz("10.0.0.1", 8040, "sys_minidump_dir=/home/dorisdb_base/be\nstorage_root_path=/data/dorisdb/be/storage\n"),
		layoutBENodeFromVarz("10.0.0.2", 8040, "sys_minidump_dir=/data1/dorisdb_base/be\nstorage_root_path=/data1/dorisdb/be/storage\n"),
	}
	assignBEProbeSummary(out, false)
	if out.BeSoftwareBase != "" || out.BeDataRoot != "" {
		t.Fatalf("common fields should be empty soft=%q data=%q", out.BeSoftwareBase, out.BeDataRoot)
	}
	if len(out.BENodes) != 2 || out.BENodes[0].SoftwareBase != "/home/dorisdb_base" || out.BENodes[0].DataRoot != "/data/dorisdb/be/storage" {
		t.Fatalf("node0 %+v", out.BENodes[0])
	}
	if out.BENodes[1].SoftwareBase != "/data1/dorisdb_base" || out.BENodes[1].DataRoot != "/data1/dorisdb/be/storage" {
		t.Fatalf("node1 %+v", out.BENodes[1])
	}
}

func TestFeSoftwareFromDirs(t *testing.T) {
	base, warn := feSoftwareFromDirs("/home/dorisdb_base/fe/meta", "/home/dorisdb_base/fe/log")
	if warn != "" || base != "/home/dorisdb_base" {
		t.Fatalf("default path base=%q warn=%q", base, warn)
	}
	base, warn = feSoftwareFromDirs("/home/dorisdb/fe/meta", "/home/dorisdb/fe/log")
	if base != "" || warn == "" {
		t.Fatalf("truncated should not be written base=%q warn=%q", base, warn)
	}
	base, warn = feSoftwareFromDirs("/home/dorisdb", "")
	if base != "" || warn == "" {
		t.Fatalf("truncated bare path base=%q warn=%q", base, warn)
	}
	if !isTruncatedDefaultPrefix("/home/dorisdb") || isTruncatedDefaultPrefix("/home/dorisdb_base") {
		t.Fatal("prefix check")
	}
	if isTruncatedDefaultPrefix("/data1/dorisdb_base") {
		t.Fatal("other root is not a truncated default")
	}
}

func TestAssignFEProbeSummaryPerNode(t *testing.T) {
	out := &LayoutProbeResult{FENodes: []LayoutFENode{
		{Host: "10.0.0.1", SoftwareBase: "/home/dorisdb_base", MetaDir: "/home/dorisdb_base/fe/meta"},
		{Host: "10.0.0.2", SoftwareBase: "/data1/dorisdb_base", MetaDir: "/data1/dorisdb_base/fe/meta"},
	}}
	assignFEProbeSummary(out, false)
	if out.FeSoftwareBase != "" {
		t.Fatalf("cluster field should stay empty, got %q", out.FeSoftwareBase)
	}
	if out.FENodes[0].SoftwareBase != "/home/dorisdb_base" || out.FENodes[1].SoftwareBase != "/data1/dorisdb_base" {
		t.Fatalf("per node changed: %+v", out.FENodes)
	}
	same := &LayoutProbeResult{FENodes: []LayoutFENode{
		{Host: "10.0.0.1", SoftwareBase: "/home/dorisdb_base"},
		{Host: "10.0.0.2", SoftwareBase: "/home/dorisdb_base"},
	}}
	assignFEProbeSummary(same, false)
	if same.FeSoftwareBase != "/home/dorisdb_base" {
		t.Fatalf("agreed base got %q", same.FeSoftwareBase)
	}
	assignFEProbeSummary(same, true)
	same.FeSoftwareBase = ""
	assignFEProbeSummary(same, true)
	if same.FeSoftwareBase != "" {
		t.Fatalf("connect failure should not fill cluster field, got %q", same.FeSoftwareBase)
	}
}

func TestLayoutBENodeSoftwarePerNode(t *testing.T) {
	a := layoutBENodeFromVarz("10.0.0.1", 8040, "sys_minidump_dir=/home/dorisdb_base/be\nstorage_root_path=/data/dorisdb/be/storage\n")
	b := layoutBENodeFromVarz("10.0.0.2", 8040, "sys_minidump_dir=/resto/dorisdb_base/be\nstorage_root_path=/data/dorisdb/be/storage\n")
	if a.SoftwareBase != "/home/dorisdb_base" || b.SoftwareBase != "/resto/dorisdb_base" {
		t.Fatalf("a=%q b=%q", a.SoftwareBase, b.SoftwareBase)
	}
	onlyLog := layoutBENodeFromVarz("10.0.0.3", 8040, "storage_root_path=/data/dorisdb/be/storage\nsys_log_dir=/home/dorisdb_base/be/log\n")
	if onlyLog.SoftwareBase != "/home/dorisdb_base" || onlyLog.DataRoot != "/data/dorisdb/be/storage" {
		t.Fatalf("log fallback %+v", onlyLog)
	}
	storageOnly := layoutBENodeFromVarz("10.0.0.4", 8040, "storage_root_path=/data/dorisdb/be/storage\n")
	if storageOnly.SoftwareBase != "" || storageOnly.DataRoot != "/data/dorisdb/be/storage" {
		t.Fatalf("storage must not become software %+v", storageOnly)
	}
}

func TestFrontendConfigIndexes(t *testing.T) {
	key, alias, val := frontendConfigIndexes([]string{"", "", "", "", "", ""})
	if key != 0 || alias != 1 || val != 2 {
		t.Fatalf("unnamed 3.2 got key=%d alias=%d val=%d", key, alias, val)
	}
	key, alias, val = frontendConfigIndexes([]string{"Key", "AliasNames", "Value", "Type", "IsMutable", "ConfigType"})
	if key != 0 || alias != 1 || val != 2 {
		t.Fatalf("named got key=%d alias=%d val=%d", key, alias, val)
	}
	key, _, val = frontendConfigIndexes([]string{"Key", "Value"})
	if key != 0 || val != 1 {
		t.Fatalf("legacy got key=%d val=%d", key, val)
	}
	if !configKeyMatches("sys_log_dir", "internal_log_dir", "internal_log_dir") {
		t.Fatal("alias should match")
	}
	if configKeyMatches("sys_log_dir", "internal_log_dir", "meta_dir") {
		t.Fatal("unrelated key matched")
	}
}
