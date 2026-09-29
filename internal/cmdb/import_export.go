package cmdb

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"auto-dba/internal/store"
)

// ImportInstanceRow 导入行（密码可空）。
type ImportInstanceRow struct {
	ID              string   `json:"id,omitempty"`
	Name            string   `json:"name"`
	DomainName      string   `json:"domain_name"`
	Host            string   `json:"host"`
	Port            int      `json:"port"`
	DBType          string   `json:"db_type"`
	IDC             string   `json:"idc,omitempty"`
	RegionCode      string   `json:"region_code,omitempty"`
	EnvCode         string   `json:"env_code,omitempty"`
	Username        string   `json:"username,omitempty"`
	Password        string   `json:"password,omitempty"`
	Tags            []string `json:"tags,omitempty"`
	CloudVendor     string   `json:"cloud_vendor,omitempty"`
	CloudInstanceID string   `json:"cloud_instance_id,omitempty"`
	CloudRegion     string   `json:"cloud_region,omitempty"`
}

type ImportResult struct {
	DryRun   bool                `json:"dry_run"`
	Created  int                 `json:"created"`
	Updated  int                 `json:"updated"`
	Skipped  int                 `json:"skipped"`
	Errors   []string            `json:"errors,omitempty"`
	Preview  []ImportInstanceRow `json:"preview,omitempty"`
}

// ExportInstances JSON 脱敏导出。
func (s *Service) ExportInstances() ([]ImportInstanceRow, error) {
	list, err := s.Store.ListInstances("", "")
	if err != nil {
		return nil, err
	}
	regions, _ := s.Store.ListRegions()
	envs, _ := s.Store.ListEnvironments()
	regionCode := map[string]string{}
	envCode := map[string]string{}
	for _, r := range regions {
		regionCode[r.ID] = r.Code
	}
	for _, e := range envs {
		envCode[e.ID] = e.Code
	}
	out := make([]ImportInstanceRow, 0, len(list))
	for _, it := range list {
		out = append(out, ImportInstanceRow{
			ID: it.ID, Name: it.Name, DomainName: it.DomainName, Host: it.Host, Port: it.Port,
			DBType: it.DBType, IDC: it.IDC, RegionCode: regionCode[it.RegionID], EnvCode: envCode[it.EnvID],
			Username: it.Username, Tags: it.Tags,
			CloudVendor: it.CloudVendor, CloudInstanceID: it.CloudInstanceID, CloudRegion: it.CloudRegion,
		})
	}
	return out, nil
}

func (s *Service) ExportInstancesCSV(w io.Writer) error {
	rows, err := s.ExportInstances()
	if err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"id", "name", "domain_name", "host", "port", "db_type", "idc", "region_code", "env_code", "username", "tags", "cloud_vendor", "cloud_instance_id", "cloud_region"})
	for _, r := range rows {
		_ = cw.Write([]string{
			r.ID, r.Name, r.DomainName, r.Host, fmt.Sprintf("%d", r.Port), r.DBType, r.IDC, r.RegionCode, r.EnvCode, r.Username,
			strings.Join(r.Tags, "|"), r.CloudVendor, r.CloudInstanceID, r.CloudRegion,
		})
	}
	cw.Flush()
	return cw.Error()
}

