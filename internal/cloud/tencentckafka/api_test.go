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
