package tencentpg

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"auto-dba/internal/cloud"
)

func TestTC3Headers(t *testing.T) {
	ts := time.Date(2026, 10, 26, 1, 2, 3, 0, time.UTC)
	h := TC3Headers("sid", "skey", "postgres", "postgres.tencentcloudapi.com", "DescribeDBInstances", "2017-03-12", "ap-guangzhou", []byte(`{}`), ts)
	if !strings.HasPrefix(h["Authorization"], "TC3-HMAC-SHA256 Credential=sid/") {
		t.Fatal(h["Authorization"])
	}
	if h["X-TC-Action"] != "DescribeDBInstances" || h["X-TC-Version"] != "2017-03-12" {
		t.Fatalf("%v", h)
	}
}

func TestRateLimitMapped(t *testing.T) {
	c := Reader{&Client{Host: "example.test", Do: func(r *http.Request) (*http.Response, error) {
		body := `{"Response":{"Error":{"Code":"RequestLimitExceeded","Message":"slow"}}}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	}}}
	_, err := c.ListInstances(context.Background(), cloud.Cred{AccountID: "a", Region: "ap-guangzhou", SecretID: "i", SecretKey: "k"})
	if !cloud.IsRateLimit(err) {
		t.Fatal(err)
	}
}

func TestReaderHasNoMutations(t *testing.T) {
	var r cloud.Reader = Reader{&Client{}}
	if r.Caps().ScaleDiskDown {
		t.Fatal("tencent disk cannot shrink")
	}
	if !r.Caps().Switchover {
		t.Fatal("SwitchDBInstancePrimary is available")
	}
}

func TestMapInstanceMemoryIsGB(t *testing.T) {
	got := mapInstance(map[string]any{
		"DBInstanceId": "postgres-has5gxcy", "DBInstanceCpu": 4,
		"DBInstanceMemory": 8, "DBInstanceStorage": 920,
	}, "ap-beijing")
	if got.CPU != 4 || got.MemoryMB != 8192 || got.DiskGB != 920 {
		t.Fatalf("%+v", got)
	}
}

func TestSpecBodyMemoryIsGB(t *testing.T) {
	body := specBody(cloud.SpecInput{CloudID: "postgres-1", CPU: 4, MemoryMB: 8192, DiskGB: 920})
	if body["Memory"] != 8 || body["Cpu"] != 4 || body["Storage"] != 920 || body["SwitchTag"] != 0 {
		t.Fatalf("%v", body)
	}
	if _, ok := body["SwitchStartTime"]; ok {
		t.Fatal("immediate switch should not send a window")
	}
	timed := specBody(cloud.SpecInput{CloudID: "postgres-1", CPU: 4, MemoryMB: 8192, SwitchTag: 1, SwitchStart: "02:00:00", SwitchEnd: "06:00:00"})
	if timed["SwitchTag"] != 1 || timed["SwitchStartTime"] != "02:00:00" || timed["SwitchEndTime"] != "06:00:00" {
		t.Fatalf("%v", timed)
	}
	maintain := specBody(cloud.SpecInput{CloudID: "postgres-1", DiskGB: 1000, SwitchTag: 2})
	if maintain["SwitchTag"] != 2 {
		t.Fatalf("%v", maintain)
	}
	if _, ok := maintain["SwitchStartTime"]; ok {
		t.Fatal("maintenance window should not send a clock range")
	}
	created := createBody(cloud.CreateInput{Zone: "ap-beijing-6", CPU: 1, MemoryMB: 2048, DiskGB: 500, SpecCode: "pg.1c2g"})
	if created["Storage"] != 500 || created["SpecCode"] != "pg.1c2g" || created["InstanceChargeType"] != "POSTPAID_BY_HOUR" || created["Period"] != 1 {
		t.Fatalf("%v", created)
	}
	if _, ok := created["Memory"]; ok {
		t.Fatal("create body should use SpecCode")
	}
	prepaid := createBody(cloud.CreateInput{
		Zone: "ap-beijing-6", StandbyZone: "ap-beijing-7", SpecCode: "pg.1c2g", DiskGB: 100,
		ChargeType: "PREPAID", Period: 12, Count: 2, Charset: "LATIN1", SyncMode: "Async",
		DBMajorVersion: "14", VpcID: "vpc-1", SubnetID: "subnet-1", Name: "app",
		SecurityGroupID: "sg-1", ProjectID: 8, NeedSupportTDE: true, DeletionProtection: true,
		Tags: []cloud.Tag{{Key: "env", Value: "prod"}},
	})
	if prepaid["InstanceChargeType"] != "PREPAID" || prepaid["Period"] != 12 || prepaid["Charset"] != "LATIN1" || prepaid["SyncMode"] != "Async" {
		t.Fatalf("%v", prepaid)
	}
	if prepaid["VpcId"] != "vpc-1" || prepaid["SubnetId"] != "subnet-1" || prepaid["DBMajorVersion"] != "14" || prepaid["Name"] != "app" {
		t.Fatalf("%v", prepaid)
	}
	if prepaid["StorageType"] != "PHYSICAL_LOCAL_SSD" || prepaid["NeedSupportTDE"] != 1 || prepaid["DeletionProtection"] != true {
		t.Fatalf("%v", prepaid)
	}
	nodes, _ := prepaid["DBNodeSet"].([]map[string]string)
	if len(nodes) != 2 || nodes[0]["Role"] != "Primary" || nodes[1]["Zone"] != "ap-beijing-7" {
		t.Fatalf("%v", nodes)
	}
}

func TestParseProductSpecsNested(t *testing.T) {
	raw := []byte(`{"Response":{"SpecInfoList":[
		{"Region":"ap-beijing","Zone":"ap-beijing-6","SpecItemInfoList":[
			{"Cpu":2,"Memory":4096,"MaxStorage":3000,"SpecCode":"pg.2c4g"},
			{"Cpu":4,"Memory":8192,"MaxStorage":6000,"SpecCode":"pg.4c8g"},
			{"Cpu":4,"Memory":8192,"MaxStorage":3000,"SpecCode":"pg.4c8g.dup"}
		]},
		{"Region":"ap-beijing","Zone":"ap-beijing-7","SpecItemInfoList":[
			{"Cpu":8,"Memory":16384,"MaxStorage":9000,"SpecCode":"pg.8c16g"}
		]}
	]}}`)
	got, err := parseProductSpecs(raw, "ap-beijing-6")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Label != "2核 4GB，最大存储 3000GB" || got[0].MaxStorageGB != 3000 {
		t.Fatalf("%+v", got)
	}
	if got[0].SpecCode != "pg.2c4g" || got[1].SpecCode != "pg.4c8g.dup" {
		t.Fatalf("spec code %+v", got)
	}
	if got[1].CPU != 4 || got[1].MemoryMB != 8192 || got[1].MaxStorageGB != 3000 || got[1].Label != "4核 8GB，最大存储 3000GB" {
		t.Fatalf("%+v", got)
	}
	if cloud.NormalizePGMemoryMB(8) != 8192 || cloud.NormalizePGMemoryMB(8192) != 8192 {
		t.Fatal("stored GB memory was not normalized")
	}
	flat, err := parseProductSpecs([]byte(`{"Response":{"SpecInfoList":[{"Region":"ap-beijing","Zone":"ap-beijing-6"}]}}`), "ap-beijing-6")
	if err != nil || len(flat) != 0 {
		t.Fatalf("outer-only specs=%+v err=%v", flat, err)
	}
}

func TestSlowLogParse(t *testing.T) {
	c := Reader{&Client{Host: "example.test", Do: func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("X-TC-Action") != "DescribeSlowQueryList" {
			t.Fatalf("action %s", r.Header.Get("X-TC-Action"))
		}
		body := `{"Response":{"TotalCount":1,"SlowQueryList":[{"DatabaseName":"app","UserName":"u","ClientAddr":"10.0.0.1","Duration":1.5,"NormalQuery":"select 1","LastTime":"2026-10-26 12:00:00"}]}}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	}}}
	rows, total, err := c.ListSlowLogs(context.Background(), cloud.Cred{SecretID: "i", SecretKey: "k", Region: "ap-guangzhou"}, "postgres-1", time.Now().Add(-time.Minute), time.Now(), 0, 10)
	if err != nil || total != 1 || len(rows) != 1 || rows[0].DBName != "app" || rows[0].DurationMS != 1500 {
		t.Fatalf("rows=%+v total=%d err=%v", rows, total, err)
	}
	if parseSlowTime("2026-11-10T10:00:00Z").IsZero() || parseSlowTime("").IsZero() == false {
		t.Fatal("slow time parse")
	}
	at := parseSlowTime("2021-07-27 03:12:01 CST")
	want := time.Date(2021, 7, 27, 3, 12, 1, 0, time.FixedZone("CST", 8*3600))
	if !at.Equal(want) {
		t.Fatalf("cst time %s", at)
	}
}

func TestSlowLogRawList(t *testing.T) {
	c := Reader{&Client{Host: "example.test", Do: func(r *http.Request) (*http.Response, error) {
		body := `{"Response":{"TotalCount":1,"RawSlowQueryList":[{"ClientAddr":"[local]","DatabaseName":"postgres","Duration":101.013,"RawQuery":"select 1","SessionStartTime":"2021-07-27 03:12:01 CST","UserName":"postgres"}]}}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	}}}
	rows, total, err := c.ListSlowLogs(context.Background(), cloud.Cred{SecretID: "i", SecretKey: "k", Region: "ap-guangzhou"}, "postgres-1", time.Now().Add(-time.Hour), time.Now(), 0, 10)
	if err != nil || total != 1 || len(rows) != 1 || rows[0].SQL != "select 1" || rows[0].Time.IsZero() {
		t.Fatalf("rows=%+v total=%d err=%v", rows, total, err)
	}
}
