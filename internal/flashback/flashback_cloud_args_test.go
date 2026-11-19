package flashback

import (
	"context"
	"testing"
)

func TestFlashbackDownloadArgParse(t *testing.T) {
	if flashbackNonNegOr("0", 2) != 0 || flashbackNonNegOr(" 16 ", 2) != 16 {
		t.Fatal("zero and trimmed values should be kept")
	}
	if flashbackNonNegOr("-1", 2) != 2 || flashbackNonNegOr("abc", 16) != 16 || flashbackNonNegOr("", 400) != 400 {
		t.Fatal("negative, text, and empty should use the fallback")
	}
	if flashbackAtLeast1Or("0", 48) != 48 || flashbackAtLeast1Or("-3", 48) != 48 || flashbackAtLeast1Or("no", 48) != 48 {
		t.Fatal("package cap should ignore non-positive and text")
	}
	if flashbackAtLeast1Or("12", 48) != 12 {
		t.Fatal("positive package cap should be kept")
	}
	if flashbackRetryCount("0") != 1 || flashbackRetryCount("-2") != 1 {
		t.Fatal("non-positive retries should mean one attempt")
	}
	if flashbackRetryCount("x") != 3 || flashbackRetryCount("") != 3 || flashbackRetryCount("5") != 5 {
		t.Fatal("retry fallback mismatch")
	}
}

func TestFlashbackOnlyRegion(t *testing.T) {
	if flashbackOnlyRegion(nil) != "" {
		t.Fatal("empty")
	}
	if flashbackOnlyRegion([]string{"", " ap-beijing "}) != "ap-beijing" {
		t.Fatal("single region")
	}
	if flashbackOnlyRegion([]string{"ap-beijing", "AP-BEIJING"}) != "ap-beijing" {
		t.Fatal("same region should count as one")
	}
	if flashbackOnlyRegion([]string{"ap-beijing", "ap-shanghai"}) != "" {
		t.Fatal("multiple regions should not be guessed")
	}
}

func TestFlashbackResolveRegionOrder(t *testing.T) {
	prev := runtime
	runtime = nil
	t.Cleanup(func() { runtime = prev })
	t.Setenv("FLASHBACK_TENCENT_REGION", "")

	got, err := flashbackResolveTencentRegion(context.Background(), &ResourceDbsInfo{
		Tags: map[string]interface{}{"flash_region": "ap-guangzhou"},
	}, "ap-shanghai")
	if err != nil || got.Region != "ap-guangzhou" {
		t.Fatalf("MDM region should win, got %+v %v", got, err)
	}

	got, err = flashbackResolveTencentRegion(context.Background(), nil, "beijing")
	if err != nil || got.Region != "ap-beijing" {
		t.Fatalf("instance cloud_region should resolve, got %+v %v", got, err)
	}

	ctx := context.WithValue(context.Background(), credKey{}, credState{account: "acc-1"})
	if _, err = flashbackResolveTencentRegion(ctx, nil, ""); err == nil {
		t.Fatal("missing account store should still fail closed")
	}
}
