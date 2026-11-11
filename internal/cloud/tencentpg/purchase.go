package tencentpg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"auto-dba/internal/cloud"
	"auto-dba/internal/cloud/tencentapi"
)

const (
	vpcHost    = "vpc.tencentcloudapi.com"
	vpcVersion = "2017-03-12"
	vpcService = "vpc"
	tagHost    = "tag.tencentcloudapi.com"
	tagVersion = "2018-08-13"
	tagService = "tag"
)

func (r Reader) PurchaseCatalog(ctx context.Context, cred cloud.Cred, vpcID string) (cloud.PurchaseCatalog, error) {
	if r.Client == nil {
		return cloud.PurchaseCatalog{}, fmt.Errorf("tencentpg: client 未配置")
	}
	return r.Client.purchaseCatalog(ctx, cred, vpcID)
}

func (c *Client) purchaseCatalog(ctx context.Context, cred cloud.Cred, vpcID string) (cloud.PurchaseCatalog, error) {
	cat := cloud.PurchaseCatalog{
		Zones: []cloud.NamedOption{}, Versions: []cloud.VersionOption{},
		VPCs: []cloud.NamedOption{}, Subnets: []cloud.SubnetOption{},
		SecurityGroups: []cloud.NamedOption{}, Projects: []cloud.ProjectOption{},
	}
	if raw, err := c.call(ctx, cred, "DescribeZones", map[string]any{}); err != nil {
		noteCatalog(&cat, "zones", err)
	} else if zones, err := parseZones(raw); err != nil {
		noteCatalog(&cat, "zones", err)
	} else {
		cat.Zones = zones
	}
	if raw, err := c.call(ctx, cred, "DescribeDBVersions", map[string]any{"StorageType": "PHYSICAL_LOCAL_SSD"}); err != nil {
		noteCatalog(&cat, "versions", err)
	} else if versions, err := parseDBVersions(raw); err != nil {
		noteCatalog(&cat, "versions", err)
	} else {
		cat.Versions = versions
	}
	vpcs, err := c.listVPCs(ctx, cred)
	if err != nil {
		noteCatalog(&cat, "vpcs", err)
	} else {
		cat.VPCs = vpcs
	}
	if strings.TrimSpace(vpcID) != "" {
		subnets, err := c.listSubnets(ctx, cred, vpcID)
		if err != nil {
			noteCatalog(&cat, "subnets", err)
		} else {
			cat.Subnets = subnets
		}
	}
	groups, err := c.listSecurityGroups(ctx, cred)
	if err != nil {
		noteCatalog(&cat, "security_groups", err)
	} else {
		cat.SecurityGroups = groups
	}
	projects, err := c.listProjects(ctx, cred)
	if err != nil {
		noteCatalog(&cat, "projects", err)
	} else {
		cat.Projects = projects
	}
	return cat, nil
}

func noteCatalog(cat *cloud.PurchaseCatalog, key string, err error) {
	if err == nil {
		return
	}
	if cat.Errors == nil {
		cat.Errors = map[string]string{}
	}
	cat.Errors[key] = err.Error()
}

func (c *Client) otherCall(ctx context.Context, cred cloud.Cred, host, service, version, action string, payload any) ([]byte, error) {
	useHost := host
	if c != nil && c.Host != "" {
		useHost = c.Host
	}
	client := &tencentapi.Client{Host: useHost, Service: service, Version: version}
	if c != nil {
		client.Do = c.Do
	}
	return client.Call(ctx, cred, action, payload)
}

func (c *Client) listVPCs(ctx context.Context, cred cloud.Cred) ([]cloud.NamedOption, error) {
	var out []cloud.NamedOption
	err := c.eachPage(ctx, cred, vpcHost, vpcService, vpcVersion, "DescribeVpcs", nil, func(raw []byte) (int, int, error) {
		page, total, err := parseVPCs(raw)
		if err != nil {
			return 0, 0, err
		}
		out = append(out, page...)
		return len(page), total, nil
	})
	if out == nil {
		out = []cloud.NamedOption{}
	}
	return out, err
}

func (c *Client) listSubnets(ctx context.Context, cred cloud.Cred, vpcID string) ([]cloud.SubnetOption, error) {
	filter := map[string]any{"Filters": []map[string]any{{"Name": "vpc-id", "Values": []string{vpcID}}}}
	var out []cloud.SubnetOption
	err := c.eachPage(ctx, cred, vpcHost, vpcService, vpcVersion, "DescribeSubnets", filter, func(raw []byte) (int, int, error) {
		page, total, err := parseSubnets(raw)
		if err != nil {
			return 0, 0, err
		}
		out = append(out, page...)
		return len(page), total, nil
	})
	if out == nil {
		out = []cloud.SubnetOption{}
	}
	return out, err
}

