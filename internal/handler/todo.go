package handler

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

// TodoHandler 待办跟进。
type TodoHandler struct {
	Store *store.DB
}

// ImageIDs 为 nil（未传）时编辑不改图片；传 [] 表示清空。
type todoBody struct {
	Title       string        `json:"title"`
	Source      string        `json:"source"`
	SourceRef   string        `json:"source_ref"`
	Owner       string        `json:"owner"`
	Followers   []string      `json:"followers"`
	PlanAt      string        `json:"plan_at"`
	Description string        `json:"description"`
	ImageIDs    *[]string     `json:"image_ids"`
	Subtasks    []subtaskBody `json:"subtasks"` // 仅新建时使用
}

type subtaskBody struct {
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Owner       string    `json:"owner"`
	PlanAt      string    `json:"plan_at"`
	ImageIDs    *[]string `json:"image_ids"`
}

func imageIDs(p *[]string) []string {
	if p == nil {
		return nil
	}
	return *p
}

var subtaskStatusLabel = map[string]string{
	store.SubtaskPending: "待处理",
	store.SubtaskDoing:   "进行中",
	store.SubtaskDone:    "已完成",
}

func (h *TodoHandler) hasURN(c *gin.Context, urn string) bool {
	u, err := h.Store.GetUserByUsername(middleware.Username(c))
	if err != nil || u == nil {
		return false
	}
	ok, _ := h.Store.UserHasURNInTenant(u.ID, middleware.TenantID(c), urn)
	return ok
}

// canFollow 负责人 / 跟进人 / 发起人可跟进；有编辑权限者可跟进任意事项。
func (h *TodoHandler) canFollow(c *gin.Context, t *store.Todo) bool {
	return store.TodoRelated(*t, middleware.Username(c)) || h.hasURN(c, store.URNTodoEdit)
}

func (h *TodoHandler) load(c *gin.Context) *store.Todo {
	t, err := h.Store.GetTodoInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return nil
	}
	if t == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "事项不存在"})
		return nil
	}
	return t
}

func (h *TodoHandler) log(t *store.Todo, action, from, content, operator string) {
	_ = h.logSubtask(t, "", action, from, content, operator, nil)
}

// logSubtask 写跟进记录；images 非空时挂到这条记录上（进展 / 闭环截图）。
func (h *TodoHandler) logSubtask(t *store.Todo, subtaskID, action, from, content, operator string, images []string) error {
	l := &store.TodoLog{
		TodoID: t.ID, SubtaskID: subtaskID, Action: action, FromStatus: from, ToStatus: t.Status,
		Content: strings.TrimSpace(content), Operator: operator,
	}
	if err := h.Store.AddTodoLog(l); err != nil {
		return err
	}
	if len(images) == 0 {
		return nil
	}
	return h.Store.SyncTodoImages(t.ID, store.TodoImageScopeLog, l.ID, images, operator)
}

// refreshSummary 按子任务重算事项的最新进展 / 下一步动作并保存。
func (h *TodoHandler) refreshSummary(t *store.Todo) error {
	subs, err := h.Store.ListTodoSubtasks(t.ID)
	if err != nil {
		return err
	}
	store.SummarizeSubtasks(t, subs)
	return h.Store.SaveTodo(t)
}

