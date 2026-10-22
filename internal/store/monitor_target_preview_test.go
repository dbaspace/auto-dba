package store

import "testing"

func TestStarRocksPreviewBeijingFENotBE(t *testing.T) {
	labels := mergeLabels(
		map[string]string{
			"domain":  "dorisdb.supply.bj.restosuite.cn",
			"idc":     "beijing",
			"version": "3.2.10",
		},
		map[string]string{
			"app":     "fe-StarRocks",
			"domain":  "dorisdb.supply.bj.restosuite.cn",
			"idc":     "beijing",
			"version": "3.2.10",
		},
	)
	ep := "10.128.20.117:8030"
	fe := starRocksPreviewEndpoints("fe", []string{ep}, labels)
	be := starRocksPreviewEndpoints("be", []string{ep}, labels)
	if len(fe) != 1 || fe[0] != ep {
		t.Fatalf("FE 预览=%v", fe)
	}
	if len(be) != 0 {
		t.Fatalf("idc=beijing 不应把 FE 放进 BE 预览: %v", be)
	}
}

func TestStarRocksPreviewPortSplitsMixedGroup(t *testing.T) {
	labels := map[string]string{"app": "fe-StarRocks", "idc": "beijing"}
	eps := []string{"10.128.20.117:8030", "10.128.16.6:8040"}
	fe := starRocksPreviewEndpoints("fe", eps, labels)
	be := starRocksPreviewEndpoints("be", eps, labels)
	if len(fe) != 1 || fe[0] != "10.128.20.117:8030" {
		t.Fatalf("FE=%v", fe)
	}
	if len(be) != 1 || be[0] != "10.128.16.6:8040" {
		t.Fatalf("端口 8040 仍判 BE，即使 app 写成 fe: %v", be)
	}
}
