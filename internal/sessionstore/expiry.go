package sessionstore

import (
	"strings"
	"time"
)

// shanghai 登录自然日固定东八区，不跟服务器本地时区走。
var shanghai = time.FixedZone("Asia/Shanghai", 8*3600)

// ShanghaiDay 返回 t 的东八区日期，格式 2006-01-02。
func ShanghaiDay(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.In(shanghai).Format("2006-01-02")
}

func extendDayOf(extendDate, onlineAt string, anchor time.Time) string {
	if d := strings.TrimSpace(extendDate); d != "" {
		return d
	}
	if t, err := time.Parse(time.RFC3339, strings.TrimSpace(onlineAt)); err == nil {
		return ShanghaiDay(t)
	}
	if !anchor.IsZero() {
		return ShanghaiDay(anchor)
	}
	return ""
}

// VisitExtend 仍有效、且延长日不是今天时，本次访问应把过期时间延长到 now+TTL。
// extendDate 为空时用 anchor（online_at 或 JWT iat）的东八区日期。两者都空则不延长。
func VisitExtend(now, expire time.Time, extendDate string, anchor time.Time) bool {
	if expire.IsZero() || !now.Before(expire) {
		return false
	}
	day := extendDayOf(extendDate, "", anchor)
	if day == "" {
		return false
	}
	return day != ShanghaiDay(now)
}

// KeepOrFresh 登录或重签：上一窗口仍有效且延长日已是今天则沿用，否则从 now 起一个新窗口。
func KeepOrFresh(now time.Time, ttl time.Duration, prevExpire time.Time, prevExtendDate, prevOnlineAt string) (time.Time, string) {
	today := ShanghaiDay(now)
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	day := extendDayOf(prevExtendDate, prevOnlineAt, time.Time{})
	if !prevExpire.IsZero() && now.Before(prevExpire) && day == today {
		if strings.TrimSpace(prevExtendDate) != "" {
			return prevExpire, strings.TrimSpace(prevExtendDate)
		}
		return prevExpire, today
	}
	return now.Add(ttl), today
}
