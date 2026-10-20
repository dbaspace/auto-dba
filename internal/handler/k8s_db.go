package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"auto-dba/internal/k8s"
	"auto-dba/internal/k8s/operator"
	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

func (h *K8sHandler) DetectOperators(c *gin.Context) {
	cli, cl, allow, ok := h.liveClient(c)
	if !ok {
		return
	}
	if allow != nil {
		c.JSON(http.StatusForbidden, gin.H{"message": "仅集群归属方可检测 Operator"})
		return
	}
	var out []operator.OperatorInfo
	for _, a := range h.Ops.All() {
		info, err := a.Detect(c.Request.Context(), cli)
		if err != nil || info == nil {
			continue
		}
		out = append(out, *info)
		st := "missing"
		if info.Installed {
			st = "installed"
		}
		_ = h.Store.UpsertK8sOperator(&store.K8sOperator{
			ID: cl.ID + ":" + info.Engine,
			ClusterID: cl.ID, TenantID: cl.TenantID, Engine: info.Engine,
			Name: info.Name, Version: info.Version, Namespace: info.Namespace, Status: st,
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (h *K8sHandler) ListOperators(c *gin.Context) {
	cl, ok := h.clusterInTenant(c)
	if !ok {
		return
	}
	list, err := h.Store.ListK8sOperators(cl.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *K8sHandler) ListDBClusters(c *gin.Context) {
	list, err := h.Store.ListK8sDBClustersInTenant(effectiveListTenantID(h.Store, c), c.Query("cluster_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *K8sHandler) GetDBCluster(c *gin.Context) {
	row, ok := h.loadDBCluster(c)
	if !ok {
		return
	}
	cli, err := h.clientForClusterID(c, row.ClusterID)
	if err == nil {
		if ad := h.Ops.Get(row.Engine); ad != nil {
			if st, err := ad.Status(c.Request.Context(), cli, operator.CRRef{Namespace: row.Namespace, Name: row.CRName}); err == nil && st != nil {
				row.Status = st.Phase
				row.Endpoint = st.Endpoint
				_ = h.Store.UpdateK8sDBCluster(row)
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": row})
}

func (h *K8sHandler) CreateDBCluster(c *gin.Context) {
	var spec operator.DBClusterSpec
	if err := c.ShouldBindJSON(&spec); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	cli, cl, allow, ok := h.liveClient(c)
	if !ok {
		return
	}
	if allow != nil && !h.nsAllowed(allow, spec.Namespace) {
		c.JSON(http.StatusForbidden, gin.H{"message": "命名空间未授权"})
		return
	}
	if h.requireProdTicket(c, cl, "db_create", spec.Name) {
		return
	}
	ad := h.Ops.Get(spec.Engine)
	if ad == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "不支持的引擎: " + spec.Engine})
		return
	}
	obj, err := ad.Render(spec)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	gvr, err := guessCRGVR(obj)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	created, err := cli.Dynamic.Resource(gvr).Namespace(spec.Namespace).Create(c.Request.Context(), obj, metav1.CreateOptions{})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"message": err.Error()})
		return
	}
	specJSON, _ := json.Marshal(spec)
	eps, _ := ad.Endpoints(c.Request.Context(), cli, operator.CRRef{Namespace: spec.Namespace, Name: spec.Name})
	endpoint := ""
	if len(eps) > 0 {
		endpoint = fmt.Sprintf("%s:%d", eps[0].Host, eps[0].Port)
	}
	row := &store.K8sDBCluster{
		ClusterID: cl.ID, Namespace: spec.Namespace, Engine: spec.Engine,
		Operator: ad.Engine(), CRKind: created.GetKind(), CRName: created.GetName(),
		SpecJSON: string(specJSON), Status: "Creating", Endpoint: endpoint,
		CreatedBy: middleware.Username(c), UpdatedBy: middleware.Username(c),
	}
	stampTenantID(c, func(tid string) { row.TenantID = tid })
	if err := h.Store.CreateK8sDBCluster(row); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if instID, err := h.registerInstanceFromDB(row, spec, eps); err == nil && instID != "" {
		row.InstanceID = instID
		_ = h.Store.UpdateK8sDBCluster(row)
	}
	h.audit(c, cl.ID, spec.Namespace, "create", "db_cluster", spec.Name, "", "ok", "")
	c.JSON(http.StatusOK, gin.H{"data": row})
}

func (h *K8sHandler) ScaleDBCluster(c *gin.Context) {
	row, ad, cli, cl, ok := h.dbContext(c)
	if !ok {
		return
	}
	var req struct {
		Replicas  int                `json:"replicas"`
		Resources operator.Resources `json:"resources"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if err := ad.Scale(c.Request.Context(), cli, operator.CRRef{Namespace: row.Namespace, Name: row.CRName}, req.Replicas, req.Resources); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"message": err.Error()})
		return
	}
	h.audit(c, cl.ID, row.Namespace, "scale", "db_cluster", row.CRName, "", "ok", "")
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *K8sHandler) UpgradeDBCluster(c *gin.Context) {
	row, ad, cli, cl, ok := h.dbContext(c)
	if !ok {
		return
	}
	var req struct {
		Version string `json:"version"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Version == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "version 必填"})
		return
	}
	if h.requireProdTicket(c, cl, "db_upgrade", row.CRName) {
		return
	}
	if err := ad.Upgrade(c.Request.Context(), cli, operator.CRRef{Namespace: row.Namespace, Name: row.CRName}, req.Version); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"message": err.Error()})
		return
	}
	h.audit(c, cl.ID, row.Namespace, "upgrade", "db_cluster", row.CRName, "", "ok", req.Version)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *K8sHandler) SwitchoverDBCluster(c *gin.Context) {
	row, ad, cli, cl, ok := h.dbContext(c)
	if !ok {
		return
	}
	var req struct {
		Target string `json:"target"`
	}
	_ = c.ShouldBindJSON(&req)
	if err := ad.Switchover(c.Request.Context(), cli, operator.CRRef{Namespace: row.Namespace, Name: row.CRName}, req.Target); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"message": err.Error()})
		return
	}
	h.audit(c, cl.ID, row.Namespace, "switchover", "db_cluster", row.CRName, "", "ok", req.Target)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *K8sHandler) BackupDBCluster(c *gin.Context) {
	row, ad, cli, cl, ok := h.dbContext(c)
	if !ok {
		return
	}
	var dest operator.BackupDest
	_ = c.ShouldBindJSON(&dest)
	if err := ad.Backup(c.Request.Context(), cli, operator.CRRef{Namespace: row.Namespace, Name: row.CRName}, dest); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"message": err.Error()})
		return
	}
	h.audit(c, cl.ID, row.Namespace, "backup", "db_cluster", row.CRName, "", "ok", dest.Kind)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *K8sHandler) DeleteDBCluster(c *gin.Context) {
	row, _, cli, cl, ok := h.dbContext(c)
	if !ok {
		return
	}
	if h.requireProdTicket(c, cl, "db_delete", row.CRName) {
		return
	}
	gvr := engineGVR(row.Engine)
	_ = cli.Dynamic.Resource(gvr).Namespace(row.Namespace).Delete(c.Request.Context(), row.CRName, metav1.DeleteOptions{})
	if c.Query("unregister_instance") == "1" && row.InstanceID != "" {
		_ = h.Store.DeleteInstance(row.InstanceID)
	}
	_ = h.Store.DeleteK8sDBCluster(row.ID)
	h.audit(c, cl.ID, row.Namespace, "delete", "db_cluster", row.CRName, "", "ok", "")
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *K8sHandler) ListHelmApps(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"data": []any{}, "message": "Helm 应用管理将在增强期接入 helm.sh/helm/v3 SDK"})
}

