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

// --- MHA 版本 / OSS ---

func (h *HAHandler) ListMHAReleases(c *gin.Context) {
	list, err := h.Store.ListMHAReleases()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *HAHandler) CreateMHARelease(c *gin.Context) {
	var r store.MHARelease
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
	if err := h.Store.UpsertMHARelease(&r); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": r})
}

func (h *HAHandler) UpdateMHARelease(c *gin.Context) {
	exist, err := h.Store.GetMHARelease(c.Param("id"))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "版本不存在"})
		return
	}
	var req store.MHARelease
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
	if err := h.Store.UpsertMHARelease(exist); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": exist})
}

func (h *HAHandler) DeleteMHARelease(c *gin.Context) {
	if err := h.Store.DeleteMHARelease(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *HAHandler) UpsertMHAReleaseArtifact(c *gin.Context) {
	rel, err := h.Store.GetMHARelease(c.Param("id"))
	if err != nil || rel == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "版本不存在"})
		return
	}
	var a store.MHAReleaseArtifact
	if err := c.ShouldBindJSON(&a); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	a.ReleaseID = rel.ID
	if strings.TrimSpace(a.ObjectKey) == "" || strings.TrimSpace(a.SHA256) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "object_key 与 sha256 必填"})
		return
	}
	if err := h.Store.UpsertMHAReleaseArtifact(&a); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": a})
}

func (h *HAHandler) GetMHAOSS(c *gin.Context) {
	b, err := h.Store.GetMHAOSSBind()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": b})
}

func (h *HAHandler) PutMHAOSS(c *gin.Context) {
	var b store.MHAOSSBind
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	b.UpdatedBy = middleware.Username(c)
	if err := h.Store.PutMHAOSSBind(&b); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	out, _ := h.Store.GetMHAOSSBind()
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (h *HAHandler) PresignMHARelease(c *gin.Context) {
	role := strings.ToLower(strings.TrimSpace(c.Query("role")))
	if role == "" {
		role = "manager"
	}
	urlStr, key, sum, pf, err := h.presignMHAArtifact(c.Param("id"), role, 2*time.Hour)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"url": urlStr, "object_key": key, "sha256": sum, "pack_format": pf, "role": role,
	}})
}

func (h *HAHandler) presignMHAArtifact(releaseID, role string, expire time.Duration) (urlStr, objectKey, sha256, packFormat string, err error) {
	rel, err := h.Store.GetMHARelease(releaseID)
	if err != nil {
		return "", "", "", "", err
	}
	if rel == nil {
		return "", "", "", "", fmt.Errorf("版本不存在")
	}
	var art *store.MHAReleaseArtifact
	for i := range rel.Artifacts {
		if rel.Artifacts[i].Role == role {
			art = &rel.Artifacts[i]
			break
		}
	}
	if art == nil {
		return "", "", "", "", fmt.Errorf("版本缺少 role=%s 制品", role)
	}
	bind, err := h.Store.GetMHAOSSBind()
	if err != nil {
		return "", "", "", "", err
	}
	if strings.TrimSpace(bind.OSSSettingID) == "" {
		return "", "", "", "", fmt.Errorf("请先绑定配置中心 OSS（/ha/mha/oss）")
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

// --- Manager 启停 ---

func (h *HAHandler) mhaClusterAuth(c *gin.Context) (*store.HACluster, ha.ControllerAuth, error) {
	cluster, err := h.Store.GetHAClusterInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || cluster == nil {
		return nil, ha.ControllerAuth{}, fmt.Errorf("集群不存在")
	}
	if !strings.EqualFold(cluster.Engine, "mysql") || !strings.EqualFold(cluster.Mode, "mha") {
		return nil, ha.ControllerAuth{}, fmt.Errorf("仅 MySQL · MHA 支持")
	}
	if strings.TrimSpace(cluster.ControllerID) == "" {
		return nil, ha.ControllerAuth{}, fmt.Errorf("请先绑定 MHA 控制器")
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

func (h *HAHandler) GetMHAManagerStatus(c *gin.Context) {
	_, auth, err := h.mhaClusterAuth(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	st, err := ha.MHAAgentManagerCtrl(ctx, auth, "status", "")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error(), "data": st})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": st})
}

func (h *HAHandler) MHAManagerCtrl(c *gin.Context) {
	action := strings.ToLower(c.Param("action"))
	if action != "start" && action != "stop" && action != "restart" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "action 须为 start|stop|restart"})
		return
	}
	_, auth, err := h.mhaClusterAuth(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	var req struct {
		Conf string `json:"conf"`
	}
	_ = c.ShouldBindJSON(&req)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()
	st, err := ha.MHAAgentManagerCtrl(ctx, auth, action, req.Conf)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error(), "data": st})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": st})
}

// --- 装包 ---

