package handler

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"auto-dba/internal/store"
	"auto-dba/internal/ticketexec"
)

func TestPrevChangeID(t *testing.T) {
	if got := prevChangeID(`{"success":false,"change_id":" abc "}`); got != "abc" {
		t.Fatalf("got %q", got)
	}
	if prevChangeID("") != "" || prevChangeID("not json") != "" {
		t.Fatal("invalid input should be empty")
	}
}

func TestFillChangeResultFailed(t *testing.T) {
	snap := &clusterChangeSnap{Status: store.SrChangeFailed, Steps: []clusterStepSnap{
		{Seq: 1, Title: "探活 Agent", Status: store.SrStepSuccess, Log: "ok"},
		{Seq: 2, Title: "升级 FE 10.0.0.1", Status: store.SrStepFailed,
			Log: "15:00:01 [replaced] ok\n15:00:05 失败: 升级后启服失败，已停止后续节点: exit status 1\n诊断信息：\nError: JDK 8 is not supported"},
		{Seq: 3, Title: "finalize", Status: store.SrStepPending},
	}}
	res := &ticketexec.Result{}
	fillChangeResult(res, snap, "f0827f58-1111", "版本升级")
	if res.Success || res.SuccessCount != 1 || res.FailCount != 1 || len(res.Records) != 3 {
		t.Fatalf("counts: %+v", res)
	}
	if !strings.Contains(res.Message, "升级 FE 10.0.0.1") || !strings.Contains(res.Message, "升级后启服失败") {
		t.Fatalf("message: %s", res.Message)
	}
	if !strings.Contains(res.Records[1].ErrorMessage, "JDK 8 is not supported") {
		t.Fatalf("record should carry diag tail: %q", res.Records[1].ErrorMessage)
	}
	if !strings.Contains(res.Summary, "f0827f58") || strings.Contains(res.Summary, "1111") {
		t.Fatalf("summary: %s", res.Summary)
	}
}

func TestWaitClusterChangeUntilFinal(t *testing.T) {
	calls := 0
	ops := clusterChangeOps{load: func(string) (*clusterChangeSnap, error) {
		calls++
		st := store.SrChangeRunning
		if calls >= 2 {
			st = store.SrChangeSuccess
		}
		return &clusterChangeSnap{Status: st, Steps: []clusterStepSnap{{Seq: 1, Title: "a", Status: store.SrStepSuccess}}}, nil
	}}
	progress := 0
	res := &ticketexec.Result{}
	start := time.Now()
	waitClusterChange(context.Background(), res, "c1", "部署", ops, func(*ticketexec.Result) { progress++ })
	if !res.Success || res.ChangeID != "c1" || progress != 1 {
		t.Fatalf("res=%+v progress=%d", res, progress)
	}
	if time.Since(start) > 2*clusterWaitInterval {
		t.Fatal("waited too long")
	}
}

func TestWaitClusterChangeTimeout(t *testing.T) {
	ops := clusterChangeOps{load: func(string) (*clusterChangeSnap, error) {
		return &clusterChangeSnap{Status: store.SrChangeRunning}, nil
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	res := &ticketexec.Result{}
	waitClusterChange(ctx, res, "c2", "升级", ops, nil)
	if res.Success || !strings.Contains(res.Message, "超时") || res.ChangeID != "c2" {
		t.Fatalf("res=%+v", res)
	}
}

func TestResumeClusterChange(t *testing.T) {
	status := map[string]string{"failed": store.SrChangeFailed, "run": store.SrChangeRunning, "ok": store.SrChangeSuccess}
	var retried []string
	ops := clusterChangeOps{
		load: func(id string) (*clusterChangeSnap, error) {
			if s, ok := status[id]; ok {
				return &clusterChangeSnap{Status: s}, nil
			}
			return nil, nil
		},
		retry: func(id string) error {
			retried = append(retried, id)
			if id == "bad" {
				return errors.New("boom")
			}
			return nil
		},
	}
	if id, err := resumeClusterChange("", ops); id != "" || err != nil {
		t.Fatal("empty prev")
	}
	if id, _ := resumeClusterChange("missing", ops); id != "" {
		t.Fatal("missing change should create new")
	}
	for _, id := range []string{"failed", "run", "ok"} {
		if got, err := resumeClusterChange(id, ops); got != id || err != nil {
			t.Fatalf("%s: got %q err %v", id, got, err)
		}
	}
	if len(retried) != 1 || retried[0] != "failed" {
		t.Fatalf("only failed should retry: %v", retried)
	}
}
