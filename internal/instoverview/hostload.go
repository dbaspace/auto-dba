package instoverview

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"auto-dba/internal/promclient"
	"auto-dba/internal/store"
)

// HostPressure 是一台主机在分析窗口内的 CPU、IO util、网络和 TCP 连接。指针为空表示没有样本。
type HostPressure struct {
	CPUAvg     *float64 `json:"cpu_avg,omitempty"`
	CPUPeak    *float64 `json:"cpu_peak,omitempty"`
	IOAvg      *float64 `json:"io_avg,omitempty"`
	IOPeak     *float64 `json:"io_peak,omitempty"`
	NetInAvg   *float64 `json:"net_in_avg,omitempty"`
	NetInPeak  *float64 `json:"net_in_peak,omitempty"`
	NetOutAvg  *float64 `json:"net_out_avg,omitempty"`
	NetOutPeak *float64 `json:"net_out_peak,omitempty"`
	ConnAvg    *float64 `json:"conn_avg,omitempty"`
	ConnPeak   *float64 `json:"conn_peak,omitempty"`
}

// PromAPI 是主机压力查询用到的 Prometheus 接口。
type PromAPI interface {
	Query(ctx context.Context, promql string, ts time.Time) ([]promclient.Sample, error)
	QueryRange(ctx context.Context, promql string, start, end time.Time, step time.Duration) ([]promclient.Sample, error)
}

const hostLoadStep = 5 * time.Minute

type loadMetric struct {
	label    string
	avgExpr  string
	peakExpr string
	inner    string
	byDevice bool
	setAvg   func(*HostPressure, float64)
	setPeak  func(*HostPressure, float64)
}

// QueryHostPressure 查窗口内的均值和峰值。子查询不可用时改用范围查询。单项失败只记入结论。
func QueryHostPressure(ctx context.Context, api PromAPI, addrs []string, start, end time.Time) (map[string]*HostPressure, []string) {
	out := map[string]*HostPressure{}
	if api == nil || len(addrs) == 0 || !end.After(start) {
		return out, nil
	}
	metrics := hostLoadMetrics(addrs, end.Sub(start))
	var notes []string
	_, err := api.Query(ctx, metrics[0].avgExpr, end)
	useRange := subqueryUnsupported(err)
	if useRange {
		for _, m := range metrics {
			if err := ctx.Err(); err != nil {
				notes = append(notes, m.label+" 查询失败："+shortPromErr(err))
				continue
			}
			samples, err := api.QueryRange(ctx, m.inner, start, end, hostLoadStep)
			if err != nil {
				notes = append(notes, m.label+" 查询失败："+shortPromErr(err))
				continue
			}
			var avg, peak map[string]float64
			if m.byDevice {
				avg, peak = foldDeviceRange(samples)
			} else {
				avg, peak = foldInstanceRange(samples)
			}
			applyPressure(out, avg, m.setAvg)
			applyPressure(out, peak, m.setPeak)
		}
		fillConnections(ctx, api, out, &notes, addrs, start, end, true)
		return out, notes
	}
	for _, m := range metrics {
		if err := ctx.Err(); err != nil {
			notes = append(notes, m.label+" 查询失败："+shortPromErr(err))
			continue
		}
		avgSamples, avgErr := api.Query(ctx, m.avgExpr, end)
		peakSamples, peakErr := api.Query(ctx, m.peakExpr, end)
		if avgErr != nil || peakErr != nil {
			notes = append(notes, m.label+" 查询失败："+shortPromErr(firstErr(avgErr, peakErr)))
			continue
		}
		applyPressure(out, foldInstant(avgSamples), m.setAvg)
		applyPressure(out, foldInstant(peakSamples), m.setPeak)
	}
	fillConnections(ctx, api, out, &notes, addrs, start, end, false)
	return out, notes
}

