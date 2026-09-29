package sshx

import (
	"strings"
	"testing"
)

func TestNormalizeEscalate(t *testing.T) {
	if NormalizeEscalate("sudo_su") != EscalateSudoSu {
		t.Fatal("sudo_su")
	}
	if NormalizeEscalate("sudo") != EscalateSudoSu {
		t.Fatal("sudo")
	}
	if NormalizeEscalate("") != EscalateNone {
		t.Fatal("empty")
	}
	if NormalizeEscalate("none") != EscalateNone {
		t.Fatal("none")
	}
}

func TestWrapEscalateNone(t *testing.T) {
	cmd := "check pg_dump logical_pg"
	if got := WrapEscalate(cmd, ""); got != cmd {
		t.Fatalf("got %q", got)
	}
}

func TestWrapEscalateSudoSu(t *testing.T) {
	cmd := "echo hello; check x"
	got := WrapEscalate(cmd, EscalateSudoSu)
	wantPrefix := "sudo -n su - root -c "
	if !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("got %q", got)
	}
	quoted := strings.TrimPrefix(got, wantPrefix)
	if quoted != ShellQuote(cmd) {
		t.Fatalf("quoted=%q want=%q", quoted, ShellQuote(cmd))
	}
}

func TestShellQuoteEmbed(t *testing.T) {
	s := "it's fine"
	q := ShellQuote(s)
	if q != `'it'\''s fine'` {
		t.Fatalf("got %q", q)
	}
}
