package instoverview

import (
	"context"
	"errors"
	"testing"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		err  error
		kind string
	}{
		{context.DeadlineExceeded, "timeout"},
		{errors.New("dial tcp: i/o timeout"), "timeout"},
		{errors.New("pq: password authentication failed"), "auth"},
		{errors.New("WRONGPASS invalid username-password pair"), "auth"},
		{errors.New("pq: permission denied for relation pg_stat_activity"), "permission"},
		{errors.New("dial tcp 10.0.0.1:5432: connect: connection refused"), "connect"},
	}
	for _, c := range cases {
		kind, msg := Classify(c.err)
		if kind != c.kind || msg == "" {
			t.Fatalf("%v => %s %s", c.err, kind, msg)
		}
	}
}