func fillConnections(ctx context.Context, api PromAPI, out map[string]*HostPressure, notes *[]string, addrs []string, start, end time.Time, useRange bool) {
	window := promWindow(end.Sub(start))
	match := hostInstanceMatcher(addrs)
	estab := connExprs("node_netstat_Tcp_CurrEstab{"+match+"}", window)
	sock := connExprs("node_sockstat_TCP_inuse{"+match+"}", window)
	avg, peak, err := queryConn(ctx, api, estab, start, end, useRange)
	if err != nil || (len(avg) == 0 && len(peak) == 0) {
		avg2, peak2, err2 := queryConn(ctx, api, sock, start, end, useRange)
		if err2 == nil && (len(avg2) > 0 || len(peak2) > 0) {
			avg, peak, err = avg2, peak2, nil
		} else if err == nil {
			err = err2
		}
	}
	if err != nil {
		*notes = append(*notes, "系统连接数查询失败："+shortPromErr(err))
		return
	}
	applyPressure(out, avg, func(p *HostPressure, v float64) { p.ConnAvg = floatPtr(v) })
	applyPressure(out, peak, func(p *HostPressure, v float64) { p.ConnPeak = floatPtr(v) })
}

type connQuery struct{ avg, peak, inner string }

func connExprs(inner, window string) connQuery {
	return connQuery{
		avg:   fmt.Sprintf("avg_over_time((%s)[%s:5m])", inner, window),
		peak:  fmt.Sprintf("max_over_time((%s)[%s:5m])", inner, window),
		inner: inner,
	}
}

func queryConn(ctx context.Context, api PromAPI, q connQuery, start, end time.Time, useRange bool) (map[string]float64, map[string]float64, error) {
	if useRange {
		samples, err := api.QueryRange(ctx, q.inner, start, end, hostLoadStep)
		if err != nil {
			return nil, nil, err
		}
		avg, peak := foldInstanceRange(samples)
		return avg, peak, nil
	}
	avgSamples, err := api.Query(ctx, q.avg, end)
	if err != nil {
		return nil, nil, err
	}
	peakSamples, err := api.Query(ctx, q.peak, end)
	if err != nil {
		return nil, nil, err
	}
	return foldInstant(avgSamples), foldInstant(peakSamples), nil
}

func hostLoadMetrics(addrs []string, window time.Duration) []loadMetric {
	match := hostInstanceMatcher(addrs)
	span := promWindow(window)
	cpu := fmt.Sprintf(`100 * (1 - avg by (instance) (rate(node_cpu_seconds_total{mode="idle",%s}[5m])))`, match)
	io := fmt.Sprintf(`100 * rate(node_disk_io_time_seconds_total{%s}[5m])`, match)
	netDev := `device!~"lo|veth.*|docker.*|br.*|cni.*|flannel.*"`
	netIn := fmt.Sprintf(`sum by (instance) (rate(node_network_receive_bytes_total{%s,%s}[5m]))`, netDev, match)
	netOut := fmt.Sprintf(`sum by (instance) (rate(node_network_transmit_bytes_total{%s,%s}[5m]))`, netDev, match)
	over := func(inner, fn string) string {
		return fmt.Sprintf("%s((%s)[%s:5m])", fn, inner, span)
	}
	ioOver := func(fn string) string {
		return fmt.Sprintf("max by (instance) (%s((%s)[%s:5m]))", fn, io, span)
	}
	return []loadMetric{
		{label: "CPU", avgExpr: over(cpu, "avg_over_time"), peakExpr: over(cpu, "max_over_time"), inner: cpu, setAvg: func(p *HostPressure, v float64) { p.CPUAvg = floatPtr(v) }, setPeak: func(p *HostPressure, v float64) { p.CPUPeak = floatPtr(v) }},
		{label: "IO util", avgExpr: ioOver("avg_over_time"), peakExpr: ioOver("max_over_time"), inner: io, byDevice: true, setAvg: func(p *HostPressure, v float64) { p.IOAvg = floatPtr(v) }, setPeak: func(p *HostPressure, v float64) { p.IOPeak = floatPtr(v) }},
		{label: "网络入站", avgExpr: over(netIn, "avg_over_time"), peakExpr: over(netIn, "max_over_time"), inner: netIn, setAvg: func(p *HostPressure, v float64) { p.NetInAvg = floatPtr(v) }, setPeak: func(p *HostPressure, v float64) { p.NetInPeak = floatPtr(v) }},
		{label: "网络出站", avgExpr: over(netOut, "avg_over_time"), peakExpr: over(netOut, "max_over_time"), inner: netOut, setAvg: func(p *HostPressure, v float64) { p.NetOutAvg = floatPtr(v) }, setPeak: func(p *HostPressure, v float64) { p.NetOutPeak = floatPtr(v) }},
	}
}

