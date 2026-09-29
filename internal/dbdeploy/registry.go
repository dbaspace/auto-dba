package dbdeploy

import (
	"encoding/json"
	"fmt"
	"strings"
)

// 能力分级（产品）
const (
	CapabilityFull     = "full"     // 自建装机 + 登记 + 云（适用时）
	CapabilityRegister = "register" // 仅登记已有 / 云入口，禁止 cluster_deploy 装机
	CapabilityComing   = "coming"   // Hub 可见，工作台引导
)

const (
	WorkbenchDbDeploy  = "dbdeploy"
	WorkbenchSrCluster = "srcluster"
	WorkbenchProxy     = "proxy"
)

// TopoSpec 拓扑元数据。
type TopoSpec struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind"` // self | cloud
}

// EngineDescriptor 集群部署引擎描述（代码种子，驱动 Hub/Meta）。
type EngineDescriptor struct {
	ID           string         `json:"id"`
	Label        string         `json:"label"`
	Family       string         `json:"family"`
	FamilyLabel  string         `json:"family_label"`
	Workbench    string         `json:"workbench"`
	Capability   string         `json:"capability"`
	Desc         string         `json:"desc"`
	Path         string         `json:"path"`
	InstanceType string         `json:"instance_type"`
	Topologies   []TopoSpec     `json:"topologies"`
	HAModes      []string       `json:"ha_modes"`
	Defaults     map[string]any `json:"defaults"`
	Roles        []string       `json:"roles,omitempty"`
	AllowDeploy  bool           `json:"allow_deploy"`
	AllowCloud   bool           `json:"allow_cloud"`
}

func topo(id, label, kind string) TopoSpec {
	return TopoSpec{ID: id, Label: label, Kind: kind}
}

