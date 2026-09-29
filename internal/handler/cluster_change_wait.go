package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"auto-dba/internal/store"
	"auto-dba/internal/ticketexec"
)

// clusterChangeSnap SR / db_deploy 变更的公共视图（两边表结构一致）。
type clusterChangeSnap struct {
	Status string
	Steps  []clusterStepSnap
}

type clusterStepSnap struct {
	Seq       int
	Title     string
	Status    string
	Log       string
	StartedAt string
	EndedAt   string
}

type clusterChangeOps struct {
	load  func(id string) (*clusterChangeSnap, error)
	retry func(id string) error
}

const (
	clusterWaitInterval = 3 * time.Second
	stepErrTailLen      = 2000
)

func srChangeOps(st *store.DB, retry func(string) error) clusterChangeOps {
	return clusterChangeOps{
		retry: retry,
		load: func(id string) (*clusterChangeSnap, error) {
			ch, err := st.GetSrChange(id)
			if err != nil || ch == nil {
				return nil, err
			}
			snap := &clusterChangeSnap{Status: ch.Status}
			for _, s := range ch.Steps {
				snap.Steps = append(snap.Steps, clusterStepSnap{Seq: s.Seq, Title: s.Title, Status: s.Status, Log: s.LogText, StartedAt: s.StartedAt, EndedAt: s.EndedAt})
			}
			return snap, nil
		},
	}
}

func dbDeployChangeOps(st *store.DB, retry func(string) error) clusterChangeOps {
	return clusterChangeOps{
		retry: retry,
		load: func(id string) (*clusterChangeSnap, error) {
			ch, err := st.GetDbDeployChange(id)
			if err != nil || ch == nil {
				return nil, err
			}
			steps := ch.Steps
			if len(steps) == 0 {
				steps, _ = st.ListDbDeployChangeSteps(id)
			}
			snap := &clusterChangeSnap{Status: ch.Status}
			for _, s := range steps {
				snap.Steps = append(snap.Steps, clusterStepSnap{Seq: s.Seq, Title: s.Title, Status: s.Status, Log: s.LogText, StartedAt: s.StartedAt, EndedAt: s.EndedAt})
			}
			return snap, nil
		},
	}
}

// prevChangeID 从工单上次执行结果中取 change_id（重试时复用，避免重复发起升级/部署）。
func prevChangeID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var r struct {
		ChangeID string `json:"change_id"`
	}
	if json.Unmarshal([]byte(raw), &r) != nil {
		return ""
	}
	return strings.TrimSpace(r.ChangeID)
}

// resumeClusterChange 工单重试：上次变更失败/取消则从失败步重试，运行中/已成功则直接等待同步；
// 返回空串表示没有可复用的变更，需要新建。
func resumeClusterChange(prevID string, ops clusterChangeOps) (string, error) {
	if prevID == "" {
		return "", nil
	}
	snap, err := ops.load(prevID)
	if err != nil || snap == nil {
		return "", nil
	}
	switch snap.Status {
	case store.SrChangeFailed, store.SrChangeCancelled:
		if err := ops.retry(prevID); err != nil {
			return prevID, fmt.Errorf("重试变更 %s 失败: %w", shortChangeID(prevID), err)
		}
	}
	return prevID, nil
}

func isFinalChangeStatus(s string) bool {
	return s == store.SrChangeSuccess || s == store.SrChangeFailed || s == store.SrChangeCancelled
}