// List GET /todos?tab=&keyword=&source=&status=&owner=
func (h *TodoHandler) List(c *gin.Context) {
	tab := c.DefaultQuery("tab", store.TodoTabMine)
	valid := false
	for _, t := range store.TodoTabs {
		if t == tab {
			valid = true
		}
	}
	if !valid {
		c.JSON(http.StatusBadRequest, gin.H{"message": "未知页签: " + tab})
		return
	}
	list, err := h.Store.ListTodosInTenant(effectiveListTenantID(h.Store, c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	user := middleware.Username(c)
	now := time.Now()
	items := store.FilterTodos(list, store.TodoFilter{
		Tab:     tab,
		Keyword: c.Query("keyword"),
		Source:  strings.TrimSpace(c.Query("source")),
		Status:  strings.TrimSpace(c.Query("status")),
		Owner:   strings.TrimSpace(c.Query("owner")),
	}, user, now)
	// 子任务表未建时仍返回列表，仅缺进度统计
	if stats, err := h.Store.TodoSubtaskStats(); err == nil {
		for i := range items {
			st := stats[items[i].ID]
			items[i].SubtaskTotal, items[i].SubtaskDone = st[0], st[1]
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "counts": store.TodoTabCounts(list, user, now)})
}

// Get GET /todos/:id — 详情 + 跟进记录 + 当前用户可执行的操作。
func (h *TodoHandler) Get(c *gin.Context) {
	t := h.load(c)
	if t == nil {
		return
	}
	store.DecorateTodo(t, time.Now())
	logs, err := h.Store.ListTodoLogs(t.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	subs, err := h.Store.ListTodoSubtasks(t.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	t.SubtaskTotal, t.SubtaskDone = store.CountSubtasks(subs)
	images, err := h.Store.ListTodoImages(t.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"todo":     t,
		"logs":     logs,
		"subtasks": subs,
		"images":   images,
		"can": gin.H{
			"follow": h.hasURN(c, store.URNTodoFollow) && h.canFollow(c, t),
			"edit":   h.hasURN(c, store.URNTodoEdit),
			"delete": h.hasURN(c, store.URNTodoDelete),
		},
	}})
}

func (h *TodoHandler) Create(c *gin.Context) {
	var b todoBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	user := middleware.Username(c)
	t := &store.Todo{
		Title: b.Title, Source: b.Source, SourceRef: b.SourceRef, Owner: b.Owner, Followers: b.Followers,
		PlanAt: b.PlanAt, Description: strings.TrimSpace(b.Description), Status: store.TodoStatusPending, CreatedBy: user,
	}
	if err := store.NormalizeTodo(t); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if err := h.Store.CheckTodoImagesBindable(imageIDs(b.ImageIDs), user); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	subs := make([]*store.TodoSubtask, 0, len(b.Subtasks))
	subImages := make([][]string, 0, len(b.Subtasks))
	for _, sb := range b.Subtasks {
		if strings.TrimSpace(sb.Title) == "" && strings.TrimSpace(sb.Description) == "" && strings.TrimSpace(sb.PlanAt) == "" {
			continue
		}
		s := &store.TodoSubtask{Title: sb.Title, Description: sb.Description, Owner: sb.Owner, PlanAt: sb.PlanAt, CreatedBy: user}
		if err := store.NormalizeSubtask(s, t.Owner); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		if err := h.Store.CheckTodoImagesBindable(imageIDs(sb.ImageIDs), user); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "子任务「" + s.Title + "」：" + err.Error()})
			return
		}
		subs = append(subs, s)
		subImages = append(subImages, imageIDs(sb.ImageIDs))
	}
	stampTenantID(c, func(tid string) { t.TenantID = tid })
	if err := h.Store.CreateTodo(t); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if err := h.Store.SyncTodoImages(t.ID, store.TodoImageScopeTodo, "", imageIDs(b.ImageIDs), user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	msg := "创建事项，负责人 " + t.Owner
	if len(subs) > 0 {
		for i, s := range subs {
			s.TodoID = t.ID
			if err := h.Store.CreateTodoSubtask(s, t.Owner); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
				return
			}
			if err := h.Store.SyncTodoImages(t.ID, store.TodoImageScopeSubtask, s.ID, subImages[i], user); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
				return
			}
		}
		if err := h.refreshSummary(t); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		msg += fmt.Sprintf("，子任务 %d 个", len(subs))
	}
	h.log(t, "create", "", msg, user)
	store.DecorateTodo(t, time.Now())
	c.JSON(http.StatusOK, gin.H{"data": t})
}

