package dbprobe

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func TestNormalizeCellBigintKeepsDigits(t *testing.T) {
	const raw = "211725322743368812"
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"BIGINT", "bigint", "BIGINT UNSIGNED", "LARGEINT", "INT8", "BIGSERIAL", "SERIAL8"} {
		got, ok := normalizeCell(n, kind).(string)
		if !ok || got != raw {
			t.Fatalf("%s: %#v", kind, normalizeCell(n, kind))
		}
	}
	body, err := json.Marshal(map[string]any{"order_id": normalizeCell(n, "BIGINT")})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"211725322743368812"`) {
		t.Fatalf("json: %s", body)
	}
	if got := normalizeCell(int64(42), "INT"); got != int64(42) {
		t.Fatalf("int: %#v", got)
	}
	if got := normalizeCell(int64(42), "SMALLINT"); got != int64(42) {
		t.Fatalf("smallint: %#v", got)
	}
	if got := normalizeCell(n); got != raw {
		t.Fatalf("untyped: %#v", got)
	}
	if got := normalizeCell(int64(501819629)); got != int64(501819629) {
		t.Fatalf("untyped shop id: %#v", got)
	}
	if got := normalizeCell(int64(1), "BIGINT"); got != "1" {
		t.Fatalf("small bigint: %#v", got)
	}
}

func TestParseClickHouseBodyKeepsInt64(t *testing.T) {
	body := []byte(`{"meta":[{"name":"order_id","type":"Int64"},{"name":"shop_id","type":"Int32"},{"name":"score","type":"Float64"},{"name":"note","type":"Nullable(String)"}],"data":[{"order_id":211725322743368812,"shop_id":501819629,"score":1.5,"note":"接驳订单接口异常: 211725322743368812"}],"rows":1}`)
	out, err := parseClickHouseBody(body, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Rows) != 1 || len(out.Rows[0]) != 4 {
		t.Fatalf("shape: %#v", out)
	}
	if out.Rows[0][0] != "211725322743368812" {
		t.Fatalf("order_id: %#v", out.Rows[0][0])
	}
	if out.Rows[0][1] != int64(501819629) {
		t.Fatalf("shop_id: %#v", out.Rows[0][1])
	}
	if out.Rows[0][2] != 1.5 {
		t.Fatalf("score: %#v", out.Rows[0][2])
	}
	encoded, err := json.Marshal(map[string]any{"rows": out.Rows})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"211725322743368812"`) {
		t.Fatalf("json: %s", encoded)
	}
	wide, err := parseClickHouseBody([]byte(`{"meta":[{"name":"id","type":"Nullable(UInt64)"}],"data":[{"id":2118543565383123560}],"rows":1}`), 10)
	if err != nil {
		t.Fatal(err)
	}
	if wide.Rows[0][0] != "2118543565383123560" {
		t.Fatalf("uint64: %#v", wide.Rows[0][0])
	}
}
