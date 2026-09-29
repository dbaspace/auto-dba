package awr

import (
	"strings"

	"auto-dba/internal/promclient"
)

const awrPromDefaultStep = promclient.DefaultStep

type awrPromClient = promclient.Client
type promSample = promclient.Sample

func (s *Service) newAwrPromClient() (*promclient.Client, bool) {
	base := s.stringArg(awrPromBaseURLKey, "")
	if strings.TrimSpace(base) == "" {
		return nil, false
	}
	cli := promclient.New(promclient.Config{
		BaseURL:         base,
		AuthToken:       s.stringArg(awrPromAuthTokenKey, ""),
		PartialResponse: s.boolArg(awrPromPartialResponseKey, true),
		Dedup:           s.boolArg(awrPromDedupKey, true),
		Timeout:         promclient.DefaultTimeout,
	})
	if cli == nil {
		return nil, false
	}
	return cli, true
}

func promSampleToSeries(s promSample) []AwrSeriesPoint {
	pts := promclient.SampleToPoints(s)
	out := make([]AwrSeriesPoint, 0, len(pts))
	for _, p := range pts {
		out = append(out, AwrSeriesPoint{T: p.T, V: p.V})
	}
	return out
}

func toFloat(v interface{}) (float64, bool) {
	return promclient.ToFloat(v)
}