// Update PUT /todos/:id — 编辑基本信息（不改状态；转交走 handover）。
func (h *TodoHandler) Update(c *gin.Context) {
	t := h.load(c)
	if t == nil {
		return
	}
	var b todoBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	var changed []string
	if strings.TrimSpace(b.Title) != t.Title {
		changed = append(changed, "名称 "+t.Title+" → "+strings.TrimSpace(b.Title))
	}
	if strings.TrimSpace(b.Owner) != t.Owner {
		changed = append(changed, "负责人 "+t.Owner+" → "+strings.TrimSpace(b.Owner))
	}
	if strings.TrimSpace(b.PlanAt) != "" {
		if pt, err := store.ParseTodoTime(b.PlanAt); err == nil && pt.UTC().Format(time.RFC3339) != t.PlanAt {
			changed = append(changed, "计划完成时间调整")
		}
	}
	user := middleware.Username(c)
	t.Title, t.Source, t.SourceRef, t.Owner, t.Followers = b.Title, b.Source, b.SourceRef, b.Owner, b.Followers
	t.PlanAt, t.Description = b.PlanAt, strings.TrimSpace(b.Description)
	if err := store.NormalizeTodo(t); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if b.ImageIDs != nil {
		before, err := h.Store.ListTodoImageIDs(t.ID, store.TodoImageScopeTodo, "")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		if err := h.Store.SyncTodoImages(t.ID, store.TodoImageScopeTodo, "", *b.ImageIDs, user); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		if !sameIDSet(before, *b.ImageIDs) {
			changed = append(changed, "更新图片")
		}
	}
	if err := h.Store.SaveTodo(t); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	msg := "编辑事项"
	if len(changed) > 0 {
		msg += "：" + strings.Join(changed, "；")
	}
	h.log(t, "edit", t.Status, msg, user)
	store.DecorateTodo(t, time.Now())
	c.JSON(http.StatusOK, gin.H{"data": t})
}

func sameIDSet(a, b []string) bool {
	na, _ := store.NormalizeTodoImageIDs(a)
	nb, err := store.NormalizeTodoImageIDs(b)
	if err != nil || len(na) != len(nb) {
		return false
	}
	set := map[string]bool{}
	for _, id := range na {
		set[id] = true
	}
	for _, id := range nb {
		if !set[id] {
			return false
		}
	}
	return true
}

// Handover POST /todos/:id/handover {owner, note}
func (h *TodoHandler) Handover(c *gin.Context) {
	var b struct {
		Owner string `json:"owner"`
		Note  string `json:"note"`
	}
	h.transition(c, &b, "handover", nil, func(t *store.Todo) (string, error) {
		old := t.Owner
		if err := store.HandoverTodo(t, b.Owner); err != nil {
			return "", err
		}
		msg := "转交：" + old + " → " + t.Owner
		if n := strings.TrimSpace(b.Note); n != "" {
			msg += "\n" + n
		}
		return msg, nil
	})
}

// Close POST /todos/:id/close {note, image_ids}
func (h *TodoHandler) Close(c *gin.Context) {
	var b struct {
		Note     string   `json:"note"`
		ImageIDs []string `json:"image_ids"`
	}
	h.transition(c, &b, "close", &b.ImageIDs, func(t *store.Todo) (string, error) {
		if err := h.Store.CheckTodoImagesBindable(b.ImageIDs, middleware.Username(c)); err != nil {
			return "", err
		}
		subs, err := h.Store.ListTodoSubtasks(t.ID)
		if err != nil {
			return "", err
		}
		total, done := store.CountSubtasks(subs)
		if err := store.CloseTodo(t, b.Note, total-done, time.Now()); err != nil {
			return "", err
		}
		return strings.TrimSpace("闭环 " + strings.TrimSpace(b.Note)), nil
	})
}

