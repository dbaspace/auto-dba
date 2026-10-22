package handler

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/dbdeploy"
	"auto-dba/internal/dns"
	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

// DNSHandler 解析域、解析范围和记录。数据在独立解析库，所有写入走 dns.Store.ApplyOps。
type DNSHandler struct {
	Store  *store.DB
	Secret string
	DNS    *dns.Provider
	Engine *dbdeploy.Engine
}

func (h *DNSHandler) db() (*dns.Store, error) {
	if h.DNS == nil || !h.DNS.Configured() {
		return nil, fmt.Errorf("未配置 dns.dsn")
	}
	return h.DNS.Get()
}

func (h *DNSHandler) fail(c *gin.Context, err error) {
	if err == nil {
		return
	}
	var le *dns.LintError
	if errors.As(err, &le) {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error(), "issues": le.Issues})
		return
	}
	var ce *dns.ConflictError
	if errors.As(err, &ce) {
		c.JSON(http.StatusConflict, gin.H{"message": err.Error(), "conflicts": ce.Items})
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
}

func (h *DNSHandler) forbid(c *gin.Context, err error) {
	c.JSON(http.StatusForbidden, gin.H{"message": err.Error()})
}

// with 取解析库，失败时直接写响应。
func (h *DNSHandler) with(c *gin.Context) (*dns.Store, bool) {
	st, err := h.db()
	if err != nil {
		h.fail(c, err)
		return nil, false
	}
	return st, true
}

type submitOpts struct {
	Source string
	Reason string
	// NoApproval 跳过按域审批（TTL 预降、恢复）。按域编辑角色仍然校验。
	NoApproval bool
}

// submit 统一的写入口：按域校验编辑角色；涉及「需审批」的域时先完整校验，再转为 DNS 变更工单。
func (h *DNSHandler) submit(c *gin.Context, st *dns.Store, ops []dns.Op, o submitOpts) {
	ctx := c.Request.Context()
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
	meta := dns.OpMeta{Actor: actor(c), Source: o.Source, Reason: o.Reason}
	if !o.NoApproval && needApproval(zones, pols) {
		pinned, err := st.PinExpect(ctx, ops)
		if err != nil {
			h.fail(c, err)
			return
		}
		dry := meta
		dry.DryRun = true
		res, err := st.ApplyOps(ctx, pinned, dry)
		if err != nil {
			h.fail(c, err)
			return
		}
		issue, err := h.createTicket(c, DnsChangePayload{Kind: dnsTicketOps, Ops: pinned, Source: o.Source, Reason: o.Reason, Items: res.Items}, res.Items)
		if err != nil {
			h.fail(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"pending_approval": true, "ticket": issue, "warnings": res.Warnings})
		return
	}
	res, err := st.ApplyOps(ctx, ops, meta)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": res, "warnings": res.Warnings})
}

func needApproval(zones []string, pols map[string]dns.ZonePolicy) bool {
	for _, z := range zones {
		if pols[z].RequireApproval {
			return true
		}
	}
	return false
}

