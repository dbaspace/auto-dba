package ossupload

import (
	"net/url"
	"testing"
)

func TestObjectURL_COSVirtualHost(t *testing.T) {
	cli, err := NewClient("https://cos.ap-guangzhou.myqcloud.com", "my-bucket", "ak", "sk", "tencent_cos")
	if err != nil {
		t.Fatal(err)
	}
	if !cli.VirtualHost {
		t.Fatal("expected VirtualHost=true for COS")
	}
	q := url.Values{}
	q.Set("uploads", "")
	got := cli.objectURL("migration/uploads/20240101/a.bin", q)
	want := "https://my-bucket.cos.ap-guangzhou.myqcloud.com/migration/uploads/20240101/a.bin?uploads="
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestObjectURL_COSEndpointAlreadyVirtual(t *testing.T) {
	cli, err := NewClient("https://my-bucket.cos.ap-guangzhou.myqcloud.com", "my-bucket", "ak", "sk", "tencent_cos")
	if err != nil {
		t.Fatal(err)
	}
	got := cli.objectURL("k.bin", nil)
	want := "https://my-bucket.cos.ap-guangzhou.myqcloud.com/k.bin"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestObjectURL_MinIOPathStyle(t *testing.T) {
	cli, err := NewClient("https://minio.example.com", "my-bucket", "ak", "sk", "s3_compatible")
	if err != nil {
		t.Fatal(err)
	}
	if cli.VirtualHost {
		t.Fatal("expected VirtualHost=false for MinIO")
	}
	got := cli.objectURL("dir/f.bin", nil)
	want := "https://minio.example.com/my-bucket/dir/f.bin"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestBuildObjectKey_FlatUnderPrefix(t *testing.T) {
	got := BuildObjectKey("starrocks", "StarRocks-3.3.5.tar.gz")
	want := "starrocks/StarRocks-3.3.5.tar.gz"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if BuildObjectKey("", "a.bin") != "a.bin" {
		t.Fatalf("empty prefix: %q", BuildObjectKey("", "a.bin"))
	}
}

func TestPreferVirtualHostByHostname(t *testing.T) {
	if !preferVirtualHost("", "cos.ap-beijing.myqcloud.com") {
		t.Fatal("myqcloud should prefer virtual-host")
	}
	if preferVirtualHost("s3_compatible", "cos.ap-beijing.myqcloud.com") {
		t.Fatal("s3_compatible should force path-style")
	}
}
