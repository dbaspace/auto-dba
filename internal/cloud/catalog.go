package cloud

import "context"

type NamedOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type VersionOption struct {
	Major   string `json:"major"`
	Version string `json:"version"`
	Kernel  string `json:"kernel,omitempty"`
	Label   string `json:"label"`
}

type SubnetOption struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	CIDR        string `json:"cidr,omitempty"`
	Zone        string `json:"zone,omitempty"`
	AvailableIP int    `json:"available_ip,omitempty"`
}

type ProjectOption struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type PurchaseCatalog struct {
	Zones          []NamedOption     `json:"zones"`
	Versions       []VersionOption   `json:"versions"`
	VPCs           []NamedOption     `json:"vpcs"`
	Subnets        []SubnetOption    `json:"subnets"`
	SecurityGroups []NamedOption     `json:"security_groups"`
	Projects       []ProjectOption   `json:"projects"`
	Errors         map[string]string `json:"errors,omitempty"`
}

// CatalogReader 用只读密钥查询购买页需要的云上选项。
type CatalogReader interface {
	PurchaseCatalog(ctx context.Context, cred Cred, vpcID string) (PurchaseCatalog, error)
}
