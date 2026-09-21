package cdc

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var srDynamicPartitionKeyRE = regexp.MustCompile(`^dynamic_partition\.[a-z0-9_]+$`)

// DefaultSRDynamicPartitionProps 与全局参数 sync_sr_dynamic_partition_defaults 一致。
var DefaultSRDynamicPartitionProps = map[string]string{
	"dynamic_partition.enable":    "true",
	"dynamic_partition.time_unit": "DAY",
	"dynamic_partition.start":     "-3",
	"dynamic_partition.end":       "3",
	"dynamic_partition.prefix":    "p",
}

func ParseJSONStringMap(raw string) map[string]string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" || raw == "null" {
		return nil
	}
	var m map[string]string
	if json.Unmarshal([]byte(raw), &m) == nil && len(m) > 0 {
		return m
	}
	var gen map[string]any
	if json.Unmarshal([]byte(raw), &gen) != nil {
		return nil
	}
	out := make(map[string]string, len(gen))
	for k, v := range gen {
		if v == nil {
			continue
		}
		s := strings.TrimSpace(fmt.Sprint(v))
		if s == "" {
			continue
		}
		out[k] = s
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func MergeStringMaps(layers ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, layer := range layers {
		for k, v := range layer {
			k = strings.TrimSpace(k)
			v = strings.TrimSpace(v)
			if k == "" || v == "" {
				continue
			}
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func ValidateSRDynamicPartitionProps(props map[string]string) error {
	for k, v := range props {
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" {
			continue
		}
		if !srDynamicPartitionKeyRE.MatchString(k) {
			return fmt.Errorf("invalid sr_dynamic_partition key %q: must match dynamic_partition.*", k)
		}
		if v == "" {
			return fmt.Errorf("sr_dynamic_partition key %q: value required", k)
		}
		if strings.ContainsAny(v, "\n\r`") {
			return fmt.Errorf("sr_dynamic_partition key %q: invalid characters in value", k)
		}
	}
	return nil
}

func EffectivePartitionColumn(taskCol, mappingCol string) string {
	if s := strings.TrimSpace(mappingCol); s != "" {
		return s
	}
	return strings.TrimSpace(taskCol)
}

func IsPartitionEligibleUDT(udt string) bool {
	h := strings.ToLower(strings.TrimSpace(udt))
	if strings.Contains(h, "time") && !strings.Contains(h, "timestamp") && !strings.Contains(h, "datetime") {
		return false
	}
	return strings.Contains(h, "date") || strings.Contains(h, "timestamp") || strings.Contains(h, "datetime")
}

func SRKeyClause(model string) string {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "primary_key", "primary key":
		return "PRIMARY KEY"
	case "duplicate_key", "duplicate key":
		return "DUPLICATE KEY"
	case "aggregate_key", "aggregate key":
		return "AGGREGATE KEY"
	default:
		return "UNIQUE KEY"
	}
}

func FormatSRTableProperties(replicationNum int, dynamicProps map[string]string) string {
	base := map[string]string{
		"replication_num":         fmt.Sprintf("%d", replicationNum),
		"enable_persistent_index": "true",
	}
	merged := MergeStringMaps(base, dynamicProps)
	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf(`  "%s" = "%s"`, k, merged[k]))
	}
	return strings.Join(lines, ",\n")
}

func ParseMappingsJSON(raw string) []TableMapping {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return nil
	}
	var out []TableMapping
	if json.Unmarshal([]byte(raw), &out) != nil {
		return nil
	}
	return out
}
