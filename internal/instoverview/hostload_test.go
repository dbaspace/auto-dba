package instoverview

import (
	"context"
	"strings"
	"testing"
	"time"

	"auto-dba/internal/promclient"
)

func TestFoldDeviceRangeTakesBusiestDisk(t *testing.T) {
	samples := []promclient.Sample{
		{Metric: map[string]string{"instance": "10.0.0.1:9100", "device": "sda"}, Values: [][]interface{}{{float64(1), "10"}, {float64(2), "30"}}},
		{Metric: map[string]string{"instance": "10.0.0.1:9100", "device": "sdb"}, Values: [][]interface{}{{float64(1), "80"}, {float64(2), "100"}}},
	}
	avg, peak, peakAt := foldDeviceRange(samples)
	if avg["10.0.0.1"] != 90 || peak["10.0.0.1"] != 100 {
		t.Fatalf("avg=%v peak=%v", avg["10.0.0.1"], peak["10.0.0.1"])
	}
	if !peakAt["10.0.0.1"].Equal(time.Unix(2, 0).UTC()) {
		t.Fatalf("peak at=%v", peakAt["10.0.0.1"])
	}
}

func TestHostLoadExprs(t *testing.T) {
	ms := hostLoadMetrics([]string{"10.0.0.1"}, 24*time.Hour)
	if len(ms) != 4 {
		t.Fatalf("metrics=%d", len(ms))
	}
	cpu := ms[0].avgExpr
	if !strings.Contains(cpu, "node_cpu_seconds_total") || !strings.Contains(cpu, "86400s:5m") || !strings.Contains(cpu, "(:[0-9]+)?") {
		t.Fatalf("cpu expr = %s", cpu)
	}
	if !strings.Contains(ms[1].inner, "node_disk_io_time_seconds_total") || !ms[1].byDevice {
		t.Fatalf("io = %+v", ms[1].inner)
	}
	if !strings.Contains(ms[2].inner, "node_network_receive_bytes_total") || !strings.Contains(ms[2].inner, `device!~"lo|veth.*|docker.*|br.*|cni.*|flannel.*"`) {
		t.Fatalf("net = %s", ms[2].inner)
	}
}

type scriptProm struct {
	rangeMode bool
	calls     []string
}

func (s *scriptProm) Query(_ context.Context, expr string, _ time.Time) ([]promclient.Sample, error) {
	s.calls = append(s.calls, expr)
	if s.rangeMode {
		return nil, errParseSubquery
	}
	return instantFor(expr), nil
}

func (s *scriptProm) QueryRange(_ context.Context, expr string, _, _ time.Time, _ time.Duration) ([]promclient.Sample, error) {
	s.calls = append(s.calls, "range:"+expr)
	return rangeFor(expr), nil
}

type errParse string

func (e errParse) Error() string { return string(e) }

const errParseSubquery errParse = "prometheus error: bad_data parse error: ranges only allowed for vector selectors"

func instantFor(expr string) []promclient.Sample {
	v := "1"
	switch {
	case strings.Contains(expr, "node_cpu"):
		v = "40"
	case strings.Contains(expr, "node_disk_io"):
		v = "15"
	case strings.Contains(expr, "node_network_receive"):
		v = "1048576"
	case strings.Contains(expr, "node_network_transmit"):
		v = "2048"
	case strings.Contains(expr, "Tcp_CurrEstab"):
		return nil
	case strings.Contains(expr, "TCP_inuse"):
		v = "12"
	default:
		return nil
	}
	if strings.Contains(expr, "max_over_time") {
		switch {
		case strings.Contains(expr, "node_cpu"):
			v = "90"
		case strings.Contains(expr, "node_disk_io"):
			v = "80"
		case strings.Contains(expr, "node_network_receive"):
			v = "2097152"
		case strings.Contains(expr, "node_network_transmit"):
			v = "4096"
		case strings.Contains(expr, "TCP_inuse"):
			v = "20"
		}
	}
	return []promclient.Sample{{Metric: map[string]string{"instance": "10.0.0.1:9100"}, Value: []interface{}{float64(1), v}}}
}

func rangeFor(expr string) []promclient.Sample {
	if strings.Contains(expr, "node_disk_io") {
		return []promclient.Sample{
			{Metric: map[string]string{"instance": "10.0.0.1:9100", "device": "sda"}, Values: [][]interface{}{{float64(1), "10"}, {float64(2), "30"}}},
			{Metric: map[string]string{"instance": "10.0.0.1:9100", "device": "sdb"}, Values: [][]interface{}{{float64(1), "80"}, {float64(2), "100"}}},
		}
	}
	return []promclient.Sample{{
		Metric: map[string]string{"instance": "10.0.0.1:9100"},
		Values: [][]interface{}{{float64(1), "10"}, {float64(2), "30"}},
	}}
}

