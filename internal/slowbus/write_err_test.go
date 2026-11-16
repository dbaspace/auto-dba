package slowbus

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"
)

func TestShortenWriteErrCollapsesDialTimeout(t *testing.T) {
	one := errors.New("kafka.(*Client).Produce: dial tcp 10.100.112.17:9092: i/o timeout")
	batch := make(kafka.WriteErrors, 100)
	for i := range batch {
		batch[i] = one
	}
	err := shortenWriteErr(batch, 100, []string{"10.100.112.17:9092"})
	got := err.Error()
	want := "慢日志总线 10.100.112.17:9092 连接超时，本批 100 条未写入"
	if got != want {
		t.Fatalf("got %s", got)
	}
	if !IsDialTimeout(err) {
		t.Fatal("timeout should be recognized")
	}
	if strings.Count(got, "dial tcp") != 0 {
		t.Fatalf("raw dial text leaked: %s", got)
	}
}

func TestShortenWriteErrKeepsAuthText(t *testing.T) {
	auth := errors.New("SASL authentication failed")
	batch := kafka.WriteErrors{auth, auth, auth}
	err := shortenWriteErr(batch, 3, []string{"10.100.112.17:9092"})
	if err.Error() != auth.Error() {
		t.Fatalf("got %s", err)
	}
	if IsDialTimeout(err) {
		t.Fatal("auth is not a dial timeout")
	}
}

func TestShortenWriteErrRedactsPassword(t *testing.T) {
	err := shortenWriteErr(errors.New("sasl password rejected"), 1, nil)
	if err.Error() != "kafka 连接失败" {
		t.Fatalf("got %s", err)
	}
}

func TestProbeReachableAndRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if err := Probe(context.Background(), ln.Addr().String()); err != nil {
		t.Fatal(err)
	}
	ln.Close()
	prev := probeTimeout
	probeTimeout = 200 * time.Millisecond
	defer func() { probeTimeout = prev }()
	err = Probe(context.Background(), ln.Addr().String())
	if err == nil || !strings.Contains(err.Error(), "连接失败") || IsDialTimeout(err) {
		t.Fatalf("refused: %v", err)
	}
}

func TestProbeTimeoutMessage(t *testing.T) {
	prev := probeTimeout
	probeTimeout = 200 * time.Millisecond
	defer func() { probeTimeout = prev }()
	err := Probe(context.Background(), "192.0.2.1:9092")
	if err == nil {
		t.Fatal("blackhole should fail")
	}
	if IsDialTimeout(err) && !strings.Contains(err.Error(), "192.0.2.1:9092") {
		t.Fatalf("got %v", err)
	}
	if !IsDialTimeout(err) && !strings.Contains(err.Error(), "连接失败") {
		t.Fatalf("got %v", err)
	}
	if strings.Count(err.Error(), "i/o timeout") > 1 {
		t.Fatalf("timeout repeated: %s", err)
	}
}

func TestDialTimeoutErrWithoutBatch(t *testing.T) {
	err := fmt.Errorf("%w", dialTimeoutErr(0, []string{"10.100.112.17:9092"}))
	if err.Error() != "慢日志总线 10.100.112.17:9092 连接超时" || !IsDialTimeout(err) {
		t.Fatal(err)
	}
}
