package cloudsvc

import (
	"context"
	"strings"
	"sync"

	"auto-dba/internal/cloud"
)

type regionPage struct {
	Region string
	Items  []cloud.Instance
	Err    error
}

func uniqueRegions(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, region := range in {
		region = strings.TrimSpace(region)
		if region == "" || seen[region] {
			continue
		}
		seen[region] = true
		out = append(out, region)
	}
	return out
}

// listRegionsParallel 用同一套密钥并行请求各个云地域。每个地域的失败不影响其它地域。
func listRegionsParallel(ctx context.Context, regions []string, list func(context.Context, string) ([]cloud.Instance, error)) []regionPage {
	out := make([]regionPage, len(regions))
	var wg sync.WaitGroup
	for i, region := range regions {
		wg.Add(1)
		go func(i int, region string) {
			defer wg.Done()
			items, err := list(ctx, region)
			out[i] = regionPage{Region: region, Items: items, Err: err}
		}(i, region)
	}
	wg.Wait()
	return out
}