func TestQueryHostPressureSubqueryFallsBackToSockstat(t *testing.T) {
	now := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	got, notes := QueryHostPressure(context.Background(), &scriptProm{}, []string{"10.0.0.1"}, now.Add(-24*time.Hour), now)
	if len(notes) != 0 {
		t.Fatalf("notes = %v", notes)
	}
	p := got["10.0.0.1"]
	if p == nil || p.CPUAvg == nil || *p.CPUAvg != 40 || p.CPUPeak == nil || *p.CPUPeak != 90 {
		t.Fatalf("cpu = %+v", p)
	}
	if p.IOAvg == nil || *p.IOAvg != 15 || p.IOPeak == nil || *p.IOPeak != 80 {
		t.Fatalf("io = %+v", p)
	}
	if p.NetInAvg == nil || *p.NetInAvg != 1048576 || p.ConnAvg == nil || *p.ConnAvg != 12 || p.ConnPeak == nil || *p.ConnPeak != 20 {
		t.Fatalf("net/conn = %+v", p)
	}
	wantAt := time.Unix(2, 0).UTC()
	if p.CPUPeakAt == nil || !p.CPUPeakAt.Equal(wantAt) || p.IOPeakAt == nil || !p.IOPeakAt.Equal(wantAt) || p.NetInPeakAt == nil || !p.NetInPeakAt.Equal(wantAt) {
		t.Fatalf("peak at = cpu %v io %v net %v", p.CPUPeakAt, p.IOPeakAt, p.NetInPeakAt)
	}
}

func TestQueryHostPressureRangeFallback(t *testing.T) {
	now := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	api := &scriptProm{rangeMode: true}
	got, notes := QueryHostPressure(context.Background(), api, []string{"10.0.0.1"}, now.Add(-24*time.Hour), now)
	if len(notes) != 0 {
		t.Fatalf("notes = %v", notes)
	}
	p := got["10.0.0.1"]
	if p == nil || p.IOAvg == nil || *p.IOAvg != 90 || p.IOPeak == nil || *p.IOPeak != 100 {
		t.Fatalf("io = %+v", p)
	}
	if p.CPUAvg == nil || *p.CPUAvg != 20 || p.CPUPeak == nil || *p.CPUPeak != 30 {
		t.Fatalf("cpu = %+v", p)
	}
	if p.ConnAvg == nil || *p.ConnAvg != 20 {
		t.Fatalf("conn = %+v", p)
	}
	wantAt := time.Unix(2, 0).UTC()
	if p.CPUPeakAt == nil || !p.CPUPeakAt.Equal(wantAt) || p.IOPeakAt == nil || !p.IOPeakAt.Equal(wantAt) {
		t.Fatalf("peak at = cpu %v io %v", p.CPUPeakAt, p.IOPeakAt)
	}
	joined := strings.Join(api.calls, "\n")
	if !strings.Contains(joined, "range:") {
		t.Fatalf("calls = %s", joined)
	}
}

type failCPU struct{}

func (failCPU) Query(_ context.Context, expr string, _ time.Time) ([]promclient.Sample, error) {
	if strings.Contains(expr, "node_cpu") {
		return nil, errParse("prometheus timeout")
	}
	return instantFor(expr), nil
}

func (failCPU) QueryRange(_ context.Context, expr string, _, _ time.Time, _ time.Duration) ([]promclient.Sample, error) {
	return rangeFor(expr), nil
}

func TestQueryHostPressureKeepsOtherMetrics(t *testing.T) {
	now := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	got, notes := QueryHostPressure(context.Background(), failCPU{}, []string{"10.0.0.1"}, now.Add(-24*time.Hour), now)
	if len(notes) != 1 || !strings.Contains(notes[0], "CPU") {
		t.Fatalf("notes = %v", notes)
	}
	p := got["10.0.0.1"]
	if p == nil || p.CPUAvg != nil || p.IOAvg == nil || *p.IOAvg != 15 {
		t.Fatalf("pressure = %+v", p)
	}
}

type rangeDown struct{}

func (rangeDown) Query(_ context.Context, expr string, _ time.Time) ([]promclient.Sample, error) {
	return instantFor(expr), nil
}

func (rangeDown) QueryRange(context.Context, string, time.Time, time.Time, time.Duration) ([]promclient.Sample, error) {
	return nil, errParse("range down")
}

func TestQueryHostPressurePeakTimeFailureKeepsValue(t *testing.T) {
	now := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	got, notes := QueryHostPressure(context.Background(), rangeDown{}, []string{"10.0.0.1"}, now.Add(-24*time.Hour), now)
	p := got["10.0.0.1"]
	if p == nil || p.CPUPeak == nil || *p.CPUPeak != 90 || p.IOPeak == nil || *p.IOPeak != 80 || p.CPUPeakAt != nil || p.IOPeakAt != nil {
		t.Fatalf("pressure = %+v", p)
	}
	if len(notes) == 0 || !strings.Contains(strings.Join(notes, "\n"), "峰值时间查询失败") {
		t.Fatalf("notes = %v", notes)
	}
}
