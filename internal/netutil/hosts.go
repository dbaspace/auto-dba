package netutil

import (
	"net"
	"strconv"
	"strings"
)

// Endpoint is a dialable host + port pair.
type Endpoint struct {
	Host string
	Port int
}

// SplitHostList splits a comma-separated host list (supports ASCII and full-width commas).
func SplitHostList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	raw = strings.ReplaceAll(raw, "，", ",")
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

// Endpoints parses raw into host/port pairs. A segment with an explicit port keeps it;
// otherwise defaultPort is used.
func Endpoints(raw string, defaultPort int) []Endpoint {
	parts := SplitHostList(raw)
	if len(parts) == 0 {
		return nil
	}
	out := make([]Endpoint, 0, len(parts))
	for _, part := range parts {
		if h, p, err := net.SplitHostPort(part); err == nil {
			port, _ := strconv.Atoi(p)
			if port <= 0 {
				port = defaultPort
			}
			out = append(out, Endpoint{Host: h, Port: port})
			continue
		}
		out = append(out, Endpoint{Host: part, Port: defaultPort})
	}
	return out
}

// HostPortPairs returns "host:port" strings suitable for dial (IPv6-safe via JoinHostPort).
func HostPortPairs(raw string, defaultPort int) []string {
	eps := Endpoints(raw, defaultPort)
	if len(eps) == 0 {
		return nil
	}
	out := make([]string, 0, len(eps))
	for _, ep := range eps {
		out = append(out, net.JoinHostPort(ep.Host, strconv.Itoa(ep.Port)))
	}
	return out
}

// FirstHostPort returns the first dial address, or empty if raw is empty.
func FirstHostPort(raw string, defaultPort int) string {
	pairs := HostPortPairs(raw, defaultPort)
	if len(pairs) == 0 {
		return ""
	}
	return pairs[0]
}
