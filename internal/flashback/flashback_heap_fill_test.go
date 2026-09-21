package flashback

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestFlashbackReplicaIdentityDefaultPKIsWarning(t *testing.T) {
	rel := &flashbackRelation{
		Schema: "public", Name: "t", ReplIdent: "d", PKCols: []string{"id"},
	}
	st, msg := flashbackReplicaIdentityCheck(map[string]*flashbackRelation{"public.t": rel})
	if st != flashbackCheckWarning {
		t.Fatalf("status=%s want warning", st)
	}
	if strings.Contains(msg, "可用于行还原") {
		t.Fatalf("must not claim ready for row restore: %s", msg)
	}
	if !strings.Contains(msg, "只保证主键") || !strings.Contains(msg, "REPLICA IDENTITY FULL") {
		t.Fatalf("msg=%s", msg)
	}

	full := &flashbackRelation{Schema: "public", Name: "f", ReplIdent: "f", PKCols: []string{"id"}}
	st, msg = flashbackReplicaIdentityCheck(map[string]*flashbackRelation{"public.f": full})
	if st != flashbackCheckPassed || !strings.Contains(msg, "均为 FULL") {
		t.Fatalf("full status=%s msg=%s", st, msg)
	}
}

func flashbackSynthDeadPage(id int32, name string) []byte {
	page := make([]byte, flashbackHeapPageSize)
	pdLower := uint16(flashbackPageHeaderSize + 4)
	pdUpper := uint16(flashbackHeapPageSize - 64)
	binary.LittleEndian.PutUint16(page[12:14], pdLower)
	binary.LittleEndian.PutUint16(page[14:16], pdUpper)
	binary.LittleEndian.PutUint16(page[16:18], uint16(flashbackHeapPageSize))
	lpLen := uint16(48)
	item := uint32(pdUpper) | (uint32(flashbackLPNormal) << 15) | (uint32(lpLen) << 17)
	binary.LittleEndian.PutUint32(page[flashbackPageHeaderSize:flashbackPageHeaderSize+4], item)
	page[pdUpper+22] = 24
	binary.LittleEndian.PutUint32(page[pdUpper+4:pdUpper+8], 9) // xmax → dead
	binary.LittleEndian.PutUint32(page[pdUpper+24:pdUpper+28], uint32(id))
	nb := []byte(name)
	page[pdUpper+28] = byte((len(nb)+1)<<1 | 1)
	copy(page[pdUpper+29:], nb)
	return page
}

func flashbackSynthLivePage(id int32, name string) []byte {
	page := make([]byte, flashbackHeapPageSize)
	pdLower := uint16(flashbackPageHeaderSize + 4)
	pdUpper := uint16(flashbackHeapPageSize - 64)
	binary.LittleEndian.PutUint16(page[12:14], pdLower)
	binary.LittleEndian.PutUint16(page[14:16], pdUpper)
	binary.LittleEndian.PutUint16(page[16:18], uint16(flashbackHeapPageSize))
	lpLen := uint16(48)
	item := uint32(pdUpper) | (uint32(flashbackLPNormal) << 15) | (uint32(lpLen) << 17)
	binary.LittleEndian.PutUint32(page[flashbackPageHeaderSize:flashbackPageHeaderSize+4], item)
	page[pdUpper+22] = 24
	// xmax=0 → live
	binary.LittleEndian.PutUint32(page[pdUpper+24:pdUpper+28], uint32(id))
	nb := []byte(name)
	page[pdUpper+28] = byte((len(nb)+1)<<1 | 1)
	copy(page[pdUpper+29:], nb)
	return page
}

func TestFlashbackDefaultPKHeapFillFromDead(t *testing.T) {
	rel := flashbackBuildRel([]flashbackColumn{
		flashbackCol("id", "int4", 4, "i"),
		flashbackCol("cname", "text", -1, "i"),
	})
	rel.PKCols = []string{"id"}
	rel.Columns[0].IsPK = true
	page := flashbackSynthDeadPage(1, "alice")
	ch := flashbackChange{
		Schema: "public", Table: "t", Op: "DELETE",
		Block: 0, Offnum: 1, PKCols: []string{"id"},
		Old: map[string]string{"id": "1"},
	}
	if !flashbackValuesIncomplete(rel, ch.Old) {
		t.Fatal("pk-only old should be incomplete")
	}
	if src := flashbackMergeOldFromPage(rel, &ch, page, 0); src == "" {
		t.Fatal("expected heap slot merge")
	}
	if ch.Old["id"] != "1" || ch.Old["cname"] != "alice" {
		t.Fatalf("old=%v", ch.Old)
	}
	if flashbackValuesIncomplete(rel, ch.Old) {
		t.Fatalf("should be complete: %v", ch.Old)
	}
}

func TestFlashbackUpdateMustNotUseLiveNewRow(t *testing.T) {
	rel := flashbackBuildRel([]flashbackColumn{
		flashbackCol("id", "int4", 4, "i"),
		flashbackCol("cname", "text", -1, "i"),
	})
	rel.PKCols = []string{"id"}
	page := flashbackSynthLivePage(1, "bob") // live = new row after UPDATE
	ch := flashbackChange{
		Schema: "public", Table: "t", Op: "UPDATE",
		Block: 0, Offnum: 1, PKCols: []string{"id"},
		Old: map[string]string{"id": "1"},
		New: map[string]string{"id": "1", "cname": "bob"},
	}
	if src := flashbackMergeOldFromPage(rel, &ch, page, 0); src != "" {
		t.Fatalf("must not merge live new row, src=%s old=%v", src, ch.Old)
	}
	if ch.Old["cname"] == "bob" {
		t.Fatal("UPDATE must not take new live row as Old")
	}
	cands := flashbackScanHeapPage(page, 0, rel, true)
	if picked := flashbackPickHeapOld(&ch, cands); len(picked) != 0 {
		t.Fatalf("pick should be empty, got %v", picked)
	}
}

func TestFlashbackPickPrefersDeadOverLive(t *testing.T) {
	rel := flashbackBuildRel([]flashbackColumn{
		flashbackCol("id", "int4", 4, "i"),
		flashbackCol("cname", "text", -1, "i"),
	})
	dead := flashbackScanHeapPage(flashbackSynthDeadPage(1, "old"), 0, rel, true)
	live := flashbackScanHeapPage(flashbackSynthLivePage(1, "new"), 0, rel, true)
	ch := flashbackChange{
		Op: "DELETE", Offnum: 1, PKCols: []string{"id"},
		Old: map[string]string{"id": "1"},
	}
	cands := append(live, dead...)
	picked := flashbackPickHeapOld(&ch, cands)
	if picked["cname"] != "old" {
		t.Fatalf("want dead old, got %v", picked)
	}
}

func TestFlashbackUndoHas(t *testing.T) {
	undo := []string{`INSERT INTO "public"."t" ("id", "note") VALUES (7, 'default-heap-old');`}
	if !flashbackUndoHas(undo, "default-heap-old") {
		t.Fatal("expected hit")
	}
	if flashbackUndoHas(undo, "missing") {
		t.Fatal("unexpected hit")
	}
}

func TestFlashbackSelftestOK(t *testing.T) {
	if flashbackSelftestOK(nil) {
		t.Fatal("empty should fail")
	}
	ok := []FlashbackSelftestCheck{{Name: "a", OK: true}, {Name: "b", OK: true}}
	if !flashbackSelftestOK(ok) {
		t.Fatal("all ok")
	}
	bad := append(ok, FlashbackSelftestCheck{Name: "c", OK: false})
	if flashbackSelftestOK(bad) {
		t.Fatal("one fail")
	}
}
