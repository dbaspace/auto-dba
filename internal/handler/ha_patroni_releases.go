package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/ha"
	"auto-dba/internal/middleware"
	"auto-dba/internal/secret"
	"auto-dba/internal/srcluster"
	"auto-dba/internal/store"
)

func (h *HAHandler) patroniClusterAuth(c *gin.Context) (*store.HACluster, ha.ControllerAuth, error) {
	cluster, err := h.Store.GetHAClusterInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || cluster == nil {
		return nil, ha.ControllerAuth{}, fmt.Errorf("集群不存在")
	}
	if !strings.EqualFold(cluster.Engine, "postgres") || !strings.EqualFold(cluster.Mode, "patroni") {
		return nil, ha.ControllerAuth{}, fmt.Errorf("仅 PostgreSQL · Patroni 支持版本代装")
	}
	if strings.TrimSpace(cluster.ControllerID) == "" {
		return nil, ha.ControllerAuth{}, fmt.Errorf("请先绑定 Patroni 控制器（Agent Base URL 用于代装）")
	}
	ctrl, err := h.Store.GetHAController(cluster.ControllerID)
	if err != nil || ctrl == nil {
		return nil, ha.ControllerAuth{}, fmt.Errorf("控制器不存在")
	}
	auth, err := h.controllerAuth(ctrl)
	if err != nil {
		return nil, ha.ControllerAuth{}, err
	}
	return cluster, auth, nil
}

func (h *HAHandler) ListPatroniReleases(c *gin.Context) {
	list, err := h.Store.ListPatroniReleases()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *HAHandler) CreatePatroniRelease(c *gin.Context) {
	var r store.PatroniRelease
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if strings.TrimSpace(r.Version) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "version 必填"})
		return
	}
	r.Enabled = true
	who := middleware.Username(c)
	r.CreatedBy, r.UpdatedBy = who, who
	if err := h.Store.UpsertPatroniRelease(&r); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": r})
}

func (h *HAHandler) UpdatePatroniRelease(c *gin.Context) {
	exist, err := h.Store.GetPatroniRelease(c.Param("id"))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "版本不存在"})
		return
	}
	var req store.PatroniRelease
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	exist.Version = firstNonEmpty(req.Version, exist.Version)
	if req.Channel != "" {
		exist.Channel = req.Channel
	}
	if req.Arch != "" {
		exist.Arch = req.Arch
	}
	exist.OS = req.OS
	exist.Remark = req.Remark
	exist.Enabled = req.Enabled
	exist.UpdatedBy = middleware.Username(c)
	if err := h.Store.UpsertPatroniRelease(exist); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": exist})
}

func (h *HAHandler) DeletePatroniRelease(c *gin.Context) {
	if err := h.Store.DeletePatroniRelease(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *HAHandler) UpsertPatroniReleaseArtifact(c *gin.Context) {
	rel, err := h.Store.GetPatroniRelease(c.Param("id"))
	if err != nil || rel == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "版本不存在"})
		return
	}
	var a store.PatroniReleaseArtifact
	if err := c.ShouldBindJSON(&a); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	a.ReleaseID = rel.ID
	if strings.TrimSpace(a.ObjectKey) == "" || strings.TrimSpace(a.SHA256) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "object_key 与 sha256 必填"})
		return
	}
	if err := h.Store.UpsertPatroniReleaseArtifact(&a); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": a})
}

func (h *HAHandler) GetPatroniOSS(c *gin.Context) {
	b, err := h.Store.GetPatroniOSSBind()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": b})
}

func (h *HAHandler) PutPatroniOSS(c *gin.Context) {
	var b store.PatroniOSSBind
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	b.UpdatedBy = middleware.Username(c)
	if err := h.Store.PutPatroniOSSBind(&b); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	out, _ := h.Store.GetPatroniOSSBind()
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (h *HAHandler) PresignPatroniRelease(c *gin.Context) {
	role := strings.ToLower(strings.TrimSpace(c.Query("role")))
	if role == "" {
		role = "patroni"
	}
	urlStr, key, sum, pf, err := h.presignPatroniArtifact(c.Param("id"), role, 2*time.Hour)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"url": urlStr, "object_key": key, "sha256": sum, "pack_format": pf, "role": role,
	}})
}

func (h *HAHandler) presignPatroniArtifact(releaseID, role string, expire time.Duration) (urlStr, objectKey, sha256, packFormat string, err error) {
	rel, err := h.Store.GetPatroniRelease(releaseID)
	if err != nil {
		return "", "", "", "", err
	}
	if rel == nil {
		return "", "", "", "", fmt.Errorf("版本不存在")
	}
	var art *store.PatroniReleaseArtifact
	for i := range rel.Artifacts {
		if rel.Artifacts[i].Role == role {
			art = &rel.Artifacts[i]
			break
		}
	}
	if art == nil {
		return "", "", "", "", fmt.Errorf("版本缺少 role=%s 制品", role)
	}
	bind, err := h.Store.GetPatroniOSSBind()
	if err != nil {
		return "", "", "", "", err
	}
	if strings.TrimSpace(bind.OSSSettingID) == "" {
		return "", "", "", "", fmt.Errorf("请先绑定配置中心 OSS（/ha/patroni/oss）")
	}
	plain, err := h.Store.GetOSSSettingPlain(bind.OSSSettingID)
	if err != nil || plain == nil {
		return "", "", "", "", fmt.Errorf("配置中心 OSS 不存在")
	}
	sk, err := secret.Decrypt(h.Secret, plain.SecretEnc)
	if err != nil {
		return "", "", "", "", err
	}
	key := strings.TrimSpace(art.ObjectKey)
	if key == "" {
		return "", "", "", "", fmt.Errorf("object_key 为空")
	}
	if expire <= 0 {
		expire = 2 * time.Hour
	}
	urlStr, err = srcluster.PresignGETURL(plain.Endpoint, plain.Bucket, key, plain.AccessKey, sk, expire)
	if err != nil {
		return "", "", "", "", err
	}
	return urlStr, key, art.SHA256, art.PackFormat, nil
}

