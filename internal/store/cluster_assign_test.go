package store

import (
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAssignSrClusterTenantMovesLinkedInstance(t *testing.T) {
	dsn := os.Getenv("AUTO_DBA_TEST_DSN")
	if dsn == "" {
		t.Skip("set AUTO_DBA_TEST_DSN to run")
	}
	d, err := Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.ensureTenancy(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	other := &Tenant{
		Code: "sr-asg-" + uuid.NewString()[:8], Name: "SR划拨测试", Status: TenantStatusActive, Plan: "standard",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := d.CreateTenant(other); err != nil {
		t.Fatal(err)
	}
	suf := uuid.NewString()[:8]
	cl := &SrCluster{
		TenantID: DefaultTenantID, Name: "sr-" + suf, Environment: "test", Version: "3.3.0",
		FeHost: "fe-" + suf + ".local", FeQueryPort: 9030, Status: SrClusterDraft,
	}
	if err := d.CreateSrCluster(cl); err != nil {
		t.Fatal(err)
	}
	inst := &Instance{
		TenantID: DefaultTenantID, Name: "sr-inst-" + suf, DomainName: "fe-" + suf + ".local", Port: 9030,
		DBType: "starrocks", Host: "fe-" + suf + ".local", Username: "root", Password: "p", IDC: "default",
		Tags: []string{"sr-cluster=" + cl.ID},
	}
	if err := d.CreateInstance(inst); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM tbl_instances WHERE id=?`, inst.ID)
		_, _ = d.Exec(`DELETE FROM tbl_sr_clusters WHERE id=?`, cl.ID)
		_, _ = d.Exec(`DELETE FROM tbl_tenants WHERE id=?`, other.ID)
	})

	if err := d.AssignSrClusterTenant(cl.ID, other.ID); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetSrCluster(cl.ID)
	if err != nil || got == nil || got.TenantID != other.ID {
		t.Fatalf("cluster tenant=%v err=%v", got, err)
	}
	it, err := d.GetInstance(inst.ID)
	if err != nil || it == nil || it.TenantID != other.ID {
		t.Fatalf("instance tenant=%v err=%v", it, err)
	}

	clash := &SrCluster{
		TenantID: DefaultTenantID, Name: "sr2-" + suf, Environment: "test",
		FeHost: cl.FeHost, FeQueryPort: 9030, Status: SrClusterDraft,
	}
	if err := d.CreateSrCluster(clash); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = d.Exec(`DELETE FROM tbl_sr_clusters WHERE id=?`, clash.ID) })
	if err := d.AssignSrClusterTenant(clash.ID, other.ID); err == nil {
		t.Fatal("expected FE endpoint conflict")
	}
}

func TestAssignLiveStarrocksETL(t *testing.T) {
	if os.Getenv("ASSIGN_LIVE_ETL") != "1" {
		t.Skip("set ASSIGN_LIVE_ETL=1 to move starrocks-test-etl")
	}
	dsn := os.Getenv("AUTO_DBA_TEST_DSN")
	if dsn == "" {
		t.Skip("set AUTO_DBA_TEST_DSN")
	}
	d, err := Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	cid := "acf574bf-111b-4669-9a99-d2a6fcc2122b"
	to := "c2795427-2148-4c4c-9b67-7b4245e7fffa"
	if err := d.AssignSrClusterTenant(cid, to); err != nil {
		t.Fatal(err)
	}
	cl, err := d.GetSrCluster(cid)
	if err != nil || cl == nil || cl.TenantID != to {
		t.Fatalf("cluster tenant=%v err=%v", cl, err)
	}
}

func TestAssignDbDeployClusterTenantAllowsSameName(t *testing.T) {
	dsn := testDSN(t)
	if dsn == "" {
		t.Skip("set AUTO_DBA_TEST_DSN or provide config/config.yaml")
	}
	d, err := Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.ensureTenancy(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	other := &Tenant{
		Code: "dd-asg-" + uuid.NewString()[:8], Name: "DB划拨同名", Status: TenantStatusActive, Plan: "standard",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := d.CreateTenant(other); err != nil {
		t.Fatal(err)
	}
	suf := uuid.NewString()[:8]
	name := "same-name-" + suf
	inDefault := &DbDeployCluster{
		TenantID: DefaultTenantID, Engine: DbDeployEnginePostgres, Name: name,
		Environment: "test", Topology: DbDeployTopoStandalone, DeployKind: DbDeployKindSelf,
		Version: "15.6", Status: DbDeployStatusReady,
	}
	if err := d.UpsertDbDeployCluster(inDefault); err != nil {
		t.Fatal(err)
	}
	fromOther := &DbDeployCluster{
		TenantID: other.ID, Engine: DbDeployEnginePostgres, Name: name,
		Environment: "test", Topology: DbDeployTopoCloudEndpoint, DeployKind: DbDeployKindCloud,
		Version: "15.7", EndpointHost: "pg-" + suf + ".cloud.local", EndpointPort: 5432,
		Status: DbDeployStatusReady,
	}
	if err := d.UpsertDbDeployCluster(fromOther); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM tbl_db_deploy_clusters WHERE id IN (?,?)`, inDefault.ID, fromOther.ID)
		_, _ = d.Exec(`DELETE FROM tbl_tenants WHERE id=?`, other.ID)
	})

	if err := d.AssignDbDeployClusterTenant(fromOther.ID, DefaultTenantID); err != nil {
		t.Fatalf("same display name must be allowed: %v", err)
	}
	got, err := d.GetDbDeployCluster(fromOther.ID)
	if err != nil || got == nil || got.TenantID != DefaultTenantID {
		t.Fatalf("cluster tenant=%v err=%v", got, err)
	}

	// 同云入口仍应拒绝
	clash := &DbDeployCluster{
		TenantID: other.ID, Engine: DbDeployEnginePostgres, Name: "other-" + suf,
		Environment: "test", Topology: DbDeployTopoCloudEndpoint, DeployKind: DbDeployKindCloud,
		Version: "15.7", EndpointHost: fromOther.EndpointHost, EndpointPort: fromOther.EndpointPort,
		Status: DbDeployStatusReady,
	}
	if err := d.UpsertDbDeployCluster(clash); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = d.Exec(`DELETE FROM tbl_db_deploy_clusters WHERE id=?`, clash.ID) })
	if err := d.AssignDbDeployClusterTenant(clash.ID, DefaultTenantID); err == nil {
		t.Fatal("expected cloud endpoint conflict")
	}
}

