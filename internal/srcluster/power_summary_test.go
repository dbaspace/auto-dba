package srcluster

import (
	"testing"

	"auto-dba/internal/store"
)

func TestPowerChangeSummary(t *testing.T) {
	stop := &store.SrChange{
		Type: store.SrChangeStopNode,
		Steps: []store.SrChangeStep{
			{StepKey: "stop_node_1", Title: "停止 10.128.19.24"},
			{StepKey: "finalize", Title: "完成"},
		},
	}
	if got := powerChangeSummary(stop); got != "停止 10.128.19.24" {
		t.Fatalf("got %q", got)
	}
	start := &store.SrChange{Type: store.SrChangeStartNode, Steps: []store.SrChangeStep{{StepKey: "start_node_1", Title: "启动 10.128.16.128"}}}
	if got := powerChangeSummary(start); got != "启动 10.128.16.128" {
		t.Fatalf("got %q", got)
	}
	if got := powerChangeSummary(&store.SrChange{Type: store.SrChangeUpgrade, Steps: []store.SrChangeStep{{Title: "升级 10.0.0.1"}}}); got != "" {
		t.Fatalf("其它类型不应改摘要: %q", got)
	}
}
