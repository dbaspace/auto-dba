package srcluster

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"auto-dba/internal/store"
)

// 数据目录类配置键：草稿/平台模板禁止出现赋值（下发时由 MaterializeConf 追加）。
var lockedDataDirKeyRe = regexp.MustCompile(`(?im)^\s*(meta_dir|storage_root_path|starlet_star_rocks_home|starlet_cache_dir)\s*=`)

// RejectLockedDataDirKeys 若草稿尝试改写数据路径类键则报错。
func RejectLockedDataDirKeys(content string) error {
	if lockedDataDirKeyRe.MatchString(content) {
		return fmt.Errorf("禁止在配置草稿中写数据目录类项（meta_dir / storage_root_path 等）；路径由平台按 data_root 锁定追加，迁盘请走独立流程")
	}
	return nil
}

func shortContentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:8])
}

// ApplyConfToNode 渲染有效模板并经 Agent file/put 写入 conf/{role}.conf。
func (e *Engine) ApplyConfToNode(ctx context.Context, cl *store.SrCluster, n *store.SrNode, changeID, stepID string) (string, error) {
	if e == nil {
		return "", fmt.Errorf("engine 未初始化")
	}
	remotePath, content, source, err := MaterializeConf(e.Store, cl, n)
	if err != nil {
		return "", err
	}
	auth, err := e.agentAuth(n.AgentID)
	if err != nil {
		return "", err
	}
	soft := strings.TrimSpace(n.SoftwarePath)
	role := strings.ToLower(n.Component)
	if soft == "" && cl != nil {
		soft = softwareBaseForRole(cl, role) + "/" + role
	}
	confDir := filepath.ToSlash(filepath.Join(soft, "conf"))
	vars := BuildConfVars(cl, n)
	needDirs := []string{confDir}
	if role == "be" {
		if vars.StorageRoot != "" {
			needDirs = append(needDirs, vars.StorageRoot)
		}
		if vars.LogDir != "" {
			needDirs = append(needDirs, vars.LogDir)
		}
	} else if role == "fe" {
		if vars.MetaDir != "" {
			needDirs = append(needDirs, vars.MetaDir)
		}
		if vars.LogDir != "" {
			needDirs = append(needDirs, vars.LogDir)
		}
	}
	if _, err := e.Agent.EnsureFS(ctx, auth, EnsureFSReq{Paths: needDirs, ChangeID: changeID, StepID: stepID}); err != nil {
		return "", err
	}
	if e.Cfg.DryRun {
		return fmt.Sprintf("dry-run: file/put %s (source=%s)", remotePath, source), nil
	}
	resp, err := e.Agent.FilePut(ctx, auth, FilePutReq{
		Path:     remotePath,
		Content:  content,
		Mode:     "0644",
		ChangeID: changeID,
		StepID:   stepID,
		Backup:   true,
	})
	if err != nil {
		return "", err
	}
	e.recordApplyRevisions(cl, n, changeID, content, source, resp)
	msg := fmt.Sprintf("%s (source=%s hash=%s)", remotePath, source, shortContentHash(content))
	if resp != nil && resp.Message != "" {
		msg += "; " + resp.Message
	}
	if resp != nil && !resp.PreviousExists {
		msg += "; 未取得下发前原文件（首次下发或 Agent 版本过低）"
	}
	return msg, nil
}

func (e *Engine) stepPutConf(ctx context.Context, cl *store.SrCluster, n *store.SrNode, step *store.SrChangeStep) error {
	msg, err := e.ApplyConfToNode(ctx, cl, n, step.ChangeID, step.ID)
	if err != nil {
		return err
	}
	step.LogText = msg
	return nil
}
