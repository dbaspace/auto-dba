package store

import "testing"

func TestShouldShowSystemNotice(t *testing.T) {
	n := &SystemNotice{Enabled: true, Version: 2, ShowMode: SystemNoticeModeOnVersionChange}
	if !ShouldShowSystemNotice(n, 0, false) {
		t.Fatal("no ack should show")
	}
	if ShouldShowSystemNotice(n, 2, true) {
		t.Fatal("same version ack should hide")
	}
	if !ShouldShowSystemNotice(n, 1, true) {
		t.Fatal("old version ack should show after bump")
	}

	once := &SystemNotice{Enabled: true, Version: 9, ShowMode: SystemNoticeModeOnce}
	if !ShouldShowSystemNotice(once, 0, false) {
		t.Fatal("once without ack should show")
	}
	if ShouldShowSystemNotice(once, 1, true) {
		t.Fatal("once with any ack should hide")
	}

	every := &SystemNotice{Enabled: true, Version: 1, ShowMode: SystemNoticeModeEveryLogin}
	if !ShouldShowSystemNotice(every, 99, true) {
		t.Fatal("every_login always shows server-side")
	}

	if ShouldShowSystemNotice(&SystemNotice{Enabled: false, ShowMode: SystemNoticeModeEveryLogin}, 0, false) {
		t.Fatal("disabled should hide")
	}
}

func TestNormalizeSystemNoticeShowMode(t *testing.T) {
	if got := NormalizeSystemNoticeShowMode(""); got != SystemNoticeModeOnVersionChange {
		t.Fatalf("got %s", got)
	}
	if got := NormalizeSystemNoticeShowMode("EVERY_LOGIN"); got != SystemNoticeModeEveryLogin {
		t.Fatalf("got %s", got)
	}
}
