package cdc

import "testing"

func TestMergeParentConnectorPreviewAppendsTables(t *testing.T) {
	base := map[string]string{
		"slot.name":          "slot_member",
		"publication.name":   "pub_member",
		"topic.prefix":       "rt_bj_db_member_profile",
		"database.password":  "secret",
		"table.include.list": "public.tbl_existing",
	}
	out, ok := MergeParentConnectorPreview(base, "public", nil, []TableMapping{{
		SourceTables: []string{"tbl_authorization"},
		TargetTable:  "tbl_authorization",
	}})
	if !ok {
		t.Fatal("expected parent preview")
	}
	if out.SlotName != "slot_member" || out.PublicationName != "pub_member" {
		t.Fatalf("slot/pub changed: %+v", out)
	}
	if out.Config["table.include.list"] != "public.tbl_existing,public.tbl_authorization" {
		t.Fatalf("include=%q", out.Config["table.include.list"])
	}
	if out.Config["database.password"] == "secret" {
		t.Fatal("password leaked")
	}
	if base["table.include.list"] != "public.tbl_existing" {
		t.Fatal("base config mutated")
	}
}

func TestMergeParentConnectorPreviewEmptyBase(t *testing.T) {
	if _, ok := MergeParentConnectorPreview(nil, "public", []string{"t"}, nil); ok {
		t.Fatal("empty base should fall back")
	}
}

func TestPartitionMissingSourceTables(t *testing.T) {
	business, signal := PartitionMissingSourceTables([]string{"public.orders", "public.debezium_signal", "debezium_signal"})
	if len(business) != 1 || business[0] != "public.orders" {
		t.Fatalf("business=%v", business)
	}
	if len(signal) != 2 {
		t.Fatalf("signal=%v", signal)
	}
}
