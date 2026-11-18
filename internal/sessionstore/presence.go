package sessionstore

import (
	"strings"
	"time"
)

const (
	// PresenceActiveWindow 最近操作落在这个窗口内视为使用中。
	PresenceActiveWindow = 15 * time.Minute
	// PresenceTouchGap 同一会话两次写入活动时间的最短间隔。
	PresenceTouchGap = 60 * time.Second
)

// ResolvePresence 比较页面活动时间和 PAT 最近调用，取较新的一个。
// 落在 PresenceActiveWindow 内为 active，否则为 away。都没有时间时 away。
func ResolvePresence(pageAt, patAt string, now time.Time) (presence, at, source string) {
	pageT, pageOK := parsePresenceTime(pageAt)
	patT, patOK := parsePresenceTime(patAt)
	var chosen time.Time
	switch {
	case pageOK && patOK:
		if !patT.Before(pageT) {
			chosen, source = patT, "pat"
		} else {
			chosen, source = pageT, "page"
		}
	case pageOK:
		chosen, source = pageT, "page"
	case patOK:
		chosen, source = patT, "pat"
	default:
		return "away", "", ""
	}
	at = chosen.UTC().Format(time.RFC3339)
	if !chosen.Before(now.Add(-PresenceActiveWindow)) {
		return "active", at, source
	}
	return "away", at, source
}

func parsePresenceTime(v string) (time.Time, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}