// Reopen POST /todos/:id/reopen {note}
func (h *TodoHandler) Reopen(c *gin.Context) {
	var b struct {
		Note string `json:"note"`
	}
	h.transition(c, &b, "reopen", nil, func(t *store.Todo) (string, error) {
		if err := store.ReopenTodo(t); err != nil {
			return "", err
		}
		return strings.TrimSpace("重新打开 " + strings.TrimSpace(b.Note)), nil
	})
}

// transition 跟进类操作的公共流程：加载 → 校验关系 → 绑定参数 → 变更 → 保存 → 记录。
// logImages 指向请求体中的图片 ID，记录写入后挂到该条跟进记录上。
func (h *TodoHandler) transition(c *gin.Context, body any, action string, logImages *[]string, apply func(t *store.Todo) (string, error)) {
	t := h.load(c)
	if t == nil {
		return
	}
	if !h.canFollow(c, t) {
		c.JSON(http.StatusForbidden, gin.H{"message": "仅负责人、跟进人、发起人或有编辑权限者可操作"})
		return
	}
	if err := c.ShouldBindJSON(body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	from := t.Status
	msg, err := apply(t)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if err := h.Store.SaveTodo(t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if err := h.logSubtask(t, "", action, from, msg, middleware.Username(c), imageIDs(logImages)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "操作已保存，但图片保存失败：" + err.Error()})
		return
	}
	store.DecorateTodo(t, time.Now())
	c.JSON(http.StatusOK, gin.H{"data": t})
}

