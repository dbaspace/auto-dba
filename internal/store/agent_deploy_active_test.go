package store

import (
	"testing"
	"time"
)

func TestAgentDeployActive(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-2 * time.Minute).Format(time.RFC3339)
	stale := now.Add(-20 * time.Minute).Format(time.RFC3339)
	cases := []struct {
		status, created string
		want            bool
	}{
		{SrDeployRunning, recent, true},
		{SrDeployPending, recent, true},
		{SrDeployRunning, stale, false},
		{SrDeploySuccess, recent, false},
		{SrDeployFailed, recent, false},
		{SrDeployRunning, "bad-time", true},
		{SrDeployRunning, now.Add(-2 * time.Minute).In(time.FixedZone("CST", 8*3600)).Format(time.RFC3339), true},
	}
	for _, c := range cases {
		if got := AgentDeployActive(c.status, c.created, now); got != c.want {
			t.Errorf("AgentDeployActive(%q,%q)=%v want %v", c.status, c.created, got, c.want)
		}
	}
}
