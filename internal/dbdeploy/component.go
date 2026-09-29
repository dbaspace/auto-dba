package dbdeploy

import (
	"context"
	"fmt"
	"strings"

	"auto-dba/internal/store"
)

const (
	TopoPdTikvTidb    = "pd_tikv_tidb"
	TopoMultiNode     = "multi_node"
	TopoReplset       = "replset"
	TopoBrokerCluster = "broker_cluster"
)

func validateComponentTopology(cl *store.DbDeployCluster, nodes []store.DbDeployNode) error {
	switch cl.Engine {
	case "tidb":
		if cl.Topology != TopoPdTikvTidb {
			return nil
		}
		need := map[string]bool{"pd": false, "tikv": false, "tidb": false}
		for _, n := range nodes {
			if _, ok := need[n.Role]; ok {
				need[n.Role] = true
			}
		}
		for role, ok := range need {
			if !ok {
				return fmt.Errorf("TiDB 拓扑 pd_tikv_tidb 缺少角色 %s", role)
			}
		}
	case "cockroachdb":
		if cl.Topology == TopoMultiNode {
			if len(nodes) < 1 {
				return fmt.Errorf("CockroachDB 多节点拓扑至少需要 1 个节点")
			}
			for _, n := range nodes {
				if n.Role != "node" && n.Role != "single" {
					return fmt.Errorf("CockroachDB 节点角色须为 node/single，收到 %s", n.Role)
				}
			}
		}
	case "mongodb":
		if cl.Topology == TopoReplset {
			hasPrimary := false
			for _, n := range nodes {
				if n.Role == "primary" || n.Role == "single" {
					hasPrimary = true
				}
			}
			if !hasPrimary {
				return fmt.Errorf("MongoDB 副本集须有 primary/single")
			}
			if len(nodes) < 1 {
				return fmt.Errorf("MongoDB 副本集至少 1 个节点")
			}
		}
	case "cassandra":
		if cl.Topology == TopoMultiNode && len(nodes) < 1 {
			return fmt.Errorf("Cassandra 多节点至少 1 个节点")
		}
	case "elasticsearch":
		if cl.Topology == TopoMultiNode {
			hasMaster := false
			for _, n := range nodes {
				if n.Role == "master" || n.Role == "single" {
					hasMaster = true
				}
			}
			if !hasMaster {
				return fmt.Errorf("Elasticsearch 多节点须有 master/single")
			}
		}
	case "kafka":
		if cl.Topology == TopoBrokerCluster {
			if len(nodes) < 1 {
				return fmt.Errorf("Kafka broker 集群至少 1 个 broker")
			}
			for _, n := range nodes {
				if n.Role != "broker" && n.Role != "single" {
					return fmt.Errorf("Kafka 角色须为 broker/single，收到 %s", n.Role)
				}
			}
		}
	case "clickhouse":
		if cl.Topology == store.DbDeployTopoMasterReplica {
			hasP, hasR := false, false
			for _, n := range nodes {
				if n.Role == "primary" || n.Role == "single" {
					hasP = true
				}
				if n.Role == "replica" {
					hasR = true
				}
			}
			if !hasP || !hasR {
				return fmt.Errorf("ClickHouse 主从须同时有 primary 与 replica")
			}
		}
	}
	return nil
}

func orderPriorityForEngine(engine, role string) int {
	switch strings.ToLower(engine) {
	case "tidb":
		switch role {
		case "pd":
			return 0
		case "tikv":
			return 1
		case "tidb":
			return 2
		default:
			return 9
		}
	case "cockroachdb":
		switch role {
		case "single":
			return 0
		case "node":
			return 1
		default:
			return 9
		}
	case "cassandra":
		switch role {
		case "seed", "single":
			return 0
		case "node":
			return 1
		default:
			return 9
		}
	case "elasticsearch":
		switch role {
		case "master", "single":
			return 0
		case "data":
			return 1
		default:
			return 9
		}
	case "mongodb":
		switch role {
		case "primary", "single":
			return 0
		case "secondary":
			return 1
		default:
			return 9
		}
	case "kafka":
		switch role {
		case "broker", "single":
			return 0
		default:
			return 9
		}
	case "clickhouse":
		switch role {
		case "primary", "single":
			return 0
		case "replica":
			return 1
		default:
			return 9
		}
	case "oceanbase", "sqlserver", "oracle":
		switch role {
		case "primary", "single":
			return 0
		case "replica":
			return 1
		default:
			return 9
		}
	}
	return -1
}

