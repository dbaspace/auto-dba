package notify

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"auto-dba/internal/store"
)

func TestLarkReceiveIDType(t *testing.T) {
	cases := []struct {
		in, typ, id string
	}{
		{"chenjiachi", "user_id", "chenjiachi"},
		{"ab12cd34", "user_id", "ab12cd34"},
		{"a@corp.com", "email", "a@corp.com"},
		{"ou_xxx", "open_id", "ou_xxx"},
		{"on_xxx", "union_id", "on_xxx"},
		{"oc_xxx", "chat_id", "oc_xxx"},
		{"13800138000", "mobile", "13800138000"},
		{"+8613800138000", "mobile", "13800138000"},
		{"+6591234567", "mobile", "+6591234567"},
		{"1234", "user_id", "1234"},
		{"email:a@corp.com", "email", "a@corp.com"},
		{"user_id:13800138000", "user_id", "13800138000"},
		{"mobile:138-0013-8000", "mobile", "13800138000"},
		{" open_id: ou_1 ", "open_id", "ou_1"},
		{"foo:bar", "user_id", "foo:bar"},
	}
	for _, c := range cases {
		typ, id := larkReceiveIDType(c.in)
		if typ != c.typ || id != c.id {
			t.Errorf("larkReceiveIDType(%q)=(%q,%q) want (%q,%q)", c.in, typ, id, c.typ, c.id)
		}
	}
}

func TestResolveLarkRecipient(t *testing.T) {
	u := &store.User{IMType: "lark", IMAccount: "u1", Email: "a@corp.com", Phone: "13800138000"}
	got := ResolveLarkRecipient(u)
	want := []string{"u1", "email:a@corp.com", "mobile:13800138000"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v want %v", got, want)
	}
	u = &store.User{IMType: "wecom", IMAccount: "w1", Email: "bad"}
	if got := ResolveLarkRecipient(u); len(got) != 0 {
		t.Fatalf("want empty, got %v", got)
	}
	u = &store.User{IMType: "lark", IMAccount: "u1", Email: "a@corp.com", LarkEmail: "a@qq.com"}
	got = ResolveLarkRecipient(u)
	want = []string{"email:a@qq.com", "u1", "email:a@corp.com"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unsynced got %v want %v", got, want)
	}
	u.LarkID, u.LarkOpenID = "u1", "ou_1"
	got = ResolveLarkRecipient(u)
	want = []string{"open_id:ou_1", "user_id:u1", "email:a@qq.com"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("synced got %v want %v", got, want)
	}
}

func TestLarkSendFallbackAndContinue(t *testing.T) {
	var mu sync.Mutex
	var sent []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			_, _ = w.Write([]byte(`{"code":0,"tenant_access_token":"t","expire":7200}`))
		case "/open-apis/contact/v3/users/batch_get_id":
			_, _ = w.Write([]byte(`{"code":0,"data":{"user_list":[{"mobile":"13800138000","user_id":"m_uid"}]}}`))
		case "/open-apis/im/v1/messages":
			var p map[string]any
			_ = json.Unmarshal(body, &p)
			id, _ := p["receive_id"].(string)
			typ := r.URL.Query().Get("receive_id_type")
			if id == "bad" || id == "nobody@corp.com" {
				_, _ = w.Write([]byte(`{"code":99992360,"msg":"invalid id"}`))
				return
			}
			mu.Lock()
			sent = append(sent, typ+"="+id)
			mu.Unlock()
			_, _ = w.Write([]byte(`{"code":0}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	s := &larkSender{appID: "a", appSecret: "b", baseURL: srv.URL}
	err := s.Send(Message{Title: "t", Text: "x", LarkRecipients: [][]string{
		{"bad", "email:ok@corp.com"},
		{"nobody@corp.com"},
		{"bad", "mobile:13800138000"},
	}})
	if err == nil || !strings.Contains(err.Error(), "3 人中 1 人失败") {
		t.Fatalf("unexpected err: %v", err)
	}
	want := "email=ok@corp.com,user_id=m_uid"
	if strings.Join(sent, ",") != want {
		t.Fatalf("sent=%v want %s", sent, want)
	}
}
