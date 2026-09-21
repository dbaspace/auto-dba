package cdc

import (
	"sync"
	"testing"
	"time"
)

func TestShardFanoutMaxConcurrencyIsSerial(t *testing.T) {
	if shardFanoutMaxConcurrency != 1 {
		t.Fatalf("shardFanoutMaxConcurrency=%d, want 1", shardFanoutMaxConcurrency)
	}
}

func TestRunShardStepsSerialDoesNotOverlap(t *testing.T) {
	var mu sync.Mutex
	active := 0
	maxActive := 0
	order := make([]int, 0, 3)
	err := runShardStepsSerial(3, func(i int) error {
		mu.Lock()
		active++
		if active > maxActive {
			maxActive = active
		}
		if active != 1 {
			mu.Unlock()
			t.Errorf("step %d overlapped, active=%d", i, active)
			return nil
		}
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
		mu.Lock()
		active--
		order = append(order, i)
		mu.Unlock()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if maxActive != 1 {
		t.Fatalf("maxActive=%d, want 1", maxActive)
	}
	if len(order) != 3 || order[0] != 0 || order[1] != 1 || order[2] != 2 {
		t.Fatalf("order=%v, want 0,1,2", order)
	}
}
