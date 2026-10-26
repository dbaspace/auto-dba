package ticketexec

import "testing"

func TestLikePrefixPatternEscapesWildcard(t *testing.T) {
	if got := likePrefixPattern(`db_order_`); got != `db\_order\_%` {
		t.Fatalf("got %q", got)
	}
	if got := likePrefixPattern(`a%b\c`); got != `a\%b\\c%` {
		t.Fatalf("got %q", got)
	}
}
