package cloudsvc

import (
	"context"
	"fmt"
	"testing"

	"auto-dba/internal/cloud"
)

func TestSlowRetryStopsAfterThreeRateLimits(t *testing.T) {
	n := 0
	err := slowRetry(context.Background(), func() error {
		n++
		return fmt.Errorf("RequestLimitExceeded")
	})
	if n != 3 {
		t.Fatalf("attempts %d", n)
	}
	if !cloud.IsRateLimit(err) {
		t.Fatal(err)
	}
}

func TestRateLimitDoesNotAdvanceCursor(t *testing.T) {
	err := fmt.Errorf("RequestLimitExceeded")
	if !cloud.IsRateLimit(err) {
		t.Fatal("expected rate limit")
	}
	// 限频是错误，调用方不把窗口标成成功，游标停在原处。
	got := cloud.AdvanceCursor(cloud.Window{}.From, cloud.Window{To: cloud.Window{}.To}, false)
	if !got.IsZero() {
		t.Fatal("failed window must not move the cursor")
	}
}