func (c *Client) listSecurityGroups(ctx context.Context, cred cloud.Cred) ([]cloud.NamedOption, error) {
	var out []cloud.NamedOption
	err := c.eachPage(ctx, cred, vpcHost, vpcService, vpcVersion, "DescribeSecurityGroups", nil, func(raw []byte) (int, int, error) {
		page, total, err := parseSecurityGroups(raw)
		if err != nil {
			return 0, 0, err
		}
		out = append(out, page...)
		return len(page), total, nil
	})
	if out == nil {
		out = []cloud.NamedOption{}
	}
	return out, err
}

func (c *Client) listProjects(ctx context.Context, cred cloud.Cred) ([]cloud.ProjectOption, error) {
	raw, err := c.otherCall(ctx, cred, tagHost, tagService, tagVersion, "DescribeProjects", map[string]any{"AllList": 1, "Limit": 1000, "Offset": 0})
	if err != nil {
		return []cloud.ProjectOption{}, err
	}
	return parseProjects(raw)
}

func (c *Client) eachPage(ctx context.Context, cred cloud.Cred, host, service, version, action string, base map[string]any, take func([]byte) (int, int, error)) error {
	offset := 0
	for page := 0; page < 10; page++ {
		payload := map[string]any{"Limit": "100", "Offset": fmt.Sprintf("%d", offset)}
		for k, v := range base {
			payload[k] = v
		}
		raw, err := c.otherCall(ctx, cred, host, service, version, action, payload)
		if err != nil {
			return err
		}
		n, total, err := take(raw)
		if err != nil {
			return err
		}
		offset += n
		if n == 0 || offset >= total || n < 100 {
			return nil
		}
	}
	return nil
}

func parseZones(raw []byte) ([]cloud.NamedOption, error) {
	var resp struct {
		Response struct {
			ZoneSet []struct {
				Zone      string `json:"Zone"`
				ZoneName  string `json:"ZoneName"`
				ZoneState string `json:"ZoneState"`
			} `json:"ZoneSet"`
		} `json:"Response"`
	}
	if err := decodeResponse(raw, &resp); err != nil {
		return nil, err
	}
	out := []cloud.NamedOption{}
	for _, z := range resp.Response.ZoneSet {
		if z.Zone == "" {
			continue
		}
		state := strings.ToUpper(strings.TrimSpace(z.ZoneState))
		if state != "" && state != "AVAILABLE" {
			continue
		}
		name := z.ZoneName
		if name == "" {
			name = z.Zone
		}
		out = append(out, cloud.NamedOption{ID: z.Zone, Name: name})
	}
	return out, nil
}

func parseDBVersions(raw []byte) ([]cloud.VersionOption, error) {
	var resp struct {
		Response struct {
			VersionSet []struct {
				DBEngine        string `json:"DBEngine"`
				DBMajorVersion  string `json:"DBMajorVersion"`
				DBVersion       string `json:"DBVersion"`
				DBKernelVersion string `json:"DBKernelVersion"`
				Status          string `json:"Status"`
			} `json:"VersionSet"`
		} `json:"Response"`
	}
	if err := decodeResponse(raw, &resp); err != nil {
		return nil, err
	}
	best := map[string]cloud.VersionOption{}
	for _, v := range resp.Response.VersionSet {
		if v.DBEngine != "" && v.DBEngine != "postgresql" {
			continue
		}
		if strings.ToUpper(v.Status) != "" && strings.ToUpper(v.Status) != "AVAILABLE" {
			continue
		}
		major := strings.TrimSpace(v.DBMajorVersion)
		if major == "" {
			continue
		}
		label := "PostgreSQL " + major
		if v.DBVersion != "" {
			label += "（" + v.DBVersion
			if v.DBKernelVersion != "" {
				label += " / " + v.DBKernelVersion
			}
			label += "）"
		}
		next := cloud.VersionOption{Major: major, Version: v.DBVersion, Kernel: v.DBKernelVersion, Label: label}
		if cur, ok := best[major]; !ok || versionNewer(next.Version, cur.Version) {
			best[major] = next
		}
	}
	out := make([]cloud.VersionOption, 0, len(best))
	for _, v := range best {
		out = append(out, v)
	}
	sortVersions(out)
	return out, nil
}

func versionNewer(a, b string) bool {
	ap, bp := splitVersion(a), splitVersion(b)
	n := len(ap)
	if len(bp) > n {
		n = len(bp)
	}
	for i := 0; i < n; i++ {
		av, bv := 0, 0
		if i < len(ap) {
			av = ap[i]
		}
		if i < len(bp) {
			bv = bp[i]
		}
		if av != bv {
			return av > bv
		}
	}
	return false
}

