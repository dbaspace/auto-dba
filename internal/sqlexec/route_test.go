package sqlexec

import "testing"

func TestDecide(t *testing.T) {
	execs := []Executor{{
		ID: "eu", Name: "欧洲", Enabled: true, EnvCodes: []string{"frankfurt", "europe"},
	}, {
		ID: "sg", Name: "新加坡", Enabled: true, EnvCodes: []string{"singapore"},
	}}
	local := []string{"beijing"}

	if d := Decide("beijing", local, execs); d.Mode != ModeLocal {
		t.Fatalf("beijing got %s", d.Mode)
	}
	if d := Decide("frankfurt", local, execs); d.Mode != ModeRemote || d.Executor.ID != "eu" {
		t.Fatalf("frankfurt %+v", d)
	}
	if d := Decide("europe", local, execs); d.Mode != ModeRemote || d.Executor.ID != "eu" {
		t.Fatalf("europe %+v", d)
	}
	if d := Decide("singapore", local, execs); d.Mode != ModeRemote || d.Executor.Name != "新加坡" {
		t.Fatalf("sg %+v", d)
	}
	if d := Decide("tokyo", local, execs); d.Mode != ModeMissing {
		t.Fatalf("unbound overseas got %s", d.Mode)
	}
	if d := Decide("tokyo", nil, execs); d.Mode != ModeLocal {
		t.Fatalf("unset local idc should stay direct, got %s", d.Mode)
	}
	disabled := []Executor{{ID: "sg", Enabled: false, EnvCodes: []string{"singapore"}}}
	if d := Decide("singapore", local, disabled); d.Mode != ModeMissing {
		t.Fatalf("disabled executor got %s", d.Mode)
	}
}
