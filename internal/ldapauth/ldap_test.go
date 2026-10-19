package ldapauth

import (
	"fmt"
	"strings"
	"testing"

	"github.com/go-ldap/ldap/v3"

	"auto-dba/internal/store"
)

func TestBuildUserFilter(t *testing.T) {
	if got := BuildUserFilter("(uid=%s)", "alice"); got != "(uid=alice)" {
		t.Fatalf("got %q", got)
	}
	if got := BuildUserFilter("(cn=%(user)s)", "bob"); got != "(cn=bob)" {
		t.Fatalf("got %q", got)
	}
	if got := BuildUserFilter("", "carol"); !strings.Contains(got, "uid=carol") || !strings.Contains(got, "sAMAccountName=carol") {
		t.Fatalf("got %q", got)
	}
	if got := BuildUserFilter("(uid=%s)", "a)(uid=*"); !strings.Contains(got, "\\29") {
		t.Fatalf("should escape filter specials: %q", got)
	}
}

func TestEncodeADUnicodePwd(t *testing.T) {
	got := encodeADUnicodePwd("ab")
	if len(got) != 8 {
		t.Fatalf("len=%d want 8", len(got))
	}
	if got[0] != '"' || got[1] != 0 {
		t.Fatalf("should start with quoted UTF-16LE quote, got %#v", []byte(got)[:2])
	}
}

func TestIsLDAPProtocolError(t *testing.T) {
	err := &ldap.Error{ResultCode: ldap.LDAPResultProtocolError, Err: fmt.Errorf("Protocol Error")}
	if !isLDAPProtocolError(err) {
		t.Fatal("expected protocol error")
	}
	if isLDAPProtocolError(fmt.Errorf("other")) {
		t.Fatal("should not match")
	}
}

func TestDisplayNameFromEntryPrefersChineseOverLogin(t *testing.T) {
	entry := &ldap.Entry{
		DN: "cn=chenjiachi,dc=example,dc=com",
		Attributes: []*ldap.EntryAttribute{
			{Name: "cn", Values: []string{"chenjiachi"}},
			{Name: "displayName", Values: []string{"陈家驰"}},
			{Name: "sAMAccountName", Values: []string{"chenjiachi"}},
		},
	}
	cfg := &store.LdapConfig{DisplayNameAttr: "cn"}
	got := displayNameFromEntry(cfg, entry, "chenjiachi")
	if got != "陈家驰" {
		t.Fatalf("got %q want 陈家驰", got)
	}
}

func TestDisplayNameStripsEmployeePrefix(t *testing.T) {
	cfg := &store.LdapConfig{DisplayNameAttr: "cn"}
	entry := &ldap.Entry{Attributes: []*ldap.EntryAttribute{
		{Name: "cn", Values: []string{"1d449671赵培超"}},
		{Name: "displayName", Values: []string{"赵培超"}},
	}}
	if got := displayNameFromEntry(cfg, entry, "zhaopeichao"); got != "赵培超" {
		t.Fatalf("prefer clean displayName, got %q", got)
	}

	onlyCN := &ldap.Entry{Attributes: []*ldap.EntryAttribute{
		{Name: "cn", Values: []string{"1d449671赵培超"}},
	}}
	if got := displayNameFromEntry(cfg, onlyCN, "zhaopeichao"); got != "赵培超" {
		t.Fatalf("strip cn prefix, got %q", got)
	}

	fromSN := &ldap.Entry{Attributes: []*ldap.EntryAttribute{
		{Name: "sn", Values: []string{"1d449671"}},
		{Name: "givenName", Values: []string{"赵培超"}},
	}}
	if got := displayNameFromEntry(nil, fromSN, "zhaopeichao"); got != "赵培超" {
		t.Fatalf("do not join employee sn, got %q", got)
	}

	surname := &ldap.Entry{Attributes: []*ldap.EntryAttribute{
		{Name: "sn", Values: []string{"赵"}},
		{Name: "givenName", Values: []string{"培超"}},
	}}
	if got := displayNameFromEntry(nil, surname, "zhaopeichao"); got != "赵培超" {
		t.Fatalf("join surname, got %q", got)
	}
}

func TestDisplayNameFromEntryConfiguredAttr(t *testing.T) {
	entry := &ldap.Entry{
		Attributes: []*ldap.EntryAttribute{
			{Name: "cn", Values: []string{"账号"}},
			{Name: "displayName", Values: []string{"陈家驰"}},
		},
	}
	cfg := &store.LdapConfig{DisplayNameAttr: "displayName"}
	got := displayNameFromEntry(cfg, entry, "chenjiachi")
	if got != "陈家驰" {
		t.Fatalf("got %q", got)
	}
}