func (h *HAHandler) InstallPatroniPackages(c *gin.Context) {
	cluster, auth, err := h.patroniClusterAuth(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	var req struct {
		ReleaseID string `json:"release_id"`
		SSHUser   string `json:"ssh_user"`
		SSHPort   int    `json:"ssh_port"`
		AgentURL  string `json:"agent_url"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.ReleaseID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "release_id 必填"})
		return
	}
	// Patroni 控制器 Base URL 多为 REST:8008；代装须走平台 Agent
	if u := strings.TrimSpace(req.AgentURL); u != "" {
		auth.BaseURL = u
	} else if strings.HasPrefix(strings.TrimSpace(auth.AppConf), "{") {
		var m map[string]any
		if json.Unmarshal([]byte(auth.AppConf), &m) == nil {
			if v, ok := m["package_agent_url"].(string); ok && strings.TrimSpace(v) != "" {
				auth.BaseURL = strings.TrimSpace(v)
			} else if v, ok := m["agent_url"].(string); ok && strings.TrimSpace(v) != "" {
				auth.BaseURL = strings.TrimSpace(v)
			}
		}
	}
	rel, err := h.Store.GetPatroniRelease(req.ReleaseID)
	if err != nil || rel == nil || !rel.Enabled {
		c.JSON(http.StatusBadRequest, gin.H{"message": "版本不存在或未启用"})
		return
	}
	sshUser := strings.TrimSpace(req.SSHUser)
	if sshUser == "" {
		sshUser = "postgres"
	}
	sshPort := req.SSHPort
	if sshPort <= 0 {
		sshPort = 22
	}
	run := &store.PatroniInstallRun{
		ClusterID: cluster.ID, ReleaseID: rel.ID, Status: "running", CreatedBy: middleware.Username(c),
	}
	_ = h.Store.CreatePatroniInstallRun(run)
	var logBuf strings.Builder
	appendLog := func(s string) { logBuf.WriteString(s); logBuf.WriteByte('\n') }
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Minute)
	defer cancel()

	urlStr, _, sum, pf, perr := h.presignPatroniArtifact(rel.ID, "patroni", 2*time.Hour)
	if perr != nil {
		msg := "presign patroni: " + perr.Error()
		appendLog(msg)
		_ = h.Store.UpdatePatroniInstallRun(run.ID, "failed", logBuf.String())
		c.JSON(http.StatusBadRequest, gin.H{"message": msg, "data": gin.H{"run_id": run.ID, "log": logBuf.String()}})
		return
	}
	var hosts []string
	for _, m := range cluster.Members {
		if !m.Enabled {
			continue
		}
		inst, _ := resolveInstanceInTenant(h.Store, c, m.InstanceID)
		if inst == nil {
			continue
		}
		host := strings.TrimSpace(inst.Host)
		if host == "" {
			host = strings.TrimSpace(inst.DomainName)
		}
		if host != "" {
			hosts = append(hosts, host)
		}
	}
	targets := []string{"local"}
	if len(hosts) > 0 {
		targets = append(targets, hosts...)
	}
	appendLog("install patroni targets=" + strings.Join(targets, ","))
	jobID, ierr := ha.PatroniAgentPackageInstall(ctx, auth, map[string]any{
		"download_url": urlStr, "sha256": sum, "pack_format": pf, "role": "patroni",
		"targets": targets, "ssh_user": sshUser, "ssh_port": sshPort,
	})
	if ierr != nil {
		appendLog(ierr.Error())
		_ = h.Store.UpdatePatroniInstallRun(run.ID, "failed", logBuf.String())
		c.JSON(http.StatusBadRequest, gin.H{"message": ierr.Error(), "data": gin.H{"run_id": run.ID, "log": logBuf.String()}})
		return
	}
	_, jerr := h.waitPatroniPkgJob(ctx, auth, jobID)
	if jerr != nil {
		appendLog(jerr.Error())
		_ = h.Store.UpdatePatroniInstallRun(run.ID, "failed", logBuf.String())
		c.JSON(http.StatusBadRequest, gin.H{"message": jerr.Error(), "data": gin.H{"run_id": run.ID, "log": logBuf.String()}})
		return
	}


	appendLog("install finished")
	_ = h.Store.UpdatePatroniInstallRun(run.ID, "success", logBuf.String())
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"run_id": run.ID, "status": "success", "log": logBuf.String()}})
}

func (h *HAHandler) ListPatroniInstallRuns(c *gin.Context) {
	list, err := h.Store.ListPatroniInstallRuns(c.Param("id"), 20)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *HAHandler) waitPatroniPkgJob(ctx context.Context, auth ha.ControllerAuth, jobID string) (map[string]any, error) {
	deadline := time.Now().Add(4 * time.Minute)
	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("装包任务超时 job=%s", jobID)
		}
		out, err := ha.PatroniAgentPackageJob(ctx, auth, jobID)
		if err != nil {
			return nil, err
		}
		job := out
		if nested, ok := out["job"].(map[string]any); ok {
			job = nested
		}
		st, _ := job["status"].(string)
		switch strings.ToLower(st) {
		case "success", "ok":
			return job, nil
		case "failed", "error":
			msg, _ := job["message"].(string)
			return job, fmt.Errorf("%s", firstNonEmpty(msg, "装包失败"))
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}
