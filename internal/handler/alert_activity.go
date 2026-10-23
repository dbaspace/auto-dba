package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/alert"
	"auto-dba/internal/dns"
	"auto-dba/internal/store"
)

// Activity GET /alerts/activity 告警、恢复、变更、巡检按时间倒序。
func (h *AlertHandler) Activity(c *gin.Context) {
	if h.Store == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "存储未就绪"})
		return
	}
	now := time.Now()
	before, _ := strconv.ParseInt(strings.TrimSpace(c.Query("before")), 10, 64)
	since, until := alert.ParseWindow(c.Query("since"), c.Query("until"), before, now)
	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	kinds := alert.ParseKinds(c.Query("kinds"))
	severity := strings.TrimSpace(c.Query("severity"))
	instanceID := strings.TrimSpace(c.Query("instance_id"))
	tenantID := effectiveListTenantID(h.Store, c)

	w := store.ActivityWindow{
		Since: since.UTC().Format(time.RFC3339), Until: until.UTC().Format(time.RFC3339),
		SinceAt: since, UntilAt: until,
		TenantID: tenantID, InstanceID: instanceID, Severity: severity, Limit: limit,
	}
	byID, refs := h.activityInstances(tenantID)
	var groups [][]alert.ActivityEvent
	var errs []string
	total := 0

	if alert.WantKind(kinds, alert.KindAlert) || alert.WantKind(kinds, alert.KindRecover) {
		if len(kinds) > 0 {
			if alert.WantKind(kinds, alert.KindAlert) {
				w.Actions = append(w.Actions, "trigger")
			}
			if alert.WantKind(kinds, alert.KindRecover) {
				w.Actions = append(w.Actions, "resolve")
			}
		}
		rows, err := h.Store.ListAlertActivity(w)
		if err != nil {
			errs = append(errs, "告警："+err.Error())
		} else {
			var evs []alert.ActivityEvent
			for _, r := range rows {
				labels := alert.DecodeLabels(r.Labels)
				id := alert.LabelInstanceID(labels)
				if instanceID != "" && id != instanceID {
					continue
				}
				kind := alert.KindAlert
				if r.Action == "resolve" {
					kind = alert.KindRecover
				}
				if !alert.WantKind(kinds, kind) {
					continue
				}
				ev, ok := alert.MapAlert(alert.AlertInput{
					EventID: r.EventID, Action: r.Action, Detail: r.Detail, Operator: r.Operator, EventAt: r.EventAt,
					AlertID: r.AlertID, Severity: r.Severity, Title: r.Title, Handle: r.Handle, Owner: r.Owner,
					CurValue: r.CurValue, Threshold: r.Threshold, Labels: labels, Instance: activityInst(byID, id),
				})
				if ok {
					evs = append(evs, ev)
				}
			}
			groups = append(groups, evs)
		}
		if n, err := h.Store.CountAlertActivity(w); err == nil {
			total += n
		}
	}
	if severity == "" && alert.WantKind(kinds, alert.KindChange) {
		if rows, err := h.Store.ListTicketActivity(w); err != nil {
			errs = append(errs, "工单："+err.Error())
		} else {
			var evs []alert.ActivityEvent
			for _, r := range rows {
				ev, ok := alert.MapTicket(alert.TicketInput{
					ID: r.ID, TicketNo: r.TicketNo, IssueType: r.IssueType, Description: r.Description,
					Status: r.Status, Submitter: r.Submitter, UpdatedAt: r.UpdatedAt, Instance: activityInst(byID, r.InstanceID),
				})
				if ok {
					evs = append(evs, ev)
				}
			}
			groups = append(groups, evs)
		}
		if n, err := h.Store.CountTicketActivity(w); err == nil {
			total += n
		}
		if rows, err := h.Store.ListHAActivity(w); err != nil {
			errs = append(errs, "高可用："+err.Error())
		} else {
			var evs []alert.ActivityEvent
			for _, r := range rows {
				ev, ok := alert.MapHA(alert.HAInput{
					ID: r.ID, Action: r.Action, Status: r.Status, Operator: r.Operator, UpdatedAt: r.UpdatedAt,
					ClusterName: r.ClusterName, Instance: activityInst(byID, r.TargetInstanceID),
				})
				if ok {
					evs = append(evs, ev)
				}
			}
			groups = append(groups, evs)
		}
		if n, err := h.Store.CountHAActivity(w); err == nil {
			total += n
		}
		if rows, err := h.Store.ListConfigActivity(w); err != nil {
			errs = append(errs, "配置："+err.Error())
		} else {
			var evs []alert.ActivityEvent
			for _, r := range rows {
				ev, ok := alert.MapConfig(alert.ConfigInput{
					ID: r.ID, Action: r.Action, Actor: r.Actor, Source: r.Source, CreatedAt: r.CreatedAt,
					Instance: activityInst(byID, r.CIID),
				})
				if ok {
					evs = append(evs, ev)
				}
			}
			groups = append(groups, evs)
		}
		if n, err := h.Store.CountConfigActivity(w); err == nil {
			total += n
		}
		if evs, n, err := h.dnsActivity(c, since, until, instanceID, tenantID, byID, refs, limit); err != nil {
			errs = append(errs, "DNS："+err.Error())
		} else {
			groups = append(groups, evs)
			total += n
		}
	}
	if severity == "" && alert.WantKind(kinds, alert.KindInspect) {
		rows, err := h.Store.ListAWRActivity(w)
		if err != nil {
			errs = append(errs, "巡检："+err.Error())
		} else {
			var inst *alert.ActivityInstance
			if instanceID != "" {
				inst = activityInst(byID, instanceID)
			}
			var evs []alert.ActivityEvent
			for _, r := range rows {
				ev, ok := alert.MapAWR(alert.AWRInput{
					Date: r.Date, Total: r.Total, Anomaly: r.Anomaly, At: r.At.Unix(),
					Instance: inst, InstanceOnly: instanceID != "",
				})
				if ok {
					evs = append(evs, ev)
				}
			}
			groups = append(groups, evs)
		}
		if n, err := h.Store.CountAWRActivity(w); err == nil {
			total += n
		}
	}

	page, next := alert.MergeActivity(groups, limit)
	firing := 0
	if h.Store != nil {
		firing = h.Store.AlertStatsInTenant(tenantID).Active
	}
	out := gin.H{
		"data":        page,
		"summary":     alert.ActivitySummary{Total: total, Firing: firing},
		"next_before": next,
	}
	if len(errs) > 0 {
		out["errors"] = errs
	}
	c.JSON(http.StatusOK, out)
}

