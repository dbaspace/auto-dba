package handler

import (
	"reflect"
	"testing"

	"auto-dba/internal/store"
)

func TestHostFromMonitorEndpoint(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"10.0.0.1:9100", "10.0.0.1"},
		{"http://10.0.0.2:8030/metrics", "10.0.0.2"},
		{"fe.example.com:8030", "fe.example.com"},
		{"192.168.1.10", "192.168.1.10"},
		{"[2001:db8::1]:9100", "2001:db8::1"},
		{"", ""},
	}
	for _, c := range cases {
		if got := hostFromMonitorEndpoint(c.in); got != c.want {
			t.Fatalf("hostFromMonitorEndpoint(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestSyncNormHost(t *testing.T) {
	if got := syncNormHost("10.0.0.1"); got != "10.0.0.1" {
		t.Fatalf("got %q", got)
	}
	if got := syncNormHost("Fe.Example.COM."); got != "fe.example.com" {
		t.Fatalf("got %q", got)
	}
}

func TestAbsorbMonitorLabelsKeepsFirstValue(t *testing.T) {
	h := &monitorHost{Labels: map[string]string{}}
	absorbMonitorLabels(h, mergeMonitorLabels(map[string]string{"idc": "a"}, map[string]string{"idc": "beijing", "app": "fe"}), "local")
	if h.Labels["idc"] != "beijing" || h.Labels["app"] != "fe" {
		t.Fatalf("first group labels=%v", h.Labels)
	}
	absorbMonitorLabels(h, map[string]string{"idc": "test", "domain": "sr"}, "beijing")
	if h.Labels["idc"] != "beijing" || h.Labels["domain"] != "sr" {
		t.Fatalf("must not replace idc, labels=%v", h.Labels)
	}
	if !reflect.DeepEqual(h.Collectors, []string{"local", "beijing"}) {
		t.Fatalf("collectors=%v", h.Collectors)
	}
}

func TestMatchMonitorHostLocation(t *testing.T) {
	envs := []store.Environment{
		{ID: "e-bj", Code: "beijing", Name: "北京"},
		{ID: "e-test", Code: "test", Name: "测试"},
	}
	regions := []store.Region{
		{ID: "r-def", Code: "default", Name: "默认"},
		{ID: "r-bj", Code: "beijing", Name: "beijing"},
		{ID: "r-other", Code: "other", Name: "其他"},
	}
	binds := []store.RegionEnvBinding{
		{RegionID: "r-bj", EnvID: "e-bj"},
		{RegionID: "r-def", EnvID: "e-test"},
		{RegionID: "r-other", EnvID: "e-test"},
	}

	regionID, envID, tags, ok := matchMonitorHostLocation(
		map[string]string{"idc": "beijing", "app": "fe-StarRocks"},
		nil, envs, regions, binds,
	)
	if !ok || regionID != "r-bj" || envID != "e-bj" {
		t.Fatalf("beijing: region=%s env=%s ok=%v", regionID, envID, ok)
	}
	if !reflect.DeepEqual(tags, []string{"app=fe-StarRocks", "idc=beijing"}) {
		t.Fatalf("tags=%v", tags)
	}

	regionID, envID, tags, ok = matchMonitorHostLocation(
		map[string]string{"env": "测试", "domain": "menu"},
		nil, envs, regions, binds,
	)
	if !ok || envID != "e-test" || regionID != "" {
		t.Fatalf("ambiguous region must stay empty: region=%s env=%s ok=%v", regionID, envID, ok)
	}
	if !reflect.DeepEqual(tags, []string{"domain=menu", "env=测试", "idc=test"}) {
		t.Fatalf("tags=%v", tags)
	}

	envs = append(envs, store.Environment{ID: "e-prod", Code: "prod", Name: "生产"})
	regions = append(regions, store.Region{ID: "r-prod", Code: "cn", Name: "prod"}, store.Region{ID: "r-spare", Code: "spare", Name: "备用"})
	binds = append(binds, store.RegionEnvBinding{RegionID: "r-spare", EnvID: "e-prod"}, store.RegionEnvBinding{RegionID: "r-prod", EnvID: "e-prod"})
	regionID, envID, _, ok = matchMonitorHostLocation(
		map[string]string{"environment": "prod"},
		nil, envs, regions, binds,
	)
	if !ok || regionID != "r-prod" || envID != "e-prod" {
		t.Fatalf("multi region pick by name: region=%s env=%s ok=%v", regionID, envID, ok)
	}

	regionID, envID, tags, ok = matchMonitorHostLocation(
		map[string]string{"idc": "no-such"},
		[]string{"beijing"}, envs, regions, binds,
	)
	if !ok || regionID != "r-bj" || envID != "e-bj" {
		t.Fatalf("collector fallback: region=%s env=%s ok=%v", regionID, envID, ok)
	}

	regionID, envID, tags, ok = matchMonitorHostLocation(
		map[string]string{"app": "node", "idc": "unknown"},
		[]string{"本地登记"}, envs, regions, binds,
	)
	if ok || regionID != "" || envID != "" {
		t.Fatalf("unknown env must not invent location: region=%s env=%s ok=%v", regionID, envID, ok)
	}
	if !reflect.DeepEqual(tags, []string{"app=node", "idc=unknown"}) {
		t.Fatalf("tags=%v", tags)
	}
}
