package cdc

import (
	"fmt"
	"strings"
)

const (
	debeziumFilterSMTType      = "io.debezium.transforms.Filter"
	debeziumFilterSMTLanguage  = "jsr223.groovy"
	debeziumFilterSMTCondition = "value.op != 'd'"
)

func ResolveFilterDeleteEvents(debeziumMode string, enabled bool) bool {
	if strings.TrimSpace(debeziumMode) != ModeBeforeAfter {
		return false
	}
	return enabled
}

func pruneEmptyTransformsKey(cfg map[string]string) {
	if cfg == nil {
		return
	}
	if strings.TrimSpace(cfg["transforms"]) == "" {
		delete(cfg, "transforms")
	}
}

func stripFilterFromTransformsChain(cfg map[string]string) {
	if cfg == nil {
		return
	}
	parts := splitTransformsChain(cfg["transforms"])
	filtered := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "filter" {
			filtered = append(filtered, p)
		}
	}
	if len(filtered) == 0 {
		delete(cfg, "transforms")
		return
	}
	cfg["transforms"] = strings.Join(filtered, ",")
}

func applyBeforeAfterDeleteFilter(cfg map[string]string, enabled bool) {
	if cfg == nil {
		return
	}
	if enabled {
		cfg["transforms.filter.type"] = debeziumFilterSMTType
		cfg["transforms.filter.language"] = debeziumFilterSMTLanguage
		cfg["transforms.filter.condition"] = debeziumFilterSMTCondition
		return
	}
	delete(cfg, "transforms.filter.type")
	delete(cfg, "transforms.filter.language")
	delete(cfg, "transforms.filter.condition")
	stripFilterFromTransformsChain(cfg)
}

func BuildConfigBeforeAfter(p DebeziumBuildParams) map[string]string {
	partitions := p.TopicPartitions
	if partitions <= 0 {
		partitions = 3
	}
	replicationFactor := p.TopicReplicationFactor
	if replicationFactor <= 0 {
		replicationFactor = 1
	}
	cfg := map[string]string{
		"connector.class":                           "io.debezium.connector.postgresql.PostgresConnector",
		"database.hostname":                         p.PGHost,
		"database.port":                             fmt.Sprintf("%d", p.PGPort),
		"database.user":                             p.PGUser,
		"database.password":                         p.PGPassword,
		"database.dbname":                           p.PGDbName,
		"table.include.list":                        buildTableIncludeListWithShards(p.Schema, p.Tables, p.ShardGroups),
		"topic.prefix":                              p.TopicPrefix,
		"snapshot.mode":                             defaultStr(p.SnapshotMode, SnapshotNever),
		"topic.creation.default.partitions":         fmt.Sprintf("%d", partitions),
		"topic.creation.default.replication.factor": fmt.Sprintf("%d", replicationFactor),
		"signal.data.collection":                    "public.debezium_signal",
		"plugin.name":                               "pgoutput",
		"publication.name":                          p.PublicationName,
		"publication.autocreate.mode":               "disabled",
		"slot.name":                                 p.SlotName,
		"time.precision.mode":                       "connect",
		"tombstones.on.delete":                      "false",
		"decimal.handling.mode":                     "string",
	}
	applyShardRouterTransforms(cfg, p.ShardGroups)
	if p.FilterDeleteEvents {
		applyBeforeAfterDeleteFilter(cfg, true)
		normalizeConnectorTransformsChain(cfg)
	} else {
		pruneEmptyTransformsKey(cfg)
	}
	return cfg
}

func BuildConfig(mode string, p DebeziumBuildParams) map[string]string {
	switch strings.TrimSpace(mode) {
	case ModeBeforeAfter:
		return BuildConfigBeforeAfter(p)
	default:
		return BuildConfigAfterOnly(p)
	}
}
