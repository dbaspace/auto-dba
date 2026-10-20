package cdc

import "strings"

type TableMapping struct {
	SourceTables       []string          `json:"source_tables"`
	TargetTable        string            `json:"target_table"`
	IsShardGroup       bool              `json:"is_shard_group"`
	ShardPattern       string            `json:"shard_pattern"`
	KeyColumns         []string          `json:"key_columns"`
	ShardKeys          []string          `json:"shard_keys"`
	PartitionColumn    string            `json:"partition_column"`
	SrDynamicPartition map[string]string `json:"sr_dynamic_partition,omitempty"`
	SourceDbScope      string            `json:"source_db_scope,omitempty"`
	SourceDbNames      []string          `json:"source_db_names,omitempty"`
	SyncTableMode      string            `json:"sync_table_mode,omitempty"`
	Status             string            `json:"status,omitempty"`
	Source             string            `json:"source,omitempty"`
	KafkaTopics        []string          `json:"kafka_topics,omitempty"`
	RoutineLoadName    string            `json:"routine_load_name,omitempty"`
	RoutineLoadStatus  string            `json:"routine_load_status,omitempty"`
}

type PreviewInput struct {
	Name                   string
	Env                    string
	SourceDB               string
	SourceSchema           string
	PGHost                 string
	PGPort                 int
	PGUser                 string
	DebeziumMode           string
	SnapshotMode           string
	FilterDeleteEvents     bool
	Tables                 []string
	Mappings               []TableMapping
	PublicationName        string
	SlotName               string
	TopicPrefix            string
	TopicPartitions        int
	TopicReplicationFactor int
}

type PreviewResult struct {
	Mode               string            `json:"mode"`
	Template           string            `json:"template"`
	Notes              []string          `json:"notes"`
	PublicationName    string            `json:"publication_name"`
	SlotName           string            `json:"slot_name"`
	TopicPrefix        string            `json:"topic_prefix"`
	FilterDeleteEvents bool              `json:"filter_delete_events"`
	Config             map[string]string `json:"config"`
}

func SplitMappings(mappings []TableMapping, fallbackTables []string) (plain []string, shards []ShardGroup) {
	if len(mappings) == 0 {
		return cleanStringSlice(fallbackTables), nil
	}
	for _, m := range mappings {
		src := cleanStringSlice(m.SourceTables)
		tgt := strings.TrimSpace(m.TargetTable)
		if tgt == "" && len(src) > 0 {
			tgt = src[0]
		}
		if IsShardMapping(m) {
			shards = append(shards, ShardGroup{
				LogicalName: tgt,
				Regex:       deriveShardRegexPart(tgt, m.ShardPattern, src),
			})
			continue
		}
		if len(src) == 1 {
			plain = append(plain, src[0])
		}
	}
	return plain, shards
}

func TemplateName(mode string) string {
	if strings.TrimSpace(mode) == ModeBeforeAfter {
		return "debezium_before_after"
	}
	return "debezium_after_only"
}

func BuildPreview(in PreviewInput) PreviewResult {
	mode := strings.TrimSpace(in.DebeziumMode)
	if mode == "" {
		mode = ModeAfterOnly
	}
	filter := ResolveFilterDeleteEvents(mode, in.FilterDeleteEvents)
	n := ResolveInitNaming(InitNamingInput{
		Env:             in.Env,
		SourceDB:        in.SourceDB,
		TaskName:        in.Name,
		PublicationName: in.PublicationName,
		SlotName:        in.SlotName,
		TopicPrefix:     in.TopicPrefix,
	})
	pub, slot, topic := n.PublicationName, n.SlotName, n.TopicPrefix
	plain, shards := SplitMappings(in.Mappings, in.Tables)
	port := in.PGPort
	if port <= 0 {
		port = 5432
	}
	cfg := BuildConfig(mode, DebeziumBuildParams{
		PGHost:                 defaultStr(in.PGHost, "pg.example.com"),
		PGPort:                 port,
		PGUser:                 defaultStr(in.PGUser, "repl"),
		PGPassword:             MaskedPassword,
		PGDbName:               defaultStr(in.SourceDB, "postgres"),
		Schema:                 defaultStr(in.SourceSchema, "public"),
		Tables:                 plain,
		ShardGroups:            shards,
		PublicationName:        pub,
		SlotName:               slot,
		TopicPrefix:            topic,
		SnapshotMode:           defaultStr(in.SnapshotMode, SnapshotNever),
		TopicPartitions:        in.TopicPartitions,
		TopicReplicationFactor: in.TopicReplicationFactor,
		FilterDeleteEvents:     filter,
	})
	notes := make([]string, 0, 4)
	if mode == ModeBeforeAfter {
		notes = append(notes, "before_after：保留完整 envelope（before / after / op / source），不使用 ExtractNewRecordState。")
		notes = append(notes, "推荐部署在 message_schema_mode=without_schema 的 Connect 集群。")
		if filter {
			notes = append(notes, "已启用 Filter SMT：value.op != 'd'，删除事件不会写入 Kafka。")
		}
	} else {
		notes = append(notes, "after_only：ExtractNewRecordState unwrap，add.fields=op,source.db,source.table,source.ts_ms。")
		notes = append(notes, "推荐部署在 message_schema_mode=with_schema 的 Connect 集群。")
		if in.FilterDeleteEvents {
			notes = append(notes, "filter_delete_events 仅 before_after 生效，after_only 已忽略。")
		}
	}
	notes = append(notes, "publication.autocreate.mode=disabled，需预先创建 FOR TABLE 发布。")
	return PreviewResult{
		Mode:               mode,
		Template:           TemplateName(mode),
		Notes:              notes,
		PublicationName:    pub,
		SlotName:           slot,
		TopicPrefix:        topic,
		FilterDeleteEvents: filter,
		Config:             cfg,
	}
}
