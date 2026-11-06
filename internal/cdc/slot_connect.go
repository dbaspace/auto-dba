package cdc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"auto-dba/internal/store"
)

// ConnectSlotMeta 是 Kafka Connect 上一个 PostgreSQL 连接器对应的复制槽。
type ConnectSlotMeta struct {
	Base          string
	ConnectorName string
	Topic         string
	SlotName      string
	Database      string
	Host          string
	Port          int
}

// LoadConnectSlotMetas 只读各 Debezium 地址上的 PostgreSQL 连接器。
// complete 为 false 表示有地址或连接器没读全，调用方不要据此清掉已有补齐结果。
func LoadConnectSlotMetas(ctx context.Context, bases []string) ([]ConnectSlotMeta, bool) {
	var out []ConnectSlotMeta
	complete := true
	seen := map[string]struct{}{}
	for _, raw := range bases {
		base := NormalizeConnectBaseURL(raw)
		if base == "" {
			continue
		}
		if _, ok := seen[base]; ok {
			continue
		}
		seen[base] = struct{}{}
		names, err := fetchConnectConnectors(ctx, base)
		if err != nil {
			complete = false
			continue
		}
		for name := range names {
			cfg, err := fetchConnectorConfig(ctx, base, name)
			if err != nil {
				complete = false
				continue
			}
			meta, ok := connectSlotMeta(base, name, cfg)
			if !ok {
				continue
			}
			out = append(out, meta)
		}
	}
	return out, complete
}

func fetchConnectorConfig(ctx context.Context, base, name string) (map[string]any, error) {
	raw, code, err := connectDo(ctx, base, http.MethodGet, "/connectors/"+url.PathEscape(name), nil)
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, errStatus(base, name, code, raw)
	}
	var body struct {
		Config map[string]any `json:"config"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	if body.Config == nil {
		body.Config = map[string]any{}
	}
	return body.Config, nil
}

func errStatus(base, name string, code int, raw []byte) error {
	return &connectStatusError{base: base, name: name, code: code, body: strings.TrimSpace(string(raw))}
}

type connectStatusError struct {
	base string
	name string
	code int
	body string
}

func (e *connectStatusError) Error() string {
	return "GET " + e.base + "/connectors/" + e.name + " -> " + strconv.Itoa(e.code) + " " + e.body
}

func connectSlotMeta(base, name string, cfg map[string]any) (ConnectSlotMeta, bool) {
	cls := strings.ToLower(configString(cfg, "connector.class"))
	if cls == "" || (!strings.Contains(cls, "postgres") && !strings.Contains(cls, "postgresql")) {
		return ConnectSlotMeta{}, false
	}
	slotName := configString(cfg, "slot.name")
	if slotName == "" {
		return ConnectSlotMeta{}, false
	}
	taskName := strings.TrimSpace(name)
	if taskName == "" {
		taskName = configString(cfg, "name")
	}
	port, _ := strconv.Atoi(configString(cfg, "database.port"))
	return ConnectSlotMeta{
		Base:          base,
		ConnectorName: taskName,
		Topic:         configString(cfg, "topic.prefix"),
		SlotName:      slotName,
		Database:      configString(cfg, "database.dbname"),
		Host:          configString(cfg, "database.hostname"),
		Port:          port,
	}, true
}

func configString(cfg map[string]any, key string) string {
	v, ok := cfg[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case float64:
		return strconv.FormatInt(int64(t), 10)
	case bool:
		return strconv.FormatBool(t)
	default:
		return strings.TrimSpace(strings.Trim(strings.TrimSpace(jsonString(t)), `"`))
	}
}

func jsonString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// AssignConnectSlotMeta 把连接器按槽名配到已采集的复制槽。槽名唯一时直接配对。
// 同名槽有多条时，只在库名和主机端口能唯一区分时写入。
func AssignConnectSlotMeta(slots []store.SyncSlot, metas []ConnectSlotMeta) map[string]ConnectSlotMeta {
	type slotKey struct {
		id string
	}
	bySlot := map[string][]store.SyncSlot{}
	for _, s := range slots {
		key := strings.ToLower(strings.TrimSpace(s.SlotName))
		if key == "" || strings.TrimSpace(s.ID) == "" {
			continue
		}
		bySlot[key] = append(bySlot[key], s)
	}
	byMeta := map[string][]ConnectSlotMeta{}
	for _, m := range metas {
		key := strings.ToLower(strings.TrimSpace(m.SlotName))
		if key == "" {
			continue
		}
		byMeta[key] = append(byMeta[key], m)
	}
	out := map[string]ConnectSlotMeta{}
	for key, slotHits := range bySlot {
		metaHits := byMeta[key]
		if len(metaHits) == 0 {
			continue
		}
		if len(metaHits) == 1 && len(slotHits) == 1 {
			out[slotHits[0].ID] = metaHits[0]
			continue
		}
		type pair struct {
			slotID string
			meta   ConnectSlotMeta
		}
		var pairs []pair
		for _, slot := range slotHits {
			for _, meta := range metaHits {
				if connectMetaMatchesSlot(slot, meta) {
					pairs = append(pairs, pair{slotID: slot.ID, meta: meta})
				}
			}
		}
		slotCount := map[string]int{}
		metaCount := map[string]int{}
		for _, p := range pairs {
			slotCount[p.slotID]++
			metaCount[metaIdentity(p.meta)]++
		}
		for _, p := range pairs {
			if slotCount[p.slotID] == 1 && metaCount[metaIdentity(p.meta)] == 1 {
				out[p.slotID] = p.meta
			}
		}
	}
	return out
}

func metaIdentity(m ConnectSlotMeta) string {
	return strings.ToLower(m.Base) + "\n" + strings.ToLower(m.ConnectorName) + "\n" + strings.ToLower(m.SlotName)
}

func connectMetaMatchesSlot(slot store.SyncSlot, meta ConnectSlotMeta) bool {
	if slot.Database != "" && meta.Database != "" && !strings.EqualFold(slot.Database, meta.Database) {
		return false
	}
	if meta.Port > 0 && slot.Port > 0 && meta.Port != slot.Port {
		return false
	}
	if meta.Host != "" && slot.Host != "" && !strings.EqualFold(strings.TrimSpace(meta.Host), strings.TrimSpace(slot.Host)) {
		return false
	}
	return true
}

// FillSlotConnectFallback 本地任务没有命中时，用采集时记下的连接器信息填展示列。
func FillSlotConnectFallback(rows []SlotRow) []SlotRow {
	for i := range rows {
		if strings.TrimSpace(rows[i].TaskID) != "" || strings.TrimSpace(rows[i].TaskName) != "" ||
			strings.TrimSpace(rows[i].ConnectBase) != "" || strings.TrimSpace(rows[i].Topic) != "" ||
			strings.TrimSpace(rows[i].Connector) != "" {
			continue
		}
		base := strings.TrimSpace(rows[i].ConnectMetaBase)
		name := strings.TrimSpace(rows[i].ConnectMetaTask)
		if name == "" {
			name = strings.TrimSpace(rows[i].ConnectMetaName)
		}
		topic := strings.TrimSpace(rows[i].ConnectMetaTopic)
		if base == "" && name == "" && topic == "" {
			continue
		}
		rows[i].ConnectBase = base
		rows[i].Connector = connectorLabel(base)
		rows[i].ConnectorName = name
		rows[i].TaskName = name
		rows[i].Topic = topic
	}
	return rows
}
