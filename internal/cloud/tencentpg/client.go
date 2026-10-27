package tencentpg

import (
	"context"
	"net/http"
	"time"

	"auto-dba/internal/cloud"
	"auto-dba/internal/cloud/tencentapi"
)

const (
	apiHost    = "postgres.tencentcloudapi.com"
	apiVersion = "2017-03-12"
	apiService = "postgres"
)

type Client struct {
	Host string
	Do   func(*http.Request) (*http.Response, error)
}

func (c *Client) api() *tencentapi.Client {
	host := apiHost
	if c != nil && c.Host != "" {
		host = c.Host
	}
	var do func(*http.Request) (*http.Response, error)
	if c != nil {
		do = c.Do
	}
	return &tencentapi.Client{Host: host, Service: apiService, Version: apiVersion, Do: do}
}

func (c *Client) call(ctx context.Context, cred cloud.Cred, action string, payload any) ([]byte, error) {
	return c.api().Call(ctx, cred, action, payload)
}

func TC3Headers(secretID, secretKey, service, host, action, version, region string, payload []byte, ts time.Time) map[string]string {
	return tencentapi.TC3Headers(secretID, secretKey, service, host, action, version, region, payload, ts)
}

func shanghai(t time.Time) string { return tencentapi.Shanghai(t) }
