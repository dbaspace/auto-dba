package store

import "testing"

func TestLoginIdleTimeoutMin(t *testing.T) {
	d := openTestDB(t)
	if got := d.LoginIdleTimeoutMin(30); got != 30 {
		t.Fatalf("empty want 30, got %d", got)
	}
	if err := d.SetGlobalArg(ArgLoginIdleTimeoutMin, "0", "off"); err != nil {
		t.Fatal(err)
	}
	if got := d.LoginIdleTimeoutMin(30); got != 0 {
		t.Fatalf("explicit 0 want 0, got %d", got)
	}
	if err := d.SetGlobalArg(ArgLoginIdleTimeoutMin, "15", ""); err != nil {
		t.Fatal(err)
	}
	if got := d.LoginIdleTimeoutMin(30); got != 15 {
		t.Fatalf("want 15, got %d", got)
	}
	if err := d.SetGlobalArg(ArgLoginIdleTimeoutMin, "abc", ""); err != nil {
		t.Fatal(err)
	}
	if got := d.LoginIdleTimeoutMin(30); got != 30 {
		t.Fatalf("invalid want 30, got %d", got)
	}
}