// AllEngines 返回部署 Hub 引擎列表（不含 host）。
func AllEngines() []EngineDescriptor {
	return []EngineDescriptor{
		{
			ID: "haproxy", Label: "HAProxy", Family: "proxy", FamilyLabel: "接入层代理",
			Workbench: WorkbenchProxy, Capability: CapabilityFull, AllowDeploy: true, AllowCloud: false,
			Desc: "主备 2 节点自建（yum/apt 装 haproxy+keepalived）+ VIP 校验与配置下发；亦可登记已有。",
			Path: "/ops/cluster-deploy/haproxy", InstanceType: "",
			Topologies: []TopoSpec{topo("standalone", "自建·代理节点", "self")},
			Defaults:   map[string]any{"conf_path": "/etc/haproxy/haproxy.cfg", "port": 3306},
			Roles:      []string{"proxy"},
		},
		{
			ID: "nginx", Label: "Nginx", Family: "proxy", FamilyLabel: "接入层代理",
			Workbench: WorkbenchProxy, Capability: CapabilityFull, AllowDeploy: true, AllowCloud: false,
			Desc: "至少主备，可扩多节点（yum/apt 装 nginx+keepalived）+ VIP VRRP；亦可登记已有。",
			Path: "/ops/cluster-deploy/nginx", InstanceType: "",
			Topologies: []TopoSpec{topo("standalone", "自建·代理节点", "self")},
			Defaults:   map[string]any{"conf_path": "/etc/nginx/nginx.conf", "port": 3306},
			Roles:      []string{"proxy"},
		},
		{
			ID: "lvs", Label: "LVS", Family: "proxy", FamilyLabel: "接入层代理",
			Workbench: WorkbenchProxy, Capability: CapabilityFull, AllowDeploy: true, AllowCloud: false,
			Desc: "Director 主备 2 节点（keepalived+ipvsadm）+ VRRP/virtual_server；VIP 必填且不可冲突。",
			Path: "/ops/cluster-deploy/lvs", InstanceType: "",
			Topologies: []TopoSpec{topo("standalone", "自建·Director/VIP", "self")},
			Defaults:   map[string]any{"conf_path": "/etc/keepalived/keepalived.conf", "port": 3306},
			Roles:      []string{"scheduler"},
		},
		{
			ID: "starrocks", Label: "StarRocks", Family: "olap", FamilyLabel: "分析型",
			Workbench: WorkbenchSrCluster, Capability: CapabilityFull, AllowDeploy: true, AllowCloud: false,
			Desc: "FE/BE 角色部署、OSS 制品、初始化/扩容/升级/下线变更单。",
			Path: "/ops/cluster-deploy/starrocks", InstanceType: "starrocks",
			Topologies: []TopoSpec{topo("fe_be", "自建·FE/BE", "self")},
			Defaults:   map[string]any{"port": 9030},
		},
		{
			ID: "mysql", Label: "MySQL", Family: "rdbms", FamilyLabel: "关系型",
			Workbench: WorkbenchDbDeploy, Capability: CapabilityFull, AllowDeploy: true, AllowCloud: true,
			Desc: "单机/主从（半同步闭环）、OSS 预置包、可选 MHA/Orchestrator 等 HA。",
			Path: "/ops/cluster-deploy/mysql", InstanceType: "mysql",
			Topologies: []TopoSpec{
				topo("standalone", "自建·单机", "self"),
				topo("master_replica", "自建·主从", "self"),
				topo("cloud_endpoint", "云托管·单入口", "cloud"),
			},
			HAModes:  []string{"", "mha", "orchestrator", "proxysql_repl", "async_repl", "manual"},
			Defaults: map[string]any{"software_base": "/usr/local/mysql", "data_root": "/data/mysql", "port": 3306},
			Roles:    []string{"single", "primary", "replica"},
		},
		{
			ID: "postgres", Label: "PostgreSQL", Family: "rdbms", FamilyLabel: "关系型",
			Workbench: WorkbenchDbDeploy, Capability: CapabilityFull, AllowDeploy: true, AllowCloud: true,
			Desc: "单机/主备同步流复制、OSS 预置包、可选 Patroni 等 HA。",
			Path: "/ops/cluster-deploy/postgres", InstanceType: "postgres",
			Topologies: []TopoSpec{
				topo("standalone", "自建·单机", "self"),
				topo("master_replica", "自建·主备", "self"),
				topo("cloud_endpoint", "云托管·单入口", "cloud"),
			},
			HAModes:  []string{"", "pg_autofailover", "patroni", "async_repl", "manual", "repmgr"},
			Defaults: map[string]any{"software_base": "/usr/local/pgsql", "data_root": "/data/pgsql", "port": 5432},
			Roles:    []string{"single", "primary", "replica"},
		},
		{
			ID: "redis", Label: "Redis / Codis", Family: "nosql", FamilyLabel: "NoSQL / 缓存",
			Workbench: WorkbenchDbDeploy, Capability: CapabilityFull, AllowDeploy: true, AllowCloud: true,
			Desc: "单机/主从/Sentinel/Codis；OSS 预置包；可选 Sentinel HA。",
			Path: "/ops/cluster-deploy/redis", InstanceType: "redis",
			Topologies: []TopoSpec{
				topo("standalone", "自建·单机", "self"),
				topo("master_replica", "自建·主从", "self"),
				topo("sentinel", "自建·Sentinel", "self"),
				topo("codis", "自建·Codis", "self"),
				topo("cloud_endpoint", "云托管·单入口（Redis）", "cloud"),
			},
			HAModes:  []string{"", "sentinel", "manual"},
			Defaults: map[string]any{"software_base": "/usr/local/redis", "data_root": "/data/redis", "port": 6379},
			Roles:    []string{"single", "primary", "replica", "sentinel", "codis_dashboard", "codis_proxy", "codis_server"},
		},
		{
			ID: "tidb", Label: "TiDB", Family: "dist_sql", FamilyLabel: "分布式 SQL",
			Workbench: WorkbenchDbDeploy, Capability: CapabilityFull, AllowDeploy: true, AllowCloud: true,
			Desc: "PD→TiKV→TiDB 组件链装机（对标 StarRocks）；亦可登记已有/云入口。",
			Path: "/ops/cluster-deploy/tidb", InstanceType: "tidb",
			Topologies: []TopoSpec{
				topo("pd_tikv_tidb", "自建·PD/TiKV/TiDB", "self"),
				topo("standalone", "登记·已有单机/入口", "self"),
				topo("cloud_endpoint", "云托管·单入口", "cloud"),
			},
			Defaults: map[string]any{"software_base": "/usr/local/tidb", "data_root": "/data/tidb", "port": 4000},
			Roles:    []string{"pd", "tikv", "tidb", "endpoint"},
		},
		{
			ID: "cockroachdb", Label: "CockroachDB", Family: "dist_sql", FamilyLabel: "分布式 SQL",
			Workbench: WorkbenchDbDeploy, Capability: CapabilityFull, AllowDeploy: true, AllowCloud: true,
			Desc: "多节点 join 装机；亦可登记已有/云入口。",
			Path: "/ops/cluster-deploy/cockroachdb", InstanceType: "cockroachdb",
			Topologies: []TopoSpec{
				topo("multi_node", "自建·多节点 join", "self"),
				topo("standalone", "自建·单节点 / 登记已有", "self"),
				topo("cloud_endpoint", "云托管·单入口", "cloud"),
			},
			Defaults: map[string]any{"software_base": "/usr/local/cockroach", "data_root": "/data/cockroach", "port": 26257},
			Roles:    []string{"single", "node", "endpoint"},
		},
		{
			ID: "oceanbase", Label: "OceanBase", Family: "rdbms", FamilyLabel: "关系型",
			Workbench: WorkbenchDbDeploy, Capability: CapabilityFull, AllowDeploy: true, AllowCloud: true,
			Desc: "自建单机/主从（MySQL 协议面）装机；OSS 预置包；亦可登记已有/云入口。",
			Path: "/ops/cluster-deploy/oceanbase", InstanceType: "oceanbase",
			Topologies: []TopoSpec{
				topo("standalone", "自建·单机", "self"),
				topo("master_replica", "自建·主从", "self"),
				topo("cloud_endpoint", "云托管·单入口", "cloud"),
			},
			HAModes:  []string{"", "manual"},
			Defaults: map[string]any{"software_base": "/usr/local/oceanbase", "data_root": "/data/oceanbase", "port": 2881},
			Roles:    []string{"single", "primary", "replica", "endpoint"},
		},
		{
			ID: "oracle", Label: "Oracle", Family: "rdbms", FamilyLabel: "关系型",
			Workbench: WorkbenchDbDeploy, Capability: CapabilityFull, AllowDeploy: true, AllowCloud: true,
			Desc: "自建单机装机（客户提供 ORACLE_HOME 制品）；亦可登记已有/云托管。",
			Path: "/ops/cluster-deploy/oracle", InstanceType: "oracle",
			Topologies: []TopoSpec{
				topo("standalone", "自建·单机", "self"),
				topo("cloud_endpoint", "云托管·单入口", "cloud"),
			},
			HAModes:  []string{"", "manual"},
			Defaults: map[string]any{"software_base": "/u01/app/oracle/product/19c/dbhome_1", "data_root": "/u01/oradata", "port": 1521},
			Roles:    []string{"single", "endpoint"},
		},
		{
			ID: "sqlserver", Label: "SQL Server", Family: "rdbms", FamilyLabel: "关系型",
			Workbench: WorkbenchDbDeploy, Capability: CapabilityFull, AllowDeploy: true, AllowCloud: true,
			Desc: "自建单机/主从（Linux sqlservr）装机；OSS 预置包；亦可登记已有/云入口。",
			Path: "/ops/cluster-deploy/sqlserver", InstanceType: "sqlserver",
			Topologies: []TopoSpec{
				topo("standalone", "自建·单机", "self"),
				topo("master_replica", "自建·主从", "self"),
				topo("cloud_endpoint", "云托管·单入口", "cloud"),
			},
			HAModes:  []string{"", "manual"},
			Defaults: map[string]any{"software_base": "/opt/mssql", "data_root": "/var/opt/mssql", "port": 1433},
			Roles:    []string{"single", "primary", "replica", "endpoint"},
		},
		{
			ID: "clickhouse", Label: "ClickHouse", Family: "olap", FamilyLabel: "分析型",
			Workbench: WorkbenchDbDeploy, Capability: CapabilityFull, AllowDeploy: true, AllowCloud: true,
			Desc: "单机/副本装机（对标 MySQL 变体）；亦可登记已有/云入口。",
			Path: "/ops/cluster-deploy/clickhouse", InstanceType: "clickhouse",
			Topologies: []TopoSpec{
				topo("standalone", "自建·单机", "self"),
				topo("master_replica", "自建·主从副本", "self"),
				topo("cloud_endpoint", "云托管·单入口", "cloud"),
			},
			HAModes:  []string{"", "manual"},
			Defaults: map[string]any{"software_base": "/usr/local/clickhouse", "data_root": "/data/clickhouse", "port": 9000},
			Roles:    []string{"single", "primary", "replica", "endpoint"},
		},
		{
			ID: "mongodb", Label: "MongoDB", Family: "nosql", FamilyLabel: "NoSQL / 缓存",
			Workbench: WorkbenchDbDeploy, Capability: CapabilityFull, AllowDeploy: true, AllowCloud: true,
			Desc: "单机/副本集装机；亦可登记已有/云入口。",
			Path: "/ops/cluster-deploy/mongodb", InstanceType: "mongodb",
			Topologies: []TopoSpec{
				topo("standalone", "自建·单机", "self"),
				topo("replset", "自建·副本集", "self"),
				topo("cloud_endpoint", "云托管·单入口", "cloud"),
			},
			HAModes:  []string{"", "manual"},
			Defaults: map[string]any{"software_base": "/usr/local/mongodb", "data_root": "/data/mongodb", "port": 27017},
			Roles:    []string{"single", "primary", "secondary", "endpoint"},
		},
		{
			ID: "cassandra", Label: "Cassandra", Family: "nosql", FamilyLabel: "NoSQL / 缓存",
			Workbench: WorkbenchDbDeploy, Capability: CapabilityFull, AllowDeploy: true, AllowCloud: false,
			Desc: "单机/多节点环装机（seed 优先）；亦可登记已有节点。",
			Path: "/ops/cluster-deploy/cassandra", InstanceType: "cassandra",
			Topologies: []TopoSpec{
				topo("standalone", "自建·单机 / 登记已有", "self"),
				topo("multi_node", "自建·多节点环", "self"),
			},
			HAModes:  []string{"", "manual"},
			Defaults: map[string]any{"software_base": "/usr/local/cassandra", "data_root": "/data/cassandra", "port": 9042},
			Roles:    []string{"single", "node", "seed"},
		},
		{
			ID: "elasticsearch", Label: "Elasticsearch", Family: "nosql", FamilyLabel: "NoSQL / 缓存",
			Workbench: WorkbenchDbDeploy, Capability: CapabilityFull, AllowDeploy: true, AllowCloud: true,
			Desc: "单机/多节点（master/data）装机；亦可登记已有/云。",
			Path: "/ops/cluster-deploy/elasticsearch", InstanceType: "elasticsearch",
			Topologies: []TopoSpec{
				topo("standalone", "自建·单机", "self"),
				topo("multi_node", "自建·多节点", "self"),
				topo("cloud_endpoint", "云托管·单入口", "cloud"),
			},
			HAModes:  []string{"", "manual"},
			Defaults: map[string]any{"software_base": "/usr/local/elasticsearch", "data_root": "/data/elasticsearch", "port": 9200},
			Roles:    []string{"single", "master", "data", "endpoint"},
		},
		{
			ID: "kafka", Label: "Kafka", Family: "mq", FamilyLabel: "消息中间件",
			Workbench: WorkbenchDbDeploy, Capability: CapabilityFull, AllowDeploy: true, AllowCloud: false,
			Desc: "Broker 集群装机（KRaft/单机）；Topic 工单仍走 Kafka 运维，与装机职责分离。",
			Path: "/ops/cluster-deploy/kafka", InstanceType: "kafka",
			Topologies: []TopoSpec{
				topo("standalone", "自建·单 Broker / 登记已有", "self"),
				topo("broker_cluster", "自建·Broker 集群", "self"),
			},
			HAModes:  []string{"", "manual"},
			Defaults: map[string]any{"software_base": "/usr/local/kafka", "data_root": "/data/kafka", "port": 9092},
			Roles:    []string{"broker", "single"},
		},
	}
}

