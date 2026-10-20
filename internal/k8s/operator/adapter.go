package operator

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"auto-dba/internal/k8s"
)

// OperatorInfo 检测到的 Operator。
type OperatorInfo struct {
	Engine    string `json:"engine"`
	Name      string `json:"name"`
	Version   string `json:"version,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Installed bool   `json:"installed"`
	CRDs      []string `json:"crds,omitempty"`
}

// InstallOpt 安装选项。
type InstallOpt struct {
	Namespace string
	Version   string
	Values    map[string]any
}

// Resources CPU/内存规格。
type Resources struct {
	CPU    string `json:"cpu,omitempty"`
	Memory string `json:"memory,omitempty"`
	Storage string `json:"storage,omitempty"`
}

// DBClusterSpec 模板化创建规格。
type DBClusterSpec struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace"`
	Engine      string            `json:"engine"`
	Version     string            `json:"version"`
	Replicas    int               `json:"replicas"`
	StorageClass string           `json:"storage_class,omitempty"`
	StorageSize string            `json:"storage_size,omitempty"`
	Resources   Resources         `json:"resources"`
	Parameters  map[string]string `json:"parameters,omitempty"`
	BackupDest  *BackupDest       `json:"backup_dest,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
}

// CRRef CR 引用。
type CRRef struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Kind      string `json:"kind,omitempty"`
}

// DBClusterStatus 状态摘要。
type DBClusterStatus struct {
	Phase      string `json:"phase"`
	Ready      int    `json:"ready"`
	Replicas   int    `json:"replicas"`
	Primary    string `json:"primary,omitempty"`
	Message    string `json:"message,omitempty"`
	Endpoint   string `json:"endpoint,omitempty"`
}

// BackupDest 备份目标。
type BackupDest struct {
	Kind      string `json:"kind"` // s3 | local
	Endpoint  string `json:"endpoint,omitempty"`
	Bucket    string `json:"bucket,omitempty"`
	Prefix    string `json:"prefix,omitempty"`
	SecretRef string `json:"secret_ref,omitempty"`
}

// Endpoint 连接端点。
type Endpoint struct {
	Name string `json:"name"`
	Host string `json:"host"`
	Port int    `json:"port"`
	Role string `json:"role,omitempty"` // rw | ro
}

// Adapter Operator 适配器。
type Adapter interface {
	Engine() string
	Detect(ctx context.Context, c *k8s.Client) (*OperatorInfo, error)
	Install(ctx context.Context, c *k8s.Client, opt InstallOpt) error
	Render(spec DBClusterSpec) (*unstructured.Unstructured, error)
	Status(ctx context.Context, c *k8s.Client, ref CRRef) (*DBClusterStatus, error)
	Scale(ctx context.Context, c *k8s.Client, ref CRRef, replicas int, res Resources) error
	Upgrade(ctx context.Context, c *k8s.Client, ref CRRef, version string) error
	Switchover(ctx context.Context, c *k8s.Client, ref CRRef, target string) error
	Backup(ctx context.Context, c *k8s.Client, ref CRRef, dest BackupDest) error
	Endpoints(ctx context.Context, c *k8s.Client, ref CRRef) ([]Endpoint, error)
}

// Registry 适配器注册表。
type Registry struct {
	byEngine map[string]Adapter
}

func NewRegistry(adapters ...Adapter) *Registry {
	r := &Registry{byEngine: map[string]Adapter{}}
	for _, a := range adapters {
		if a != nil {
			r.byEngine[a.Engine()] = a
		}
	}
	return r
}

func (r *Registry) Get(engine string) Adapter {
	if r == nil {
		return nil
	}
	return r.byEngine[engine]
}

func (r *Registry) All() []Adapter {
	if r == nil {
		return nil
	}
	out := make([]Adapter, 0, len(r.byEngine))
	for _, a := range r.byEngine {
		out = append(out, a)
	}
	return out
}

// DefaultRegistry 内置适配器。
func DefaultRegistry() *Registry {
	return NewRegistry(
		NewCNPG(),
		NewPerconaMySQL(),
		NewRedisOT(),
		NewStarRocks(),
		NewKubeBlocks(),
	)
}
