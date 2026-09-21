package rediscmd

import "testing"

func TestMergeRulesDisableCommand(t *testing.T) {
	en := false
	min := 3
	saved := &RulesConfig{
		Blacklist: []string{"FLUSHALL", "EVAL"},
		Commands: []CmdOverride{
			{Name: "SET", Enabled: &en, MinArgs: &min},
		},
	}
	if err := ValidateCommandWith(saved, "SET a b"); err == nil {
		t.Fatal("disabled SET should fail")
	}
	if err := ValidateCommandWith(saved, "GET a"); err != nil {
		t.Fatalf("GET should pass: %v", err)
	}
	if err := ValidateCommandWith(saved, "EVAL 1"); err == nil {
		t.Fatal("EVAL in blacklist should fail")
	}
	cats := EnabledCatalog(saved)
	for _, c := range cats {
		if c.Name == "SET" {
			t.Fatal("SET should not appear in enabled catalog")
		}
	}
}

func TestMergeRulesMinArgsOverride(t *testing.T) {
	min := 5
	en := true
	saved := &RulesConfig{
		Commands: []CmdOverride{{Name: "DEL", Enabled: &en, MinArgs: &min}},
	}
	if err := ValidateCommandWith(saved, "DEL a"); err == nil {
		t.Fatal("DEL with 1 arg should fail when min_args=5")
	}
	if err := ValidateCommandWith(saved, "DEL a b c d e"); err != nil {
		t.Fatal(err)
	}
}
