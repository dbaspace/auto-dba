package kafka

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	"auto-dba/internal/store"
)

const dialTimeout = 10 * time.Second

func bootstrapAddr(inst *store.Instance) string {
	if inst == nil {
		return "localhost:9092"
	}
	h := strings.TrimSpace(inst.Host)
	if h == "" {
		h = strings.TrimSpace(inst.DomainName)
	}
	port := inst.Port
	if port <= 0 {
		port = 9092
	}
	if h == "" {
		return fmt.Sprintf("localhost:%d", port)
	}
	if strings.Contains(h, ",") {
		first := strings.TrimSpace(strings.Split(h, ",")[0])
		if first == "" {
			return fmt.Sprintf("localhost:%d", port)
		}
		if !strings.Contains(first, ":") {
			return fmt.Sprintf("%s:%d", first, port)
		}
		return first
	}
	if _, _, err := net.SplitHostPort(h); err == nil {
		return h
	}
	return fmt.Sprintf("%s:%d", h, port)
}

func dialBroker(ctx context.Context, addr string) (*kafkago.Conn, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	conn, err := kafkago.DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("kafka dial %s: %w", addr, err)
	}
	return conn, nil
}

// Ping 探测 Kafka broker 是否可达。
func Ping(ctx context.Context, inst *store.Instance) error {
	conn, err := dialBroker(ctx, bootstrapAddr(inst))
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}

func controllerAddr(c kafkago.Broker) string {
	return net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
}
