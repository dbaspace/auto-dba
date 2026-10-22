package dns

import (
	"encoding/json"
	"strings"
	"testing"
)

func testSnap() *Snapshot {
	return &Snapshot{
		Zones: []Zone{
			{ID: "z1", Origin: "example.com.", Status: StatusActive, TTL: 600},
			{ID: "z2", Origin: "off.com.", Status: StatusRemoved},
		},
		Ranges: []Range{
			{ID: DefaultRangeID, Name: DefaultRangeName, Status: StatusActive},
			{ID: "r1", Name: "office", NetworkList: []string{"10.1.0.0/16"}, Status: StatusActive},
			{ID: "r2", Name: "idc", NetworkList: []string{"10.2.0.0/16"}, Status: StatusActive},
		},
		Records: []RecordSnap{
			{ID: "a1", Name: "db", Type: "A", Data: "10.2.0.10", TTL: 600, ZoneID: "z1", RangeID: "r1", Status: StatusActive},
			{ID: "a2", Name: "db", Type: "A", Data: "10.2.0.11", TTL: 600, ZoneID: "z1", RangeID: "r2", Status: StatusActive},
			{ID: "c1", Name: "www", Type: "CNAME", Data: "db.example.com", TTL: 600, ZoneID: "z1", RangeID: "r1", Status: StatusActive},
			{ID: "c2", Name: "ext", Type: "CNAME", Data: "cdn.other.net", TTL: 600, ZoneID: "z1", RangeID: "r1", Status: StatusActive},
			{ID: "m1", Name: "@", Type: "MX", Data: "mail.example.com|10", TTL: 600, ZoneID: "z1", RangeID: "r1", Status: StatusActive},
			{ID: "t1", Name: "txt", Type: "TXT", Data: "v=spf1 -all", TTL: 600, ZoneID: "z1", RangeID: "r1", Status: StatusActive},
		},
	}
}

func codes(issues []Issue) map[string]int {
	m := map[string]int{}
	for _, it := range issues {
		m[it.Code]++
	}
	return m
}

func TestLintRules(t *testing.T) {
	s := testSnap()
	s.Records = append(s.Records,
		RecordSnap{ID: "x1", Name: "www", Type: "A", Data: "10.2.0.12", TTL: 10, ZoneID: "z1", RangeID: "r1", Status: StatusActive},
		RecordSnap{ID: "x2", Name: "ip", Type: "CNAME", Data: "10.0.0.1", TTL: 600, ZoneID: "z1", RangeID: "r1", Status: StatusActive},
		RecordSnap{ID: "x3", Name: "l1", Type: "CNAME", Data: "l2.example.com", TTL: 600, ZoneID: "z1", RangeID: "r2", Status: StatusActive},
		RecordSnap{ID: "x4", Name: "l2", Type: "CNAME", Data: "l1.example.com.", TTL: 600, ZoneID: "z1", RangeID: "r2", Status: StatusActive},
		RecordSnap{ID: "x5", Name: "dup", Type: "CNAME", Data: "db.example.com", TTL: 600, ZoneID: "z1", RangeID: "r1", Status: StatusActive},
		RecordSnap{ID: "x6", Name: "dup", Type: "CNAME", Data: "www.example.com", TTL: 600, ZoneID: "z1", RangeID: "r1", Status: StatusActive},
		RecordSnap{ID: "x7", Name: "gone", Type: "CNAME", Data: "nope.example.com", TTL: 600, ZoneID: "z1", RangeID: "r1", Status: StatusActive},
		RecordSnap{ID: "x8", Name: "off", Type: "CNAME", Data: "10.0.0.1", TTL: 600, ZoneID: "z2", RangeID: "r1", Status: StatusActive},
		RecordSnap{ID: "x9", Name: "pub", Type: "A", Data: "192.168.5.5", TTL: 600, ZoneID: "z1", RangeID: "r1", Status: StatusActive},
	)
	got := codes(Lint(s))
	want := map[string]int{
		"cname_conflict": 1, "cname_multi": 1, "target_is_ip": 1, "cname_loop": 1, "ttl_low": 1, "target_missing": 2,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %d want %d (all %v)", k, got[k], v, got)
		}
	}
	if got["ip_outside_ranges"] != 1 {
		t.Errorf("ip_outside_ranges: got %v", got)
	}
}

