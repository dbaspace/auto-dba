package handler

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/cdc"
	"auto-dba/internal/store"
)

const (
	slotRestartLogMax     = 500
	slotRestartJobTimeout = 15 * time.Minute
)

type slotRestartJob struct {
	mu         sync.Mutex
	TenantID   string
	Status     string
	Total      int
	Done       int
	Percent    int
	Current    string
	Succeeded  int
	Failed     int
	Skipped    int
	Message    string
	StartedAt  string
	FinishedAt string
	Logs       []string
}

type slotRestartView struct {
	Status     string   `json:"status"`
	Total      int      `json:"total"`
	Done       int      `json:"done"`
	Percent    int      `json:"percent"`
	Current    string   `json:"current"`
	Succeeded  int      `json:"succeeded"`
	Failed     int      `json:"failed"`
	Skipped    int      `json:"skipped"`
	Message    string   `json:"message"`
	StartedAt  string   `json:"started_at,omitempty"`
	FinishedAt string   `json:"finished_at,omitempty"`
	Logs       []string `json:"logs,omitempty"`
	Started    bool     `json:"started"`
}

// slotRestartItem 优先用 host+port+slot_name 稳定键；id 仅兼容旧前端。
type slotRestartItem struct {
	ID       string `json:"id"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	SlotName string `json:"slot_name"`
	Database string `json:"database"`
}

func (j *slotRestartJob) view(started bool) slotRestartView {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.viewLocked(started)
}

func (j *slotRestartJob) viewLocked(started bool) slotRestartView {
	logs := append([]string(nil), j.Logs...)
	return slotRestartView{
		Status: j.Status, Total: j.Total, Done: j.Done, Percent: j.Percent,
		Current: j.Current, Succeeded: j.Succeeded, Failed: j.Failed, Skipped: j.Skipped,
		Message: j.Message, StartedAt: j.StartedAt, FinishedAt: j.FinishedAt,
		Logs: logs, Started: started,
	}
}

func (h *SlotHandler) restartJob(tenantID string) *slotRestartJob {
	if strings.TrimSpace(tenantID) == "" {
		tenantID = "_"
	}
	h.restartMu.Lock()
	defer h.restartMu.Unlock()
	if h.restartJobs == nil {
		h.restartJobs = map[string]*slotRestartJob{}
	}
	job, ok := h.restartJobs[tenantID]
	if !ok {
		job = &slotRestartJob{Status: "idle", Message: "尚未重启"}
		h.restartJobs[tenantID] = job
	}
	return job
}

// RestartProgress GET /sync-slots/restart
func (h *SlotHandler) RestartProgress(c *gin.Context) {
	c.JSON(http.StatusOK, h.restartJob(effectiveListTenantID(h.Store, c)).view(false))
}

// Restart POST /sync-slots/restart：异步批量重启（heartbeat + tasks/0），立即返回进度。
func (h *SlotHandler) Restart(c *gin.Context) {
	items := parseSlotRestartItems(c)
	if len(items) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请提交 items（host/port/slot_name）或 ids"})
		return
	}
	tid := effectiveListTenantID(h.Store, c)
	if cj := h.collectJob(tid); cj != nil {
		cj.mu.Lock()
		collectBusy := cj.Status == "running"
		cj.mu.Unlock()
		if collectBusy {
			c.JSON(http.StatusConflict, gin.H{"message": "采集进行中，请等待完成后再重启"})
			return
		}
	}
	job := h.restartJob(tid)

	job.mu.Lock()
	if job.Status == "running" {
		snap := job.viewLocked(false)
		job.mu.Unlock()
		snap.Message = "重启进行中"
		c.JSON(http.StatusOK, snap)
		return
	}
	now := time.Now().Format(time.RFC3339)
	job.TenantID = tid
	job.Status = "running"
	job.Total = len(items)
	job.Done = 0
	job.Percent = 0
	job.Current = ""
	job.Succeeded = 0
	job.Failed = 0
	job.Skipped = 0
	job.Message = fmt.Sprintf("已开始批量重启（%d 条）", len(items))
	job.StartedAt = now
	job.FinishedAt = ""
	job.Logs = nil
	startLine := formatSlotOpLine("重启", fmt.Sprintf("开始批量重启，共 %d 条", len(items)))
	job.appendLogLocked(startLine)
	snap := job.viewLocked(true)
	job.mu.Unlock()
	h.persistSlotOpLog(tid, store.SyncSlotOpRestart, startLine)

	itemCopy := append([]slotRestartItem(nil), items...)
	go h.runSlotRestart(context.Background(), tid, job, itemCopy)
	c.JSON(http.StatusOK, snap)
}

func parseSlotRestartItems(c *gin.Context) []slotRestartItem {
	var req struct {
		ID    string            `json:"id"`
		IDs   []string          `json:"ids"`
		Items []slotRestartItem `json:"items"`
	}
	_ = c.ShouldBindJSON(&req)
	seen := map[string]struct{}{}
	var out []slotRestartItem
	add := func(it slotRestartItem) {
		it.ID = strings.TrimSpace(it.ID)
		it.Host = strings.TrimSpace(it.Host)
		it.SlotName = strings.TrimSpace(it.SlotName)
		it.Database = strings.TrimSpace(it.Database)
		key := ""
		if it.Host != "" && it.Port > 0 && it.SlotName != "" {
			key = fmt.Sprintf("k:%s|%d|%s", strings.ToLower(it.Host), it.Port, strings.ToLower(it.SlotName))
		} else if it.ID != "" {
			key = "id:" + it.ID
		} else {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, it)
	}
	for _, it := range req.Items {
		add(it)
	}
	add(slotRestartItem{ID: req.ID})
	for _, id := range req.IDs {
		add(slotRestartItem{ID: id})
	}
	return out
}

func (j *slotRestartJob) appendLogLocked(line string) {
	j.Logs = append(j.Logs, line)
	if len(j.Logs) > slotRestartLogMax {
		j.Logs = j.Logs[len(j.Logs)-slotRestartLogMax:]
	}
}

func (h *SlotHandler) runSlotRestart(parent context.Context, tid string, job *slotRestartJob, items []slotRestartItem) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, slotRestartJobTimeout)
	defer cancel()

	tasks, err := h.Store.ListCDCInTenant(tid, "", "")
	if err != nil {
		h.finishRestartJob(tid, job, fmt.Sprintf("加载 CDC 任务失败：%v", err))
		return
	}
	insts, err := h.Store.ListInstancesInTenant(tid, "", "")
	if err != nil {
		h.finishRestartJob(tid, job, fmt.Sprintf("加载实例失败：%v", err))
		return
	}

	for i := range items {
		if i > 0 {
			time.Sleep(time.Duration(10+rand.Intn(41)) * time.Millisecond)
		}
		select {
		case <-ctx.Done():
			h.finishRestartJob(tid, job, "重启超时已中止")
			return
		default:
		}

		label, result, detail := h.restartOneSlot(ctx, tid, items[i], tasks, insts)
		job.mu.Lock()
		job.Done = i + 1
		if job.Total > 0 {
			job.Percent = job.Done * 100 / job.Total
		}
		job.Current = label
		switch result {
		case "ok":
			job.Succeeded++
		case "skip":
			job.Skipped++
		default:
			job.Failed++
		}
		job.Message = fmt.Sprintf("重启中 %d/%d：%s", job.Done, job.Total, label)
		itemLine := formatSlotOpLine("重启", label+" "+detail)
		job.appendLogLocked(itemLine)
		job.mu.Unlock()
		h.persistSlotOpLog(tid, store.SyncSlotOpRestart, itemLine)
	}

	job.mu.Lock()
	msg := fmt.Sprintf("重启完成：成功 %d，失败 %d，跳过 %d（共 %d）",
		job.Succeeded, job.Failed, job.Skipped, job.Total)
	if job.Failed == job.Total && job.Total > 0 {
		msg = fmt.Sprintf("全部失败：共 %d 条", job.Total)
	} else if job.Failed > 0 || job.Skipped > 0 {
		msg = fmt.Sprintf("部分完成：成功 %d，失败 %d，跳过 %d", job.Succeeded, job.Failed, job.Skipped)
	}
	job.Status = "done"
	job.Percent = 100
	job.Current = ""
	job.Message = msg
	job.FinishedAt = time.Now().Format(time.RFC3339)
	doneLine := formatSlotOpLine("重启", msg)
	job.appendLogLocked(doneLine)
	job.mu.Unlock()
	h.persistSlotOpLog(tid, store.SyncSlotOpRestart, doneLine)
}

func (h *SlotHandler) finishRestartJob(tid string, job *slotRestartJob, msg string) {
	job.mu.Lock()
	job.Status = "done"
	job.Message = msg
	job.FinishedAt = time.Now().Format(time.RFC3339)
	line := formatSlotOpLine("重启", msg)
	job.appendLogLocked(line)
	job.mu.Unlock()
	h.persistSlotOpLog(tid, store.SyncSlotOpRestart, line)
}

// restartOneSlot 返回展示名、结果(ok|skip|fail)、日志详情。
func (h *SlotHandler) restartOneSlot(ctx context.Context, tid string, item slotRestartItem, tasks []store.CDCTask, insts []store.Instance) (label, result, detail string) {
	slot, how, errMsg := h.resolveRestartSlot(tid, item)
	if slot == nil {
		label = restartItemLabel(item)
		if errMsg == "" {
			errMsg = "跳过：槽记录不存在"
		}
		return label, "skip", errMsg
	}
	_ = how
	rows := cdc.AttachSlotCDC([]store.SyncSlot{*slot}, tasks, insts)
	taskName := slot.SlotName
	hostDB := fmt.Sprintf("%s/%s", slot.Host, slot.Database)
	if len(rows) > 0 && rows[0].TaskName != "" {
		taskName = rows[0].TaskName
	}
	slotName := strings.TrimSpace(slot.SlotName)
	label = fmt.Sprintf("%s slot=%s (%s)", taskName, slotName, hostDB)
	if len(rows) == 0 || rows[0].ConnectorName == "" || rows[0].ConnectBase == "" {
		return label, "skip", fmt.Sprintf("跳过：未匹配到槽 %s、库 %s 对应的 connector", slotName, slot.Database)
	}
	if strings.HasSuffix(strings.ToLower(rows[0].ConnectorName), "-add") {
		return label, "skip", fmt.Sprintf("跳过：槽 %s、库 %s 不能用加表任务名 %s 当 connector", slotName, slot.Database, rows[0].ConnectorName)
	}
	inst := cdc.FindInstanceForSlot(*slot, insts)
	if inst == nil {
		return label, "fail", "失败：未找到匹配实例凭证"
	}
	connName := rows[0].ConnectorName
	if err := cdc.RestartSyncSlotItem(ctx, inst, h.Secret, slot.Database, rows[0].ConnectBase, connName); err != nil {
		return label, "fail", "失败：" + err.Error()
	}
	return label, "ok", fmt.Sprintf("成功：heartbeat + %s/tasks/0", connName)
}

func restartItemLabel(item slotRestartItem) string {
	if item.Host != "" && item.SlotName != "" {
		return fmt.Sprintf("%s@%s:%d", item.SlotName, item.Host, item.Port)
	}
	if item.ID != "" {
		return item.ID
	}
	return "?"
}

// resolveRestartSlot 优先稳定键 host+port+slot_name，其次 id；区分「不存在」与「租户不可见」。
func (h *SlotHandler) resolveRestartSlot(tid string, item slotRestartItem) (slot *store.SyncSlot, via string, errMsg string) {
	if item.Host != "" && item.Port > 0 && item.SlotName != "" {
		s, err := h.Store.GetSyncSlotByKey(item.Host, item.Port, item.SlotName)
		if err != nil {
			return nil, "", "跳过：读取槽失败"
		}
		if s == nil {
			return nil, "", "跳过：槽记录不存在（host/port/slot_name 未命中，请刷新列表后重试）"
		}
		ok, err := h.Store.GetSlotInTenant(s.ID, tid)
		if err != nil || !ok {
			return nil, "", "跳过：槽记录不在当前租户"
		}
		return s, "key", ""
	}
	id := strings.TrimSpace(item.ID)
	if id == "" {
		return nil, "", "跳过：缺少 host/port/slot_name 或 id"
	}
	raw, err := h.Store.GetSyncSlot(id)
	if err != nil {
		return nil, "", "跳过：读取槽失败"
	}
	if raw == nil {
		return nil, "", "跳过：槽记录已失效（可能刚完成采集换了 id，请刷新后按槽名重选）"
	}
	ok, err := h.Store.GetSlotInTenant(id, tid)
	if err != nil || !ok {
		return nil, "", "跳过：槽记录不在当前租户"
	}
	return raw, "id", ""
}