func TestAssignProxyServiceTenant(t *testing.T) {
	dsn := os.Getenv("AUTO_DBA_TEST_DSN")
	if dsn == "" {
		t.Skip("set AUTO_DBA_TEST_DSN to run")
	}
	d, err := Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.ensureTenancy(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	other := &Tenant{
		Code: "px-asg-" + uuid.NewString()[:8], Name: "代理划拨测试", Status: TenantStatusActive, Plan: "standard",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := d.CreateTenant(other); err != nil {
		t.Fatal(err)
	}
	suf := uuid.NewString()[:8]
	s := &ProxyService{
		TenantID: DefaultTenantID, Name: "px-" + suf, Kind: "haproxy",
		VIP: "10.9.9." + suf[:3], ListenPort: 3306, Status: "draft",
	}
	if err := d.UpsertProxyService(s); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM tbl_proxy_services WHERE id=?`, s.ID)
		_, _ = d.Exec(`DELETE FROM tbl_tenants WHERE id=?`, other.ID)
	})
	if err := d.AssignProxyServiceTenant(s.ID, other.ID); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetProxyService(s.ID)
	if err != nil || got == nil || got.TenantID != other.ID {
		t.Fatalf("proxy tenant=%v err=%v", got, err)
	}
	clash := &ProxyService{
		TenantID: DefaultTenantID, Name: s.Name, Kind: "haproxy", Status: "draft",
	}
	if err := d.UpsertProxyService(clash); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = d.Exec(`DELETE FROM tbl_proxy_services WHERE id=?`, clash.ID) })
	if err := d.AssignProxyServiceTenant(clash.ID, other.ID); err == nil {
		t.Fatal("expected same-kind name conflict")
	}
}