func splitVersion(v string) []int {
	var out []int
	n := 0
	seen := false
	for _, r := range v {
		if r >= '0' && r <= '9' {
			n = n*10 + int(r-'0')
			seen = true
			continue
		}
		if seen {
			out = append(out, n)
			n = 0
			seen = false
		}
	}
	if seen {
		out = append(out, n)
	}
	return out
}

func sortVersions(list []cloud.VersionOption) {
	for i := 1; i < len(list); i++ {
		j := i
		for j > 0 && versionNewer(list[j-1].Major, list[j].Major) {
			list[j-1], list[j] = list[j], list[j-1]
			j--
		}
	}
}

func parseVPCs(raw []byte) ([]cloud.NamedOption, int, error) {
	var resp struct {
		Response struct {
			TotalCount int `json:"TotalCount"`
			VpcSet     []struct {
				VpcID     string `json:"VpcId"`
				VpcName   string `json:"VpcName"`
				CidrBlock string `json:"CidrBlock"`
			} `json:"VpcSet"`
		} `json:"Response"`
	}
	if err := decodeResponse(raw, &resp); err != nil {
		return nil, 0, err
	}
	out := make([]cloud.NamedOption, 0, len(resp.Response.VpcSet))
	for _, v := range resp.Response.VpcSet {
		if v.VpcID == "" {
			continue
		}
		name := v.VpcName
		if name == "" {
			name = v.VpcID
		}
		if v.CidrBlock != "" {
			name += "（" + v.CidrBlock + "）"
		}
		out = append(out, cloud.NamedOption{ID: v.VpcID, Name: name})
	}
	return out, resp.Response.TotalCount, nil
}

func parseSubnets(raw []byte) ([]cloud.SubnetOption, int, error) {
	var resp struct {
		Response struct {
			TotalCount int `json:"TotalCount"`
			SubnetSet  []struct {
				SubnetID    string `json:"SubnetId"`
				SubnetName  string `json:"SubnetName"`
				CidrBlock   string `json:"CidrBlock"`
				Zone        string `json:"Zone"`
				AvailableIP int    `json:"AvailableIpAddressCount"`
			} `json:"SubnetSet"`
		} `json:"Response"`
	}
	if err := decodeResponse(raw, &resp); err != nil {
		return nil, 0, err
	}
	out := make([]cloud.SubnetOption, 0, len(resp.Response.SubnetSet))
	for _, s := range resp.Response.SubnetSet {
		if s.SubnetID == "" {
			continue
		}
		name := s.SubnetName
		if name == "" {
			name = s.SubnetID
		}
		out = append(out, cloud.SubnetOption{ID: s.SubnetID, Name: name, CIDR: s.CidrBlock, Zone: s.Zone, AvailableIP: s.AvailableIP})
	}
	return out, resp.Response.TotalCount, nil
}

func parseSecurityGroups(raw []byte) ([]cloud.NamedOption, int, error) {
	var resp struct {
		Response struct {
			TotalCount       int `json:"TotalCount"`
			SecurityGroupSet []struct {
				ID   string `json:"SecurityGroupId"`
				Name string `json:"SecurityGroupName"`
			} `json:"SecurityGroupSet"`
		} `json:"Response"`
	}
	if err := decodeResponse(raw, &resp); err != nil {
		return nil, 0, err
	}
	out := make([]cloud.NamedOption, 0, len(resp.Response.SecurityGroupSet))
	for _, g := range resp.Response.SecurityGroupSet {
		if g.ID == "" {
			continue
		}
		name := g.Name
		if name == "" {
			name = g.ID
		}
		out = append(out, cloud.NamedOption{ID: g.ID, Name: name})
	}
	return out, resp.Response.TotalCount, nil
}

func parseProjects(raw []byte) ([]cloud.ProjectOption, error) {
	var resp struct {
		Response struct {
			Projects []struct {
				ProjectID   int    `json:"ProjectId"`
				ProjectName string `json:"ProjectName"`
			} `json:"Projects"`
		} `json:"Response"`
	}
	if err := decodeResponse(raw, &resp); err != nil {
		return nil, err
	}
	out := []cloud.ProjectOption{}
	for _, p := range resp.Response.Projects {
		name := p.ProjectName
		if name == "" {
			name = fmt.Sprintf("%d", p.ProjectID)
		}
		out = append(out, cloud.ProjectOption{ID: p.ProjectID, Name: name})
	}
	return out, nil
}

func decodeResponse(raw []byte, dest any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return fmt.Errorf("云接口没有返回内容")
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("云接口返回无法解析")
	}
	return nil
}
