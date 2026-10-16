package srcluster

import "testing"

func TestInferFeSoftwareBaseFromInternalLog(t *testing.T) {
	got := InferFeSoftwareBaseFromInternalLog("/data1/dorisdb_base/fe/log")
	if got != "/data1/dorisdb_base" {
		t.Fatalf("got %q", got)
	}
	if InferFeSoftwareBaseFromInternalLog("") != "" {
		t.Fatal("empty")
	}
}

func TestInferBeSoftwareBaseFromMinidump(t *testing.T) {
	got := InferBeSoftwareBaseFromMinidump("/data1/dorisdb_base/be")
	if got != "/data1/dorisdb_base" {
		t.Fatalf("got %q", got)
	}
	got = InferBeSoftwareBaseFromMinidump("/data1/dorisdb_base/be/minidump")
	if got != "/data1/dorisdb_base" {
		t.Fatalf("nested got %q", got)
	}
}

func TestParseBEVarz(t *testing.T) {
	body := "foo=1\nsys_minidump_dir=/data1/dorisdb_base/be\nstorage_root_path=/data1/dorisdb/be/storage\nbar=2\n"
	md, st := ParseBEVarz(body)
	if md != "/data1/dorisdb_base/be" || st != "/data1/dorisdb/be/storage" {
		t.Fatalf("md=%q st=%q", md, st)
	}
	_, st2 := ParseBEVarz("storage_root_path=/a/storage,medium:hdd;/b/storage\n")
	if st2 != "/a/storage" {
		t.Fatalf("multi disk got %q", st2)
	}
}

func TestResolveStorageRoot(t *testing.T) {
	if got := resolveStorageRoot("/data"); got != "/data/be/storage" {
		t.Fatalf("mount got %q", got)
	}
	if got := resolveStorageRoot("/data1/dorisdb/be/storage"); got != "/data1/dorisdb/be/storage" {
		t.Fatalf("full got %q", got)
	}
}
