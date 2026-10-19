package mail

import (
	"net/smtp"
	"testing"
	"time"
)

func TestLoginAuthChallenges(t *testing.T) {
	a := &loginAuth{username: "u@example.com", password: "secret", host: "smtp.example.com"}
	mech, init, err := a.Start(&smtp.ServerInfo{Name: "smtp.example.com", TLS: true})
	if err != nil || mech != "LOGIN" || init != nil {
		t.Fatalf("start mech=%s init=%q err=%v", mech, init, err)
	}
	user, err := a.Next([]byte("Username:"), true)
	if err != nil || string(user) != "u@example.com" {
		t.Fatalf("user=%q err=%v", user, err)
	}
	pass, err := a.Next([]byte("Password:"), true)
	if err != nil || string(pass) != "secret" {
		t.Fatalf("pass=%q err=%v", pass, err)
	}
}

func TestWaitRateSpacesSends(t *testing.T) {
	nextSend = time.Time{}
	start := time.Now()
	waitRate(2)
	waitRate(2)
	if time.Since(start) < 400*time.Millisecond {
		t.Fatalf("rate 2 should space sends by about 500ms, took %s", time.Since(start))
	}
}
