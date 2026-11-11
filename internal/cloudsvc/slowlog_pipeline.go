package cloudsvc

import (
	"strings"
	"time"

	"auto-dba/internal/config"
	"auto-dba/internal/store"
)

const slowPipelineLag = 30 * time.Minute

type SlowPipelineItem struct {
	InstanceID  string `json:"instance_id"`
	Name        string `json:"name"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	CloudID     string `json:"cloud_instance_id"`
	Status      string `json:"status"`
	StatusText  string `json:"status_text"`
	Collected   int    `json:"collected"`
	CollectedAt string `json:"collected_at"`
	Kafka       bool   `json:"kafka"`
	Produced    int    `json:"produced"`
	ProducedAt  string `json:"produced_at"`
	Ingested    int    `json:"ingested"`
	IngestedAt  string `json:"ingested_at"`
	CursorAt    string `json:"cursor_at"`
	DelaySec    int    `json:"delay_sec"`
	Lag         int    `json:"lag"`
	Error       string `json:"error"`
}

type SlowPipeline struct {
	Instances int                `json:"instances"`
	OK        int                `json:"ok"`
	Lag       int                `json:"lag"`
	Failed    int                `json:"failed"`
	Records   int                `json:"records"`
	Items     []SlowPipelineItem `json:"items"`
}

func (s *Service) SlowlogPipeline(tenantID string) (*SlowPipeline, error) {
	list, err := s.DB.ListInstancesInTenant(tenantID, "", "")
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	bus := config.Snapshot().SlowlogBus
	out := &SlowPipeline{Items: []SlowPipelineItem{}}
	var picked []store.Instance
	var cloudIDs []string
	for _, it := range list {
		if !postgresType(it.DBType) || strings.TrimSpace(it.CloudInstanceID) == "" {
			continue
		}
		picked = append(picked, it)
		cloudIDs = append(cloudIDs, it.CloudInstanceID)
	}
	counts, err := s.DB.CountCloudSlowLogsSinceIDs(cloudIDs, now.Add(-time.Hour))
	if err != nil {
		return nil, err
	}
	for _, it := range picked {
		cur, err := s.DB.GetCloudCursor(it.CloudVendor, "postgres", it.CloudAccountID, it.CloudInstanceID, "slow_log")
		if err != nil {
			return nil, err
		}
		item := buildSlowPipelineItem(it, cur, bus.Allows(it.CloudInstanceID), now, counts[it.CloudInstanceID])
		out.Items = append(out.Items, item)
		out.Instances++
		out.Records += item.Collected
		switch item.Status {
		case "ok":
			out.OK++
		case "lag":
			out.Lag++
		case "failed":
			out.Failed++
		}
	}
	return out, nil
}

func postgresType(dbType string) bool {
	switch strings.ToLower(strings.TrimSpace(dbType)) {
	case "postgres", "postgresql", "pg":
		return true
	default:
		return false
	}
}

func buildSlowPipelineItem(it store.Instance, cur *store.CloudCursor, kafka bool, now time.Time, stored int) SlowPipelineItem {
	item := SlowPipelineItem{
		InstanceID: it.ID,
		Name:       it.Name,
		Host:       it.Host,
		Port:       it.Port,
		CloudID:    it.CloudInstanceID,
		Kafka:      kafka,
		Status:     "none",
		StatusText: "未同步",
	}
	if cur == nil || (cur.CursorAt == "" && cur.Status == "" && cur.LastCount == 0 && cur.UpdatedAt == "") {
		return item
	}
	item.Collected = stored
	item.CollectedAt = firstTime(cur.ProducedAt, cur.UpdatedAt)
	item.Produced = stored
	item.ProducedAt = cur.ProducedAt
	item.Ingested = stored
	item.IngestedAt = cur.IngestedAt
	item.CursorAt = cur.CursorAt
	item.Error = cur.Error
	if stored > 0 && strings.TrimSpace(cur.IngestedAt) == "" {
		item.DelaySec = 0
	} else {
		item.DelaySec = pipelineDelay(cur.ProducedAt, cur.IngestedAt, now)
	}
	item.Status, item.StatusText = classifySlowPipeline(cur, now)
	return item
}

func classifySlowPipeline(cur *store.CloudCursor, now time.Time) (string, string) {
	if cur.Status == "error" || cur.FailCount > 0 {
		return "failed", "失败"
	}
	if cur.Status == "gap" || cursorBehind(cur.CursorAt, now) || cur.IngestedCount < cur.LastCount {
		return "lag", "延迟告警"
	}
	return "ok", "运行正常"
}

func cursorBehind(cursorAt string, now time.Time) bool {
	t, err := time.Parse(time.RFC3339, cursorAt)
	if err != nil {
		return false
	}
	return now.Sub(t) > slowPipelineLag
}

func pipelineDelay(produced, ingested string, now time.Time) int {
	start, err := time.Parse(time.RFC3339, produced)
	if err != nil {
		return 0
	}
	end := now
	if t, err := time.Parse(time.RFC3339, ingested); err == nil {
		end = t
	}
	d := end.Sub(start)
	if d < 0 {
		return 0
	}
	return int(d.Round(time.Second).Seconds())
}

func firstTime(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
