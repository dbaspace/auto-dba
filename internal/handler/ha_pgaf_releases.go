package handler

import (
	"context"
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

func (h *HAHandler) pgafClusterAuth(c *gin.Context) (*store.HACluster, ha.ControllerAuth, error) {
	cluster, err := h.Store.GetHAClusterInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || cluster == nil {
		return nil, ha.ControllerAuth{}, fmt.Errorf("集群不存在")
	}
	if !strings.EqualFold(cluster.Engine, "postgres") || !strings.EqualFold(cluster.Mode, "pg_autofailover") {
		return nil, ha.ControllerAuth{}, fmt.Errorf("仅 PostgreSQL · pg_autofailover 支持")
	}
	if strings.TrimSpace(cluster.ControllerID) == "" {
		return nil, ha.ControllerAuth{}, fmt.Errorf("请先绑定 pg_autofailover 控制器")
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

func (h *HAHandler) ListPGAFReleases(c *gin.Context) {
	list, err := h.Store.ListPGAFReleases()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *HAHandler) CreatePGAFRelease(c *gin.Context) {
	var r store.PGAFRelease
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
	if err := h.Store.UpsertPGAFRelease(&r); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": r})
}

func (h *HAHandler) UpdatePGAFRelease(c *gin.Context) {
	exist, err := h.Store.GetPGAFRelease(c.Param("id"))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "版本不存在"})
		return
	}
	var req store.PGAFRelease
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
	if err := h.Store.UpsertPGAFRelease(exist); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": exist})
}

func (h *HAHandler) DeletePGAFRelease(c *gin.Context) {
	if err := h.Store.DeletePGAFRelease(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *HAHandler) UpsertPGAFReleaseArtifact(c *gin.Context) {
	rel, err := h.Store.GetPGAFRelease(c.Param("id"))
	if err != nil || rel == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "版本不存在"})
		return
	}
	var a store.PGAFReleaseArtifact
	if err := c.ShouldBindJSON(&a); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	a.ReleaseID = rel.ID
	if strings.TrimSpace(a.ObjectKey) == "" || strings.TrimSpace(a.SHA256) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "object_key 与 sha256 必填"})
		return
	}
	if err := h.Store.UpsertPGAFReleaseArtifact(&a); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": a})
}

func (h *HAHandler) GetPGAFOSS(c *gin.Context) {
	b, err := h.Store.GetPGAFOSSBind()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": b})
}

func (h *HAHandler) PutPGAFOSS(c *gin.Context) {
	var b store.PGAFOSSBind
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	b.UpdatedBy = middleware.Username(c)
	if err := h.Store.PutPGAFOSSBind(&b); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	out, _ := h.Store.GetPGAFOSSBind()
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (h *HAHandler) PresignPGAFRelease(c *gin.Context) {
	role := strings.ToLower(strings.TrimSpace(c.Query("role")))
	if role == "" {
		role = "monitor"
	}
	urlStr, key, sum, pf, err := h.presignPGAFArtifact(c.Param("id"), role, 2*time.Hour)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"url": urlStr, "object_key": key, "sha256": sum, "pack_format": pf, "role": role,
	}})
}