func promWindow(d time.Duration) string {
	sec := int(d.Seconds())
	if sec < 60 {
		sec = 60
	}
	return fmt.Sprintf("%ds", sec)
}

func subqueryUnsupported(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "parse error") || strings.Contains(s, "ranges only allowed") || strings.Contains(s, "subquery")
}

func firstErr(a, b error) error {
	if a != nil {
		return a
	}
	return b
}

func shortPromErr(err error) string {
	if err == nil {
		return ""
	}
	s := strings.TrimSpace(err.Error())
	s = strings.TrimPrefix(s, "prometheus error: ")
	r := []rune(s)
	if len(r) > 80 {
		s = string(r[:80])
	}
	return s
}

func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func floatPtr(v float64) *float64 { return &v }

func ensurePressure(m map[string]*HostPressure, host string) *HostPressure {
	p := m[host]
	if p == nil {
		p = &HostPressure{}
		m[host] = p
	}
	return p
}

func applyPressure(m map[string]*HostPressure, vals map[string]float64, set func(*HostPressure, float64)) {
	if set == nil {
		return
	}
	for host, v := range vals {
		if host == "" || !finite(v) {
			continue
		}
		set(ensurePressure(m, host), v)
	}
}

func foldInstant(samples []promclient.Sample) map[string]float64 {
	out := map[string]float64{}
	seen := map[string]bool{}
	for _, s := range samples {
		host := store.HostAddrKey(s.Metric["instance"])
		v, ok := promclient.InstantValue(s)
		if host == "" || !ok || !finite(v) {
			continue
		}
		if !seen[host] || v > out[host] {
			out[host] = v
			seen[host] = true
		}
	}
	return out
}

func seriesValues(s promclient.Sample) []float64 {
	pts := promclient.SampleToPoints(s)
	if len(pts) == 0 {
		if v, ok := promclient.InstantValue(s); ok && finite(v) {
			return []float64{v}
		}
		return nil
	}
	out := make([]float64, 0, len(pts))
	for _, p := range pts {
		if finite(p.V) {
			out = append(out, p.V)
		}
	}
	return out
}

func foldInstanceRange(samples []promclient.Sample) (avg, peak map[string]float64) {
	sum := map[string]float64{}
	n := map[string]int{}
	peak = map[string]float64{}
	seen := map[string]bool{}
	for _, s := range samples {
		host := store.HostAddrKey(s.Metric["instance"])
		if host == "" {
			continue
		}
		for _, v := range seriesValues(s) {
			sum[host] += v
			n[host]++
			if !seen[host] || v > peak[host] {
				peak[host] = v
				seen[host] = true
			}
		}
	}
	avg = map[string]float64{}
	for host, c := range n {
		if c > 0 {
			avg[host] = sum[host] / float64(c)
		}
	}
	return avg, peak
}

// foldDeviceRange 把每块盘的窗口均值取最大的一块作为主机均值，峰值取全部采样点的最大。
func foldDeviceRange(samples []promclient.Sample) (avg, peak map[string]float64) {
	type acc struct {
		sum  float64
		n    int
		peak float64
		has  bool
	}
	byDev := map[string]map[string]*acc{}
	for _, s := range samples {
		host := store.HostAddrKey(s.Metric["instance"])
		if host == "" {
			continue
		}
		dev := strings.TrimSpace(s.Metric["device"])
		if dev == "" {
			dev = "-"
		}
		vals := seriesValues(s)
		if len(vals) == 0 {
			continue
		}
		if byDev[host] == nil {
			byDev[host] = map[string]*acc{}
		}
		a := byDev[host][dev]
		if a == nil {
			a = &acc{}
			byDev[host][dev] = a
		}
		for _, v := range vals {
			a.sum += v
			a.n++
			if !a.has || v > a.peak {
				a.peak = v
				a.has = true
			}
		}
	}
	avg = map[string]float64{}
	peak = map[string]float64{}
	for host, devs := range byDev {
		var bestAvg, bestPeak float64
		ok := false
		for _, a := range devs {
			if a.n == 0 {
				continue
			}
			mean := a.sum / float64(a.n)
			if !ok || mean > bestAvg {
				bestAvg = mean
			}
			if !ok || a.peak > bestPeak {
				bestPeak = a.peak
			}
			ok = true
		}
		if ok {
			avg[host] = bestAvg
			peak[host] = bestPeak
		}
	}
	return avg, peak
}
