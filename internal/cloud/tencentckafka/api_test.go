package tencentckafka

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"auto-dba/internal/cloud"
)

func TestListInstancesMapsCKafka(t *testing.T) {
	pages := []string{
		`{"Response":{"Result":{"TotalCount":1,"InstanceList":[{"InstanceId":"ckafka-a","InstanceName":"orders","Status":1,"DiskSize":200,"Vip":"10.0.0.8","Vport":"9092","ZoneId":100003}]}}}`,
		`{"Response":{"Result":{"TotalCount":1,"InstanceList":[]}}}`,
	}
	n := 0
	r := Reader{&Client{Host: "example.test", Do: func(req *http.Request) (*http.Response, error) {
		body := pages[n]
		if n < len(pages)-1 {
			n++
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	},
	}}
	list, err := r.ListInstances(context.Background(), cloud.Cred{Region: "ap-beijing", SecretID: "id", SecretKey: "key"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].CloudID != "ckafka-a" || list[0].Status != "running" || list[0].DiskGB != 200 || list[0].Host != "10.0.0.8" || list[0].Port != 9092 {
		t.Fatalf("list = %+v", list)
	}
	host, port := cloud.NetFromExtras(cloud.EngineKafka, list[0].Extras)
	if host != "10.0.0.8" || port != 9092 {
		t.Fatalf("net = %s:%d", host, port)
	}
}

func TestListUsesVipListQuotaAndClusterType(t *testing.T) {
	pages := []string{
		`{"Response":{"Result":{"TotalCount":1,"InstanceList":[{"InstanceId":"ckafka-b","InstanceName":"pro","Status":1,"DiskSize":0,"VipList":[{"Vip":"10.9.0.1"}],"MaxTopicNumber":80,"TopicNum":9,"MaxPartitionNumber":400,"PartitionNum":16,"InstanceType":"profession","Version":"2.8.1"}]}}}`,
		`{"Response":{"Result":{"TotalCount":1,"InstanceList":[]}}}`,
	}
	n := 0
	r := Reader{&Client{Host: "example.test", Do: func(req *http.Request) (*http.Response, error) {
		body := pages[n]
		if n < len(pages)-1 {
			n++
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	}}}
	list, err := r.ListInstances(context.Background(), cloud.Cred{Region: "ap-beijing", SecretID: "id", SecretKey: "key"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Host != "10.9.0.1" || list[0].Port != 9092 || list[0].DiskGB != 0 || list[0].Version != "2.8.1" {
		t.Fatalf("list = %+v", list[0])
	}
	topic, topicUsed, part, used, typ := cloud.KafkaListFields(list[0].Extras)
	if topic != 80 || topicUsed != 9 || part != 400 || used != 16 || typ != "profession" {
		t.Fatalf("quota = %d %d %d %d %s", topic, topicUsed, part, used, typ)
	}
}

func TestEnrichFillsProDiskWithoutReplacingVersion(t *testing.T) {
	r := Reader{&Client{Host: "example.test", Do: func(req *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(req.Body)
		if !strings.Contains(string(b), `"InstanceId":"ckafka-pro"`) {
			t.Fatalf("body %s", b)
		}
		body := `{"Response":{"Result":{"InstanceId":"ckafka-pro","DiskSize":0,"DynamicDiskConfig":{"DiskQuota":800},"CreatedTopics":3,"RemainingTopics":7,"CreatedPartitions":12,"RemainingPartitions":88,"InstanceType":"profession","Version":"2.4.2","VipList":[{"Vip":"10.8.0.9","Vport":"9092"}]}}}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	}}}
	inst := cloud.Instance{
		CloudID: "ckafka-pro", Version: "2.4.1",
		Extras: map[string]any{"InstanceId": "ckafka-pro", "MaxTopicNumber": float64(50), "PartitionNum": float64(4)},
	}
	if err := r.Enrich(context.Background(), cloud.Cred{Region: "ap-beijing", SecretID: "id", SecretKey: "key"}, &inst); err != nil {
		t.Fatal(err)
	}
	if inst.DiskGB != 800 || inst.Version != "2.4.1" || inst.Host != "10.8.0.9" || inst.Port != 9092 {
		t.Fatalf("inst = %+v", inst)
	}
	topic, topicUsed, part, used, typ := cloud.KafkaListFields(inst.Extras)
	if topic != 50 || topicUsed != 3 || part != 100 || used != 12 || typ != "profession" {
		t.Fatalf("quota = %d %d %d %d %s", topic, topicUsed, part, used, typ)
	}
}

func TestEnrichErrorKeepsListFacts(t *testing.T) {
	r := Reader{&Client{Host: "example.test", Do: func(req *http.Request) (*http.Response, error) {
		body := `{"Response":{"Error":{"Code":"InternalError","Message":"boom"}}}`
		return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	}}}
	inst := cloud.Instance{CloudID: "ckafka-c", DiskGB: 200, Version: "1.1.1", Host: "10.0.0.1", Port: 9092, Extras: map[string]any{"kafka_topic_limit": 10}}
	if err := r.Enrich(context.Background(), cloud.Cred{Region: "ap-beijing", SecretID: "id", SecretKey: "key"}, &inst); err == nil {
		t.Fatal("want error")
	}
	if inst.DiskGB != 200 || inst.Version != "1.1.1" || inst.Host != "10.0.0.1" {
		t.Fatalf("inst changed %+v", inst)
	}
}

func TestCreateSendsZoneDiskAndNetwork(t *testing.T) {
	var got string
	w := Writer{&Client{Host: "example.test", Do: func(req *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(req.Body)
		got = string(b)
		body := `{"Response":{"Result":{"Data":{"FlowId":88,"InstanceId":"ckafka-new"}}}}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	},
	}}
	task, err := w.Create(context.Background(), cloud.Cred{Region: "ap-beijing", SecretID: "id", SecretKey: "key"}, cloud.CreateInput{
		Name: "orders", Zone: "100003", DiskGB: 200, VpcID: "vpc-1", SubnetID: "subnet-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if task.ID != "88" || task.Detail != "ckafka-new" || task.Status != "running" {
		t.Fatalf("task = %+v", task)
	}
	for _, part := range []string{`"ZoneId":100003`, `"DiskSize":200`, `"VpcId":"vpc-1"`, `"SubnetId":"subnet-1"`, `"InstanceName":"orders"`} {
		if !strings.Contains(got, part) {
			t.Fatalf("body missing %s: %s", part, got)
		}
	}
}

func TestCreateRejectsZoneName(t *testing.T) {
	w := Writer{&Client{}}
	_, err := w.Create(context.Background(), cloud.Cred{}, cloud.CreateInput{Name: "a", Zone: "ap-beijing-6", DiskGB: 200, VpcID: "v", SubnetID: "s"})
	if err == nil {
		t.Fatal("zone name should fail")
	}
}