// stepComponentHealth 组件链验收（health 替代 repl）。
func (e *Engine) stepComponentHealth(ctx context.Context, cl *store.DbDeployCluster, nodes []store.DbDeployNode, step *store.DbDeployChangeStep) error {
	ordered := orderNodesForDeploy(cl, nodes)
	var logs []string
	for i := range ordered {
		n := &ordered[i]
		if n.Role == "endpoint" {
			continue
		}
		auth, err := e.agentAuthWithPassword(n.AgentID)
		if err != nil {
			return fmt.Errorf("%s: %w", n.Host, err)
		}
		body := map[string]any{
			"engine": cl.Engine,
			"role":   n.Role,
			"port":   nodePort(cl, n),
			"host":   n.Host,
		}
		if cl.Engine == "tidb" && n.Role != "pd" {
			if pd := pickRole(nodes, "pd"); pd != nil {
				body["pd_endpoints"] = fmt.Sprintf("%s:%d", pd.Host, nodePort(cl, pd))
			}
		}
		if cl.Engine == "cockroachdb" || cl.Engine == "cassandra" {
			if seed := pickSeedOrPrimary(nodes); seed != nil && seed.ID != n.ID {
				body["join_addr"] = fmt.Sprintf("%s:%d", seed.Host, nodePort(cl, seed))
			}
		}
		if cl.Engine == "mongodb" && cl.Topology == TopoReplset {
			if p := pickPrimary(nodes); p != nil {
				body["primary_host"] = p.Host
				body["primary_port"] = nodePort(cl, p)
			}
		}
		resp, err := e.Agent.ReplVerify(ctx, auth, body)
		if err != nil {
			return fmt.Errorf("%s(%s) 组件验收失败: %w", n.Host, n.Role, err)
		}
		if resp == nil || !resp.OK {
			msg := ""
			if resp != nil {
				msg = resp.Msg
				if msg == "" {
					msg = resp.Detail
				}
			}
			return fmt.Errorf("%s(%s) 组件验收未通过: %s", n.Host, n.Role, msg)
		}
		logs = append(logs, fmt.Sprintf("%s/%s ok: %s", n.Host, n.Role, resp.Detail))
	}
	if len(logs) == 0 {
		return fmt.Errorf("无节点可验收")
	}
	step.LogText = strings.Join(logs, "\n")
	return nil
}

func pickRole(nodes []store.DbDeployNode, role string) *store.DbDeployNode {
	for i := range nodes {
		if nodes[i].Role == role {
			return &nodes[i]
		}
	}
	return nil
}

func pickSeedOrPrimary(nodes []store.DbDeployNode) *store.DbDeployNode {
	if n := pickRole(nodes, "seed"); n != nil {
		return n
	}
	return pickPrimary(nodes)
}

func isComponentChainDeploy(cl *store.DbDeployCluster) bool {
	if cl == nil {
		return false
	}
	if cl.DeployKind == store.DbDeployKindCloud || cl.Topology == store.DbDeployTopoCloudEndpoint {
		return false
	}
	switch cl.Engine {
	case "tidb":
		return cl.Topology == TopoPdTikvTidb
	case "cockroachdb":
		return cl.Topology == TopoMultiNode || cl.Topology == store.DbDeployTopoStandalone
	case "clickhouse":
		return cl.Topology == store.DbDeployTopoStandalone || cl.Topology == store.DbDeployTopoMasterReplica
	case "mongodb":
		return cl.Topology == store.DbDeployTopoStandalone || cl.Topology == TopoReplset
	case "cassandra", "elasticsearch":
		return cl.Topology == TopoMultiNode || cl.Topology == store.DbDeployTopoStandalone
	case "kafka":
		return cl.Topology == TopoBrokerCluster || cl.Topology == store.DbDeployTopoStandalone
	case "oceanbase", "sqlserver":
		return cl.Topology == store.DbDeployTopoStandalone || cl.Topology == store.DbDeployTopoMasterReplica
	case "oracle":
		return cl.Topology == store.DbDeployTopoStandalone
	default:
		return false
	}
}
