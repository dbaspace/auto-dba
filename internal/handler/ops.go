package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"auto-dba/internal/archive"
	"auto-dba/internal/cdc"
	"auto-dba/internal/config"
	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

func notConfigured(c *gin.Context, name string) {
	c.JSON(http.StatusOK, gin.H{
		"ok":      false,
		"message": name + " 外部依赖未配置。独立部署下任务已落库，执行编排需在 config.yaml 中填写对应地址。",
	})
}

// cdcConnectHint 对齐 hub：优先引导「配置 → 连接器」，yaml 仅作全局兜底。
func cdcConnectHint(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"ok": false,
		"message": "未解析到 Debezium Connect 地址。请先在「配置 → 连接器」填写 dbz_host，并在创建 CDC 任务时选择该连接器；" +
			"也可在 config.yaml 配置 cdc.debezium_url（可选全局兜底）。",
	})
}

type CDCHandler struct {
	Store  *store.DB
	Cfg    *config.Config
	Secret string
}

func (h *CDCHandler) ListInstances(c *gin.Context) {
	list, err := h.Store.ListInstancesInTenant(effectiveListTenantID(h.Store, c), c.Query("db_type"), c.Query("q"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *CDCHandler) List(c *gin.Context) {
	list, err := h.Store.ListCDCInTenant(effectiveListTenantID(h.Store, c), c.Query("status"), c.Query("task_type"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []store.CDCTask{}
	}
	// 对齐 Hub CdcSyncList：列表只读 DB，can_reinit 按状态规则；不在列表同步探测 Connect。
	// connector_missing / Connect 存在性仍由 Get / reinit candidates 的 AnnotateReinit 负责。
	for i := range list {
		list[i].Description = normalizeCDCDescription(list[i].Description)
		if ok, _ := cdc.ReinitEligibleStatus(&list[i]); ok {
			list[i].CanReinit = true
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *CDCHandler) Get(c *gin.Context) {
	t, err := h.Store.GetCDCInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || t == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "任务不存在"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	one := []store.CDCTask{*t}
	h.orch().AnnotateReinit(ctx, one)
	cancel()
	t.CanReinit, t.ConnectorMissing = one[0].CanReinit, one[0].ConnectorMissing
	t.Description = normalizeCDCDescription(t.Description)
	c.JSON(http.StatusOK, gin.H{"data": t})
}

func (h *CDCHandler) DebeziumPreview(c *gin.Context) {
	var raw map[string]any
	if err := c.ShouldBindJSON(&raw); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "message": "请求体无效"})
		return
	}
	extra := parseCDCExtraMap(jsonAnyString(raw["description"]))
	pick := func(keys ...string) string {
		for _, k := range keys {
			if s := jsonAnyString(raw[k]); s != "" {
				return s
			}
			if extra != nil {
				if s := jsonAnyString(extra[k]); s != "" {
					return s
				}
			}
		}
		return ""
	}
	srcID := pick("source_instance_id")
	in := cdc.PreviewInput{
		Name:                   pick("name"),
		Env:                    pick("env"),
		SourceDB:               pick("source_db"),
		SourceSchema:           pick("source_schema"),
		DebeziumMode:           pick("debezium_mode"),
		SnapshotMode:           pick("snapshot_mode"),
		FilterDeleteEvents:     jsonAnyBool(raw["filter_delete_events"]),
		PublicationName:        pick("publication_name"),
		SlotName:               pick("slot_name"),
		TopicPrefix:            pick("topic_prefix"),
		TopicPartitions:        jsonAnyInt(raw["kafka_topic_partitions"]),
		TopicReplicationFactor: jsonAnyInt(raw["kafka_topic_replication_factor"]),
	}
	if extra != nil {
		if in.TopicPartitions <= 0 {
			in.TopicPartitions = jsonAnyInt(extra["kafka_topic_partitions"])
		}
		if in.TopicReplicationFactor <= 0 {
			in.TopicReplicationFactor = jsonAnyInt(extra["kafka_topic_replication_factor"])
		}
	}
	if extra != nil && raw["filter_delete_events"] == nil {
		in.FilterDeleteEvents = jsonAnyBool(extra["filter_delete_events"])
	}
	in.Tables = jsonAnyStringSlice(raw["tables"])
	if len(in.Tables) == 0 && extra != nil {
		in.Tables = jsonAnyStringSlice(extra["tables"])
	}
	in.Mappings = cdcMappingsFromAny(raw["mappings"])
	if len(in.Mappings) == 0 && extra != nil {
		in.Mappings = cdcMappingsFromAny(extra["mappings"])
	}
	if srcID != "" {
		if inst, _ := resolveInstanceInTenant(h.Store, c, srcID); inst != nil {
			in.PGHost = strings.TrimSpace(inst.Host)
			if in.PGHost == "" {
				in.PGHost = strings.TrimSpace(inst.DomainName)
			}
			in.PGPort = inst.Port
			in.PGUser = strings.TrimSpace(inst.Username)
		}
	}
	out := cdc.BuildPreview(in)
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": out})
}

const cdcExtraPrefix = "-- auto-dba-cdc:"

func jsonAnyBool(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		s := strings.ToLower(strings.TrimSpace(x))
		return s == "1" || s == "true" || s == "yes" || s == "on"
	case json.Number:
		n, _ := x.Int64()
		return n != 0
	case float64:
		return x != 0
	case int:
		return x != 0
	}
	return false
}

func jsonAnyStringSlice(v any) []string {
	switch x := v.(type) {
	case nil:
		return nil
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s := jsonAnyString(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return nil
		}
		parts := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '，' || r == '\n' })
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if t := strings.TrimSpace(p); t != "" {
				out = append(out, t)
			}
		}
		return out
	}
	return nil
}