func (h *HAHandler) InstallMHAPackages(c *gin.Context) {
	cluster, auth, err := h.mhaClusterAuth(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	var req struct {
		ReleaseID string `json:"release_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.ReleaseID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "release_id 必填"})
		return
	}
	rel, err := h.Store.GetMHARelease(req.ReleaseID)
	if err != nil || rel == nil || !rel.Enabled {
		c.JSON(http.StatusBadRequest, gin.H{"message": "版本不存在或未启用"})
		return
	}

	sshUser := "root"
	sshPort := 22
	if saved, _ := h.Store.GetHAMHAConf(cluster.ID); saved != nil && strings.TrimSpace(saved.OptionsJSON) != "" {
		opts := ha.DefaultMHAConfOptions(cluster.Name)
		_ = jsonUnmarshalLoose(saved.OptionsJSON, &opts)
		if strings.TrimSpace(opts.SSHUser) != "" {
			sshUser = opts.SSHUser
		}
	}

	run := &store.MHAInstallRun{
		ClusterID: cluster.ID,
		ReleaseID: rel.ID,
		Status:    "running",
		CreatedBy: middleware.Username(c),
	}
	_ = h.Store.CreateMHAInstallRun(run)

	var logBuf strings.Builder
	appendLog := func(s string) { logBuf.WriteString(s); logBuf.WriteByte('\n') }

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Minute)
	defer cancel()

	// 1) Manager 主机：node + manager
	for _, role := range []string{"node", "manager"} {
		urlStr, _, sum, pf, perr := h.presignMHAArtifact(rel.ID, role, 2*time.Hour)
		if perr != nil {
			if role == "manager" {
				msg := "presign " + role + ": " + perr.Error()
				appendLog(msg)
				_ = h.Store.UpdateMHAInstallRun(run.ID, "failed", logBuf.String())
				c.JSON(http.StatusBadRequest, gin.H{"message": msg, "data": gin.H{"run_id": run.ID, "log": logBuf.String()}})
				return
			}
			appendLog("skip role=" + role + ": " + perr.Error())
			continue
		}
		appendLog("install local role=" + role)
		jobID, ierr := ha.MHAAgentPackageInstall(ctx, auth, map[string]any{
			"download_url": urlStr,
			"sha256":       sum,
			"pack_format":  pf,
			"role":         role,
			"targets":      []string{"local"},
			"ssh_user":     sshUser,
			"ssh_port":     sshPort,
		})
		if ierr != nil {
			appendLog("job error: " + ierr.Error())
			_ = h.Store.UpdateMHAInstallRun(run.ID, "failed", logBuf.String())
			c.JSON(http.StatusBadRequest, gin.H{"message": ierr.Error(), "data": gin.H{"run_id": run.ID, "log": logBuf.String()}})
			return
		}
		job, jerr := h.waitMHAPkgJob(ctx, auth, jobID)
		appendLog(fmt.Sprintf("job=%s status=%v", jobID, job["status"]))
		if jerr != nil {
			appendLog(jerr.Error())
			_ = h.Store.UpdateMHAInstallRun(run.ID, "failed", logBuf.String())
			c.JSON(http.StatusBadRequest, gin.H{"message": jerr.Error(), "data": gin.H{"run_id": run.ID, "log": logBuf.String()}})
			return
		}
	}

	// 2) 成员节点：仅 node 包，经 Manager SSH
	urlNode, _, sumNode, pfNode, nerr := h.presignMHAArtifact(rel.ID, "node", 2*time.Hour)
	if nerr != nil {
		appendLog("members skip: " + nerr.Error())
	} else {
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
			appendLog("install node on members: " + strings.Join(hosts, ","))
			jobID, ierr := ha.MHAAgentPackageInstall(ctx, auth, map[string]any{
				"download_url": urlNode,
				"sha256":       sumNode,
				"pack_format":  pfNode,
				"role":         "node",
				"targets":      hosts,
				"ssh_user":     sshUser,
				"ssh_port":     sshPort,
			})
			if ierr != nil {
				appendLog("members job error: " + ierr.Error())
				_ = h.Store.UpdateMHAInstallRun(run.ID, "failed", logBuf.String())
				c.JSON(http.StatusBadRequest, gin.H{"message": ierr.Error(), "data": gin.H{"run_id": run.ID, "log": logBuf.String()}})
				return
			}
			job, jerr := h.waitMHAPkgJob(ctx, auth, jobID)
			appendLog(fmt.Sprintf("members job=%s status=%v", jobID, job["status"]))
			if jerr != nil {
				appendLog(jerr.Error())
				_ = h.Store.UpdateMHAInstallRun(run.ID, "failed", logBuf.String())
				c.JSON(http.StatusBadRequest, gin.H{"message": jerr.Error(), "data": gin.H{"run_id": run.ID, "log": logBuf.String()}})
				return
			}
		}
	}

	appendLog("install finished")
	_ = h.Store.UpdateMHAInstallRun(run.ID, "success", logBuf.String())
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"run_id": run.ID, "status": "success", "log": logBuf.String()}})
}

func (h *HAHandler) ListMHAInstallRuns(c *gin.Context) {
	list, err := h.Store.ListMHAInstallRuns(c.Param("id"), 20)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *HAHandler) waitMHAPkgJob(ctx context.Context, auth ha.ControllerAuth, jobID string) (map[string]any, error) {
	deadline := time.Now().Add(4 * time.Minute)
	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("装包任务超时 job=%s", jobID)
		}
		out, err := ha.MHAAgentPackageJob(ctx, auth, jobID)
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

func jsonUnmarshalLoose(s string, v any) error {
	return json.Unmarshal([]byte(s), v)
}
