package srcluster

import (
	"net/url"
	"strings"
	"testing"
)

func TestS3URL_VirtualHostCOS(t *testing.T) {
	rt := &ossRuntime{
		Endpoint:    "https://cos.ap-guangzhou.myqcloud.com",
		Bucket:      "dolphinscheduler-1324130148",
		VirtualHost: true,
		Region:      "ap-guangzhou",
	}
	full, host, uri, err := rt.s3URL("starrocks/StarRocks-3.2.10.fe.tar.gz", nil)
	if err != nil {
		t.Fatal(err)
	}
	wantHost := "dolphinscheduler-1324130148.cos.ap-guangzhou.myqcloud.com"
	if host != wantHost {
		t.Fatalf("host=%s want %s", host, wantHost)
	}
	if uri != "/starrocks/StarRocks-3.2.10.fe.tar.gz" {
		t.Fatalf("uri=%s", uri)
	}
	if !strings.HasPrefix(full, "https://"+wantHost+"/") {
		t.Fatalf("full=%s", full)
	}
	q := url.Values{}
	q.Set("list-type", "2")
	listURL, listHost, listURI, err := rt.s3URL("", q)
	if err != nil {
		t.Fatal(err)
	}
	if listHost != wantHost || listURI != "/" {
		t.Fatalf("list host=%s uri=%s", listHost, listURI)
	}
	if !strings.Contains(listURL, "list-type=2") {
		t.Fatalf("listURL missing query: %s", listURL)
	}
	u, _ := url.Parse(listURL)
	if u.Path != "/" && u.Path != "" {
		t.Fatalf("virtual-host list path should be /, got %q url=%s", u.Path, listURL)
	}
}

func TestPreferVirtualHost(t *testing.T) {
	if !preferVirtualHost("tencent_cos", "cos.ap-guangzhou.myqcloud.com") {
		t.Fatal("tencent_cos")
	}
	if preferVirtualHost("s3_compatible", "minio.local") {
		t.Fatal("s3_compatible should be path-style")
	}
}
