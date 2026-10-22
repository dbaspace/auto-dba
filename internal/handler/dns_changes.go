package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/dns"
)

func (h *DNSHandler) ListChanges(c *gin.Context) {
	st, ok := h.with(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	size, _ := strconv.Atoi(c.Query("page_size"))
	from, _ := strconv.ParseInt(c.Query("from"), 10, 64)
	to, _ := strconv.ParseInt(c.Query("to"), 10, 64)
	list, total, err := st.ListChangeBatches(c.Request.Context(), dns.ChangeFilter{
		ObjectID: c.Query("object_id"), ZoneID: c.Query("zone_id"), Actor: c.Query("actor"), Source: c.Query("source"),
		Keyword: c.Query("keyword"), From: from, To: to, Page: page, PageSize: size,
	})
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "total": total})
}

func (h *DNSHandler) GetChange(c *gin.Context) {
	st, ok := h.with(c)
	if !ok {
		return
	}
	list, err := st.GetChangeBatch(c.Request.Context(), c.Param("batch"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

// RollbackChange 按批次回滚。涉及「需审批」的域时转为工单。
func (h *DNSHandler) RollbackChange(c *gin.Context) {
	st, ok := h.with(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	batch := c.Param("batch")
	logs, err := st.GetChangeBatch(ctx, batch)
	if err != nil {
		h.fail(c, err)
		return
	}
	ops, err := dns.RollbackOps(logs)
	if err != nil {
		h.fail(c, err)
		return
	}
	zones, err := st.AffectedZones(ctx, ops)
	if err != nil {
		h.fail(c, err)
		return
	}
	for _, l := range logs {
		if l.ZoneID != "" {
			zones = append(zones, l.ZoneID)
		}
	}
	pols, err := st.ListPolicies(ctx)
	if err != nil {
		h.fail(c, err)
		return
	}
	if err := h.checkEditors(c, st, zones, pols); err != nil {
		h.forbid(c, err)
		return
	}
	meta := dns.OpMeta{Actor: actor(c)}
	if needApproval(zones, pols) {
		dry := dns.OpMeta{Actor: actor(c), Source: dns.SourceRollback, DryRun: true}
		res, err := st.ApplyOps(ctx, ops, dry)
		if err != nil {
			h.fail(c, err)
			return
		}
		issue, err := h.createTicket(c, DnsChangePayload{Kind: dnsTicketRollback, BatchID: batch, Source: dns.SourceRollback, Reason: "回滚批次 " + batch, Items: res.Items}, res.Items)
		if err != nil {
			h.fail(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"pending_approval": true, "ticket": issue})
		return
	}
	res, err := st.Rollback(ctx, batch, meta)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": res, "warnings": res.Warnings})
}

// Inspect GET /dns/inspect 全量体检。
func (h *DNSHandler) Inspect(c *gin.Context) {
	st, ok := h.with(c)
	if !ok {
		return
	}
	snap, err := st.Snapshot(c.Request.Context(), true)
	if err != nil {
		h.fail(c, err)
		return
	}
	issues := dns.Inspect(snap, h.hostRefs())
	counts := map[string]int{}
	for _, it := range issues {
		counts[it.Level]++
	}
	c.JSON(http.StatusOK, gin.H{"data": issues, "counts": counts, "records": len(snap.Records), "checked_at": time.Now().Unix()})
}

// Reverse GET /dns/reverse?ip= 指向该 IP 的记录、CNAME 与实例。
func (h *DNSHandler) Reverse(c *gin.Context) {
	st, ok := h.with(c)
	if !ok {
		return
	}
	snap, err := st.Snapshot(c.Request.Context(), false)
	if err != nil {
		h.fail(c, err)
		return
	}
	res, err := dns.Reverse(snap, c.Query("ip"))
	if err != nil {
		h.fail(c, err)
		return
	}
	idx := h.instanceIndex()
	insts := appendRefs(nil, idx[res.IP])
	for _, n := range res.Names {
		insts = appendRefs(insts, idx[n])
	}
	if insts == nil {
		insts = []dns.HostRef{}
	}
	zones := map[string]string{}
	ranges := map[string]string{}
	for _, z := range snap.Zones {
		zones[z.ID] = z.Origin
	}
	for _, r := range snap.Ranges {
		ranges[r.ID] = r.Name
	}
	c.JSON(http.StatusOK, gin.H{"data": res, "instances": insts, "zones": zones, "ranges": ranges})
}

// Export GET /dns/export?zone_id=&range_id=&format=csv|zone
func (h *DNSHandler) Export(c *gin.Context) {
	st, ok := h.with(c)
	if !ok {
		return
	}
	format := strings.ToLower(strings.TrimSpace(c.DefaultQuery("format", "csv")))
	zoneID, rangeID := c.Query("zone_id"), c.Query("range_id")
	snap, err := st.Snapshot(c.Request.Context(), format == "zone")
	if err != nil {
		h.fail(c, err)
		return
	}
	stamp := time.Now().Format("20060102150405")
	switch format {
	case "zone":
		if zoneID == "" || rangeID == "" {
			h.fail(c, fmt.Errorf("导出 zone 文件须选择解析域和解析范围"))
			return
		}
		body, err := dns.ExportZone(snap, zoneID, rangeID)
		if err != nil {
			h.fail(c, err)
			return
		}
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="dns-%s.zone"`, stamp))
		c.Data(http.StatusOK, "text/plain; charset=utf-8", body)
	case "csv":
		body, err := dns.ExportCSV(snap, zoneID, rangeID)
		if err != nil {
			h.fail(c, err)
			return
		}
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="dns-%s.csv"`, stamp))
		c.Data(http.StatusOK, "text/csv; charset=utf-8", body)
	default:
		h.fail(c, fmt.Errorf("格式仅支持 csv 或 zone"))
	}
}

type importBody struct {
	Format    string `json:"format"`
	Content   string `json:"content"`
	Origin    string `json:"origin"`
	RangeName string `json:"range_name"`
	Reason    string `json:"reason"`
}

func (h *DNSHandler) planImport(c *gin.Context, st *dns.Store, b importBody) (*dns.ImportPlan, error) {
	if strings.TrimSpace(b.Content) == "" {
		return nil, fmt.Errorf("文件内容为空")
	}
	if len(b.Content) > 5<<20 {
		return nil, fmt.Errorf("文件超过 5MB")
	}
	var rows []dns.ImportRow
	var err error
	rangeName := ""
	switch strings.ToLower(strings.TrimSpace(b.Format)) {
	case "zone":
		if strings.TrimSpace(b.RangeName) == "" {
			return nil, fmt.Errorf("导入 zone 文件须选择目标解析范围")
		}
		rangeName = b.RangeName
		rows, err = dns.ParseZoneFile(strings.NewReader(b.Content), b.Origin)
	case "", "csv":
		rows, err = dns.ParseCSV(strings.NewReader(b.Content))
	default:
		return nil, fmt.Errorf("格式仅支持 csv 或 zone")
	}
	if err != nil {
		return nil, err
	}
	snap, err := st.Snapshot(c.Request.Context(), false)
	if err != nil {
		return nil, err
	}
	return dns.PlanImport(snap, rows, rangeName), nil
}

// ImportPreview POST /dns/import/preview 解析并对比，同时试执行一次校验，不落库。
func (h *DNSHandler) ImportPreview(c *gin.Context) {
	var body importBody
	if err := c.ShouldBindJSON(&body); err != nil {
		h.fail(c, err)
		return
	}
	st, ok := h.with(c)
	if !ok {
		return
	}
	plan, err := h.planImport(c, st, body)
	if err != nil {
		h.fail(c, err)
		return
	}
	resp := gin.H{"items": plan.Items, "counts": plan.Counts, "issues": []dns.Issue{}, "warnings": []dns.Issue{}}
	if len(plan.Ops) > 0 {
		res, err := st.ApplyOps(c.Request.Context(), plan.Ops, dns.OpMeta{Actor: actor(c), Source: dns.SourceImport, DryRun: true})
		if le, ok := asLint(err); ok {
			resp["issues"] = le.Issues
		} else if err != nil {
			resp["error"] = err.Error()
		} else {
			resp["warnings"] = res.Warnings
		}
	}
	c.JSON(http.StatusOK, resp)
}

// ImportApply POST /dns/import/apply 重新解析后作为一个批次写入。
func (h *DNSHandler) ImportApply(c *gin.Context) {
	var body importBody
	if err := c.ShouldBindJSON(&body); err != nil {
		h.fail(c, err)
		return
	}
	st, ok := h.with(c)
	if !ok {
		return
	}
	plan, err := h.planImport(c, st, body)
	if err != nil {
		h.fail(c, err)
		return
	}
	if plan.Counts[dns.ImportError] > 0 {
		h.fail(c, fmt.Errorf("有 %d 行错误，请修正后再导入", plan.Counts[dns.ImportError]))
		return
	}
	if len(plan.Ops) == 0 {
		h.fail(c, fmt.Errorf("没有需要新增或修改的记录"))
		return
	}
	h.submit(c, st, plan.Ops, submitOpts{Source: dns.SourceImport, Reason: body.Reason})
}

func (h *DNSHandler) ListPolicies(c *gin.Context) {
	st, ok := h.with(c)
	if !ok {
		return
	}
	pols, err := st.ListPolicies(c.Request.Context())
	if err != nil {
		h.fail(c, err)
		return
	}
	type roleOpt struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Code string `json:"code"`
	}
	roles := []roleOpt{}
	if h.Store != nil {
		if list, err := h.Store.ListRoles(); err == nil {
			for _, r := range list {
				roles = append(roles, roleOpt{ID: r.ID, Name: r.Name, Code: r.Code})
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": pols, "roles": roles, "approvers": h.dnsApprovers()})
}

func (h *DNSHandler) SavePolicy(c *gin.Context) {
	var body dns.ZonePolicy
	if err := c.ShouldBindJSON(&body); err != nil {
		h.fail(c, err)
		return
	}
	st, ok := h.with(c)
	if !ok {
		return
	}
	body.ZoneID = c.Param("zone_id")
	zones, err := st.ListZones(c.Request.Context())
	if err != nil {
		h.fail(c, err)
		return
	}
	found := false
	for _, z := range zones {
		if z.ID == body.ZoneID {
			found = true
			break
		}
	}
	if !found {
		h.fail(c, fmt.Errorf("解析域不存在"))
		return
	}
	if err := st.SavePolicy(c.Request.Context(), body, actor(c)); err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *DNSHandler) ListTTLHolds(c *gin.Context) {
	st, ok := h.with(c)
	if !ok {
		return
	}
	list, err := st.ListTTLHolds(c.Request.Context(), c.Query("status"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

// LowerTTL POST /dns/ttl-holds 预降 TTL。不走按域审批，按域编辑角色仍校验。
func (h *DNSHandler) LowerTTL(c *gin.Context) {
	var body struct {
		RecordIDs []string `json:"record_ids"`
		TTL       int64    `json:"ttl"`
		Minutes   int      `json:"minutes"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		h.fail(c, err)
		return
	}
	st, ok := h.with(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	ops := make([]dns.Op, 0, len(body.RecordIDs))
	for _, id := range body.RecordIDs {
		ops = append(ops, dns.Op{Action: dns.ActionUpdate, Object: dns.ObjectRecord, ID: id})
	}
	zones, err := st.AffectedZones(ctx, ops)
	if err != nil {
		h.fail(c, err)
		return
	}
	pols, err := st.ListPolicies(ctx)
	if err != nil {
		h.fail(c, err)
		return
	}
	if err := h.checkEditors(c, st, zones, pols); err != nil {
		h.forbid(c, err)
		return
	}
	res, err := st.LowerTTL(ctx, body.RecordIDs, body.TTL, body.Minutes, actor(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": res})
}

func (h *DNSHandler) RestoreTTL(c *gin.Context) {
	st, ok := h.with(c)
	if !ok {
		return
	}
	status, err := st.RestoreTTL(c.Request.Context(), c.Param("id"), actor(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": status})
}

func asLint(err error) (*dns.LintError, bool) {
	le, ok := err.(*dns.LintError)
	return le, ok
}
