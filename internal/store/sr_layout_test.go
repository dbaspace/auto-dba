package store

import "testing"

func TestResolveSrPathsClusterOverridesGlobal(t *testing.T) {
	cl := &SrCluster{SoftwareBase: "/data1/fe_base", BeSoftwareBase: "/data1/be_base", DataRoot: "/data1/storage"}
	fe, be, data := ResolveSrPaths(nil, cl)
	if fe != "/data1/fe_base" || be != "/data1/be_base" || data != "/data1/storage" {
		t.Fatalf("got %s %s %s", fe, be, data)
	}
}

func TestResolveSrPathsEmptyUsesBuiltinNotFE(t *testing.T) {
	cl := &SrCluster{SoftwareBase: "/opt/fe"}
	fe, be, data := ResolveSrPaths(nil, cl)
	if fe != "/opt/fe" {
		t.Fatalf("fe=%s", fe)
	}
	if be != DefaultSrSoftwareBase {
		t.Fatalf("BE 空值不应抄 FE，got %s", be)
	}
	if data != DefaultSrDataRoot {
		t.Fatalf("data=%s", data)
	}
}

func TestResolveSrPathsNilCluster(t *testing.T) {
	fe, be, data := ResolveSrPaths(nil, nil)
	if fe != DefaultSrSoftwareBase || be != DefaultSrSoftwareBase || data != DefaultSrDataRoot {
		t.Fatalf("got %s %s %s", fe, be, data)
	}
}