// waitClusterChange 轮询变更直至终态，把步骤写进 res；progress 在步骤状态变化时回调（用于落库进度）。
func waitClusterChange(ctx context.Context, res *ticketexec.Result, changeID, label string, ops clusterChangeOps, progress func(*ticketexec.Result)) {
	res.ChangeID = changeID
	lastSig := ""
	for {
		snap, err := ops.load(changeID)
		if err == nil && snap != nil {
			fillChangeResult(res, snap, changeID, label)
			if isFinalChangeStatus(snap.Status) {
				return
			}
			if sig := changeSignature(snap); sig != lastSig {
				lastSig = sig
				if progress != nil {
					progress(res)
				}
			}
		} else if err != nil {
			res.Summary = fmt.Sprintf("%s change_id=%s 读取失败，稍后重试: %v", label, shortChangeID(changeID), err)
		}
		select {
		case <-ctx.Done():
			res.Success = false
			res.Message = fmt.Sprintf("等待%s结束超时（变更 %s 可能仍在执行），请到集群变更页查看；变更结束后重试工单可同步结果", label, shortChangeID(changeID))
			res.EndedAt = time.Now().UTC().Format(time.RFC3339)
			return
		case <-time.After(clusterWaitInterval):
		}
	}
}

func changeSignature(snap *clusterChangeSnap) string {
	var b strings.Builder
	b.WriteString(snap.Status)
	for _, s := range snap.Steps {
		b.WriteString("|")
		b.WriteString(s.Status)
	}
	return b.String()
}

func fillChangeResult(res *ticketexec.Result, snap *clusterChangeSnap, changeID, label string) {
	res.Records = res.Records[:0]
	ok, failed := 0, 0
	var failedStep *clusterStepSnap
	for i := range snap.Steps {
		s := &snap.Steps[i]
		rec := ticketexec.Record{SQL: s.Title, Status: s.Status, StartedAt: s.StartedAt, EndedAt: s.EndedAt, Order: s.Seq}
		switch s.Status {
		case store.SrStepSuccess, store.SrStepSkipped:
			ok++
		case store.SrStepFailed:
			failed++
			rec.ErrorMessage = logTail(s.Log, stepErrTailLen)
			if failedStep == nil {
				failedStep = s
			}
		}
		res.Records = append(res.Records, rec)
	}
	res.SuccessCount, res.FailCount = ok, failed
	total := len(snap.Steps)
	short := shortChangeID(changeID)
	switch snap.Status {
	case store.SrChangeSuccess:
		res.Success = true
		res.Message = ""
		res.Summary = fmt.Sprintf("%s完成（%d/%d 步），change_id=%s", label, ok, total, short)
		res.EndedAt = time.Now().UTC().Format(time.RFC3339)
	case store.SrChangeFailed, store.SrChangeCancelled:
		res.Success = false
		verb := "失败"
		if snap.Status == store.SrChangeCancelled {
			verb = "已取消"
		}
		res.Summary = fmt.Sprintf("%s%s（成功 %d/%d 步），change_id=%s", label, verb, ok, total, short)
		if failedStep != nil {
			res.Message = fmt.Sprintf("步骤「%s」失败: %s", failedStep.Title, lastFailLine(failedStep.Log))
		} else {
			res.Message = res.Summary
		}
		res.EndedAt = time.Now().UTC().Format(time.RFC3339)
	default:
		res.Success = false
		res.Message = ""
		cur := ""
		for _, s := range snap.Steps {
			if s.Status == store.SrStepRunning {
				cur = s.Title
				break
			}
		}
		res.Summary = fmt.Sprintf("%s执行中（%d/%d 步完成），change_id=%s", label, ok, total, short)
		if cur != "" {
			res.Summary += "，当前：" + cur
		}
	}
}

func logTail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	s = s[len(s)-n:]
	for i := 0; i < len(s) && i < 4; i++ {
		// 避免从 UTF-8 多字节字符中间截断
		if s[i]&0xC0 != 0x80 {
			return "…" + s[i:]
		}
	}
	return "…" + s
}

// lastFailLine 取步骤日志里最后一条「失败:」行，没有则取最后一个非空行。
func lastFailLine(log string) string {
	lines := strings.Split(strings.TrimSpace(log), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if idx := strings.Index(lines[i], "失败: "); idx >= 0 {
			return strings.TrimSpace(lines[i][idx+len("失败: "):])
		}
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); t != "" {
			return t
		}
	}
	return ""
}

func shortChangeID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
