package srcluster

import (
	"log"
	"strings"

	"auto-dba/internal/store"
)

// recordConfRevision 记录版本失败只打日志：版本表缺失（DDL 未执行）不应阻断保存 / 下发。
func recordConfRevision(st *store.DB, r *store.SrConfRevision) {
	if st == nil || r == nil {
		return
	}
	if err := st.InsertSrConfRevision(r, store.SrConfRevKeepPerNode); err != nil {
		log.Printf("srcluster: 记录配置版本失败 node=%s kind=%s: %v", r.NodeID, r.Kind, err)
	}
}

// recordApplyRevisions 下发成功后记录「下发前远端原文件」与「实际下发内容」。
func (e *Engine) recordApplyRevisions(cl *store.SrCluster, n *store.SrNode, changeID, content, source string, resp *FilePutResp) {
	operator := ""
	if ch, err := e.Store.GetSrChange(changeID); err == nil && ch != nil {
		operator = ch.CreatedBy
	}
	clusterID := ""
	if cl != nil {
		clusterID = cl.ID
	}
	if resp != nil && resp.PreviousExists {
		recordConfRevision(e.Store, &store.SrConfRevision{
			ClusterID: clusterID, NodeID: n.ID, Role: n.Component, Kind: store.SrConfRevBeforeApply,
			Content: resp.PreviousContent, Source: "remote", ChangeID: changeID,
			Note: strings.TrimSpace("远端备份 " + resp.BackupPath), CreatedBy: operator,
		})
	}
	recordConfRevision(e.Store, &store.SrConfRevision{
		ClusterID: clusterID, NodeID: n.ID, Role: n.Component, Kind: store.SrConfRevApplied,
		Content: content, Source: source, ChangeID: changeID, CreatedBy: operator,
	})
}