// GetEngine 按 id 查找（兼容 codis）。
func GetEngine(id string) *EngineDescriptor {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "codis" {
		e := GetEngine("redis")
		if e == nil {
			return nil
		}
		cp := *e
		cp.ID = "codis"
		cp.Label = "Codis"
		cp.Topologies = []TopoSpec{topo("codis", "自建·Codis", "self")}
		cp.HAModes = []string{"", "manual"}
		cp.Defaults = map[string]any{"software_base": "/usr/local/codis", "data_root": "/data/codis", "port": 6379}
		cp.Path = "/ops/cluster-deploy/redis"
		cp.Roles = []string{"codis_dashboard", "codis_proxy", "codis_server"}
		return &cp
	}
	for _, e := range AllEngines() {
		if e.ID == id {
			cp := e
			return &cp
		}
	}
	return nil
}

// IsKnownDeployEngine 是否为部署注册表中的引擎。
func IsKnownDeployEngine(id string) bool {
	return GetEngine(id) != nil
}

// AssertAllowDeploy 装机门禁。
func AssertAllowDeploy(engine string) error {
	e := GetEngine(engine)
	if e == nil {
		return fmt.Errorf("未知部署引擎: %s", engine)
	}
	if e.Workbench == WorkbenchSrCluster {
		return nil
	}
	if e.Workbench == WorkbenchProxy {
		return fmt.Errorf("%s 请在代理工作台执行自建部署，不走 DB 装机变更单", e.Label)
	}
	if !e.AllowDeploy || e.Capability != CapabilityFull {
		return fmt.Errorf("引擎 %s 当前为「%s」，仅支持登记已有/云入口，暂不可装机部署", e.Label, capabilityLabel(e.Capability))
	}
	return nil
}

