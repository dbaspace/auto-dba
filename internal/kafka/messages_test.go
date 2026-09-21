package kafka

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

func TestKafkaReadAbortErrCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := kafkaReadAbortErr(ctx, context.Canceled)
	if !errors.Is(got, context.Canceled) {
		t.Fatalf("got %v", got)
	}
}

func TestKafkaReadAbortErrDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)
	got := kafkaReadAbortErr(ctx, io.EOF)
	if !errors.Is(got, context.DeadlineExceeded) {
		t.Fatalf("got %v", got)
	}
}

func TestKafkaReadAbortErrEOF(t *testing.T) {
	if got := kafkaReadAbortErr(context.Background(), io.EOF); got != nil {
		t.Fatalf("EOF should end read, not fail query, got %v", got)
	}
	if got := kafkaReadAbortErr(context.Background(), nil); got != nil {
		t.Fatalf("nil err got %v", got)
	}
}

func TestKafkaReadRangeEmpty(t *testing.T) {
	_, n := kafkaReadRange(12, 12, -1, 100, false, 12)
	if n != 0 {
		t.Fatalf("empty partition count=%d", n)
	}
}

func TestKafkaReadRangeOldestNewest(t *testing.T) {
	start, n := kafkaReadRange(10, 30, -1, 5, false, 10)
	if start != 10 || n != 5 {
		t.Fatalf("oldest start=%d n=%d", start, n)
	}
	start, n = kafkaReadRange(10, 30, -1, 5, true, 10)
	if start != 25 || n != 5 {
		t.Fatalf("newest start=%d n=%d", start, n)
	}
	start, n = kafkaReadRange(10, 15, -1, 100, false, 10)
	if start != 10 || n != 5 {
		t.Fatalf("cap start=%d n=%d", start, n)
	}
}

func TestKafkaReadRangeSinceFloor(t *testing.T) {
	start, n := kafkaReadRange(10, 100, -1, 5, false, 80)
	if start != 80 || n != 5 {
		t.Fatalf("oldest since start=%d n=%d", start, n)
	}
	start, n = kafkaReadRange(10, 100, -1, 5, true, 80)
	if start != 95 || n != 5 {
		t.Fatalf("newest since start=%d n=%d", start, n)
	}
	start, n = kafkaReadRange(10, 100, -1, 50, true, 90)
	if start != 90 || n != 10 {
		t.Fatalf("newest capped by since start=%d n=%d", start, n)
	}
	_, n = kafkaReadRange(10, 50, -1, 10, false, 50)
	if n != 0 {
		t.Fatalf("floor at last should be empty, n=%d", n)
	}
	// 时间下界抬空后，回退用 first 作为 floor 应能读到末尾
	start, n = kafkaReadRange(10, 50, -1, 5, true, 10)
	if start != 45 || n != 5 {
		t.Fatalf("fallback newest start=%d n=%d", start, n)
	}
}
