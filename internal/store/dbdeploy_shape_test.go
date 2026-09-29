package store

import "testing"

func TestValidateDbDeployClusterShape_Cloud(t *testing.T) {
	err := validateDbDeployClusterShape(&DbDeployCluster{
		Engine: DbDeployEngineCodis, DeployKind: DbDeployKindCloud, Topology: DbDeployTopoCloudEndpoint,
	})
	if err == nil {
		t.Fatal("cloud+codis should fail")
	}
	err = validateDbDeployClusterShape(&DbDeployCluster{
		Engine: DbDeployEngineRedis, DeployKind: DbDeployKindCloud, Topology: DbDeployTopoCloudEndpoint,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = validateDbDeployClusterShape(&DbDeployCluster{
		Engine: DbDeployEngineMySQL, DeployKind: DbDeployKindSelf, Topology: DbDeployTopoCloudEndpoint,
	})
	if err == nil {
		t.Fatal("self+cloud_endpoint should fail")
	}
	err = validateDbDeployClusterShape(&DbDeployCluster{
		Engine: "tidb", DeployKind: DbDeployKindCloud, Topology: DbDeployTopoCloudEndpoint,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = validateDbDeployClusterShape(&DbDeployCluster{
		Engine: "kafka", DeployKind: DbDeployKindSelf, Topology: DbDeployTopoStandalone,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = validateDbDeployClusterShape(&DbDeployCluster{
		Engine: "tidb", DeployKind: DbDeployKindSelf, Topology: DbDeployTopoPdTikvTidb,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = validateDbDeployClusterShape(&DbDeployCluster{
		Engine: "kafka", DeployKind: DbDeployKindSelf, Topology: DbDeployTopoBrokerCluster,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = validateDbDeployClusterShape(&DbDeployCluster{
		Engine: "mongodb", DeployKind: DbDeployKindSelf, Topology: DbDeployTopoReplset,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = validateDbDeployClusterShape(&DbDeployCluster{
		Engine: "oracle", DeployKind: DbDeployKindSelf, Topology: DbDeployTopoStandalone,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = validateDbDeployClusterShape(&DbDeployCluster{
		Engine: "oracle", DeployKind: DbDeployKindSelf, Topology: DbDeployTopoMasterReplica,
	})
	if err == nil {
		t.Fatal("oracle master_replica should fail")
	}
	err = validateDbDeployClusterShape(&DbDeployCluster{
		Engine: "oceanbase", DeployKind: DbDeployKindSelf, Topology: DbDeployTopoMasterReplica,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = validateDbDeployClusterShape(&DbDeployCluster{
		Engine: "sqlserver", DeployKind: DbDeployKindSelf, Topology: DbDeployTopoStandalone,
	})
	if err != nil {
		t.Fatal(err)
	}
}
