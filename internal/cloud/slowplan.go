package cloud

import "time"

const (
	// SlowStep 是每个增量窗口的最长跨度。
	SlowStep = 5 * time.Minute
	// SlowOverlap 是窗口起点相对游标的重叠，用来接住落在边界上的慢 SQL。
	SlowOverlap = time.Minute
	// SlowSafetyLag 是窗口终点相对现在的延迟。云上慢日志有入库延迟，贴着现在会把晚到的几条跳过。
	SlowSafetyLag = 2 * time.Minute
	// SlowMaxWindows 是一轮里每个实例最多连续补的窗口数。每轮 30 分钟，停两小时大约 4 到 5 轮追平。
	SlowMaxWindows = 6
	// SlowAPIQPS 是每个账号加地域的云接口上限。
	SlowAPIQPS = 5
)

// SlowPull 是一轮同步要拉的窗口。Gap 表示游标早于云上还能查到的 7 天，那段补不回来。
type SlowPull struct {
	Gap     bool
	GapFrom time.Time
	GapTo   time.Time
	Windows []Window
}

// PlanSlowPull 按游标生成这一轮的窗口。没有游标只给最近 5 分钟，终点是现在减 2 分钟。
// 已有游标从游标往后，每段最多 5 分钟，一轮最多 6 段，起点重叠 1 分钟。
func PlanSlowPull(last, now time.Time) SlowPull {
	if now.IsZero() {
		now = time.Now()
	}
	safe := now.Add(-SlowSafetyLag)
	floor := now.Add(-SlowLogAPILookback)
	var plan SlowPull
	if !last.IsZero() && last.Before(floor) {
		plan.Gap = true
		plan.GapFrom = last
		plan.GapTo = floor
		last = floor
	}
	if last.IsZero() {
		from := safe.Add(-SlowStep)
		if from.Before(safe) {
			plan.Windows = []Window{{From: from, To: safe}}
		}
		return plan
	}
	cursor := last
	for len(plan.Windows) < SlowMaxWindows && cursor.Before(safe) {
		end := cursor.Add(SlowStep)
		if end.After(safe) {
			end = safe
		}
		from := cursor.Add(-SlowOverlap)
		if from.Before(floor) {
			from = floor
		}
		if !from.Before(end) {
			break
		}
		plan.Windows = append(plan.Windows, Window{From: from, To: end})
		cursor = end
	}
	return plan
}
