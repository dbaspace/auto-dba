package cloud

import (
	"testing"
	"time"
)

func TestPlanSlowPullFirstWindow(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	plan := PlanSlowPull(time.Time{}, now)
	if plan.Gap || len(plan.Windows) != 1 {
		t.Fatalf("first pull %+v", plan)
	}
	safe := now.Add(-SlowSafetyLag)
	w := plan.Windows[0]
	if !w.From.Equal(safe.Add(-SlowStep)) || !w.To.Equal(safe) {
		t.Fatalf("window %+v", w)
	}
	if w.To.Sub(w.From) != SlowStep {
		t.Fatalf("length %s", w.To.Sub(w.From))
	}
}

func TestPlanSlowPullCaughtUp(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	safe := now.Add(-SlowSafetyLag)
	plan := PlanSlowPull(safe, now)
	if plan.Gap || len(plan.Windows) != 0 {
		t.Fatalf("caught up should be empty: %+v", plan)
	}
}

func TestPlanSlowPullCapsAtSixWindows(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	safe := now.Add(-SlowSafetyLag)
	last := safe.Add(-2 * time.Hour)
	plan := PlanSlowPull(last, now)
	if len(plan.Windows) != SlowMaxWindows {
		t.Fatalf("windows %d", len(plan.Windows))
	}
	if !plan.Windows[0].From.Equal(last.Add(-SlowOverlap)) {
		t.Fatalf("overlap start %s", plan.Windows[0].From)
	}
	if !plan.Windows[0].To.Equal(last.Add(SlowStep)) {
		t.Fatalf("first end %s", plan.Windows[0].To)
	}
	end := plan.Windows[len(plan.Windows)-1].To
	if end.Sub(last) != time.Duration(SlowMaxWindows)*SlowStep {
		t.Fatalf("advanced %s", end.Sub(last))
	}
	if !end.Before(safe) {
		t.Fatal("one round must not jump to now")
	}
}

func TestPlanSlowPullGapDoesNotQueryExpiredRange(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	floor := now.Add(-SlowLogAPILookback)
	last := now.Add(-10 * 24 * time.Hour)
	plan := PlanSlowPull(last, now)
	if !plan.Gap || !plan.GapFrom.Equal(last) || !plan.GapTo.Equal(floor) {
		t.Fatalf("gap %+v", plan)
	}
	if len(plan.Windows) == 0 || plan.Windows[0].From.Before(floor) {
		t.Fatalf("window starts before retention: %+v", plan.Windows)
	}
}

func TestPlanSlowPullShortRemainder(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	safe := now.Add(-SlowSafetyLag)
	last := safe.Add(-90 * time.Second)
	plan := PlanSlowPull(last, now)
	if len(plan.Windows) != 1 || !plan.Windows[0].To.Equal(safe) {
		t.Fatalf("%+v", plan.Windows)
	}
}

func TestCatchUpFortyFiveInstances(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	safe := now.Add(-SlowSafetyLag)
	const instances = 45
	const regions = 3
	last := make([]time.Time, instances)
	for i := range last {
		last[i] = safe.Add(-2 * time.Hour)
	}
	apiCalls := 0
	rounds := 0
	start := time.Now()
	for {
		pending := 0
		roundCalls := 0
		for i := range last {
			plan := PlanSlowPull(last[i], now)
			if len(plan.Windows) > SlowMaxWindows {
				t.Fatalf("instance %d windows %d", i, len(plan.Windows))
			}
			roundCalls += len(plan.Windows)
			if len(plan.Windows) == 0 {
				continue
			}
			pending++
			last[i] = plan.Windows[len(plan.Windows)-1].To
		}
		if pending == 0 {
			break
		}
		rounds++
		apiCalls += roundCalls
		if rounds > 20 {
			t.Fatal("did not catch up")
		}
	}
	elapsed := time.Since(start)
	t.Logf("45 instances, %d regions, 2h behind: rounds=%d apiCalls=%d sim=%s peakConcurrent=%d",
		regions, rounds, apiCalls, elapsed, regions*2)
	if rounds < 4 || rounds > 6 {
		t.Fatalf("rounds %d", rounds)
	}
	for i, at := range last {
		if at.Before(safe) {
			t.Fatalf("instance %d still behind %s", i, safe.Sub(at))
		}
	}
}
