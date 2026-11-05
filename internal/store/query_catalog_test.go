package store

import "testing"

func TestMergeQueryCatalogRespectsPermission(t *testing.T) {
	viewer := Viewer{Username: "alice"}
	owned := []catalogRow{
		{db: Database{ID: "open", DBName: "open_db", InstanceID: "i1", Visibility: "global", Tags: []string{"idc=test"}}, inst: QueryCatalogInstance{ID: "i1", Name: "pg-a", DBType: "postgres", IDC: "test"}},
		{db: Database{ID: "secret", DBName: "secret_db", InstanceID: "i1", Visibility: "private", AccessUsers: []string{"bob"}}, inst: QueryCatalogInstance{ID: "i1", Name: "pg-a", DBType: "postgres", IDC: "test"}},
	}
	grant := []catalogRow{
		{db: Database{ID: "shared", DBName: "shared_db", InstanceID: "i2", Visibility: "private"}, inst: QueryCatalogInstance{ID: "i2", Name: "pg-b", DBType: "postgresql", IDC: "prod"}},
		{db: Database{ID: "bus", DBName: "bus", InstanceID: "k1"}, inst: QueryCatalogInstance{ID: "k1", Name: "kafka", DBType: "kafka"}},
	}
	dbs, insts := mergeQueryCatalog(owned, viewer, grant)
	got := map[string]QueryCatalogDB{}
	for _, db := range dbs {
		got[db.ID] = db
	}
	if _, ok := got["secret"]; ok {
		t.Fatal("private registered db must stay hidden without a grant")
	}
	if _, ok := got["open"]; !ok {
		t.Fatal("global registered db missing")
	}
	if _, ok := got["shared"]; !ok {
		t.Fatal("explicit grant should be visible even when private")
	}
	if _, ok := got["bus"]; ok {
		t.Fatal("kafka must not enter the query catalog")
	}
	if len(insts) != 2 {
		t.Fatalf("instances %+v", insts)
	}
	if got["open"].Tags[0] != "idc=test" {
		t.Fatalf("tags %+v", got["open"].Tags)
	}

	admin := Viewer{Username: "admin", Privileged: true}
	adminDBs, _ := mergeQueryCatalog(owned, admin)
	if len(adminDBs) != 2 {
		t.Fatalf("privileged user should see private registered db, got %+v", adminDBs)
	}
}

func TestQueryCatalogExcludedEngine(t *testing.T) {
	if !queryCatalogExcludedEngine(" Redis ") || !queryCatalogExcludedEngine("kafka") {
		t.Fatal("kafka and redis should be excluded")
	}
	if queryCatalogExcludedEngine("postgres") || queryCatalogExcludedEngine("postgresql") {
		t.Fatal("postgres should stay")
	}
}
