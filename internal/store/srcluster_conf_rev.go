package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// SR 节点配置版本类型。
const (
	// SrConfRevBeforeApply 下发覆盖前远端上的原文件（由 Agent 回传）。
	SrConfRevBeforeApply = "before_apply"
	// SrConfRevApplied 实际下发到远端的最终内容（含平台追加的数据目录项）。
	SrConfRevApplied = "applied"
	// SrConfRevDraft 早期版本记录的节点草稿，已不再写入，查询时过滤。
	SrConfRevDraft = "draft"

	SrConfRevKeepPerNode = 100
)

// SrConfRevision 节点 fe.conf / be.conf 的一个历史版本。
type SrConfRevision struct {
	ID          string `json:"id"`
	ClusterID   string `json:"cluster_id"`
	NodeID      string `json:"node_id"`
	Role        string `json:"role"`
	Kind        string `json:"kind"`
	Content     string `json:"content,omitempty"`
	ContentHash string `json:"content_hash"`
	Source      string `json:"source"`
	ChangeID    string `json:"change_id"`
	Note        string `json:"note"`
	CreatedBy   string `json:"created_by"`
	CreatedAt   string `json:"created_at"`
}

const srConfRevCols = `id, cluster_id, node_id, role, kind, content, content_hash, source, change_id, note, created_by, created_at`

// SrConfContentHash 内容指纹（sha256 前 16 位十六进制）。
func SrConfContentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:8])
}

func validSrConfRevKind(k string) bool {
	switch k {
	case SrConfRevBeforeApply, SrConfRevApplied:
		return true
	}
	return false
}

// srConfRevNow 定宽微秒时间戳：同一秒内先后写入的 before_apply / applied 也能按字典序排出先后。
func srConfRevNow() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000000Z07:00")
}

func scanSrConfRevision(sc interface{ Scan(dest ...any) error }) (*SrConfRevision, error) {
	var r SrConfRevision
	if err := sc.Scan(&r.ID, &r.ClusterID, &r.NodeID, &r.Role, &r.Kind, &r.Content, &r.ContentHash,
		&r.Source, &r.ChangeID, &r.Note, &r.CreatedBy, &r.CreatedAt); err != nil {
		return nil, err
	}
	return &r, nil
}

// InsertSrConfRevision 写入一个版本并按节点保留最近 keep 个（keep<=0 用默认值）。
func (d *DB) InsertSrConfRevision(r *SrConfRevision, keep int) error {
	if r == nil || strings.TrimSpace(r.NodeID) == "" {
		return fmt.Errorf("node_id 必填")
	}
	if !validSrConfRevKind(r.Kind) {
		return fmt.Errorf("非法版本类型 %q", r.Kind)
	}
	if keep <= 0 {
		keep = SrConfRevKeepPerNode
	}
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	r.Role = strings.ToLower(strings.TrimSpace(r.Role))
	r.ContentHash = SrConfContentHash(r.Content)
	r.CreatedAt = srConfRevNow()
	if _, err := d.Exec(`INSERT INTO tbl_sr_conf_revisions (`+srConfRevCols+`)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.ClusterID, r.NodeID, r.Role, r.Kind, r.Content, r.ContentHash,
		r.Source, r.ChangeID, r.Note, r.CreatedBy, r.CreatedAt); err != nil {
		return err
	}
	_, err := d.Exec(`DELETE FROM tbl_sr_conf_revisions WHERE node_id=? AND created_at < (
		SELECT created_at FROM tbl_sr_conf_revisions WHERE node_id=? ORDER BY created_at DESC LIMIT 1 OFFSET ?)`,
		r.NodeID, r.NodeID, keep-1)
	return err
}

// LatestSrConfRevision 节点某类型的最新版本；无则 nil。
func (d *DB) LatestSrConfRevision(nodeID, kind string) (*SrConfRevision, error) {
	r, err := scanSrConfRevision(d.QueryRow(`SELECT `+srConfRevCols+` FROM tbl_sr_conf_revisions
		WHERE node_id=? AND kind=? ORDER BY created_at DESC, id DESC LIMIT 1`, nodeID, kind))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return r, err
}

// ListSrConfApplyRevisions 节点下发相关版本（before_apply / applied，含正文，新→旧）；历史草稿行被过滤。
func (d *DB) ListSrConfApplyRevisions(nodeID string, limit int) ([]SrConfRevision, error) {
	if limit <= 0 || limit > SrConfRevKeepPerNode {
		limit = SrConfRevKeepPerNode
	}
	rows, err := d.Query(`SELECT `+srConfRevCols+` FROM tbl_sr_conf_revisions
		WHERE node_id=? AND kind IN (?, ?) ORDER BY created_at DESC, id DESC LIMIT ?`,
		nodeID, SrConfRevBeforeApply, SrConfRevApplied, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SrConfRevision{}
	for rows.Next() {
		r, err := scanSrConfRevision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// GetSrConfRevision 按 id 取版本（含正文）；不存在返回 nil。
func (d *DB) GetSrConfRevision(id string) (*SrConfRevision, error) {
	r, err := scanSrConfRevision(d.QueryRow(`SELECT `+srConfRevCols+` FROM tbl_sr_conf_revisions WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return r, err
}

// ListSrConfRevisionsByChange 某次变更产生的全部版本（含正文）。
func (d *DB) ListSrConfRevisionsByChange(changeID string) ([]SrConfRevision, error) {
	rows, err := d.Query(`SELECT `+srConfRevCols+` FROM tbl_sr_conf_revisions
		WHERE change_id=? ORDER BY created_at, id`, changeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SrConfRevision{}
	for rows.Next() {
		r, err := scanSrConfRevision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

func (d *DB) DeleteSrConfRevisionsByNode(nodeID string) error {
	_, err := d.Exec(`DELETE FROM tbl_sr_conf_revisions WHERE node_id=?`, nodeID)
	return err
}

func (d *DB) DeleteSrConfRevisionsByCluster(clusterID string) error {
	_, err := d.Exec(`DELETE FROM tbl_sr_conf_revisions WHERE cluster_id=?`, clusterID)
	return err
}