// checkEditors 域策略里设置了可编辑角色时，只有这些角色和管理员能改。
func (h *DNSHandler) checkEditors(c *gin.Context, st *dns.Store, zones []string, pols map[string]dns.ZonePolicy) error {
	var limited []string
	for _, z := range zones {
		if len(pols[z].EditorRoleIDs) > 0 {
			limited = append(limited, z)
		}
	}
	if len(limited) == 0 || h.isDNSAdmin(c) {
		return nil
	}
	mine := map[string]bool{}
	if h.Store != nil {
		ids, err := h.Store.ListUserRoleIDs(middleware.UserID(c))
		if err != nil {
			return err
		}
		for _, id := range ids {
			mine[id] = true
		}
	}
	origins := map[string]string{}
	if list, err := st.ListZones(c.Request.Context()); err == nil {
		for _, z := range list {
			origins[z.ID] = z.Origin
		}
	}
	for _, z := range limited {
		ok := false
		for _, rid := range pols[z].EditorRoleIDs {
			if mine[rid] {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("没有修改解析域 %s 的权限", firstNonBlank(origins[z], z))
		}
	}
	return nil
}

func (h *DNSHandler) isDNSAdmin(c *gin.Context) bool {
	if middleware.IsAdminRole(middleware.Role(c)) {
		return true
	}
	if h.Store == nil {
		return false
	}
	ok, err := h.Store.UserIsAdmin(middleware.UserID(c))
	return err == nil && ok
}

func firstNonBlank(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func (h *DNSHandler) ListZones(c *gin.Context) {
	st, ok := h.with(c)
	if !ok {
		return
	}
	list, err := st.ListZones(c.Request.Context())
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

type zoneBody struct {
	Origin string `json:"origin"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

func (h *DNSHandler) CreateZone(c *gin.Context) {
	var body zoneBody
	if err := c.ShouldBindJSON(&body); err != nil {
		h.fail(c, err)
		return
	}
	st, ok := h.with(c)
	if !ok {
		return
	}
	if body.Status == "" {
		body.Status = dns.StatusActive
	}
	h.submit(c, st, []dns.Op{dns.ZoneOp(dns.ActionCreate, "", dns.ZoneSnap{Origin: body.Origin, Status: body.Status})},
		submitOpts{Source: dns.SourceUI, Reason: body.Reason})
}

func (h *DNSHandler) UpdateZone(c *gin.Context) {
	var body zoneBody
	if err := c.ShouldBindJSON(&body); err != nil {
		h.fail(c, err)
		return
	}
	st, ok := h.with(c)
	if !ok {
		return
	}
	id := c.Param("id")
	h.submit(c, st, []dns.Op{dns.ZoneOp(dns.ActionUpdate, id, dns.ZoneSnap{ID: id, Origin: body.Origin, Status: body.Status})},
		submitOpts{Source: dns.SourceUI, Reason: body.Reason})
}

func (h *DNSHandler) DeleteZone(c *gin.Context) {
	st, ok := h.with(c)
	if !ok {
		return
	}
	h.submit(c, st, []dns.Op{dns.ZoneOp(dns.ActionDelete, c.Param("id"), dns.ZoneSnap{})},
		submitOpts{Source: dns.SourceUI, Reason: c.Query("reason")})
}

func (h *DNSHandler) ListRanges(c *gin.Context) {
	st, ok := h.with(c)
	if !ok {
		return
	}
	list, err := st.ListRanges(c.Request.Context(), c.Query("include_default") == "1")
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

type rangeBody struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Status      string   `json:"status"`
	NetworkList []string `json:"network_list"`
	Reason      string   `json:"reason"`
}

func (b rangeBody) snap(id string) dns.RangeSnap {
	return dns.RangeSnap{ID: id, Name: b.Name, NetworkList: b.NetworkList, Description: b.Description, Status: b.Status}
}

func (h *DNSHandler) CreateRange(c *gin.Context) {
	var body rangeBody
	if err := c.ShouldBindJSON(&body); err != nil {
		h.fail(c, err)
		return
	}
	st, ok := h.with(c)
	if !ok {
		return
	}
	if body.Status == "" {
		body.Status = dns.StatusActive
	}
	h.submit(c, st, []dns.Op{dns.RangeOp(dns.ActionCreate, "", body.snap(""))}, submitOpts{Source: dns.SourceUI, Reason: body.Reason})
}

func (h *DNSHandler) UpdateRange(c *gin.Context) {
	var body rangeBody
	if err := c.ShouldBindJSON(&body); err != nil {
		h.fail(c, err)
		return
	}
	st, ok := h.with(c)
	if !ok {
		return
	}
	id := c.Param("id")
	h.submit(c, st, []dns.Op{dns.RangeOp(dns.ActionUpdate, id, body.snap(id))}, submitOpts{Source: dns.SourceUI, Reason: body.Reason})
}

func (h *DNSHandler) DeleteRange(c *gin.Context) {
	st, ok := h.with(c)
	if !ok {
		return
	}
	h.submit(c, st, []dns.Op{dns.RangeOp(dns.ActionDelete, c.Param("id"), dns.RangeSnap{})},
		submitOpts{Source: dns.SourceUI, Reason: c.Query("reason")})
}

// recordView 列表行：记录本身、关联实例与进行中的 TTL 预降。
type recordView struct {
	dns.Record
	Instances []dns.HostRef `json:"instances"`
	TTLHold   *dns.TTLHold  `json:"ttl_hold,omitempty"`
}

func (h *DNSHandler) ListRecords(c *gin.Context) {
	st, ok := h.with(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	page, _ := strconv.Atoi(c.Query("page"))
	size, _ := strconv.Atoi(c.Query("page_size"))
	list, total, err := st.ListRecords(ctx, dns.RecordFilter{
		ZoneID: c.Query("zone_id"), RangeID: c.Query("parsing_range_id"),
		Type: c.Query("type"), Status: c.Query("status"), Name: c.Query("name"), Data: c.Query("data"),
		Page: page, PageSize: size,
	})
	if err != nil {
		h.fail(c, err)
		return
	}
	holds := map[string]*dns.TTLHold{}
	if hl, err := st.ListTTLHolds(ctx, dns.TTLHoldHolding); err == nil {
		for i := range hl {
			holds[hl[i].RecordID] = &hl[i]
		}
	}
	idx := h.instanceIndex()
	out := make([]recordView, 0, len(list))
	for _, r := range list {
		v := recordView{Record: r, Instances: []dns.HostRef{}, TTLHold: holds[r.ID]}
		fq := dns.FQDN(r.Name, r.ZoneOrigin)
		v.Instances = append(v.Instances, idx[fq]...)
		if r.Type == "A" || r.Type == "AAAA" {
			v.Instances = appendRefs(v.Instances, idx[strings.ToLower(r.Data)])
		}
		out = append(out, v)
	}
	c.JSON(http.StatusOK, gin.H{"data": out, "total": total, "page": page, "page_size": size})
}

func appendRefs(list, more []dns.HostRef) []dns.HostRef {
	for _, m := range more {
		dup := false
		for _, x := range list {
			if x.InstanceID == m.InstanceID {
				dup = true
				break
			}
		}
		if !dup {
			list = append(list, m)
		}
	}
	return list
}

// hostRefs 平台实例的 host 与访问域名。
func (h *DNSHandler) hostRefs() []dns.HostRef {
	if h.Store == nil {
		return nil
	}
	insts, err := h.Store.ListInstances("", "")
	if err != nil {
		return nil
	}
	var out []dns.HostRef
	for _, it := range insts {
		seen := map[string]bool{}
		for _, a := range append([]string{it.Host, it.DomainName}, it.Domains...) {
			a = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(a), "."))
			if a == "" || seen[a] {
				continue
			}
			seen[a] = true
			out = append(out, dns.HostRef{Addr: a, InstanceID: it.ID, InstanceName: it.Name, DBType: it.DBType})
		}
	}
	return out
}

func (h *DNSHandler) instanceIndex() map[string][]dns.HostRef {
	idx := map[string][]dns.HostRef{}
	for _, r := range h.hostRefs() {
		idx[r.Addr] = append(idx[r.Addr], r)
	}
	return idx
}

type recordBody struct {
	Names       []string `json:"names"`
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Data        string   `json:"data"`
	MailHost    string   `json:"mail_host"`
	Preference  int      `json:"preference"`
	TTL         int64    `json:"ttl"`
	ZoneID      string   `json:"zone_id"`
	RangeID     string   `json:"parsing_range_id"`
	Description string   `json:"description"`
	Status      string   `json:"status"`
	Reason      string   `json:"reason"`
}

func (b recordBody) snap(id, name string) dns.RecordSnap {
	return dns.RecordSnap{
		ID: id, Name: name, Type: b.Type, Data: b.Data, MailHost: b.MailHost, Preference: b.Preference, TTL: b.TTL,
		ZoneID: b.ZoneID, RangeID: b.RangeID, Description: b.Description, Status: b.Status,
	}
}

func (h *DNSHandler) CreateRecords(c *gin.Context) {
	var body recordBody
	if err := c.ShouldBindJSON(&body); err != nil {
		h.fail(c, err)
		return
	}
	names := body.Names
	if len(names) == 0 && strings.TrimSpace(body.Name) != "" {
		names = []string{body.Name}
	}
	if len(names) == 0 {
		h.fail(c, fmt.Errorf("请填写主机名"))
		return
	}
	if body.Status == "" {
		body.Status = dns.StatusActive
	}
	st, ok := h.with(c)
	if !ok {
		return
	}
	ops := make([]dns.Op, 0, len(names))
	for _, n := range names {
		ops = append(ops, dns.RecordOp(dns.ActionCreate, "", body.snap("", n)))
	}
	h.submit(c, st, ops, submitOpts{Source: dns.SourceUI, Reason: body.Reason})
}

func (h *DNSHandler) UpdateRecord(c *gin.Context) {
	var body recordBody
	if err := c.ShouldBindJSON(&body); err != nil {
		h.fail(c, err)
		return
	}
	st, ok := h.with(c)
	if !ok {
		return
	}
	id := c.Param("id")
	h.submit(c, st, []dns.Op{dns.RecordOp(dns.ActionUpdate, id, body.snap(id, body.Name))}, submitOpts{Source: dns.SourceUI, Reason: body.Reason})
}

func (h *DNSHandler) DeleteRecord(c *gin.Context) {
	st, ok := h.with(c)
	if !ok {
		return
	}
	h.submit(c, st, []dns.Op{dns.RecordOp(dns.ActionDelete, c.Param("id"), dns.RecordSnap{})},
		submitOpts{Source: dns.SourceUI, Reason: c.Query("reason")})
}

// BatchDeleteRecords POST /dns-records/batch-delete，作为一个批次删除。
func (h *DNSHandler) BatchDeleteRecords(c *gin.Context) {
	var body struct {
		IDs    []string `json:"ids"`
		Reason string   `json:"reason"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		h.fail(c, err)
		return
	}
	if len(body.IDs) == 0 {
		h.fail(c, fmt.Errorf("请选择记录"))
		return
	}
	st, ok := h.with(c)
	if !ok {
		return
	}
	ops := make([]dns.Op, 0, len(body.IDs))
	for _, id := range body.IDs {
		ops = append(ops, dns.RecordOp(dns.ActionDelete, id, dns.RecordSnap{}))
	}
	h.submit(c, st, ops, submitOpts{Source: dns.SourceUI, Reason: body.Reason})
}

// nodeView 心跳表中的节点，附带集群部署信息。
type nodeView struct {
	dns.Node
	Deployed    bool   `json:"deployed"`
	Port        int    `json:"port"`
	MetricsPort int    `json:"metrics_port"`
	ClusterName string `json:"cluster_name"`
}

func (h *DNSHandler) ListNodes(c *gin.Context) {
	st, ok := h.with(c)
	if !ok {
		return
	}
	list, err := st.ListNodes(c.Request.Context(), time.Now().Unix())
	if err != nil {
		h.fail(c, err)
		return
	}
	deployed := map[string]dbdeploy.DNSNode{}
	if h.Engine != nil {
		if ns, err := h.Engine.ListDNSNodes(); err == nil {
			for _, n := range ns {
				deployed[strings.ToLower(n.Host)] = n
			}
		}
	}
	out := make([]nodeView, 0, len(list))
	seen := map[string]bool{}
	for _, n := range list {
		v := nodeView{Node: n}
		key := strings.ToLower(n.IP)
		if d, ok := deployed[key]; ok {
			v.Deployed, v.Port, v.MetricsPort, v.ClusterName = true, d.Port, d.MetricsPort, d.ClusterName
			seen[key] = true
		} else if d, ok := deployed[strings.ToLower(n.Name)]; ok {
			v.Deployed, v.Port, v.MetricsPort, v.ClusterName = true, d.Port, d.MetricsPort, d.ClusterName
			seen[strings.ToLower(n.Name)] = true
		}
		out = append(out, v)
	}
	for key, d := range deployed {
		if seen[key] {
			continue
		}
		out = append(out, nodeView{Node: dns.Node{IP: d.Host}, Deployed: true, Port: d.Port, MetricsPort: d.MetricsPort, ClusterName: d.ClusterName})
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func actor(c *gin.Context) string {
	if v, ok := c.Get(middleware.CtxUsername); ok {
		if s, _ := v.(string); s != "" {
			return s
		}
	}
	return ""
}

func isIP(s string) bool {
	return net.ParseIP(strings.TrimSpace(s)) != nil
}