func (h *AlertHandler) activityInstances(tenantID string) (map[string]alert.InstRef, []alert.InstRef) {
	byID := map[string]alert.InstRef{}
	list, err := h.Store.ListInstancesInTenant(tenantID, "", "")
	if err != nil {
		return byID, nil
	}
	refs := make([]alert.InstRef, 0, len(list))
	for _, it := range list {
		r := alert.InstRef{
			ID: it.ID, Name: it.Name, DBType: it.DBType, Host: it.Host, Domain: it.DomainName,
			Domains: it.Domains, Port: it.Port, TenantID: it.TenantID,
		}
		byID[it.ID] = r
		refs = append(refs, r)
	}
	return byID, refs
}

func activityInst(byID map[string]alert.InstRef, id string) *alert.ActivityInstance {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	if r, ok := byID[id]; ok {
		return r.ToInstance()
	}
	return &alert.ActivityInstance{ID: id}
}

func (h *AlertHandler) dnsActivity(c *gin.Context, since, until time.Time, instanceID, tenantID string, byID map[string]alert.InstRef, refs []alert.InstRef, limit int) ([]alert.ActivityEvent, int, error) {
	if h.DNS == nil || !h.DNS.Configured() {
		return nil, 0, nil
	}
	st, err := h.DNS.Get()
	if err != nil || st == nil {
		if err == nil {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	pageSize := limit
	if pageSize > 100 {
		pageSize = 100
	}
	batches, total, err := st.ListChangeBatches(c.Request.Context(), dns.ChangeFilter{
		From: since.Unix(), To: until.Unix() - 1, Page: 1, PageSize: pageSize,
	})
	if err != nil {
		return nil, 0, err
	}
	var evs []alert.ActivityEvent
	for _, b := range batches {
		logs, err := st.GetChangeBatch(c.Request.Context(), b.BatchID)
		if err != nil {
			continue
		}
		var values []string
		for _, l := range logs {
			values = append(values, alert.SnapValues(l.Before)...)
			values = append(values, alert.SnapValues(l.After)...)
		}
		matched := alert.MatchInstance(refs, values)
		if instanceID != "" && (matched == nil || matched.ID != instanceID) {
			continue
		}
		if tenantID != "" && matched == nil {
			continue
		}
		var inst *alert.ActivityInstance
		if matched != nil {
			inst = matched.ToInstance()
			if byID != nil {
				if full, ok := byID[matched.ID]; ok {
					inst = full.ToInstance()
				}
			}
		}
		evs = append(evs, alert.MapDNS(alert.DNSInput{
			BatchID: b.BatchID, Summary: b.Summary, Actor: b.Actor, Reason: b.Reason,
			Count: b.Count, Created: b.CreatedTime, Instance: inst,
		}))
	}
	if instanceID != "" || tenantID != "" {
		total = len(evs)
	}
	return evs, total, nil
}
