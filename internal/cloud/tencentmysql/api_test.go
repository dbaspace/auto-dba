package tencentmysql

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"auto-dba/internal/cloud"
)

func TestListInstancesFillsDiskUsed(t *testing.T) {
	r := Reader{&Client{Do: func(req *http.Request) (*http.Response, error) {
		var body string
		switch req.Header.Get("X-TC-Action") {
		case "DescribeDBInstances":
			body = `{"Response":{"TotalCount":1,"Items":[{"InstanceId":"cdb-a","InstanceName":"prod","Status":1,"Cpu":4,"Memory":8000,"Volume":100,"Vip":"10.1.1.1","Vport":3306}]}}`
		case "GetMonitorData":
			raw, _ := io.ReadAll(req.Body)
			if !strings.Contains(string(raw), `"MetricName":"Capacity"`) || !strings.Contains(string(raw), "cdb-a") {
				t.Fatalf("monitor body %s", raw)
			}
			body = `{"Response":{"DataPoints":[{"Dimensions":[{"Name":"InstanceId","Value":"cdb-a"}],"Values":[null,1536.4]}]}}`
		default:
			t.Fatalf("action %s host %s", req.Header.Get("X-TC-Action"), req.Host)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	}}}
	list, err := r.ListInstances(context.Background(), cloud.Cred{Region: "ap-beijing", SecretID: "id", SecretKey: "key"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].CloudID != "cdb-a" || list[0].DiskGB != 100 || list[0].DiskUsed != 2 {
		t.Fatalf("list = %+v", list)
	}
	mb, ok := cloud.DiskUsedMB(list[0].Extras)
	if !ok || mb != 1536 {
		t.Fatalf("used mb=%d ok=%v", mb, ok)
	}
}

func TestListInstancesKeepsInventoryWhenMonitorFails(t *testing.T) {
	r := Reader{&Client{Do: func(req *http.Request) (*http.Response, error) {
		body := `{"Response":{"Error":{"Code":"InternalError","Message":"monitor down"}}}`
		if req.Header.Get("X-TC-Action") == "DescribeDBInstances" {
			body = `{"Response":{"TotalCount":1,"Items":[{"InstanceId":"cdb-b","InstanceName":"test","Status":1,"Volume":200}]}}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	}}}
	list, err := r.ListInstances(context.Background(), cloud.Cred{Region: "ap-beijing", SecretID: "id", SecretKey: "key"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].DiskGB != 200 || list[0].DiskUsed != 0 {
		t.Fatalf("list = %+v", list)
	}
	if _, ok := cloud.DiskUsedMB(list[0].Extras); ok {
		t.Fatal("failed monitor should not invent usage")
	}
}
