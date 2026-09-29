package handler

import (
	"strings"

	"auto-dba/internal/ticketexec"
)

// ddlDoneSource 上次执行中某数据源的结果；重试时原样带入，保证再次重试仍能识别。
func ddlDoneSource(prev *ticketexec.Result, instanceID, dbName string, stmtN int) *ticketexec.Result {
	if prev == nil || stmtN <= 0 {
		return nil
	}
	var recs []ticketexec.Record
	for _, r := range prev.Records {
		if strings.TrimSpace(r.InstanceID) != instanceID {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(r.Status), "failed") {
			return nil
		}
		recs = append(recs, r)
	}
	if len(recs) == 0 {
		return nil
	}
	host := strings.TrimSpace(recs[0].Host)
	var stat *ticketexec.TargetStat
	for i := range prev.TargetStats {
		st := &prev.TargetStats[i]
		if strings.TrimSpace(st.Host) == host && strings.TrimSpace(st.DB) == strings.TrimSpace(dbName) {
			stat = st
			break
		}
	}
	success := 0
	if stat != nil {
		if stat.Failed > 0 {
			return nil
		}
		success = stat.Success
	} else {
		for _, r := range recs {
			if strings.EqualFold(strings.TrimSpace(r.Status), "success") && !strings.HasPrefix(r.SQL, ticketexec.CleanupRecordPrefix) {
				success++
			}
		}
	}
	// 必须每条语句都成功；部分成功的库仍从第 1 条重新执行
	if success != stmtN {
		return nil
	}
	carried := &ticketexec.Result{Success: true, Records: recs, SuccessCount: stmtN, Compacted: prev.Compacted}
	if stat != nil {
		carried.TargetStats = []ticketexec.TargetStat{*stat}
	}
	return carried
}
