package instoverview

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"auto-dba/internal/store"
)

type srNode struct {
	Host           string
	Version        string
	Role           string
	Alive          string
	Decommissioned bool
	Tablet         string
	Total          string
	DataUsed       string
	UsedPct        string
}

func probeStarRocks(ctx context.Context, inst *store.Instance, pass string) Sample {
	if inst == nil {
		return Sample{ErrorKind: "connect", Error: "连不上"}
	}
	hosts := splitFEHosts(inst.Host)
	if len(hosts) == 0 {
		return Sample{ErrorKind: "connect", Error: "连不上"}
	}
	user := strings.TrimSpace(inst.Username)
	if user == "" {
		user = "root"
	}
	var last error
	for _, token := range hosts {
		if err := ctx.Err(); err != nil {
			kind, msg := Classify(err)
			return Sample{ErrorKind: kind, Error: msg}
		}
		host, port := feHostPort(token, inst.Port)
		db, err := openStarRocks(ctx, host, port, user, pass)
		if err != nil {
			last = err
			continue
		}
		s, err := scanStarRocks(ctx, db)
		_ = db.Close()
		if err != nil {
			last = err
			continue
		}
		s.Reachable = true
		return s
	}
	if last == nil {
		last = fmt.Errorf("connect: connection refused")
	}
	kind, msg := Classify(last)
	return Sample{ErrorKind: kind, Error: msg}
}

func splitFEHosts(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '，' || r == ';' || r == '\n'
	})
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func feHostPort(token string, fallback int) (string, int) {
	token = strings.TrimSpace(token)
	if host, port, err := net.SplitHostPort(token); err == nil {
		if n, nerr := strconv.Atoi(port); nerr == nil && n > 0 {
			return host, n
		}
	}
	if fallback <= 0 {
		fallback = 9030
	}
	return token, fallback
}

func openStarRocks(ctx context.Context, host string, port int, user, pass string) (*sql.DB, error) {
	cfg := mysql.NewConfig()
	cfg.User = user
	cfg.Passwd = pass
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort(host, strconv.Itoa(port))
	cfg.Timeout = 800 * time.Millisecond
	cfg.ReadTimeout = 2 * time.Second
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	pingCtx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	err = db.PingContext(pingCtx)
	cancel()
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func scanStarRocks(ctx context.Context, db *sql.DB) (Sample, error) {
	fe, err := queryProc(ctx, db, "/frontends")
	if err != nil {
		return Sample{}, err
	}
	be, beErr := queryProc(ctx, db, "/backends")
	s := summarizeStarRocks(fe, be)
	if beErr != nil {
		s.Partial = true
		kind, msg := Classify(beErr)
		s.ErrorKind = kind
		s.Error = msg
	}
	return s, nil
}

func queryProc(ctx context.Context, db *sql.DB, path string) ([]srNode, error) {
	rows, err := db.QueryContext(ctx, "SHOW PROC '"+path+"'")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	idx := map[string]int{}
	for i, c := range cols {
		idx[strings.ToLower(strings.TrimSpace(c))] = i
	}
	col := func(name string) int {
		if i, ok := idx[name]; ok {
			return i
		}
		return -1
	}
	ipIdx := col("ip")
	if ipIdx < 0 {
		ipIdx = col("host")
	}
	nameIdx := col("name")
	verIdx := col("version")
	roleIdx := col("role")
	aliveIdx := col("alive")
	decomIdx := col("systemdecommissioned")
	tabletIdx := col("tabletnum")
	totalIdx := col("totalcapacity")
	dataIdx := col("datausedcapacity")
	pctIdx := col("usedpct")
	var out []srNode
	for rows.Next() {
		vals := make([]sql.RawBytes, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		cell := func(i int) string {
			if i < 0 || i >= len(vals) || vals[i] == nil {
				return ""
			}
			return strings.TrimSpace(string(vals[i]))
		}
		host := cell(ipIdx)
		if host == "" {
			host, _, _ = strings.Cut(cell(nameIdx), "_")
		}
		out = append(out, srNode{
			Host:           host,
			Version:        cell(verIdx),
			Role:           strings.ToUpper(cell(roleIdx)),
			Alive:          cell(aliveIdx),
			Decommissioned: procTrue(cell(decomIdx)),
			Tablet:         cell(tabletIdx),
			Total:          cell(totalIdx),
			DataUsed:       cell(dataIdx),
			UsedPct:        cell(pctIdx),
		})
	}
	return out, rows.Err()
}

func summarizeStarRocks(fe, be []srNode) Sample {
	var s Sample
	s.FETotal = len(fe)
	var version string
	for _, n := range fe {
		if procTrue(n.Alive) {
			s.FEAlive++
		}
		if n.Version == "" {
			continue
		}
		if version == "" || n.Role == "LEADER" {
			version = n.Version
		}
	}
	s.Version = version
	s.BETotal = len(be)
	for _, n := range be {
		if procTrue(n.Alive) {
			s.BEAlive++
		}
		if n.Decommissioned {
			s.BEDecommissioning = true
		}
		if b, ok := parseCapacityBytes(n.Total); ok {
			s.DiskKnown = true
			s.DiskBytes += b
		}
		if b, ok := parseCapacityBytes(n.DataUsed); ok {
			s.DataKnown = true
			s.DataBytes += b
		}
		if pct, ok := parsePercent(n.UsedPct); ok {
			if !s.DiskUsedKnown || pct > s.DiskUsedRate {
				s.DiskUsedRate = pct
				s.StorageHotHost = n.Host
			}
			s.DiskUsedKnown = true
		}
		if n.Tablet != "" {
			if v, err := strconv.ParseInt(strings.TrimSpace(n.Tablet), 10, 64); err == nil {
				s.TabletKnown = true
				s.TabletCount += v
			}
		}
	}
	if s.DiskKnown && s.DataKnown && s.DiskBytes > 0 {
		s.StorageRate = float64(s.DataBytes) / float64(s.DiskBytes) * 100
		s.StorageRateKnown = true
	}
	return s
}

func procTrue(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes", "y", "t":
		return true
	default:
		return false
	}
}

func parseCapacityBytes(raw string) (int64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "—" || raw == "-" || strings.EqualFold(raw, "n/a") {
		return 0, false
	}
	fields := strings.Fields(raw)
	num := fields[0]
	unit := ""
	if len(fields) > 1 {
		unit = fields[1]
	} else {
		i := 0
		for i < len(num) && (num[i] == '.' || num[i] == '-' || (num[i] >= '0' && num[i] <= '9')) {
			i++
		}
		unit = num[i:]
		num = num[:i]
	}
	f, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0, false
	}
	mul := float64(1)
	switch strings.ToUpper(strings.TrimSpace(unit)) {
	case "", "B":
		mul = 1
	case "KB", "K":
		mul = 1024
	case "MB", "M":
		mul = 1024 * 1024
	case "GB", "G":
		mul = 1024 * 1024 * 1024
	case "TB", "T":
		mul = 1024 * 1024 * 1024 * 1024
	case "PB", "P":
		mul = 1024 * 1024 * 1024 * 1024 * 1024
	default:
		return 0, false
	}
	return int64(f * mul), true
}

func parsePercent(raw string) (float64, bool) {
	raw = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(raw), "%"))
	if raw == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}
