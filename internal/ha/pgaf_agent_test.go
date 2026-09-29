package ha

import "testing"

func TestParsePGAFOptions(t *testing.T) {
	o := ParsePGAFOptions(`{"formation":"app","group":"1","monitor":"postgres://x"}`)
	if o.Formation != "app" || o.Group != "1" || o.Monitor != "postgres://x" {
		t.Fatalf("%+v", o)
	}
	o2 := ParsePGAFOptions("postgres://monitor/db")
	if o2.Monitor != "postgres://monitor/db" || o2.Formation != "default" {
		t.Fatalf("%+v", o2)
	}
	o3 := ParsePGAFOptions("/var/lib/postgresql/monitor")
	if o3.PGData != "/var/lib/postgresql/monitor" {
		t.Fatalf("%+v", o3)
	}
}
