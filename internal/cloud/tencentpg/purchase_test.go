package tencentpg

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"auto-dba/internal/cloud"
)

func TestPurchaseCatalog(t *testing.T) {
	c := Reader{&Client{Host: "example.test", Do: func(r *http.Request) (*http.Response, error) {
		action := r.Header.Get("X-TC-Action")
		if action == "DescribeVpcs" || action == "DescribeSubnets" || action == "DescribeSecurityGroups" {
			raw, _ := io.ReadAll(r.Body)
			text := string(raw)
			if !strings.Contains(text, `"Limit":"100"`) || !strings.Contains(text, `"Offset":"0"`) {
				t.Fatalf("%s limit type %s", action, text)
			}
		}
		body := `{"Response":{"Error":{"Code":"Missing","Message":"no projects"}}}`
		switch action {
		case "DescribeZones":
			body = `{"Response":{"ZoneSet":[{"Zone":"ap-beijing-6","ZoneName":"北京六区","ZoneState":"AVAILABLE"},{"Zone":"ap-beijing-1","ZoneName":"售罄","ZoneState":"UNAVAILABLE"}]}}`
		case "DescribeDBVersions":
			body = `{"Response":{"VersionSet":[
				{"DBEngine":"postgresql","DBMajorVersion":"14","DBVersion":"14.2","DBKernelVersion":"v14.2_r1.1","Status":"AVAILABLE"},
				{"DBEngine":"postgresql","DBMajorVersion":"14","DBVersion":"14.11","DBKernelVersion":"v14.11_r1.0","Status":"AVAILABLE"},
				{"DBEngine":"mssql_compatible","DBMajorVersion":"14","DBVersion":"14.3","Status":"AVAILABLE"}
			]}}`
		case "DescribeVpcs":
			body = `{"Response":{"TotalCount":1,"VpcSet":[{"VpcId":"vpc-1","VpcName":"ai","CidrBlock":"10.0.0.0/16"}]}}`
		case "DescribeSubnets":
			body = `{"Response":{"TotalCount":1,"SubnetSet":[{"SubnetId":"subnet-1","SubnetName":"net","CidrBlock":"10.0.1.0/24","Zone":"ap-beijing-6","AvailableIpAddressCount":20}]}}`
		case "DescribeSecurityGroups":
			body = `{"Response":{"TotalCount":1,"SecurityGroupSet":[{"SecurityGroupId":"sg-1","SecurityGroupName":"default"}]}}`
		case "DescribeProjects":
			body = `{"Response":{"Error":{"Code":"AuthFailure","Message":"no project permission"}}}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	}}}
	got, err := c.PurchaseCatalog(context.Background(), cloud.Cred{SecretID: "i", SecretKey: "k", Region: "ap-beijing"}, "vpc-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Zones) != 1 || got.Zones[0].ID != "ap-beijing-6" || got.Zones[0].Name != "北京六区" {
		t.Fatalf("zones %+v", got.Zones)
	}
	if len(got.Versions) != 1 || got.Versions[0].Major != "14" || got.Versions[0].Version != "14.11" {
		t.Fatalf("versions %+v", got.Versions)
	}
	if len(got.VPCs) != 1 || got.VPCs[0].ID != "vpc-1" || len(got.Subnets) != 1 || got.Subnets[0].AvailableIP != 20 {
		t.Fatalf("net %+v %+v", got.VPCs, got.Subnets)
	}
	if len(got.SecurityGroups) != 1 || got.SecurityGroups[0].ID != "sg-1" {
		t.Fatalf("sg %+v", got.SecurityGroups)
	}
	if got.Errors["projects"] == "" || !strings.Contains(got.Errors["projects"], "no project permission") {
		t.Fatalf("errors %+v", got.Errors)
	}
	if _, err := parseZones(nil); err == nil || strings.Contains(err.Error(), "unexpected") {
		t.Fatal(err)
	}
}
