package ha

import (
	"strings"
	"testing"

	"auto-dba/internal/store"
)

func TestRenderMHAAppConf(t *testing.T) {
	cl := &store.HACluster{
		ID: "c1", Name: "Demo-App", Engine: "mysql", Mode: "mha",
		Members: []store.HAMember{
			{InstanceID: "i1", Role: "primary", Host: "10.0.0.1", Port: 3306, InstanceName: "master", Enabled: true},
			{InstanceID: "i2", Role: "replica", Host: "10.0.0.2", Port: 3306, InstanceName: "slave1", Enabled: true},
			{InstanceID: "i3", Role: "replica", Host: "10.0.0.3", Port: 3307, InstanceName: "slave2", Enabled: true},
		},
	}
	out, err := RenderMHAAppConf(cl, MHAConfOptions{User: "mha", Password: "secret", SSHUser: "mysql"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"[server default]",
		"user=mha",
		"password=secret",
		"ssh_user=mysql",
		"hostname=10.0.0.1",
		"hostname=10.0.0.2",
		"hostname=10.0.0.3",
		"candidate_master=1",
		"/etc/mha/demo-app.cnf",
	} {
		if want == "/etc/mha/demo-app.cnf" {
			opts := DefaultMHAConfOptions(cl.Name)
			if opts.ConfPath != want {
				t.Fatalf("conf path got %s", opts.ConfPath)
			}
			continue
		}
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestRenderMHAAppConfNeedReplica(t *testing.T) {
	cl := &store.HACluster{
		ID: "c1", Name: "x",
		Members: []store.HAMember{
			{Role: "primary", Host: "10.0.0.1", Port: 3306, Enabled: true},
		},
	}
	if _, err := RenderMHAAppConf(cl, MHAConfOptions{}); err == nil {
		t.Fatal("expected error for single member")
	}
}
