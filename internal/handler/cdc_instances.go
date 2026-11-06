package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

// cdcGrantedDomain 当前用户在该实例上可用来拨号的授权域名。
type cdcGrantedDomain struct {
	Domain  string `json:"domain"`
	Port    int    `json:"port"`
	EnvCode string `json:"env_code"`
}

// cdcInstanceView 在实例上附带授权域名。本租实例不填这两项，拨号仍走实例 host。
type cdcInstanceView struct {
	store.Instance
	GrantedDomains  []cdcGrantedDomain `json:"granted_domains,omitempty"`
	DomainGrantOnly bool               `json:"domain_grant_only,omitempty"`
}

func cdcDomainMatchesEnv(envCode, env string) bool {
	env = strings.TrimSpace(env)
	if env == "" {
		return true
	}
	envCode = strings.TrimSpace(envCode)
	return envCode == "" || strings.EqualFold(envCode, env)
}

func (h *CDCHandler) ListInstances(c *gin.Context) {
	tid := effectiveListTenantID(h.Store, c)
	dbType := c.Query("db_type")
	q := c.Query("q")
	list, err := h.Store.ListInstancesInTenant(tid, dbType, q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []store.Instance{}
	}
	// 平台审计已经能看到全部实例，不再按授权合并。
	if tid != "" {
		ids, gerr := grantedVisibleInstanceIDs(h.Store, tid, middleware.UserID(c))
		if gerr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": gerr.Error()})
			return
		}
		list, err = appendInstancesByIDs(h.Store, list, ids, dbType, q)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
	}
	views, err := h.decorateCDCInstances(tid, middleware.UserID(c), strings.TrimSpace(c.Query("env")), list)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": views})
}

func (h *CDCHandler) decorateCDCInstances(tid, uid, env string, list []store.Instance) ([]cdcInstanceView, error) {
	byInst := map[string][]cdcGrantedDomain{}
	if tid != "" && h.Store != nil {
		granted, err := h.Store.ListGrantedAccessDomains(tid, uid, "", "", "")
		if err != nil {
			return nil, err
		}
		owned, err := h.Store.ListAccessDomainsInTenant(tid, "", "", "")
		if err != nil {
			return nil, err
		}
		add := func(instID, domain, envCode string, port int) {
			instID = strings.TrimSpace(instID)
			domain = strings.TrimSpace(domain)
			if instID == "" || domain == "" {
				return
			}
			if !cdcDomainMatchesEnv(envCode, env) {
				return
			}
			key := strings.ToLower(domain)
			for _, exist := range byInst[instID] {
				if strings.ToLower(exist.Domain) == key {
					return
				}
			}
			byInst[instID] = append(byInst[instID], cdcGrantedDomain{
				Domain:  domain,
				Port:    port,
				EnvCode: strings.TrimSpace(envCode),
			})
		}
		for _, ad := range granted {
			add(ad.InstanceID, ad.Domain, ad.EnvCode, 0)
		}
		for _, ad := range owned {
			add(ad.InstanceID, ad.Domain, ad.EnvCode, 0)
		}
	}
	out := make([]cdcInstanceView, 0, len(list))
	for i := range list {
		inst := list[i]
		redactInstance(&inst)
		view := cdcInstanceView{Instance: inst}
		// 本租实例不覆盖拨号地址，避免同租户任务从实例 host 改成域名。
		if tid != "" && strings.TrimSpace(inst.TenantID) != tid {
			domains := byInst[inst.ID]
			if inst.Port > 0 {
				for j := range domains {
					if domains[j].Port <= 0 {
						domains[j].Port = inst.Port
					}
				}
			}
			if len(domains) > 0 {
				view.GrantedDomains = domains
			}
			only, err := cdcDomainGrantOnly(h.Store, tid, uid, &inst)
			if err != nil {
				return nil, err
			}
			view.DomainGrantOnly = only
		}
		out = append(out, view)
	}
	return out, nil
}

// cdcDomainGrantOnly 实例不是本租的，也没有本租库、库授权或实例授权，只能靠域名到达。
func cdcDomainGrantOnly(db *store.DB, tid, uid string, inst *store.Instance) (bool, error) {
	if db == nil || inst == nil {
		return false, nil
	}
	tid = strings.TrimSpace(tid)
	if tid == "" || strings.TrimSpace(inst.TenantID) == tid {
		return false, nil
	}
	n, err := db.CountDatabasesOnInstanceInTenant(inst.ID, tid)
	if err != nil || n > 0 {
		return false, err
	}
	granted, err := db.ListGrantedDatabases(tid, uid, inst.ID, "")
	if err != nil || len(granted) > 0 {
		return false, err
	}
	ok, err := db.HasResourceGrant(tid, uid, "instance", inst.ID)
	if err != nil || ok {
		return false, err
	}
	return db.InstanceReachableViaAccessDomain(inst.ID, tid, uid)
}