func cdcMappingsFromAny(v any) []cdc.TableMapping {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out []cdc.TableMapping
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return out
}

func jsonAnyString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(x)
	case json.Number:
		return strings.TrimSpace(x.String())
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strings.TrimSpace(strconv.FormatFloat(x, 'f', -1, 64))
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case map[string]any:
		for _, k := range []string{"id", "instance_id", "value"} {
			if s := jsonAnyString(x[k]); s != "" {
				return s
			}
		}
	case []any:
		if len(x) > 0 {
			return jsonAnyString(x[0])
		}
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func jsonAnyInt(v any) int {
	switch x := v.(type) {
	case nil:
		return 0
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	case json.Number:
		n, _ := x.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(x))
		return n
	}
	return 0
}

func cdcAutoCreateEnabled(raw map[string]any) bool {
	if raw == nil {
		return true
	}
	if raw["auto_create_table"] != nil {
		return jsonAnyBool(raw["auto_create_table"])
	}
	extra := parseCDCExtraMap(jsonAnyString(raw["description"]))
	if extra != nil && extra["auto_create_table"] != nil {
		return jsonAnyBool(extra["auto_create_table"])
	}
	return true
}

func parseCDCExtraMap(desc string) map[string]any {
	idx := strings.Index(desc, cdcExtraPrefix)
	if idx < 0 {
		return nil
	}
	var extra map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(desc[idx+len(cdcExtraPrefix):])), &extra); err != nil {
		return nil
	}
	return extra
}

func normalizeCDCDescription(desc string) string {
	extra := parseCDCExtraMap(desc)
	if extra == nil {
		return desc
	}
	changed := false
	for _, key := range []string{"mappings", "tables", "source_db_names"} {
		if normalizeCDCExtraArray(extra, key) {
			changed = true
		}
	}
	if !changed {
		return desc
	}
	text := desc
	if idx := strings.Index(desc, cdcExtraPrefix); idx >= 0 {
		text = strings.TrimSpace(desc[:idx])
	}
	return encodeCDCExtraJSON(text, extra)
}

func normalizeCDCExtraArray(extra map[string]any, key string) bool {
	v, ok := extra[key]
	if !ok {
		return false
	}
	switch x := v.(type) {
	case []any:
		return false
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			extra[key] = []any{}
			return true
		}
		if strings.HasPrefix(s, "[") {
			var arr []any
			if json.Unmarshal([]byte(s), &arr) == nil {
				extra[key] = arr
				return true
			}
			extra[key] = []any{}
			return true
		}
		parts := strings.FieldsFunc(s, func(r rune) bool {
			return r == ',' || r == '，'
		})
		out := make([]any, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		extra[key] = out
		return true
	default:
		return false
	}
}

func cdcInstanceIDsFromPayload(raw map[string]any) (sourceID, targetID string) {
	sourceID = jsonAnyString(raw["source_instance_id"])
	targetID = jsonAnyString(raw["target_instance_id"])
	if extra := parseCDCExtraMap(jsonAnyString(raw["description"])); extra != nil {
		if sourceID == "" {
			sourceID = jsonAnyString(extra["source_instance_id"])
		}
		if targetID == "" {
			targetID = jsonAnyString(extra["target_instance_id"])
		}
	}
	return sourceID, targetID
}

func cdcNeedsApproval(taskType string) bool {
	switch strings.TrimSpace(taskType) {
	case "init", "add_table", "alter_schema", "remove_table", "delete_task":
		return true
	default:
		return false
	}
}

func cdcApproved(t *store.CDCTask) bool {
	if t == nil {
		return false
	}
	if t.ApprovalStatus == store.StatusPending || t.ApprovalStatus == store.StatusRejected || t.ApprovalStatus == store.StatusCanceled {
		return false
	}
	if (t.ApprovalStatus == "" || t.ApprovalStatus == "none") && t.IssueID == "" {
		return true
	}
	return t.ApprovalStatus == store.StatusApproved
}

func cdcResolveReviewer(raw ...string) string {
	for _, v := range raw {
		if s := strings.TrimSpace(v); s != "" {
			if i := strings.IndexAny(s, ",，"); i >= 0 {
				s = strings.TrimSpace(s[:i])
			}
			if s != "" {
				return s
			}
		}
	}
	return ""
}

func (h *CDCHandler) Create(c *gin.Context) {
	var raw map[string]any
	if err := c.ShouldBindJSON(&raw); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	t := hydrateCDCFromMap(raw)
	if t.TaskType == "" {
		t.TaskType = "init"
	}
	if err := h.prepareCDCCreate(c, &t); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	created, err := h.persistCDCTask(c, &t, cdcReviewersFromMap(raw)...)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": created})
}

