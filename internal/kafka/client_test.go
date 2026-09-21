package kafka

import (
	"testing"

	"auto-dba/internal/store"
)

func TestBootstrapAddr(t *testing.T) {
	cases := []struct {
		host, domain string
		port         int
		want         string
	}{
		{"10.0.0.1", "", 9092, "10.0.0.1:9092"},
		{"", "kafka.internal", 9093, "kafka.internal:9093"},
		{"broker:9092", "", 1, "broker:9092"},
		{"a:9092,b:9093", "", 9092, "a:9092"},
		{"a,b", "", 9092, "a:9092"},
		{"", "", 0, "localhost:9092"},
	}
	for _, c := range cases {
		got := bootstrapAddr(&store.Instance{Host: c.host, DomainName: c.domain, Port: c.port})
		if got != c.want {
			t.Fatalf("host=%q domain=%q port=%d got %q want %q", c.host, c.domain, c.port, got, c.want)
		}
	}
}
