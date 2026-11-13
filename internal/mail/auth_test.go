package mail

import (
	"net"
	"net/smtp"
	"strconv"
	"strings"
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

func TestSendTimesOutWhenSMTPHangs(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				<-done
				_ = c.Close()
			}(c)
		}
	}()
	t.Cleanup(func() {
		close(done)
		_ = ln.Close()
	})
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	p, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	oldDial, oldSend := smtpDialTimeout, smtpSendTimeout
	smtpDialTimeout = 200 * time.Millisecond
	smtpSendTimeout = 300 * time.Millisecond
	t.Cleanup(func() {
		smtpDialTimeout = oldDial
		smtpSendTimeout = oldSend
	})
	nextSend = time.Time{}
	start := time.Now()
	err = Send(Config{
		Host: "127.0.0.1",
		Port: p,
		From: "from@example.com",
		Rate: 1000,
	}, "to@example.com", "subject", "body", "")
	if err == nil || !strings.Contains(err.Error(), "SMTP 超时") {
		t.Fatalf("err=%v elapsed=%s", err, time.Since(start))
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("hung SMTP should time out quickly, took %s", time.Since(start))
	}
}
