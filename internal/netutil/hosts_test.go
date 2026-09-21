package netutil

import (
	"reflect"
	"testing"
)

func TestSplitHostList(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"  a  ", []string{"a"}},
		{"a,b,c", []string{"a", "b", "c"}},
		{"a，b，c", []string{"a", "b", "c"}},
		{" a, ,b ,", []string{"a", "b"}},
		{"clickhouse01.test,clickhouse02.test,clickhouse03.test", []string{
			"clickhouse01.test", "clickhouse02.test", "clickhouse03.test",
		}},
	}
	for _, c := range cases {
		got := SplitHostList(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Fatalf("SplitHostList(%q)=%v want %v", c.in, got, c.want)
		}
	}
}

func TestHostPortPairs(t *testing.T) {
	cases := []struct {
		raw  string
		port int
		want []string
	}{
		{"a,b,c", 8123, []string{"a:8123", "b:8123", "c:8123"}},
		{"a:9000,b", 8123, []string{"a:9000", "b:8123"}},
		{"broker:9092,b:9093", 9092, []string{"broker:9092", "b:9093"}},
		{"10.0.0.1", 3306, []string{"10.0.0.1:3306"}},
	}
	for _, c := range cases {
		got := HostPortPairs(c.raw, c.port)
		if !reflect.DeepEqual(got, c.want) {
			t.Fatalf("HostPortPairs(%q,%d)=%v want %v", c.raw, c.port, got, c.want)
		}
	}
}

func TestFirstHostPort(t *testing.T) {
	if got := FirstHostPort("a,b", 8123); got != "a:8123" {
		t.Fatalf("got %q", got)
	}
	if got := FirstHostPort("", 8123); got != "" {
		t.Fatalf("got %q", got)
	}
}