func TestLintClean(t *testing.T) {
	for _, it := range Lint(testSnap()) {
		if it.Level == LevelError {
			t.Fatalf("unexpected error %+v", it)
		}
	}
}

func TestSimulate(t *testing.T) {
	s := testSnap()
	cases := []struct {
		name, typ, src, rcode, rng string
		answers                    []string
	}{
		{"db.example.com", "A", "10.1.3.4", RcodeNoError, "office", []string{"10.2.0.10"}},
		{"db.example.com", "A", "10.2.3.4", RcodeNoError, "idc", []string{"10.2.0.11"}},
		{"www.example.com", "A", "10.1.3.4", RcodeNoError, "office", []string{"db.example.com", "10.2.0.10"}},
		{"db.example.com", "A", "192.168.1.1", RcodeNXDomain, DefaultRangeName, nil},
		{"www.example.com", "A", "10.2.3.4", RcodeNXDomain, "idc", nil},
		{"ext.example.com", "A", "10.1.0.1", RcodeForward, "office", []string{"cdn.other.net"}},
		{"www.baidu.com", "A", "10.1.0.1", RcodeForward, "office", nil},
		{"txt.example.com", "A", "10.1.0.1", RcodeNoError, "office", nil},
		{"example.com", "MX", "10.1.0.1", RcodeNoError, "office", []string{"10 mail.example.com"}},
		{"x.off.com", "A", "10.1.0.1", RcodeForward, "office", nil},
	}
	for _, c := range cases {
		res, err := Simulate(s, c.name, c.typ, c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if res.Rcode != c.rcode || res.RangeName != c.rng {
			t.Errorf("%s from %s: rcode %s range %s, want %s %s\n%v", c.name, c.src, res.Rcode, res.RangeName, c.rcode, c.rng, res.Steps)
		}
		var data []string
		for _, a := range res.Answers {
			data = append(data, a.Data)
		}
		if strings.Join(data, ",") != strings.Join(c.answers, ",") {
			t.Errorf("%s: answers %v want %v", c.name, data, c.answers)
		}
	}
	if _, err := Simulate(s, "a.example.com", "SRV", "10.1.0.1"); err == nil {
		t.Error("SRV should be rejected")
	}
}

func TestRollbackOps(t *testing.T) {
	logs := []ChangeLog{
		{Seq: 1, Action: ActionCreate, ObjectType: ObjectRecord, ObjectID: "n1", After: json.RawMessage(`{"id":"n1"}`)},
		{Seq: 2, Action: ActionUpdate, ObjectType: ObjectRecord, ObjectID: "u1", Before: json.RawMessage(`{"id":"u1","ttl":600}`), After: json.RawMessage(`{"id":"u1","ttl":30}`)},
		{Seq: 3, Action: ActionDelete, ObjectType: ObjectRecord, ObjectID: "d1", Before: json.RawMessage(`{"id":"d1"}`)},
	}
	ops, err := RollbackOps(logs)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 3 {
		t.Fatalf("len %d", len(ops))
	}
	if ops[0].Action != ActionCreate || ops[0].ID != "d1" || string(ops[0].Expect) != "null" {
		t.Errorf("delete inverse %+v", ops[0])
	}
	if ops[1].Action != ActionUpdate || string(ops[1].After) != `{"id":"u1","ttl":600}` || string(ops[1].Expect) != `{"id":"u1","ttl":30}` {
		t.Errorf("update inverse %+v", ops[1])
	}
	if ops[2].Action != ActionDelete || ops[2].ID != "n1" || string(ops[2].Expect) != `{"id":"n1"}` {
		t.Errorf("create inverse %+v", ops[2])
	}
	if _, err := RollbackOps(nil); err == nil {
		t.Error("empty batch should fail")
	}
}

func TestExpectMatch(t *testing.T) {
	cur := &RecordSnap{ID: "a", Name: "db", Type: "A", Data: "1.1.1.1", TTL: 600}
	ok, _ := expectMatch(json.RawMessage(`{"id":"a","name":"db","type":"A","data":"1.1.1.1","ttl":600}`), cur, normRecordCmp)
	if !ok {
		t.Error("same value should match")
	}
	ok, _ = expectMatch(json.RawMessage(`{"id":"a","name":"db","type":"A","data":"1.1.1.1","ttl":300}`), cur, normRecordCmp)
	if ok {
		t.Error("changed ttl should conflict")
	}
	ok, _ = expectMatch(json.RawMessage(`null`), cur, normRecordCmp)
	if ok {
		t.Error("null expect with existing object should conflict")
	}
	ok, _ = expectMatch[RecordSnap](json.RawMessage(`null`), nil, normRecordCmp)
	if !ok {
		t.Error("null expect with missing object should match")
	}
}

func TestCSVRoundTrip(t *testing.T) {
	s := testSnap()
	body, err := ExportCSV(s, "z1", "")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := ParseCSV(strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(s.Records) {
		t.Fatalf("rows %d want %d", len(rows), len(s.Records))
	}
	plan := PlanImport(s, rows, "")
	if plan.Counts[ImportUnchanged] != len(s.Records) || len(plan.Ops) != 0 {
		t.Fatalf("re-import should be unchanged: %v %+v", plan.Counts, plan.Items)
	}
}

func TestPlanImport(t *testing.T) {
	s := testSnap()
	csvText := "zone,range,name,type,data,ttl,status,description\n" +
		"example.com,office,db,A,10.2.0.10,300,,\n" + // ttl changed → update
		"example.com,office,new,A,10.1.0.9,600,Active,new one\n" + // create
		"example.com,office,new,A,10.1.0.9,600,Active,dup\n" + // duplicate line
		"example.com,nowhere,x,A,10.1.0.9,600,,\n" + // bad range
		"example.com,office,srv,SRV,0 5 5060 sip,600,,\n" + // unsupported
		"example.com,office,bad,A,not-ip,600,,\n" + // invalid data
		"example.com,office,@,MX,10 mail.example.com,600,,\n" // unchanged (pref host form)
	rows, err := ParseCSV(strings.NewReader(csvText))
	if err != nil {
		t.Fatal(err)
	}
	plan := PlanImport(s, rows, "")
	want := map[string]int{ImportUpdate: 1, ImportCreate: 1, ImportError: 3, ImportUnsupported: 1, ImportUnchanged: 1}
	for k, v := range want {
		if plan.Counts[k] != v {
			t.Errorf("%s: got %d want %d (%+v)", k, plan.Counts[k], v, plan.Items)
		}
	}
	if len(plan.Ops) != 2 {
		t.Fatalf("ops %d", len(plan.Ops))
	}
	if plan.Ops[0].Action != ActionUpdate || len(plan.Ops[0].Expect) == 0 {
		t.Errorf("update op should pin expect: %+v", plan.Ops[0])
	}
}

func TestParseZoneFile(t *testing.T) {
	text := `; comment
$ORIGIN example.com.
$TTL 300
@        IN SOA ns1 hostmaster 1 2 3 4 5
@        IN NS  ns1
db           IN A     10.1.0.10
             IN A     10.1.0.11
www   60     IN CNAME db
ext          CNAME    cdn.other.net.
txt          IN TXT   "v=spf1" " -all ; not comment"
@            IN MX    10 mail
far.other.org. IN A 1.2.3.4
`
	rows, err := ParseZoneFile(strings.NewReader(text), "")
	if err != nil {
		t.Fatal(err)
	}
	byLine := map[int]ImportRow{}
	for _, r := range rows {
		byLine[r.Line] = r
	}
	if r := byLine[4]; r.Skip == "" {
		t.Errorf("SOA should be skipped: %+v", r)
	}
	if r := byLine[7]; r.Name != "db" || r.Data != "10.1.0.11" || r.TTL != 300 {
		t.Errorf("continuation line: %+v", r)
	}
	if r := byLine[8]; r.Name != "www" || r.TTL != 60 || r.Data != "db.example.com" {
		t.Errorf("cname relative: %+v", r)
	}
	if r := byLine[9]; r.Data != "cdn.other.net" {
		t.Errorf("cname absolute: %+v", r)
	}
	if r := byLine[10]; r.Data != "v=spf1 -all ; not comment" {
		t.Errorf("txt: %q", r.Data)
	}
	if r := byLine[11]; r.Name != "@" || r.Data != "10 mail.example.com" {
		t.Errorf("mx: %+v", r)
	}
	if r := byLine[12]; r.Err == "" {
		t.Errorf("out-of-zone name should error: %+v", r)
	}
	plan := PlanImport(testSnap(), rows, "office")
	if plan.Counts[ImportCreate] == 0 {
		t.Errorf("expected creates: %v", plan.Counts)
	}
}

func TestExportZone(t *testing.T) {
	body, err := ExportZone(testSnap(), "z1", "r1")
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, want := range []string{"$ORIGIN example.com.", "$TTL 600", "CNAME db.example.com.", "MX    10 mail.example.com.", `"v=spf1 -all"`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q\n%s", want, s)
		}
	}
	if strings.Contains(s, "10.2.0.11") {
		t.Error("other range leaked")
	}
	rows, err := ParseZoneFile(strings.NewReader(s), "")
	if err != nil {
		t.Fatal(err)
	}
	plan := PlanImport(testSnap(), rows, "office")
	if plan.Counts[ImportUnchanged] != 5 {
		t.Errorf("zone round trip: %v %+v", plan.Counts, plan.Items)
	}
}

func TestInspectAndReverse(t *testing.T) {
	s := testSnap()
	refs := []HostRef{
		{Addr: "10.2.0.10", InstanceID: "i1", InstanceName: "pg-1"},
		{Addr: "pg.example.com", InstanceID: "i2", InstanceName: "pg-2"},
		{Addr: "10.9.9.9", InstanceID: "i3", InstanceName: "mysql"},
	}
	got := codes(Inspect(s, refs))
	if got["dangling_record"] != 1 || got["instance_domain_missing"] != 1 {
		t.Errorf("inspect codes %v", got)
	}
	rev, err := Reverse(s, "10.2.0.10")
	if err != nil {
		t.Fatal(err)
	}
	if len(rev.Records) != 1 || len(rev.CNAMEs) != 1 || strings.Join(rev.Names, ",") != "db.example.com,www.example.com" {
		t.Errorf("reverse %+v", rev)
	}
}

func TestCorefileMetrics(t *testing.T) {
	body, err := RenderCorefile(DeployOptions{Listen: "0.0.0.0:53", Upstream: "223.5.5.5:53", DBHost: "10.0.0.8"}, "d", "u", "p", "n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "prometheus :9153") {
		t.Fatalf("missing prometheus\n%s", body)
	}
	opt, err := ParseDeployOptions(`{"upstream":"1.1.1.1:53","db_host":"x","metrics_port":9253}`)
	if err != nil || opt.MetricsPort != 9253 {
		t.Fatalf("%+v %v", opt, err)
	}
}

func TestNodeSync(t *testing.T) {
	// 节点状态由 ListNodes 计算；这里校验阈值语义。
	now := int64(10000)
	changed := now - 300
	n := Node{UpdatedTime: now - 5, DataUpdatedTime: changed - 10}
	n.Online = now-n.UpdatedTime <= NodeOfflineSeconds
	n.Synced = n.DataUpdatedTime >= changed || now-changed <= SyncLagSeconds
	if !n.Online || n.Synced {
		t.Fatalf("want online & unsynced: %+v", n)
	}
}
