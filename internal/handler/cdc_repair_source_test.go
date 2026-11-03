package handler

import (
	"testing"

	"auto-dba/internal/store"
)

func TestMergeInstanceSourceHostAppendsUnknownDomain(t *testing.T) {
	inst := &store.Instance{
		DomainName: "pg.old.example",
		Host:       "pg.old.example",
		Port:       5432,
		Domains:    []string{"pg.old.example"},
	}
	if !mergeInstanceSourceHost(inst, "pg.new.example") {
		t.Fatal("expected domain appended")
	}
	if inst.Host != "pg.old.example" || inst.Port != 5432 {
		t.Fatalf("instance host/port must stay: %s:%d", inst.Host, inst.Port)
	}
	if inst.DomainName != "pg.old.example" {
		t.Fatalf("domain_name changed: %s", inst.DomainName)
	}
	found := false
	for _, d := range inst.Domains {
		if d == "pg.new.example" {
			found = true
		}
	}
	if !found {
		t.Fatalf("domains = %#v", inst.Domains)
	}
}

func TestMergeInstanceSourceHostKeepsExistingDomain(t *testing.T) {
	inst := &store.Instance{
		DomainName: "pg.old.example",
		Host:       "10.0.0.1",
		Port:       5432,
		Domains:    []string{"pg.old.example", "PG.NEW.example"},
	}
	if mergeInstanceSourceHost(inst, "pg.new.example") {
		t.Fatal("unexpected change")
	}
	if len(inst.Domains) != 2 {
		t.Fatalf("domains duplicated: %#v", inst.Domains)
	}
}

func repairTask(id, sourceID, host string) store.CDCTask {
	desc := "x"
	if host != "" {
		desc = encodeCDCExtraJSON("x", map[string]any{"source_host": host, "source_port": "5432"})
	}
	return store.CDCTask{ID: id, SourceID: sourceID, TaskType: "init", Description: desc}
}

func TestRepairSourceSiblingsOnlySameEffectiveHost(t *testing.T) {
	inst := &store.Instance{ID: "i1", Host: "pg.member.example", Port: 5432}
	root := repairTask("root", "i1", "pg.bill.example")
	list := []store.CDCTask{
		root,
		repairTask("bill2", "i1", "PG.BILL.example"),
		repairTask("member", "i1", "pg.member.example"),
		repairTask("legacy", "i1", ""),
		repairTask("other-inst", "i2", "pg.bill.example"),
	}
	got := repairSourceSiblings(&root, list, inst, cdcEffectiveSourceHost(&root, inst))
	if len(got) != 1 || got[0].ID != "bill2" {
		ids := []string{}
		for _, t := range got {
			ids = append(ids, t.ID)
		}
		t.Fatalf("siblings = %v", ids)
	}
}

func TestCDCEffectiveSourceHostFallsBackToInstance(t *testing.T) {
	inst := &store.Instance{Host: "pg.member.example", DomainName: "pg.member.example", Port: 5432}
	task := repairTask("t", "i1", "")
	if got := cdcEffectiveSourceHost(&task, inst); got != "pg.member.example" {
		t.Fatalf("host = %s", got)
	}
}
