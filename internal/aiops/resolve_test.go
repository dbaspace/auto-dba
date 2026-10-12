package aiops

import "testing"

func TestSplitHostPort(t *testing.T) {
	h, p := splitHostPort("db.example.com:5432")
	if h != "db.example.com" || p != 5432 {
		t.Fatalf("got %s %d", h, p)
	}
	h, p = splitHostPort("onlyhost")
	if h != "onlyhost" || p != 0 {
		t.Fatalf("got %s %d", h, p)
	}
}

func TestAlertNameFromLabels(t *testing.T) {
	if AlertNameFromLabels(map[string]string{"alertname": "HostCPUHigh"}) != "HostCPUHigh" {
		t.Fatal("miss")
	}
}