func (h *CDCHandler) prepareCDCCreate(c *gin.Context, t *store.CDCTask) error {
	tid := middleware.TenantID(c)
	if t.TaskType == "" {
		t.TaskType = "init"
	}
	if t.TaskType == "add_table" || t.TaskType == "alter_schema" || t.TaskType == "remove_table" || t.TaskType == "delete_task" {
		if t.ParentID == "" {
			return fmt.Errorf("%s 必须指定 parent_id", t.TaskType)
		}
		parent, _ := h.Store.GetCDCInTenant(t.ParentID, tid)
		if parent == nil || parent.TaskType != "init" {
			return fmt.Errorf("%s 要求 parent 为初始化任务", t.TaskType)
		}
		if t.TaskType == "add_table" && parent.Status != store.StatusRunning {
			return fmt.Errorf("add_table 要求 parent 为 running 的 init 任务")
		}
		if t.TaskType == "delete_task" && parent.TaskType != "init" {
			return fmt.Errorf("delete_task 要求 parent 为初始化任务")
		}
		if t.SourceID == "" {
			t.SourceID = parent.SourceID
		}
		if t.TargetID == "" {
			t.TargetID = parent.TargetID
		}
		if t.SourceDB == "" {
			t.SourceDB = parent.SourceDB
		}
		if t.TargetDB == "" {
			t.TargetDB = parent.TargetDB
		}
	}
	if t.Name == "" || t.SourceID == "" {
		return fmt.Errorf("name/source_instance_id 必填")
	}
	if it, err := resolveInstanceInTenant(h.Store, c, t.SourceID); err != nil || it == nil {
		return fmt.Errorf("源实例不存在")
	}
	if t.TargetID != "" {
		if it, err := resolveInstanceInTenant(h.Store, c, t.TargetID); err != nil || it == nil {
			return fmt.Errorf("目标实例不存在")
		}
	}
	if t.TaskType == "init" && t.TargetID == "" && cdcAutoCreateEnabled(map[string]any{"description": t.Description}) {
		return fmt.Errorf("自动建表开启时必须指定 target_instance_id")
	}
	if t.TaskType == "init" {
		if err := cdc.ValidateShardCreate(cdc.ParseTaskExtra(t.Description), t.SourceDB); err != nil {
			return err
		}
		extraMap := cdc.ParseTaskExtra(t.Description)
		n := cdc.ApplyInitNaming(extraMap, t.Name, t.SourceDB)
		text, extraAny := splitCDCDescMap(t.Description)
		if extraAny == nil {
			extraAny = map[string]any{}
		}
		extraAny["publication_name"] = n.PublicationName
		extraAny["slot_name"] = n.SlotName
		extraAny["connector_name"] = n.ConnectorName
		extraAny["topic_prefix"] = n.TopicPrefix
		if id := extraMap["connector_id"]; id != "" {
			conn, _ := h.Store.GetConnectorInTenant(id, tid)
			if conn == nil {
				return fmt.Errorf("Debezium 服务不存在：%s", id)
			}
			{
				if u := cdc.NormalizeConnectBaseURL(conn.DBZHost); u != "" {
					extraAny["connect_base_url"] = u
				}
				if jsonAnyString(extraAny["kafka_bootstrap_servers"]) == "" {
					if kb := cdc.NormalizeKafkaBootstrap(conn.KafkaHost); kb != "" {
						extraAny["kafka_bootstrap_servers"] = kb
					}
				}
			}
		}
		t.Description = encodeCDCExtraJSON(text, extraAny)
	}
	extra := cdc.ParseTaskExtra(t.Description)
	maps := cdc.ParseMappingsJSON(extra["mappings"])
	taskDBs := cdc.ResolveShardDBs(extra, t.SourceDB)
	if t.TaskType != "init" && t.ParentID != "" {
		if parent, _ := h.Store.GetCDCInTenant(t.ParentID, tid); parent != nil {
			taskDBs = cdc.ResolveShardDBs(cdc.ParseTaskExtra(parent.Description), parent.SourceDB)
		}
	}
	if err := cdc.ValidateMappingShardScopes(maps, taskDBs); err != nil {
		return err
	}
	return nil
}

func (h *CDCHandler) persistCDCTask(c *gin.Context, t *store.CDCTask, reviewerRaw ...string) (*store.CDCTask, error) {
	return h.persistCDCTaskShared(c, t, nil, reviewerRaw...)
}

func (h *CDCHandler) createSharedCDCIssue(c *gin.Context, t *store.CDCTask, reviewerRaw ...string) (*store.Issue, error) {
	preferred := ""
	if len(reviewerRaw) > 0 {
		preferred = strings.TrimSpace(reviewerRaw[0])
	}
	reviewer, err := resolveTenantAssignee(h.Store, middleware.TenantID(c), preferred)
	if err != nil {
		return nil, err
	}
	th := &TicketHandler{Store: h.Store}
	if approvers := th.resolveApprovers(t.SourceID, t.SourceDB); len(approvers) > 0 && !usernameInList(reviewer, approvers) {
		return nil, fmt.Errorf("审核人不在实例/库审批人名单中")
	}
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	issue := &store.Issue{
		IssueType:   store.IssueTypeCDC,
		Type:        store.IssueTypeCDC,
		InstanceID:  t.SourceID,
		DBName:      t.SourceDB,
		SQLContent:  fmt.Sprintf("cdc_sync %s %s %s", t.TaskType, t.ID, t.Name),
		Description: t.Description,
		Submitter:   middleware.Username(c),
		AssignedTo:  reviewer,
	}
	stampTenantID(c, func(tid string) {
		if issue != nil {
			issue.TenantID = tid
		}
	})
	if err := h.Store.CreateIssue(issue); err != nil {
		return nil, err
	}
	NotifyIssueCreatedAsync(h.Store, h.Secret, issue)
	return issue, nil
}

