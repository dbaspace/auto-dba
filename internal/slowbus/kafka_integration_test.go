package slowbus

import (
	"context"
	"os"
	"testing"
	"time"

	"auto-dba/internal/config"
)

// 本地验证：docker run -d --name slowlog-kafka -p 19092:9092 apache/kafka:3.8.0
// 然后 SLOWLOG_TEST_BROKERS=127.0.0.1:19092 go test ./internal/slowbus -run TestKafkaRoundTrip -count=1
func TestKafkaRoundTrip(t *testing.T) {
	brokers := os.Getenv("SLOWLOG_TEST_BROKERS")
	if brokers == "" {
		t.Skip("set SLOWLOG_TEST_BROKERS to run against a local Kafka")
	}
	bus, err := Open(config.SlowlogBusConfig{Brokers: brokers})
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()
	msg := Prepare(Message{
		CloudID: "pg-it", AccountID: "acc", TenantID: "ten", Vendor: "tencent", Region: "ap-guangzhou",
		LogTime: time.Now().UTC().Format(time.RFC3339Nano), SQL: "select 1", Raw: "select 1",
		Dedupe: DedupeKey("select 1", "select 1"), Duration: 1500,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := bus.Publish(ctx, []Message{msg}); err != nil {
		t.Fatal(err)
	}
	got := make(chan Message, 1)
	go bus.Run(ctx, func(_ context.Context, rows []Message) error {
		for _, row := range rows {
			if row.CloudID == "pg-it" {
				got <- row
			}
		}
		return nil
	})
	select {
	case row := <-got:
		if row.Dedupe != msg.Dedupe || row.SQL != "select 1" {
			t.Fatalf("%+v", row)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for consume")
	}
}
