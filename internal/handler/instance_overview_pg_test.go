package handler

import (
	"context"
	"fmt"
	"testing"
)

func TestPGPromTimedOut(t *testing.T) {
	if !pgPromTimedOut(fmt.Errorf("Prometheus 网关超时（504）")) {
		t.Fatal("504")
	}
	if !pgPromTimedOut(context.DeadlineExceeded) {
		t.Fatal("deadline")
	}
	if pgPromTimedOut(fmt.Errorf("prometheus http 400: bad")) {
		t.Fatal("400 is not a timeout")
	}
	if pgPromTimedOut(nil) {
		t.Fatal("nil")
	}
}
