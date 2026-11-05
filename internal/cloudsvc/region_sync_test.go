package cloudsvc

import (
	"context"
	"fmt"
	"testing"
	"time"

	"auto-dba/internal/cloud"
)

func TestListRegionsParallel_overlapsAndKeepsOtherRegions(t *testing.T) {
	regions := []string{"ap-beijing", "eu-frankfurt", "ap-singapore", "na-siliconvalley"}
	started := make(chan string, len(regions))
	release := make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan []regionPage, 1)
	go func() {
		done <- listRegionsParallel(ctx, regions, func(ctx context.Context, region string) ([]cloud.Instance, error) {
			started <- region
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			if region == "eu-frankfurt" {
				return nil, fmt.Errorf("boom")
			}
			return []cloud.Instance{{CloudID: region + "-1"}}, nil
		})
	}()
	for i := 0; i < len(regions); i++ {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("地域请求没有并行开始")
		}
	}
	close(release)
	var pages []regionPage
	select {
	case pages = <-done:
	case <-ctx.Done():
		t.Fatal("并行请求没有返回")
	}
	if len(pages) != len(regions) {
		t.Fatalf("pages=%d", len(pages))
	}
	byRegion := map[string]regionPage{}
	for _, page := range pages {
		byRegion[page.Region] = page
	}
	if byRegion["eu-frankfurt"].Err == nil {
		t.Fatal("失败地域应保留错误")
	}
	for _, region := range []string{"ap-beijing", "ap-singapore", "na-siliconvalley"} {
		page := byRegion[region]
		if page.Err != nil || len(page.Items) != 1 || page.Items[0].CloudID != region+"-1" {
			t.Fatalf("%s %+v", region, page)
		}
	}
}

func TestUniqueRegions(t *testing.T) {
	got := uniqueRegions([]string{" ap-beijing ", "", "ap-beijing", "eu-frankfurt"})
	if len(got) != 2 || got[0] != "ap-beijing" || got[1] != "eu-frankfurt" {
		t.Fatalf("%v", got)
	}
}