func capabilityLabel(c string) string {
	switch c {
	case CapabilityFull:
		return "可部署"
	case CapabilityRegister:
		return "仅登记"
	case CapabilityComing:
		return "即将支持"
	default:
		return c
	}
}

// ValidateClusterAgainstRegistry 创建/更新集群时的注册表校验。
func ValidateClusterAgainstRegistry(engine, deployKind, topology string, haEnabled bool) error {
	e := GetEngine(engine)
	if e == nil {
		return fmt.Errorf("未知部署引擎: %s（请从集群部署 Hub 进入已登记类型）", engine)
	}
	if e.Workbench == WorkbenchSrCluster {
		return fmt.Errorf("StarRocks 请使用 SR 工作台")
	}
	if e.Workbench == WorkbenchProxy {
		return fmt.Errorf("%s 请使用接入层代理工作台（自建部署/登记已有）", e.Label)
	}
	if e.Capability == CapabilityComing {
		return fmt.Errorf("%s 即将支持，请先在实例管理登记", e.Label)
	}
	dk := strings.ToLower(strings.TrimSpace(deployKind))
	tp := strings.ToLower(strings.TrimSpace(topology))
	if dk == "cloud" || tp == "cloud_endpoint" {
		if !e.AllowCloud {
			return fmt.Errorf("%s 不支持云托管单入口登记", e.Label)
		}
		for _, t := range e.Topologies {
			if t.ID == "cloud_endpoint" {
				return nil
			}
		}
		return fmt.Errorf("%s 未声明云拓扑", e.Label)
	}
	found := false
	for _, t := range e.Topologies {
		if t.ID == tp {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("引擎 %s 不支持拓扑 %s", e.Label, tp)
	}
	if haEnabled {
		hasHA := false
		for _, m := range e.HAModes {
			if strings.TrimSpace(m) != "" {
				hasHA = true
				break
			}
		}
		if !hasHA {
			return fmt.Errorf("%s 本期不支持部署内 HA 接入", e.Label)
		}
	}
	return nil
}

// AgentInfo 集群部署用到的现场 Agent（进程/端口分开，不可混填）。
type AgentInfo struct {
	Kind        string `json:"kind"`         // platform | node | db
	Label       string `json:"label"`        // 展示名
	Port        int    `json:"port"`         // 默认监听端口
	Artifact    string `json:"artifact"`     // 仓库路径
	Where       string `json:"where"`        // 登记/下发入口
	SharedWith  string `json:"shared_with"`  // 共用范围
	UsedBy      string `json:"used_by"`      // 本 Hub 内用途
	Note        string `json:"note"`         // 一句话
}

// AgentsOverview 三种 Agent 对照（Hub 页说明用）。
func AgentsOverview() []AgentInfo {
	return []AgentInfo{
		{
			Kind: "platform", Label: "平台 Agent", Port: 8090,
			Artifact: "deploy/backup-agent → platform-agent",
			Where: "配置中心 → 平台 Agent（/config/agents）",
			SharedWith: "备份 / Redis RDB / 高可用控制器 / 接入层代理 共用同一类 Agent",
			UsedBy: "HAProxy · Nginx · LVS 自建装包与 conf；HA 钩子也走此类",
			Note: "发行版包 + keepalived VIP；勿与 DB/Node Agent 端口冲突",
		},
		{
			Kind: "node", Label: "Node Agent", Port: 8091,
			Artifact: "deploy/node-agent",
			Where: "StarRocks 工作台 → Node Agent Tab",
			SharedWith: "仅 StarRocks 使用（独立进程，不与平台 Agent 共用）",
			UsedBy: "FE/BE 角色装机、OSS 拉包、变更单",
			Note: "只在 SR 工作台下发/登记；仅装 8090 平台 Agent 不够",
		},
		{
			Kind: "db", Label: "DB Agent", Port: 8092,
			Artifact: "deploy/db-agent",
			Where: "引擎工作台 → DB Agent Tab（MySQL/PG/Redis 等）",
			SharedWith: "关系型 / NoSQL / 分布式 SQL 等 db-deploy 引擎共用",
			UsedBy: "OSS 制品装机、复制验收、systemd 启停",
			Note: "云托管单入口无需 Agent；勿填 8090/8091",
		},
	}
}

func agentForWorkbench(wb string) AgentInfo {
	for _, a := range AgentsOverview() {
		switch wb {
		case WorkbenchProxy:
			if a.Kind == "platform" {
				return a
			}
		case WorkbenchSrCluster:
			if a.Kind == "node" {
				return a
			}
		case WorkbenchDbDeploy:
			if a.Kind == "db" {
				return a
			}
		}
	}
	return AgentInfo{}
}

// ListEnginesForAPI Hub 卡片数据。
func ListEnginesForAPI() []map[string]any {
	var out []map[string]any
	for _, e := range AllEngines() {
		ag := agentForWorkbench(e.Workbench)
		item := map[string]any{
			"id":               e.ID,
			"label":            e.Label,
			"family":           e.Family,
			"family_label":     e.FamilyLabel,
			"workbench":        e.Workbench,
			"capability":       e.Capability,
			"capability_label": capabilityLabel(e.Capability),
			"desc":             e.Desc,
			"path":             e.Path,
			"instance_type":    firstNonEmptyStr(e.InstanceType, e.ID),
			"allow_deploy":     e.AllowDeploy,
			"allow_cloud":      e.AllowCloud,
			"available":        e.Capability != CapabilityComing,
		}
		if ag.Kind != "" {
			item["agent_kind"] = ag.Kind
			item["agent_label"] = ag.Label
			item["agent_port"] = ag.Port
			item["agent_note"] = ag.Note
		}
		out = append(out, item)
	}
	return out
}

func firstNonEmptyStr(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// DiffAgainstInstanceTypes 与 ticket_instance_types 交叉校验（host 忽略）。
// 返回告警文案；空切片表示一致。
func DiffAgainstInstanceTypes(ticketTypes []string) []string {
	reg := map[string]bool{}
	for _, e := range AllEngines() {
		id := firstNonEmptyStr(e.InstanceType, e.ID)
		if e.Workbench == WorkbenchProxy || strings.TrimSpace(e.InstanceType) == "" {
			continue
		}
		reg[strings.ToLower(id)] = true
	}
	ticket := map[string]bool{}
	for _, t := range ticketTypes {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || t == "host" {
			continue
		}
		ticket[t] = true
	}
	var warns []string
	for t := range ticket {
		if !reg[t] {
			warns = append(warns, fmt.Sprintf("ticket_instance_types 含 %s，但部署注册表未覆盖", t))
		}
	}
	for id := range reg {
		if !ticket[id] {
			warns = append(warns, fmt.Sprintf("部署注册表含 %s，但 ticket_instance_types 缺失", id))
		}
	}
	return warns
}

// ParseTicketInstanceTypeValues 从全局参数 JSON 提取 value 列表。
func ParseTicketInstanceTypeValues(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var list []map[string]any
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil
	}
	var out []string
	for _, it := range list {
		if v, ok := it["value"].(string); ok {
			out = append(out, v)
		}
	}
	return out
}