// ImportInstances 支持 dry_run 与 conflict=skip|update。
func (s *Service) ImportInstances(rows []ImportInstanceRow, dryRun bool, conflict string, actor string, encryptPassword func(plain string) (string, error)) (*ImportResult, error) {
	if conflict == "" {
		conflict = "skip"
	}
	res := &ImportResult{DryRun: dryRun, Preview: rows}
	regions, _ := s.Store.ListRegions()
	envs, _ := s.Store.ListEnvironments()
	regionByCode := map[string]string{}
	envByCode := map[string]string{}
	for _, r := range regions {
		regionByCode[strings.ToLower(r.Code)] = r.ID
	}
	for _, e := range envs {
		envByCode[strings.ToLower(e.Code)] = e.ID
	}
	exist, _ := s.Store.ListInstances("", "")
	byID := map[string]*store.Instance{}
	byHostPort := map[string]*store.Instance{}
	for i := range exist {
		it := &exist[i]
		byID[it.ID] = it
		byHostPort[fmt.Sprintf("%s:%d", it.Host, it.Port)] = it
	}

	for _, row := range rows {
		row.Name = strings.TrimSpace(row.Name)
		row.Host = strings.TrimSpace(row.Host)
		row.DomainName = strings.TrimSpace(row.DomainName)
		if row.Host == "" || row.Port <= 0 || row.DBType == "" {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: host/port/db_type 必填", row.Name))
			res.Skipped++
			continue
		}
		if row.Name == "" {
			row.Name = row.Host
		}
		if row.DomainName == "" {
			row.DomainName = row.Host
		}
		key := fmt.Sprintf("%s:%d", row.Host, row.Port)
		var matched *store.Instance
		if row.ID != "" {
			matched = byID[row.ID]
		}
		if matched == nil {
			matched = byHostPort[key]
		}
		if matched != nil {
			if conflict == "skip" {
				res.Skipped++
				continue
			}
			if dryRun {
				res.Updated++
				continue
			}
			before := *matched
			matched.Name = row.Name
			matched.DomainName = row.DomainName
			matched.Domains = []string{row.DomainName}
			matched.DBType = row.DBType
			matched.IDC = row.IDC
			matched.Tags = row.Tags
			matched.CloudVendor = row.CloudVendor
			matched.CloudInstanceID = row.CloudInstanceID
			matched.CloudRegion = row.CloudRegion
			if row.RegionCode != "" {
				matched.RegionID = regionByCode[strings.ToLower(row.RegionCode)]
			}
			if row.EnvCode != "" {
				matched.EnvID = envByCode[strings.ToLower(row.EnvCode)]
			}
			if row.Username != "" {
				matched.Username = row.Username
			}
			if strings.TrimSpace(row.Password) != "" && encryptPassword != nil {
				enc, err := encryptPassword(row.Password)
				if err != nil {
					res.Errors = append(res.Errors, row.Name+": 密码加密失败")
					res.Skipped++
					continue
				}
				matched.Password = enc
			}
			if err := s.Store.UpdateInstance(matched); err != nil {
				res.Errors = append(res.Errors, row.Name+": "+err.Error())
				res.Skipped++
				continue
			}
			_ = s.Store.AppendCmdbAudit(actor, "update", TypeInstance, matched.ID, "import", RedactInstance(before), RedactInstance(*matched))
			res.Updated++
			continue
		}
		if dryRun {
			res.Created++
			continue
		}
		it := store.Instance{
			Name: row.Name, DomainName: row.DomainName, Domains: []string{row.DomainName},
			Host: row.Host, Port: row.Port, DBType: row.DBType, IDC: row.IDC,
			Username: row.Username, Tags: row.Tags,
			CloudVendor: row.CloudVendor, CloudInstanceID: row.CloudInstanceID, CloudRegion: row.CloudRegion,
			Visibility: "global",
		}
		if row.ID != "" {
			it.ID = row.ID
		}
		if row.RegionCode != "" {
			it.RegionID = regionByCode[strings.ToLower(row.RegionCode)]
		}
		if row.EnvCode != "" {
			it.EnvID = envByCode[strings.ToLower(row.EnvCode)]
		}
		if strings.TrimSpace(row.Password) != "" && encryptPassword != nil {
			enc, err := encryptPassword(row.Password)
			if err != nil {
				res.Errors = append(res.Errors, row.Name+": 密码加密失败")
				res.Skipped++
				continue
			}
			it.Password = enc
		}
		if err := s.Store.CreateInstance(&it); err != nil {
			res.Errors = append(res.Errors, row.Name+": "+err.Error())
			res.Skipped++
			continue
		}
		_ = s.Store.AppendCmdbAudit(actor, "create", TypeInstance, it.ID, "import", nil, RedactInstance(it))
		res.Created++
		byHostPort[key] = &it
		byID[it.ID] = &it
	}
	return res, nil
}

func ParseImportJSON(raw []byte) ([]ImportInstanceRow, error) {
	var rows []ImportInstanceRow
	if err := json.Unmarshal(raw, &rows); err == nil {
		return rows, nil
	}
	var wrap struct {
		Instances []ImportInstanceRow `json:"instances"`
		Data      []ImportInstanceRow `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, err
	}
	if len(wrap.Instances) > 0 {
		return wrap.Instances, nil
	}
	return wrap.Data, nil
}

func ParseImportCSV(r io.Reader) ([]ImportInstanceRow, error) {
	cr := csv.NewReader(r)
	records, err := cr.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) < 2 {
		return []ImportInstanceRow{}, nil
	}
	header := map[string]int{}
	for i, h := range records[0] {
		header[strings.ToLower(strings.TrimSpace(h))] = i
	}
	col := func(row []string, name string) string {
		i, ok := header[name]
		if !ok || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}
	var out []ImportInstanceRow
	for _, row := range records[1:] {
		port := 0
		fmt.Sscanf(col(row, "port"), "%d", &port)
		tags := []string{}
		if t := col(row, "tags"); t != "" {
			tags = strings.Split(t, "|")
		}
		out = append(out, ImportInstanceRow{
			ID: col(row, "id"), Name: col(row, "name"), DomainName: col(row, "domain_name"),
			Host: col(row, "host"), Port: port, DBType: col(row, "db_type"), IDC: col(row, "idc"),
			RegionCode: col(row, "region_code"), EnvCode: col(row, "env_code"),
			Username: col(row, "username"), Password: col(row, "password"), Tags: tags,
			CloudVendor: col(row, "cloud_vendor"), CloudInstanceID: col(row, "cloud_instance_id"), CloudRegion: col(row, "cloud_region"),
		})
	}
	return out, nil
}
