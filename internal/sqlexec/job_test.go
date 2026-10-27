package sqlexec

import (
	"testing"
	"time"
)

func TestRunnerKeepsTerminalJobUntilRetention(t *testing.T) {
	r := NewRunner()
	view := r.Submit("job-1", JobRequest{})
	if view.Status != JobRunning || view.StartedAt == "" {
		t.Fatalf("submit view %+v", view)
	}
	deadline := time.Now().Add(2 * time.Second)
	var got JobView
	var ok bool
	for time.Now().Before(deadline) {
		got, ok = r.Get("job-1", 0)
		if ok && got.Status == JobDone && got.EndedAt != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ok || got.Status != JobDone || got.StartedAt == "" || got.EndedAt == "" {
		t.Fatalf("finished view ok=%v %+v", ok, got)
	}

	r.mu.Lock()
	job := r.jobs["job-1"]
	job.mu.Lock()
	job.ended = time.Now().Add(-jobRetain - time.Minute)
	job.mu.Unlock()
	r.mu.Unlock()

	if _, ok := r.Get("job-1", 0); ok {
		t.Fatal("expired job should be gone")
	}
	if _, ok := r.Get("missing", 0); ok {
		t.Fatal("missing job should be gone")
	}
}
