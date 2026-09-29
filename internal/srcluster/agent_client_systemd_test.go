package srcluster

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type failTransport struct{}

func (failTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("short client used")
}

func TestSystemdCtrlUsesLongClientForStartStop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"message":"done"}`))
	}))
	defer srv.Close()
	c := &AgentClient{HTTP: &http.Client{Transport: failTransport{}}, HTTPLong: srv.Client()}
	auth := AgentAuth{BaseURL: srv.URL}
	for _, act := range []string{"start", "stop", "restart"} {
		if _, err := c.SystemdCtrl(context.Background(), auth, SystemdCtrlReq{Action: act, Unit: "fe-9030"}); err != nil {
			t.Fatalf("%s should use long client: %v", act, err)
		}
	}
	if _, err := c.SystemdCtrl(context.Background(), auth, SystemdCtrlReq{Action: "status", Unit: "fe-9030"}); err == nil {
		t.Fatal("status should stay on short client")
	}
}
