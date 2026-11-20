package slowreport

import (
	"context"
	"strings"
	"time"

	"auto-dba/internal/mail"
	"auto-dba/internal/store"
)

// Deliver 生成 HTML。fireKey 非空时记入定时槽；手动发送传空。
func Deliver(ctx context.Context, db *store.DB, rule store.SlowReport, trigger, fireKey string, now time.Time, cpu CPUFunc, cfg *store.SMTPConfig) error {
	scope := Resolve(db, rule)
	var doc Document
	var buildErr error
	if scope.Err != "" {
		doc = Document{Title: ruleTitle(rule), Note: scope.Err + "。业务线：" + emptyDash(scope.TenantLabel)}
		buildErr = errString(scope.Err)
	} else {
		doc, buildErr = Build(ctx, db, rule, scope, now, cpu)
	}
	body := ""
	if buildErr == nil || scope.Err != "" {
		body = RenderHTML(doc)
	}
	sentAt := now.UTC().Format(time.RFC3339)
	subject := ruleTitle(rule) + " · " + emptyDash(scope.EnvLabel)
	errMsg := ""
	ok := false
	if buildErr != nil && scope.Err == "" {
		errMsg = buildErr.Error()
	} else if scope.Err != "" {
		errMsg = scope.Err
	} else if cfg == nil || !cfg.Enabled || !cfg.Ready() {
		errMsg = "邮件服务未启用或未配置完整"
	} else {
		tos := splitRecipients(rule.Recipients)
		if len(tos) == 0 {
			errMsg = "没有收件人"
		} else {
			mc := mail.Config{Host: cfg.Host, Port: cfg.Port, Username: cfg.Username, Password: cfg.Password, From: cfg.From, UseTLS: cfg.UseTLS, AuthWay: cfg.AuthWay, Rate: cfg.Rate}
			note := "慢 SQL 周报已作为 HTML 附件发送，请下载后用浏览器打开。"
			var fails []string
			for _, to := range tos {
				if err := mail.SendHTMLAttachment(mc, to, subject, note, ruleTitle(rule)+".html", body); err != nil {
					fails = append(fails, to+": "+err.Error())
				}
			}
			if len(fails) == 0 {
				ok = true
			} else {
				errMsg = strings.Join(fails, "; ")
			}
		}
	}
	mailRow := &store.SlowReportMail{
		ReportID: rule.ID, SentAt: sentAt, Trigger: trigger,
		EnvLabel: scope.EnvLabel, TenantLabel: scope.TenantLabel,
		Recipients: rule.Recipients, Subject: subject, HTML: body, OK: ok, Error: errMsg,
	}
	if err := db.InsertSlowReportMail(mailRow); err != nil {
		return err
	}
	key := fireKey
	if buildErr != nil && scope.Err == "" {
		key = ""
	}
	if err := db.TouchSlowReportSend(rule.ID, key, sentAt, errMsg); err != nil {
		return err
	}
	_ = db.PurgeSlowReportMailsBefore(now.AddDate(0, 0, -180).UTC().Format(time.RFC3339))
	return nil
}

func Scan(ctx context.Context, db *store.DB, encKey string, now time.Time, cpu CPUFunc) error {
	rules, err := db.ListSlowReports()
	if err != nil {
		return err
	}
	cfg, _ := db.GetSMTPConfig(encKey)
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		if err := Deliver(ctx, db, rule, "cron", "", now, cpu, cfg); err != nil {
			return err
		}
	}
	return nil
}

func ruleTitle(rule store.SlowReport) string {
	if strings.TrimSpace(rule.Name) != "" {
		return strings.TrimSpace(rule.Name)
	}
	return "慢 SQL 周报"
}

func splitRecipients(s string) []string {
	f := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '，' || r == '；'
	})
	var out []string
	seen := map[string]struct{}{}
	for _, p := range f {
		p = strings.TrimSpace(p)
		if p == "" || !strings.Contains(p, "@") {
			continue
		}
		if _, ok := seen[strings.ToLower(p)]; ok {
			continue
		}
		seen[strings.ToLower(p)] = struct{}{}
		out = append(out, p)
	}
	return out
}

type strErr string

func (e strErr) Error() string { return string(e) }

func errString(s string) error { return strErr(s) }