func (h *HAHandler) presignPGAFArtifact(releaseID, role string, expire time.Duration) (urlStr, objectKey, sha256, packFormat string, err error) {
	rel, err := h.Store.GetPGAFRelease(releaseID)
	if err != nil {
		return "", "", "", "", err
	}
	if rel == nil {
		return "", "", "", "", fmt.Errorf("版本不存在")
	}
	var art *store.PGAFReleaseArtifact
	for i := range rel.Artifacts {
		if rel.Artifacts[i].Role == role {
			art = &rel.Artifacts[i]
			break
		}
	}
	if art == nil {
		return "", "", "", "", fmt.Errorf("版本缺少 role=%s 制品", role)
	}
	bind, err := h.Store.GetPGAFOSSBind()
	if err != nil {
		return "", "", "", "", err
	}
	if strings.TrimSpace(bind.OSSSettingID) == "" {
		return "", "", "", "", fmt.Errorf("请先绑定配置中心 OSS（/ha/pgaf/oss）")
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

func (h *HAHandler) InstallPGAFPackages(c *gin.Context) {
	cluster, auth, err := h.pgafClusterAuth(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	var req struct {
		ReleaseID string `json:"release_id"`
		SSHUser   string `json:"ssh_user"`
		SSHPort   int    `json:"ssh_port"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.ReleaseID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "release_id 必填"})
		return
	}
	rel, err := h.Store.GetPGAFRelease(req.ReleaseID)
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
	run := &store.PGAFInstallRun{
		ClusterID: cluster.ID, ReleaseID: rel.ID, Status: "running", CreatedBy: middleware.Username(c),
	}
	_ = h.Store.CreatePGAFInstallRun(run)
	var logBuf strings.Builder
	appendLog := func(s string) { logBuf.WriteString(s); logBuf.WriteByte('\n') }
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Minute)
	defer cancel()

	for _, role := range []string{"keeper", "monitor"} {
		urlStr, _, sum, pf, perr := h.presignPGAFArtifact(rel.ID, role, 2*time.Hour)
		if perr != nil {
			if role == "monitor" {
				msg := "presign " + role + ": " + perr.Error()
				appendLog(msg)
				_ = h.Store.UpdatePGAFInstallRun(run.ID, "failed", logBuf.String())
				c.JSON(http.StatusBadRequest, gin.H{"message": msg, "data": gin.H{"run_id": run.ID, "log": logBuf.String()}})
				return
			}
			appendLog("skip role=" + role + ": " + perr.Error())
			continue
		}
		appendLog("install local role=" + role)
		jobID, ierr := ha.PGAFAgentPackageInstall(ctx, auth, map[string]any{
			"download_url": urlStr, "sha256": sum, "pack_format": pf, "role": role,
			"targets": []string{"local"}, "ssh_user": sshUser, "ssh_port": sshPort,
		})
		if ierr != nil {
			appendLog(ierr.Error())
			_ = h.Store.UpdatePGAFInstallRun(run.ID, "failed", logBuf.String())
			c.JSON(http.StatusBadRequest, gin.H{"message": ierr.Error(), "data": gin.H{"run_id": run.ID, "log": logBuf.String()}})
			return
		}
		job, jerr := h.waitPGAFPkgJob(ctx, auth, jobID)
		appendLog(fmt.Sprintf("job=%s status=%v", jobID, job["status"]))
		if jerr != nil {
			appendLog(jerr.Error())
			_ = h.Store.UpdatePGAFInstallRun(run.ID, "failed", logBuf.String())
			c.JSON(http.StatusBadRequest, gin.H{"message": jerr.Error(), "data": gin.H{"run_id": run.ID, "log": logBuf.String()}})
			return
		}
	}

	urlKeep, _, sumKeep, pfKeep, kerr := h.presignPGAFArtifact(rel.ID, "keeper", 2*time.Hour)
	if kerr == nil {
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
		if len(hosts) > 0 {
			appendLog("install keeper on members: " + strings.Join(hosts, ","))
			jobID, ierr := ha.PGAFAgentPackageInstall(ctx, auth, map[string]any{
				"download_url": urlKeep, "sha256": sumKeep, "pack_format": pfKeep, "role": "keeper",
				"targets": hosts, "ssh_user": sshUser, "ssh_port": sshPort,
			})
			if ierr != nil {
				appendLog(ierr.Error())
				_ = h.Store.UpdatePGAFInstallRun(run.ID, "failed", logBuf.String())
				c.JSON(http.StatusBadRequest, gin.H{"message": ierr.Error(), "data": gin.H{"run_id": run.ID, "log": logBuf.String()}})
				return
			}
			_, jerr := h.waitPGAFPkgJob(ctx, auth, jobID)
			if jerr != nil {
				appendLog(jerr.Error())
				_ = h.Store.UpdatePGAFInstallRun(run.ID, "failed", logBuf.String())
				c.JSON(http.StatusBadRequest, gin.H{"message": jerr.Error(), "data": gin.H{"run_id": run.ID, "log": logBuf.String()}})
				return
			}
		}
	}

	appendLog("install finished")
	_ = h.Store.UpdatePGAFInstallRun(run.ID, "success", logBuf.String())
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"run_id": run.ID, "status": "success", "log": logBuf.String()}})
}

func (h *HAHandler) ListPGAFInstallRuns(c *gin.Context) {
	list, err := h.Store.ListPGAFInstallRuns(c.Param("id"), 20)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *HAHandler) waitPGAFPkgJob(ctx context.Context, auth ha.ControllerAuth, jobID string) (map[string]any, error) {
	deadline := time.Now().Add(4 * time.Minute)
	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("装包任务超时 job=%s", jobID)
		}
		out, err := ha.PGAFAgentPackageJob(ctx, auth, jobID)
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

// GetPatroniCluster 拉取 Patroni /cluster。
func (h *HAHandler) GetPatroniCluster(c *gin.Context) {
	cluster, err := h.Store.GetHAClusterInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || cluster == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "集群不存在"})
		return
	}
	if !strings.EqualFold(cluster.Mode, "patroni") {
		c.JSON(http.StatusBadRequest, gin.H{"message": "仅 patroni 模式支持"})
		return
	}
	if strings.TrimSpace(cluster.ControllerID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请先绑定控制器"})
		return
	}
	ctrl, err := h.Store.GetHAController(cluster.ControllerID)
	if err != nil || ctrl == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "控制器不存在"})
		return
	}
	auth, err := h.controllerAuth(ctrl)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	cl, err := ha.PatroniGetCluster(ctx, auth)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": cl})
}