func (h *K8sHandler) loadDBCluster(c *gin.Context) (*store.K8sDBCluster, bool) {
	row, err := h.Store.GetK8sDBCluster(c.Param("dbId"))
	if err != nil || row == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "不存在"})
		return nil, false
	}
	tid := effectiveListTenantID(h.Store, c)
	if tid != "" && row.TenantID != tid && !platformAdminAuditing(h.Store, c) {
		c.JSON(http.StatusNotFound, gin.H{"message": "不存在"})
		return nil, false
	}
	return row, true
}

func (h *K8sHandler) dbContext(c *gin.Context) (*store.K8sDBCluster, operator.Adapter, *k8s.Client, *store.K8sCluster, bool) {
	row, ok := h.loadDBCluster(c)
	if !ok {
		return nil, nil, nil, nil, false
	}
	ad := h.Ops.Get(row.Engine)
	if ad == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "无适配器"})
		return nil, nil, nil, nil, false
	}
	cli, err := h.clientForClusterID(c, row.ClusterID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"message": err.Error()})
		return nil, nil, nil, nil, false
	}
	cl, err := h.Store.GetK8sCluster(row.ClusterID)
	if err != nil || cl == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "集群不存在"})
		return nil, nil, nil, nil, false
	}
	return row, ad, cli, cl, true
}

