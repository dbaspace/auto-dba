package dns

import "strings"

const (
	StatusActive  = "Active"
	StatusRemoved = "Removed"

	SerialUpdateZones = "hdns-UpdateZones"
	SerialSyncZone    = "hdns-SyncZone"

	DefaultRangeName = "DEFAULT"
	DefaultRangeID   = "dns-range-default"

	RecordTypes = "A,AAAA,CNAME,TXT,MX"
)

// Zone 对应 hdns 表 zones。Origin 带结尾点。
type Zone struct {
	ID          string `json:"id"`
	Origin      string `json:"origin"`
	NS          string `json:"ns"`
	Mbox        string `json:"mbox"`
	Serial      int64  `json:"serial"`
	TTL         int64  `json:"ttl"`
	Status      string `json:"status"`
	UpdatedTime int64  `json:"updated_time"`
}

// Range 对应 parsing_ranges。
type Range struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	NetworkList []string `json:"network_list"`
	Description string   `json:"description"`
	Status      string   `json:"status"`
	UpdatedTime int64    `json:"updated_time"`
}

// Record 对应 rrs。Data 为库内原文；MX 另给邮件主机和优先级。
type Record struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Type             string   `json:"type"`
	Data             string   `json:"data"`
	MailHost         string   `json:"mail_host,omitempty"`
	Preference       int      `json:"preference,omitempty"`
	TTL              int64    `json:"ttl"`
	ZoneID           string   `json:"zone_id"`
	ZoneOrigin       string   `json:"zone_origin,omitempty"`
	ParsingRangeID   string   `json:"parsing_range_id"`
	ParsingRangeName string   `json:"parsing_range_name,omitempty"`
	NetworkList      []string `json:"network_list,omitempty"`
	Description      string   `json:"description"`
	Status           string   `json:"status"`
	UpdatedTime      int64    `json:"updated_time"`
}

// Node 来自 coredns 心跳表。
// DataUpdatedTime 为节点最近一次重新加载数据的时间；ChangeTime 为平台最近一次变更时间。
type Node struct {
	IP              string `json:"ip"`
	Name            string `json:"name"`
	UpdatedTime     int64  `json:"updated_time"`
	DataUpdatedTime int64  `json:"data_updated_time"`
	ChangeTime      int64  `json:"change_time"`
	Online          bool   `json:"online"`
	Synced          bool   `json:"synced"`
}

const (
	NodeOfflineSeconds = 60
	SyncLagSeconds     = 120
	DefaultMetricsPort = 9153
)

// DeployOptions 写在集群 ha_options_json，供变更单生成 Corefile。
type DeployOptions struct {
	Listen      string `json:"listen"`
	Upstream    string `json:"upstream"`
	DBHost      string `json:"db_host"`
	DBPort      int    `json:"db_port"`
	MetricsPort int    `json:"metrics_port,omitempty"`
}

func NormalizeStatus(s string) string {
	switch strings.TrimSpace(s) {
	case "", StatusActive, "运行", "运行中":
		return StatusActive
	case StatusRemoved, "停止", "停用":
		return StatusRemoved
	default:
		return ""
	}
}
