package dbmig

import "testing"

func TestBuildObjectsFromSels(t *testing.T) {
	got := BuildObjectsFromSels("postgres", []ObjectSel{
		{SourceDB: "src_a", SourceTable: "t1", TargetDB: "tgt_a", TargetTable: "t1_new", TotalBytes: 12},
		{SourceDB: "  ", SourceTable: "skip"},
		{SourceDB: "src_b", SourceTable: "public.t2", TargetDB: "", TargetTable: ""},
	})
	if len(got) != 2 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].SourceDB != "src_a" || got[0].TargetDB != "tgt_a" || got[0].TargetTable != "t1_new" || got[0].TotalBytes != 12 {
		t.Fatalf("first=%+v", got[0])
	}
	if got[1].SourceDB != "src_b" || got[1].SourceTable != "t2" || got[1].TargetDB != "src_b" || got[1].TargetTable != "t2" {
		t.Fatalf("second=%+v", got[1])
	}
}