func (h *K8sHandler) clientForClusterID(c *gin.Context, clusterID string) (*k8s.Client, error) {
	cl, err := h.Store.GetK8sClusterInTenant(clusterID, effectiveListTenantID(h.Store, c))
	if err != nil || cl == nil {
		return nil, fmt.Errorf("集群不存在")
	}
	cred, err := h.credFromCluster(cl, middleware.Username(c), middleware.Role(c))
	if err != nil {
		return nil, err
	}
	return h.Mgr.Get(cl.ID, cred)
}

func (h *K8sHandler) registerInstanceFromDB(row *store.K8sDBCluster, spec operator.DBClusterSpec, eps []operator.Endpoint) (string, error) {
	if h.Store == nil || len(eps) == 0 {
		return "", nil
	}
	host := eps[0].Host
	port := eps[0].Port
	dbType := spec.Engine
	switch dbType {
	case "postgres":
		dbType = "postgresql"
	}
	name := fmt.Sprintf("k8s/%s/%s", row.Namespace, row.CRName)
	inst := &store.Instance{
		Name: name, DBType: dbType, Host: host, Port: port,
		DomainName: host, Domains: []string{host},
		TenantID: row.TenantID, Visibility: "global",
		Tags: []string{
			"source=k8s",
			"k8s_cluster_id=" + row.ClusterID,
			"k8s_namespace=" + row.Namespace,
			"k8s_cr=" + row.CRName,
		},
		Description: "auto-registered from k8s db cluster",
	}
	if err := h.Store.CreateInstance(inst); err != nil {
		return "", err
	}
	return inst.ID, nil
}

func guessCRGVR(obj *unstructured.Unstructured) (schema.GroupVersionResource, error) {
	gv := obj.GroupVersionKind()
	switch gv.Group + "/" + gv.Kind {
	case "postgresql.cnpg.io/Cluster":
		return schema.GroupVersionResource{Group: "postgresql.cnpg.io", Version: "v1", Resource: "clusters"}, nil
	case "pxc.percona.com/PerconaXtraDBCluster":
		return schema.GroupVersionResource{Group: "pxc.percona.com", Version: "v1", Resource: "perconaxtradbclusters"}, nil
	case "redis.redis.opstreelabs.in/Redis":
		return schema.GroupVersionResource{Group: "redis.redis.opstreelabs.in", Version: "v1beta2", Resource: "redis"}, nil
	case "starrocks.com/StarRocksCluster":
		return schema.GroupVersionResource{Group: "starrocks.com", Version: "v1", Resource: "starrocksclusters"}, nil
	case "apps.kubeblocks.io/Cluster":
		return schema.GroupVersionResource{Group: "apps.kubeblocks.io", Version: "v1alpha1", Resource: "clusters"}, nil
	default:
		return schema.GroupVersionResource{}, fmt.Errorf("未知 CR: %s/%s", gv.Group, gv.Kind)
	}
}

func engineGVR(engine string) schema.GroupVersionResource {
	switch engine {
	case "postgres":
		return schema.GroupVersionResource{Group: "postgresql.cnpg.io", Version: "v1", Resource: "clusters"}
	case "mysql":
		return schema.GroupVersionResource{Group: "pxc.percona.com", Version: "v1", Resource: "perconaxtradbclusters"}
	case "redis":
		return schema.GroupVersionResource{Group: "redis.redis.opstreelabs.in", Version: "v1beta2", Resource: "redis"}
	case "starrocks":
		return schema.GroupVersionResource{Group: "starrocks.com", Version: "v1", Resource: "starrocksclusters"}
	default:
		return schema.GroupVersionResource{Group: "apps.kubeblocks.io", Version: "v1alpha1", Resource: "clusters"}
	}
}