func (h *CDCHandler) persistCDCTaskShared(c *gin.Context, t *store.CDCTask, shared *store.Issue, reviewerRaw ...string) (*store.CDCTask, error) {
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	user := middleware.Username(c)
	reviewer := ""
	if len(reviewerRaw) > 0 {
		reviewer = strings.TrimSpace(reviewerRaw[0])
	}
	text, extra := splitCDCDescMap(t.Description)
	if extra == nil {
		extra = map[string]any{}
	}
	if user != "" {
		extra["created_by"] = user
	}
	if reviewer != "" {
		extra["assigned_to"] = reviewer
	}
	t.Description = encodeCDCExtraJSON(text, extra)
	t.ApprovalStatus = "none"
	t.Status = store.StatusInit
	if cdcNeedsApproval(t.TaskType) {
		if shared == nil {
			issue, err := h.createSharedCDCIssue(c, t, reviewerRaw...)
			if err != nil {
				return nil, err
			}
			shared = issue
		}
		t.ApprovalStatus = store.StatusPending
		t.Status = store.StatusPending
		t.IssueID = shared.ID
		if shared.AssignedTo != "" {
			extra["assigned_to"] = shared.AssignedTo
			t.Description = encodeCDCExtraJSON(text, extra)
		}
	}
	stampTenantID(c, func(tid string) { t.TenantID = tid })
	if err := h.Store.CreateCDC(t); err != nil {
		return nil, err
	}
	_ = h.Store.InsertCDCLog(&store.CDCSyncLog{
		TaskID:   t.ID,
		Level:    "INFO",
		Step:     "create",
		Message:  fmt.Sprintf("任务已创建 type=%s name=%s publication=%s slot=%s connector=%s", t.TaskType, t.Name, jsonAnyString(extra["publication_name"]), jsonAnyString(extra["slot_name"]), jsonAnyString(extra["connector_name"])),
		Operator: user,
	})
	return t, nil
}

func (h *CDCHandler) logCDCUserAction(taskID, action, operator string, actionErr error) {
	if h == nil || h.Store == nil || strings.TrimSpace(taskID) == "" {
		return
	}
	if operator == "" {
		operator = "unknown"
	}
	level, msg := "INFO", ""
	switch action {
	case "pause":
		if actionErr != nil {
			level, msg = "ERROR", fmt.Sprintf("用户 %s 手动暂停失败: %v", operator, actionErr)
		} else {
			msg = fmt.Sprintf("用户 %s 手动暂停任务", operator)
		}
	case "resume":
		if actionErr != nil {
			level, msg = "ERROR", fmt.Sprintf("用户 %s 手动恢复失败: %v", operator, actionErr)
		} else {
			msg = fmt.Sprintf("用户 %s 手动恢复任务", operator)
		}
	case "restart":
		if actionErr != nil {
			level, msg = "ERROR", fmt.Sprintf("用户 %s 手动重启失败: %v", operator, actionErr)
		} else {
			msg = fmt.Sprintf("用户 %s 手动重启 Connector", operator)
		}
	case "retry":
		if actionErr != nil {
			level, msg = "ERROR", fmt.Sprintf("用户 %s 重试编排失败: %v", operator, actionErr)
		} else {
			msg = fmt.Sprintf("用户 %s 重试编排", operator)
		}
	default:
		return
	}
	_ = h.Store.InsertCDCLog(&store.CDCSyncLog{
		TaskID: taskID, Level: level, Step: "manual", Message: msg, Operator: operator,
	})
}

