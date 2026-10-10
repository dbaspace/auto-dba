package alert

import (
	"testing"
	"time"

	"auto-dba/internal/store"
)

func TestShouldFire(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	since := now.Add(-6 * time.Minute).Format(time.RFC3339)
	if !ShouldFire(since, 5*time.Minute, now) {
		t.Fatal("should fire after for")
	}
	since2 := now.Add(-2 * time.Minute).Format(time.RFC3339)
	if ShouldFire(since2, 5*time.Minute, now) {
		t.Fatal("should not fire yet")
	}
}

func TestApplySilence(t *testing.T) {
	a := &store.Alert{
		Handle: store.AlertHandleUnclaimed,
		Labels: map[string]string{"service": "order", "env": "prod"},
	}
	sil := []store.AlertSilence{{Matchers: "service=order, env=prod"}}
	if !ApplySilence(a, sil) || a.Handle != store.AlertHandleSilenced {
		t.Fatalf("%+v", a)
	}
	if ApplySilence(a, sil) {
		t.Fatal("second apply should be no-op change=false")
	}
	if !ApplySilence(a, nil) || a.Handle != store.AlertHandleUnclaimed {
		t.Fatalf("unsilence: %+v", a)
	}
}

func TestMergeLabelsAndTitle(t *testing.T) {
	labels := MergeLabels(map[string]string{"team": "dba"}, map[string]string{"service": "mysql", "__name__": "up"})
	if labels["team"] != "dba" || labels["service"] != "mysql" || labels["__name__"] != "" {
		t.Fatalf("%+v", labels)
	}
	r := store.AlertRule{Name: "连接池", Annotations: map[string]string{"summary": "MySQL 连接过高"}}
	if TitleFrom(r, labels) != "MySQL 连接过高" {
		t.Fatal(TitleFrom(r, labels))
	}
}
