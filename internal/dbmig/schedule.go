package dbmig

import (
	"strings"
	"time"
)

// ScheduleDueAction 判断到期任务是启动、继续等待，还是已超出容忍窗口。
func ScheduleDueAction(scheduledAt string, now time.Time, toleranceMin int) string {
	if toleranceMin <= 0 {
		toleranceMin = 10
	}
	at, err := time.Parse(time.RFC3339, strings.TrimSpace(scheduledAt))
	if err != nil {
		return "skip"
	}
	if now.Before(at) {
		return "wait"
	}
	if now.Sub(at) > time.Duration(toleranceMin)*time.Minute {
		return "missed"
	}
	return "start"
}
