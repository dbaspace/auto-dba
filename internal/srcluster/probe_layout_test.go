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

func TestInferFeSoftwareBaseFromPath(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"/home/dorisdb_base/fe/meta", "/home/dorisdb_base"},
		{"/home/dorisdb_base/fe/log", "/home/dorisdb_base"},
		{"/home/dorisdb_base/fe", "/home/dorisdb_base"},
		{"/home/log", "/home/log"},
		{"/home/fe/log", "/home"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := InferFeSoftwareBaseFromPath(tc.in); got != tc.want {
			t.Fatalf("InferFeSoftwareBaseFromPath(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestPickDeeperSoftwareBase(t *testing.T) {
	if got := pickDeeperSoftwareBase("/home", "/home/dorisdb_base"); got != "/home/dorisdb_base" {
		t.Fatalf("got %q", got)
	}
	if got := pickDeeperSoftwareBase("/home/dorisdb_base", "/home"); got != "/home/dorisdb_base" {
		t.Fatalf("got %q", got)
	}
	if got := pickDeeperSoftwareBase("", "/home/dorisdb_base"); got != "/home/dorisdb_base" {
		t.Fatalf("empty a got %q", got)
	}
	if got := pickDeeperSoftwareBase("/home/dorisdb_base", ""); got != "/home/dorisdb_base" {
		t.Fatalf("empty b got %q", got)
	}
}

func TestAlignFeSoftwareBaseWithBE(t *testing.T) {
	got, did := alignFeSoftwareBaseWithBE("/home", "/home/dorisdb_base")
	if !did || got != "/home/dorisdb_base" {
		t.Fatalf("got=%q did=%v", got, did)
	}
	got, did = alignFeSoftwareBaseWithBE("/home/dorisdb_base", "/home/dorisdb_base")
	if did || got != "/home/dorisdb_base" {
		t.Fatalf("same got=%q did=%v", got, did)
	}
	got, did = alignFeSoftwareBaseWithBE("/opt/fe_root", "/opt/be_root")
	if did || got != "/opt/fe_root" {
		t.Fatalf("unrelated got=%q did=%v", got, did)
	}
	got, did = alignFeSoftwareBaseWithBE("/home/fe/log", "/home/dorisdb_base")
	// Infer already stripped; this tests raw align only — /home/fe/log is not prefix of BE
	if did {
		t.Fatalf("unexpected align got=%q", got)
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
	cases := []string{"/data", "/data1/dorisdb/be/storeage", "/data1/dorisdb/be/storage", "/data/dorisdb"}
	for _, in := range cases {
		if got := resolveStorageRoot(in); got != in {
			t.Fatalf("resolveStorageRoot(%q)=%q", in, got)
		}
	}
	if got := resolveStorageRoot("  /data1/dorisdb/be/storeage/  "); got != "/data1/dorisdb/be/storeage" {
		t.Fatalf("trim got %q", got)
	}
	if got := resolveStorageRoot(""); got != "/data" {
		t.Fatalf("empty got %q", got)
	}
}