func (h *CDCHandler) Action(status, approval string) gin.HandlerFunc {
	return func(c *gin.Context) {
		t, err := h.Store.GetCDCInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
		if err != nil || t == nil {
			c.JSON(http.StatusNotFound, gin.H{"message": "任务不存在"})
			return
		}
		if status == store.StatusRunning && !cdcApproved(t) {
			c.JSON(http.StatusBadRequest, gin.H{"message": "CDC 任务须审批通过后才能执行"})
			return
		}
		if t.Status == store.StatusReinitializing {
			c.JSON(http.StatusBadRequest, gin.H{"message": "任务正在重新初始化，请稍后再操作"})
			return
		}
		if strings.Contains(c.Request.URL.Path, "/retry") && cdc.ParseTaskExtra(t.Description)["error_step"] == "reinit" {
			h.retryReinit(c, t)
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
		defer cancel()
		op, _ := cdcRequestActor(c)
		action := "status"
		var actErr error
		switch {
		case status == store.StatusPaused:
			action = "pause"
			actErr = h.orch().Pause(ctx, t.ID)
		case strings.Contains(c.Request.URL.Path, "/retry"):
			action = "retry"
			actErr = h.orch().Apply(ctx, t.ID, "")
		case strings.Contains(c.Request.URL.Path, "/restart"):
			action = "restart"
			actErr = h.orch().Restart(ctx, t.ID)
		case status == store.StatusRunning:
			action = "resume"
			actErr = h.orch().Resume(ctx, t.ID)
		default:
			actErr = h.Store.UpdateCDCStatus(t.ID, status, approval)
		}
		h.logCDCUserAction(t.ID, action, op, actErr)
		if actErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "message": actErr.Error()})
			return
		}
		// 与 hub 对齐：有连接器 / 任务覆盖 / yaml 任一可解析出 Connect 地址则不再弹「改 yaml」。
		// 仅在真正无地址且本次操作为恢复类（status=running）时提示去配连接器。
		if status == store.StatusRunning {
			extra := cdc.ParseTaskExtra(t.Description)
			if base := h.orch().ResolveConnectBase(extra); base == "" {
				cdcConnectHint(c)
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

func (h *CDCHandler) Delete(c *gin.Context) {
	t, err := h.Store.GetCDCInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || t == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "任务不存在"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	if t.TaskType == "init" {
		_ = h.orch().CleanupTaskResources(ctx, t)
	}
	if err := h.Store.DeleteCDC(t.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type SlotHandler struct {
	Store  *store.DB
	Secret string

	collectMu   sync.Mutex
	collectJobs map[string]*slotCollectJob
}

type slotCollectJob struct {
	mu         sync.Mutex
	Status     string
	Total      int
	Done       int
	Percent    int
	Current    string
	Collected  int
	Skipped    int
	Message    string
	StartedAt  string
	FinishedAt string
}

type slotCollectView struct {
	Status     string `json:"status"`
	Total      int    `json:"total"`
	Done       int    `json:"done"`
	Percent    int    `json:"percent"`
	Current    string `json:"current"`
	Collected  int    `json:"collected"`
	Skipped    int    `json:"skipped"`
	Message    string `json:"message"`
	StartedAt  string `json:"started_at,omitempty"`
	FinishedAt string `json:"finished_at,omitempty"`
	Started    bool   `json:"started"`
}

func (j *slotCollectJob) view(started bool) slotCollectView {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.viewLocked(started)
}

func (j *slotCollectJob) viewLocked(started bool) slotCollectView {
	return slotCollectView{
		Status: j.Status, Total: j.Total, Done: j.Done, Percent: j.Percent,
		Current: j.Current, Collected: j.Collected, Skipped: j.Skipped, Message: j.Message,
		StartedAt: j.StartedAt, FinishedAt: j.FinishedAt, Started: started,
	}
}

func (h *SlotHandler) collectJob(tenantID string) *slotCollectJob {
	if strings.TrimSpace(tenantID) == "" {
		tenantID = "_"
	}
	h.collectMu.Lock()
	defer h.collectMu.Unlock()
	if h.collectJobs == nil {
		h.collectJobs = map[string]*slotCollectJob{}
	}
	job, ok := h.collectJobs[tenantID]
	if !ok {
		job = &slotCollectJob{Status: "idle", Message: "尚未采集"}
		h.collectJobs[tenantID] = job
	}
	return job
}

func (h *SlotHandler) List(c *gin.Context) {
	tid := effectiveListTenantID(h.Store, c)
	list, err := h.Store.ListSlotsInTenant(tid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	tasks, err := h.Store.ListCDCInTenant(tid, "", "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	insts, err := h.Store.ListInstancesInTenant(tid, "", "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	rows := cdc.AttachSlotCDC(list, tasks, insts)
	if rows == nil {
		rows = []cdc.SlotRow{}
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

func (h *SlotHandler) CollectProgress(c *gin.Context) {
	c.JSON(http.StatusOK, h.collectJob(effectiveListTenantID(h.Store, c)).view(false))
}

func (h *SlotHandler) Collect(c *gin.Context) {
	tid := effectiveListTenantID(h.Store, c)
	all, err := h.Store.ListInstancesInTenant(tid, "", "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	insts := make([]store.Instance, 0, len(all))
	for _, inst := range all {
		if store.NormalizeDBType(inst.DBType) == "postgres" {
			insts = append(insts, inst)
		}
	}
	job := h.collectJob(tid)
	job.mu.Lock()
	if job.Status == "running" {
		snap := job.viewLocked(false)
		job.mu.Unlock()
		c.JSON(http.StatusOK, snap)
		return
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	job.Status = "running"
	job.Total = len(insts)
	job.Done = 0
	job.Percent = 0
	job.Collected = 0
	job.Skipped = 0
	job.Current = ""
	job.StartedAt = now
	job.FinishedAt = ""
	if len(insts) == 0 {
		job.Status = "done"
		job.Percent = 100
		job.Message = "没有已登记的 PostgreSQL 实例"
		job.FinishedAt = now
		snap := job.viewLocked(true)
		job.mu.Unlock()
		c.JSON(http.StatusOK, snap)
		return
	}
	job.Message = fmt.Sprintf("准备采集 %d 个实例", len(insts))
	snap := job.viewLocked(true)
	job.mu.Unlock()
	tasks, _ := h.Store.ListCDCInTenant(tid, "", "")
	known, _ := h.Store.ListSlotsInTenant(tid)
	go h.runSlotCollect(job, append([]store.Instance(nil), insts...), tasks, known)
	c.JSON(http.StatusOK, snap)
}

func (h *SlotHandler) runSlotCollect(job *slotCollectJob, insts []store.Instance, tasks []store.CDCTask, known []store.SyncSlot) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	collected, skipped := 0, 0
	var notes []string
	for i := range insts {
		inst := &insts[i]
		if name := demoSlotName(inst.ID); name != "" {
			_ = h.Store.DeleteSlotsByName(name)
		}
		label := cdc.SlotDisplayHost(inst)
		if label == "" {
			label = inst.Name
		}
		target := fmt.Sprintf("%s:%d", label, inst.Port)
		job.mu.Lock()
		job.Current = target
		job.Message = fmt.Sprintf("正在采集 %s（%d/%d）", target, i+1, len(insts))
		job.mu.Unlock()

		slots, qerr := cdc.CollectInstanceSlots(ctx, inst, h.Secret, slotDBNames(inst, tasks, known))
		if qerr != nil {
			skipped++
			notes = append(notes, fmt.Sprintf("%s 采集失败：%s", target, qerr.Error()))
		} else if err := h.Store.ReplaceHostSlots(label, inst.Port, cdc.InstanceHostAliases(inst), slots); err != nil {
			skipped++
			notes = append(notes, fmt.Sprintf("%s 写入失败：%s", target, err.Error()))
		} else {
			collected += len(slots)
		}
		job.mu.Lock()
		job.Done = i + 1
		job.Collected = collected
		job.Skipped = skipped
		if job.Total > 0 {
			job.Percent = job.Done * 100 / job.Total
		}
		job.mu.Unlock()
	}
	msg := fmt.Sprintf("已采集 %d 个复制槽", collected)
	if skipped > 0 {
		msg += fmt.Sprintf("，跳过 %d 个实例", skipped)
	}
	if len(notes) > 0 {
		shown := notes
		if len(shown) > 3 {
			shown = append(append([]string{}, notes[:3]...), fmt.Sprintf("另有 %d 条失败", len(notes)-3))
		}
		msg += "：" + strings.Join(shown, "；")
	}
	job.mu.Lock()
	job.Status = "done"
	job.Done = len(insts)
	job.Percent = 100
	job.Current = ""
	job.Collected = collected
	job.Skipped = skipped
	job.Message = msg
	job.FinishedAt = time.Now().Format("2006-01-02 15:04:05")
	job.mu.Unlock()
}

func slotDBNames(inst *store.Instance, tasks []store.CDCTask, slots []store.SyncSlot) []string {
	if inst == nil {
		return nil
	}
	var names []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name != "" {
			names = append(names, name)
		}
	}
	for _, task := range tasks {
		if task.SourceID == inst.ID {
			add(task.SourceDB)
		}
	}
	for _, slot := range slots {
		if cdc.SlotHostMatches(slot.Host, slot.Port, inst) {
			add(slot.Database)
		}
	}
	return names
}

func demoSlotName(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if len(id) > 8 {
		id = id[:8]
	}
	return "demo_slot_" + id
}

func (h *SlotHandler) Delete(c *gin.Context) {
	ids := slotIDs(c)
	if len(ids) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "id 必填"})
		return
	}
	tid := effectiveListTenantID(h.Store, c)
	deleted := 0
	for _, id := range ids {
		ok, err := h.Store.GetSlotInTenant(id, tid)
		if err != nil || !ok {
			continue
		}
		if err := h.Store.DeleteSlot(id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		deleted++
	}
	if deleted == 0 {
		c.JSON(http.StatusNotFound, gin.H{"message": "复制槽不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "deleted": deleted})
}

func (h *SlotHandler) Restart(c *gin.Context) {
	ids := slotIDs(c)
	if len(ids) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "id 必填"})
		return
	}
	tid := effectiveListTenantID(h.Store, c)
	tasks, err := h.Store.ListCDCInTenant(tid, "", "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	insts, err := h.Store.ListInstancesInTenant(tid, "", "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Minute)
	defer cancel()
	restarted, skipped := 0, 0
	var notes []string
	for _, id := range ids {
		ok, err := h.Store.GetSlotInTenant(id, tid)
		if err != nil || !ok {
			skipped++
			continue
		}
		slot, err := h.Store.GetSyncSlot(id)
		if err != nil || slot == nil {
			skipped++
			continue
		}
		rows := cdc.AttachSlotCDC([]store.SyncSlot{*slot}, tasks, insts)
		if len(rows) == 0 || rows[0].ConnectorName == "" || rows[0].ConnectBase == "" {
			skipped++
			continue
		}
		if err := cdc.RestartDebeziumConnector(ctx, rows[0].ConnectBase, rows[0].ConnectorName); err != nil {
			notes = append(notes, rows[0].ConnectorName+"："+err.Error())
			continue
		}
		restarted++
	}
	msg := fmt.Sprintf("已重启 %d 个连接器", restarted)
	if skipped > 0 {
		msg += fmt.Sprintf("，跳过 %d 条未匹配连接器的记录", skipped)
	}
	if len(notes) > 0 {
		msg += "；失败：" + strings.Join(notes, "；")
	}
	c.JSON(http.StatusOK, gin.H{"ok": restarted > 0 || len(notes) == 0, "restarted": restarted, "skipped": skipped, "message": msg})
}

func slotIDs(c *gin.Context) []string {
	var req struct {
		ID  string   `json:"id"`
		IDs []string `json:"ids"`
	}
	_ = c.ShouldBindJSON(&req)
	if req.ID == "" {
		req.ID = c.Query("id")
	}
	seen := map[string]struct{}{}
	var out []string
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	add(req.ID)
	for _, id := range req.IDs {
		add(id)
	}
	return out
}

type ArchiveHandler struct {
	Store  *store.DB
	Secret string
	Engine *archive.Engine
}

func (h *ArchiveHandler) List(c *gin.Context) {
	list, err := h.Store.ListArchivesInTenant(effectiveListTenantID(h.Store, c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []store.ArchiveTask{}
	}
	out := make([]gin.H, 0, len(list))
	for i := range list {
		out = append(out, decorateArchive(&list[i]))
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (h *ArchiveHandler) Create(c *gin.Context) {
	var req struct {
		store.ArchiveTask
		Approver string `json:"approver"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	t := req.ArchiveTask
	strategy := store.ArchiveStrategy(&t)
	if t.Name == "" || t.SourceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "name/source_instance_id 必填"})
		return
	}
	if strategy != "custom_script" && strings.TrimSpace(t.TableName) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "table_name 必填"})
		return
	}
	if strings.TrimSpace(t.TableName) == "" {
		t.TableName = "script"
	}
	preferred := strings.TrimSpace(req.Approver)
	if preferred == "" {
		preferred = strings.TrimSpace(t.AssignedTo)
	}
	if preferred == "" {
		mig, _ := h.Store.GetGlobalArg("migration_approver_user_ids")
		if i := strings.Index(mig, ","); i >= 0 {
			mig = strings.TrimSpace(mig[:i])
		}
		preferred = strings.TrimSpace(mig)
	}
	approver, aerr := resolveTenantAssignee(h.Store, middleware.TenantID(c), preferred)
	if aerr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": aerr.Error()})
		return
	}
	t.AssignedTo = approver
	t.ApprovalStatus = store.StatusPending
	t.Status = store.StatusPending
	issue := &store.Issue{
		IssueType:   store.IssueTypeArchive,
		Type:        store.IssueTypeArchive,
		InstanceID:  t.SourceID,
		DBName:      t.SourceDB,
		SQLContent:  "archive [" + strategy + "] " + t.Name + " " + strings.TrimSpace(t.SourceDB+"."+t.TableName),
		Description: t.Condition,
		Submitter:   middleware.Username(c),
		AssignedTo:  approver,
	}
	stampTenantID(c, func(tid string) {
		if issue != nil {
			issue.TenantID = tid
		}
	})
	if err := h.Store.CreateIssue(issue); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	NotifyIssueCreatedAsync(h.Store, h.Secret, issue)
	t.IssueID = issue.ID
	if err := h.Store.CreateArchive(&t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": t})
}

func (h *ArchiveHandler) Stats(c *gin.Context) {
	st, err := h.Store.ArchiveStatsInTenant(effectiveListTenantID(h.Store, c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if runs, err := h.Store.ListArchiveRunsInTenant(effectiveListTenantID(h.Store, c), "", 20); err == nil && len(runs) > 0 {
		recent := make([]map[string]any, 0, len(runs))
		for _, r := range runs {
			recent = append(recent, map[string]any{
				"id": r.ID, "task_id": r.TaskID, "task_name": r.TaskName, "name": r.TaskName,
				"status": r.Status, "volume": r.Volume, "duration": r.Duration,
				"deleted_rows": r.DeletedRows, "rounds": r.Rounds, "message": r.Message,
				"created_at": r.CreatedAt, "started_at": r.StartedAt, "finished_at": r.FinishedAt,
			})
		}
		st["recent_runs"] = recent
	}
	c.JSON(http.StatusOK, gin.H{"data": st})
}

func archiveApproved(t *store.ArchiveTask) bool {
	if t == nil {
		return false
	}
	if t.ApprovalStatus == store.StatusPending || t.ApprovalStatus == store.StatusRejected || t.ApprovalStatus == store.StatusCanceled {
		return false
	}
	if t.ApprovalStatus == "" && t.IssueID == "" {
		return true
	}
	return t.ApprovalStatus == store.StatusApproved
}

type KafkaHandler struct {
	Store  *store.DB
	Secret string
}

func (h *KafkaHandler) List(c *gin.Context) {
	list, err := h.Store.ListKafkaInTenant(effectiveListTenantID(h.Store, c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []store.KafkaOrder{}
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *KafkaHandler) Create(c *gin.Context) {
	var req struct {
		store.KafkaOrder
		InstanceID string `json:"instance_id"`
		Domain     string `json:"domain"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	t := req.KafkaOrder
	instanceID := strings.TrimSpace(req.InstanceID)
	domain := strings.TrimSpace(req.Domain)
	if instanceID == "" || domain == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请选择 Kafka 地址（域名）"})
		return
	}
	inst, err := resolveInstanceInTenant(h.Store, c, instanceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if inst == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "实例不存在"})
		return
	}
	if !strings.EqualFold(strings.TrimSpace(inst.DBType), "kafka") {
		c.JSON(http.StatusBadRequest, gin.H{"message": "仅支持 db_type=kafka 的实例"})
		return
	}
	if err := ensureTicketDomainVisible(h.Store, c, inst, domain); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if strings.TrimSpace(t.Cluster) == "" {
		t.Cluster = domain
	}
	t.Submitter = middleware.Username(c)
	if t.TicketNo == "" {
		t.TicketNo = "K" + strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	}
	assigned, aerr := resolveTenantAssignee(h.Store, middleware.TenantID(c), t.AssignedTo)
	if aerr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": aerr.Error()})
		return
	}
	t.AssignedTo = assigned
	if err := h.Store.CreateKafka(&t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if tid := middleware.TenantID(c); tid != "" {
		_ = h.Store.AssignTenantID("tbl_kafka_orders", t.ID, tid)
	}
	issue := &store.Issue{
		ID: t.ID, TicketNo: t.TicketNo, IssueType: store.IssueTypeKafka,
		InstanceID: instanceID, Domain: domain,
		SQLContent:  fmt.Sprintf("%s %s/%s", t.Action, t.Cluster, t.Topic),
		Description: t.Description, Submitter: t.Submitter, AssignedTo: t.AssignedTo,
	}
	stampTenantID(c, func(tid string) {
		if issue != nil {
			issue.TenantID = tid
		}
	})
	_ = h.Store.CreateIssue(issue)
	NotifyIssueCreatedAsync(h.Store, h.Secret, issue)
	c.JSON(http.StatusOK, gin.H{"data": t})
}

func (h *KafkaHandler) Instances(c *gin.Context) {
	list, err := h.Store.ListInstancesInTenant(effectiveListTenantID(h.Store, c), "kafka", "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

type InsightHandler struct {
	Store  *store.DB
	Secret string
}

func (h *InsightHandler) List(c *gin.Context) {
	list, err := h.Store.ListInsight(c.Query("kind"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []store.InsightJob{}
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *InsightHandler) Create(c *gin.Context) {
	var t store.InsightJob
	if err := c.ShouldBindJSON(&t); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if t.Kind == "" {
		t.Kind = "sql-insight"
	}
	t.Status = store.StatusInit
	t.Summary = "独立部署未接入 AWR/活动快照采集，仅保存分析任务。"
	if err := h.Store.CreateInsight(&t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": t})
}

type SessionHandler struct {
	Store  *store.DB
	Secret string
}

type MultilangHandler struct{ Store *store.DB }

func (h *MultilangHandler) List(c *gin.Context) {
	list, err := h.Store.ListMultilang()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []store.MultilangTask{}
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *MultilangHandler) Create(c *gin.Context) {
	var t store.MultilangTask
	if err := c.ShouldBindJSON(&t); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if err := h.Store.CreateMultilang(&t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": t})
}

type ExternalHandler struct{ Cfg *config.Config }

func (h *ExternalHandler) Dolphin(c *gin.Context) {
	if strings.TrimSpace(h.Cfg.External.DolphinSchedulerURL) == "" {
		notConfigured(c, "DolphinScheduler")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "url": h.Cfg.External.DolphinSchedulerURL})
}

func (h *ExternalHandler) StarRocks(c *gin.Context) {
	if strings.TrimSpace(h.Cfg.External.StarRocksFEURL) == "" {
		notConfigured(c, "StarRocks FE")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "url": h.Cfg.External.StarRocksFEURL})
}

type DCLHandler struct {
	Store  *store.DB
	Secret string
}

func (h *DCLHandler) Submit(c *gin.Context) {
	var req ticketSubmitReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	req.Type = store.IssueTypeDCL
	th := &TicketHandler{Store: h.Store}
	c.Request.Header.Set("Content-Type", "application/json")
	// reuse submit logic via constructing issue
	username := middleware.Username(c)
	ok, _, msg, err := th.canActOnTarget(middleware.TenantID(c), req.InstanceID, req.DBName, username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"message": msg})
		return
	}
	reviewer, aerr := resolveTenantAssignee(h.Store, middleware.TenantID(c), req.reviewer())
	if aerr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": aerr.Error()})
		return
	}
	domain := strings.TrimSpace(req.Domain)
	if domain != "" && strings.TrimSpace(req.InstanceID) != "" {
		inst, ierr := h.Store.GetInstance(req.InstanceID)
		if ierr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": ierr.Error()})
			return
		}
		if inst == nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "数据源域名不属于所选实例登记的域名"})
			return
		}
		if err := ensureTicketDomainVisible(h.Store, c, inst, domain); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		if strings.TrimSpace(req.DBName) != "" {
			for _, name := range strings.Split(req.DBName, ",") {
				name = strings.TrimSpace(name)
				if name == "" {
					continue
				}
				dbEnt, derr := h.Store.FindDatabase(req.InstanceID, name)
				if derr != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"message": derr.Error()})
					return
				}
				if dbEnt != nil && len(dbEnt.Domains) > 0 && !databaseContainsDomain(dbEnt, domain) {
					c.JSON(http.StatusBadRequest, gin.H{"message": "所选数据库未绑定该域名: " + name})
					return
				}
			}
		}
	}
	issue := &store.Issue{
		IssueType: store.IssueTypeDCL, InstanceID: req.InstanceID, Domain: domain, DBName: req.DBName,
		SQLContent: req.SQLContent, Description: req.Description, Status: store.StatusPending,
		Submitter: username, AssignedTo: reviewer,
	}
	stampTenantID(c, func(tid string) {
		if issue != nil {
			issue.TenantID = tid
		}
	})
	if err := h.Store.CreateIssue(issue); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	NotifyIssueCreatedAsync(h.Store, h.Secret, issue)
	c.JSON(http.StatusOK, gin.H{"data": issue})
}
