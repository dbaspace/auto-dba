package sqlexec

import (
	"context"
	"testing"
)

func TestRunMetaValidatesInput(t *testing.T) {
	ctx := context.Background()
	_, err := RunMeta(ctx, MetaRequest{})
	if err == nil {
		t.Fatal("empty target should fail")
	}
	_, err = RunMeta(ctx, MetaRequest{Target: Target{Host: "h"}})
	if err == nil {
		t.Fatal("empty databases should fail")
	}
	dbs := make([]MetaDB, metaMaxDatabases+1)
	for i := range dbs {
		dbs[i] = MetaDB{DBName: "d"}
	}
	_, err = RunMeta(ctx, MetaRequest{Target: Target{Host: "h"}, Databases: dbs})
	if err == nil {
		t.Fatal("too many databases should fail")
	}
}
