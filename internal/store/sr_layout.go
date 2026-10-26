package store

import (
	"fmt"
	"strings"
)

const (
	ArgSrFeSoftwareBase = "sr_fe_software_base"
	ArgSrBeSoftwareBase = "sr_be_software_base"
	ArgSrBeDataRoot     = "sr_be_data_root"

	DefaultSrSoftwareBase = "/home/dorisdb_base"
	DefaultSrDataRoot     = "/data"
)

// SrLayoutDefaults StarRocks 集群未单独填写时使用的全局路径。
type SrLayoutDefaults struct {
	FeSoftwareBase string `json:"fe_software_base"`
	BeSoftwareBase string `json:"be_software_base"`
	BeDataRoot     string `json:"be_data_root"`
}

// EnsureSrLayoutDefaults 仅在参数不存在时写入缺省路径。
func (d *DB) EnsureSrLayoutDefaults() {
	if d == nil {
		return
	}
	_ = d.EnsureGlobalArgDefault(ArgSrFeSoftwareBase, DefaultSrSoftwareBase, "StarRocks FE software_base 全局默认；集群未填写时使用")
	_ = d.EnsureGlobalArgDefault(ArgSrBeSoftwareBase, DefaultSrSoftwareBase, "StarRocks BE software_base 全局默认；集群未填写时使用，不回退到 FE 路径")
	_ = d.EnsureGlobalArgDefault(ArgSrBeDataRoot, DefaultSrDataRoot, "StarRocks BE 数据目录全局默认；集群未填写时使用")
}

// SrLayoutDefaults 读取全局路径；空值回退到内置缺省。
func (d *DB) SrLayoutDefaults() SrLayoutDefaults {
	out := SrLayoutDefaults{
		FeSoftwareBase: DefaultSrSoftwareBase,
		BeSoftwareBase: DefaultSrSoftwareBase,
		BeDataRoot:     DefaultSrDataRoot,
	}
	if d == nil {
		return out
	}
	if v, err := d.GetGlobalArg(ArgSrFeSoftwareBase); err == nil {
		if s := strings.TrimRight(strings.TrimSpace(v), "/"); s != "" {
			out.FeSoftwareBase = s
		}
	}
	if v, err := d.GetGlobalArg(ArgSrBeSoftwareBase); err == nil {
		if s := strings.TrimRight(strings.TrimSpace(v), "/"); s != "" {
			out.BeSoftwareBase = s
		}
	}
	if v, err := d.GetGlobalArg(ArgSrBeDataRoot); err == nil {
		if s := strings.TrimRight(strings.TrimSpace(v), "/"); s != "" {
			out.BeDataRoot = s
		}
	}
	return out
}

// SetSrLayoutDefaults 保存三项全局路径，须为绝对路径。
func (d *DB) SetSrLayoutDefaults(v SrLayoutDefaults) error {
	if d == nil {
		return fmt.Errorf("存储未初始化")
	}
	fe, err := normalizeSrLayoutPath(v.FeSoftwareBase, "FE software_base")
	if err != nil {
		return err
	}
	be, err := normalizeSrLayoutPath(v.BeSoftwareBase, "BE software_base")
	if err != nil {
		return err
	}
	data, err := normalizeSrLayoutPath(v.BeDataRoot, "BE 数据目录")
	if err != nil {
		return err
	}
	if err := d.SetGlobalArg(ArgSrFeSoftwareBase, fe, "StarRocks FE software_base 全局默认；集群未填写时使用"); err != nil {
		return err
	}
	if err := d.SetGlobalArg(ArgSrBeSoftwareBase, be, "StarRocks BE software_base 全局默认；集群未填写时使用，不回退到 FE 路径"); err != nil {
		return err
	}
	return d.SetGlobalArg(ArgSrBeDataRoot, data, "StarRocks BE 数据目录全局默认；集群未填写时使用")
}

func normalizeSrLayoutPath(p, label string) (string, error) {
	p = strings.TrimRight(strings.TrimSpace(p), "/")
	if p == "" {
		return "", fmt.Errorf("%s 不能为空", label)
	}
	if !strings.HasPrefix(p, "/") || strings.Contains(p, "..") {
		return "", fmt.Errorf("%s 须为绝对路径", label)
	}
	return p, nil
}

// ResolveSrPaths 集群字段优先，空则全局，再空则内置缺省。BE 软件目录不回退到 FE。
func ResolveSrPaths(d *DB, cl *SrCluster) (fe, be, data string) {
	def := SrLayoutDefaults{
		FeSoftwareBase: DefaultSrSoftwareBase,
		BeSoftwareBase: DefaultSrSoftwareBase,
		BeDataRoot:     DefaultSrDataRoot,
	}
	if d != nil {
		def = d.SrLayoutDefaults()
	}
	fe = def.FeSoftwareBase
	be = def.BeSoftwareBase
	data = def.BeDataRoot
	if cl != nil {
		if s := strings.TrimRight(strings.TrimSpace(cl.SoftwareBase), "/"); s != "" {
			fe = s
		}
		if s := strings.TrimRight(strings.TrimSpace(cl.BeSoftwareBase), "/"); s != "" {
			be = s
		}
		if s := strings.TrimRight(strings.TrimSpace(cl.DataRoot), "/"); s != "" {
			data = s
		}
	}
	if fe == "" {
		fe = DefaultSrSoftwareBase
	}
	if be == "" {
		be = DefaultSrSoftwareBase
	}
	if data == "" {
		data = DefaultSrDataRoot
	}
	return fe, be, data
}
