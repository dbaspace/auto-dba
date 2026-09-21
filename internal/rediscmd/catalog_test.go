package rediscmd

import "testing"

func TestValidateCommand(t *testing.T) {
	if err := ValidateCommand("SET foo bar"); err != nil {
		t.Fatalf("SET should pass: %v", err)
	}
	if err := ValidateCommand("GET foo"); err != nil {
		t.Fatalf("GET should pass: %v", err)
	}
	if err := ValidateCommand("FLUSHALL"); err == nil {
		t.Fatal("FLUSHALL should be rejected")
	}
	if err := ValidateCommand("KEYS *"); err == nil {
		t.Fatal("KEYS should be rejected")
	}
	if err := ValidateCommand("EVAL"); err == nil {
		t.Fatal("EVAL not in whitelist")
	}
	if err := ValidateCommand("SET"); err == nil {
		t.Fatal("SET without args should fail")
	}
	if err := ValidateCommand("  "); err == nil {
		t.Fatal("empty should fail")
	}
}

func TestCatalogNonEmpty(t *testing.T) {
	if len(Catalog()) < 10 {
		t.Fatal("catalog too small")
	}
	if Lookup("hset") == nil || Lookup("DEL") == nil {
		t.Fatal("lookup should be case-insensitive")
	}
}

func TestParseScript(t *testing.T) {
	st := ParseScript("# comment\n\nSET a b\nDEL a\nFLUSHALL\n")
	if st.CommentCount != 3 { // #comment + blank + trailing blank
		t.Fatalf("comment/empty count: %d", st.CommentCount)
	}
	if len(st.Valid) != 2 || len(st.Invalid) != 1 {
		t.Fatalf("valid=%d invalid=%d", len(st.Valid), len(st.Invalid))
	}
	if st.HighestCRUD != "delete" {
		t.Fatalf("highest crud: %s", st.HighestCRUD)
	}
	if err := ValidateScript("SET a b\nDEL a"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateScript("# only\n"); err == nil {
		t.Fatal("empty valid should fail")
	}
	if err := ValidateScript("SET a b\nKEYS *"); err == nil {
		t.Fatal("invalid line should fail")
	}
}