// subtaskOp 子任务操作的公共流程：加载事项 → 校验关系与未闭环 → 绑定参数 → 执行 → 重算汇总 → 记录。
// apply 返回日志内容与关联的子任务 ID；logImages 同 transition。
func (h *TodoHandler) subtaskOp(c *gin.Context, body any, action string, logImages *[]string, apply func(t *store.Todo) (string, string, error)) {
	t := h.load(c)
	if t == nil {
		return
	}
	if !h.canFollow(c, t) {
		c.JSON(http.StatusForbidden, gin.H{"message": "仅负责人、跟进人、发起人或有编辑权限者可操作"})
		return
	}
	if t.Status == store.TodoStatusClosed {
		c.JSON(http.StatusBadRequest, gin.H{"message": "事项已闭环，请先重新打开"})
		return
	}
	if body != nil {
		if err := c.ShouldBindJSON(body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
	}
	from := t.Status
	msg, sid, err := apply(t)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if err := h.refreshSummary(t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if err := h.logSubtask(t, sid, action, from, msg, middleware.Username(c), imageIDs(logImages)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "操作已保存，但图片保存失败：" + err.Error()})
		return
	}
	now := time.Now()
	beforeClose := t.Status
	autoClosed, err := store.MaybeAutoCloseTodo(t, now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if autoClosed {
		if err := h.Store.SaveTodo(t); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		h.log(t, "close", beforeClose, "闭环 子任务已全部完成，系统自动闭环", middleware.Username(c))
	}
	store.DecorateTodo(t, now)
	c.JSON(http.StatusOK, gin.H{"data": t})
}

// loadSubtask 读取路径中的子任务；不存在时返回错误。
func (h *TodoHandler) loadSubtask(c *gin.Context, t *store.Todo) (*store.TodoSubtask, error) {
	s, err := h.Store.GetTodoSubtask(t.ID, c.Param("sid"))
	if err != nil {
		return nil, err
	}
	if s == nil {
		return nil, errors.New("子任务不存在")
	}
	return s, nil
}

// AddSubtask POST /todos/:id/subtasks {title, description, owner, plan_at, image_ids}
func (h *TodoHandler) AddSubtask(c *gin.Context) {
	var b subtaskBody
	h.subtaskOp(c, &b, "subtask_add", nil, func(t *store.Todo) (string, string, error) {
		user := middleware.Username(c)
		if err := h.Store.CheckTodoImagesBindable(imageIDs(b.ImageIDs), user); err != nil {
			return "", "", err
		}
		s := &store.TodoSubtask{
			TodoID: t.ID, Title: b.Title, Description: b.Description, Owner: b.Owner, PlanAt: b.PlanAt,
			CreatedBy: user,
		}
		if err := h.Store.CreateTodoSubtask(s, t.Owner); err != nil {
			return "", "", err
		}
		if err := h.Store.SyncTodoImages(t.ID, store.TodoImageScopeSubtask, s.ID, imageIDs(b.ImageIDs), user); err != nil {
			return "", "", err
		}
		return "新增子任务：" + s.Title, s.ID, nil
	})
}

// UpdateSubtask PUT /todos/:id/subtasks/:sid {title, description, owner, plan_at, image_ids}
func (h *TodoHandler) UpdateSubtask(c *gin.Context) {
	var b subtaskBody
	h.subtaskOp(c, &b, "subtask_edit", nil, func(t *store.Todo) (string, string, error) {
		user := middleware.Username(c)
		s, err := h.loadSubtask(c, t)
		if err != nil {
			return "", "", err
		}
		old := *s
		s.Title, s.Description, s.Owner, s.PlanAt = b.Title, b.Description, b.Owner, b.PlanAt
		if err := store.NormalizeSubtask(s, t.Owner); err != nil {
			return "", "", err
		}
		var changed []string
		if b.ImageIDs != nil {
			before, err := h.Store.ListTodoImageIDs(t.ID, store.TodoImageScopeSubtask, s.ID)
			if err != nil {
				return "", "", err
			}
			if err := h.Store.SyncTodoImages(t.ID, store.TodoImageScopeSubtask, s.ID, *b.ImageIDs, user); err != nil {
				return "", "", err
			}
			if !sameIDSet(before, *b.ImageIDs) {
				changed = append(changed, "更新图片")
			}
		}
		if err := h.Store.SaveTodoSubtask(s, t.Owner); err != nil {
			return "", "", err
		}
		if s.Title != old.Title {
			changed = append(changed, "名称 "+old.Title+" → "+s.Title)
		}
		if s.Description != old.Description {
			changed = append(changed, "描述调整")
		}
		if s.Owner != old.Owner {
			changed = append(changed, "负责人 "+old.Owner+" → "+s.Owner)
		}
		if s.PlanAt != old.PlanAt {
			changed = append(changed, "计划完成时间调整")
		}
		msg := "【" + s.Title + "】编辑子任务"
		if len(changed) > 0 {
			msg += "：" + strings.Join(changed, "；")
		}
		return msg, s.ID, nil
	})
}

// SubtaskProgress POST /todos/:id/subtasks/:sid/progress {status, progress, image_ids}
func (h *TodoHandler) SubtaskProgress(c *gin.Context) {
	var b struct {
		Status   string   `json:"status"`
		Progress string   `json:"progress"`
		ImageIDs []string `json:"image_ids"`
	}
	h.subtaskOp(c, &b, "subtask_progress", &b.ImageIDs, func(t *store.Todo) (string, string, error) {
		if err := h.Store.CheckTodoImagesBindable(b.ImageIDs, middleware.Username(c)); err != nil {
			return "", "", err
		}
		s, err := h.loadSubtask(c, t)
		if err != nil {
			return "", "", err
		}
		from := s.Status
		if err := store.ApplySubtaskProgress(s, b.Status, b.Progress, time.Now()); err != nil {
			return "", "", err
		}
		if err := h.Store.SaveTodoSubtask(s, t.Owner); err != nil {
			return "", "", err
		}
		store.MarkTodoFollowing(t)
		head := "【" + s.Title + "】"
		if from != s.Status {
			head += subtaskStatusLabel[from] + " → " + subtaskStatusLabel[s.Status]
		} else {
			head += subtaskStatusLabel[s.Status]
		}
		return head + "\n" + s.Progress, s.ID, nil
	})
}

// DeleteSubtask DELETE /todos/:id/subtasks/:sid
func (h *TodoHandler) DeleteSubtask(c *gin.Context) {
	h.subtaskOp(c, nil, "subtask_delete", nil, func(t *store.Todo) (string, string, error) {
		s, err := h.loadSubtask(c, t)
		if err != nil {
			return "", "", err
		}
		if err := h.Store.DeleteTodoSubtask(t.ID, s.ID); err != nil {
			return "", "", err
		}
		// 进展截图挂在跟进记录上，随记录保留；只删子任务本身的图片
		if err := h.Store.DeleteTodoImagesByRef(t.ID, store.TodoImageScopeSubtask, s.ID); err != nil {
			return "", "", err
		}
		return "删除子任务：" + s.Title, s.ID, nil
	})
}

func (h *TodoHandler) Delete(c *gin.Context) {
	t := h.load(c)
	if t == nil {
		return
	}
	if err := h.Store.DeleteTodo(t.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"id": t.ID}})
}

