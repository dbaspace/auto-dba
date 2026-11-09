package handler

import (
	"context"
	"testing"
)

func TestTakeNFull(t *testing.T) {
	ch := make(chan int, 2)
	ch <- 1
	ch <- 2
	got := takeN(context.Background(), ch, 2)
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("got %v", got)
	}
}

func TestTakeNStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ch := make(chan int)
	got := takeN(ctx, ch, 3)
	if len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestTakeNZero(t *testing.T) {
	got := takeN(context.Background(), make(chan int), 0)
	if got != nil {
		t.Fatalf("got %v", got)
	}
}