// UploadImage POST /todos/images（multipart: file）— 上传一张未绑定图片，保存事项 / 子任务 / 进展 / 闭环时按 ID 绑定。
func (h *TodoHandler) UploadImage(c *gin.Context) {
	if !h.hasURN(c, store.URNTodoCreate) && !h.hasURN(c, store.URNTodoFollow) && !h.hasURN(c, store.URNTodoEdit) {
		c.JSON(http.StatusForbidden, gin.H{"message": "无待办新建 / 跟进 / 编辑权限"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, store.TodoImageMaxBytes+1<<20)
	fh, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Sprintf("请选择图片（单张不超过 %dMB）", store.TodoImageMaxBytes>>20)})
		return
	}
	if fh.Size > store.TodoImageMaxBytes {
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Sprintf("图片不能超过 %dMB", store.TodoImageMaxBytes>>20)})
		return
	}
	f, err := fh.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, store.TodoImageMaxBytes+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	img := &store.TodoImage{Filename: fh.Filename, CreatedBy: middleware.Username(c)}
	if err := h.Store.CreateTodoImage(img, data); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	_ = h.Store.PurgeOrphanTodoImages(time.Now())
	c.JSON(http.StatusOK, gin.H{"data": img})
}

// Image GET /todos/images/:iid — 查看图片；未绑定的图片仅上传者可见。
func (h *TodoHandler) Image(c *gin.Context) {
	m, data, err := h.Store.GetTodoImageData(c.Param("iid"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if m == nil || (m.TodoID == "" && m.CreatedBy != middleware.Username(c)) {
		c.JSON(http.StatusNotFound, gin.H{"message": "图片不存在"})
		return
	}
	if m.TodoID != "" {
		if t, err := h.Store.GetTodoInTenant(m.TodoID, effectiveListTenantID(h.Store, c)); err != nil || t == nil {
			c.JSON(http.StatusNotFound, gin.H{"message": "图片不存在"})
			return
		}
	}
	c.Header("Cache-Control", "private, max-age=86400")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "default-src 'none'")
	c.Header("Content-Disposition", "inline; filename*=UTF-8''"+url.PathEscape(m.Filename))
	c.Data(http.StatusOK, m.Mime, data)
}

// Users GET /todos/users — 负责人 / 跟进人候选（当前租户活跃成员）。
func (h *TodoHandler) Users(c *gin.Context) {
	tid := middleware.TenantID(c)
	if tid == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请先选择租户"})
		return
	}
	members, err := h.Store.ListTenantMembers(tid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	out := make([]gin.H, 0, len(members))
	for _, m := range members {
		if m.Status != store.MemberStatusActive {
			continue
		}
		out = append(out, gin.H{"username": m.Username, "display_name": m.DisplayName})
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}
