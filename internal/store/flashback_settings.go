package store

import (
	"strings"
)

const (
	ArgFlashbackTencentSecretID       = "flashback_tencent_secret_id"
	ArgFlashbackTencentSecretKey      = "flashback_tencent_secret_key"
	ArgFlashbackTencentRegion         = "flashback_tencent_region"
	ArgFlashbackTencentRegionMap      = "flashback_tencent_region_map"
	ArgFlashbackAliyunAccessKeyID     = "flashback_aliyun_access_key_id"
	ArgFlashbackAliyunAccessKeySecret = "flashback_aliyun_access_key_secret"
	ArgFlashbackHuaweiAccessKeyID     = "flashback_huawei_access_key_id"
	ArgFlashbackHuaweiSecretAccessKey = "flashback_huawei_secret_access_key"
	ArgFlashbackAWSAccessKeyID        = "flashback_aws_access_key_id"
	ArgFlashbackAWSSecretAccessKey    = "flashback_aws_secret_access_key"
	ArgFlashbackCloudLookbackHours    = "flashback_cloud_wal_lookback_hours"
	ArgFlashbackCloudDownloadMbps     = "flashback_cloud_download_mbps"
	ArgFlashbackCloudAPIIntervalMS    = "flashback_cloud_api_interval_ms"
	ArgFlashbackCloudMaxPackages      = "flashback_cloud_max_packages"
	ArgFlashbackCloudPkgRetries       = "flashback_cloud_pkg_retries"
)

type FlashbackCloudVendorStatus struct {
	Vendor     string `json:"vendor"`
	Name       string `json:"name"`
	IDKey      string `json:"id_key"`
	KeyKey     string `json:"key_key"`
	Configured bool   `json:"configured"`
}

type FlashbackCloudArgItem struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
	Secret      bool   `json:"secret"`
	Source      string `json:"source,omitempty"`
}

type FlashbackCloudSettings struct {
	Vendors []FlashbackCloudVendorStatus `json:"vendors"`
	Args    []FlashbackCloudArgItem      `json:"args"`
}

type flashbackCloudArgMeta struct {
	Key, Description string
	Secret           bool
}

func flashbackCloudArgMetas() []flashbackCloudArgMeta {
	return []flashbackCloudArgMeta{
		{ArgFlashbackTencentSecretID, "腾讯云 SecretId", true},
		{ArgFlashbackTencentSecretKey, "腾讯云 SecretKey", true},
		{ArgFlashbackTencentRegion, "腾讯云默认 Region，如 ap-guangzhou", false},
		{ArgFlashbackTencentRegionMap, "腾讯云 Region 映射 JSON（可选）", false},
		{ArgFlashbackAliyunAccessKeyID, "阿里云 AccessKey ID", true},
		{ArgFlashbackAliyunAccessKeySecret, "阿里云 AccessKey Secret", true},
		{ArgFlashbackHuaweiAccessKeyID, "华为云 AccessKey ID", true},
		{ArgFlashbackHuaweiSecretAccessKey, "华为云 Secret Access Key", true},
		{ArgFlashbackAWSAccessKeyID, "AWS AccessKey ID", true},
		{ArgFlashbackAWSSecretAccessKey, "AWS Secret Access Key", true},
		{ArgFlashbackCloudLookbackHours, "云日志回溯小时数", false},
		{ArgFlashbackCloudDownloadMbps, "云日志下载限速 Mbps", false},
		{ArgFlashbackCloudAPIIntervalMS, "云 API 间隔毫秒", false},
		{ArgFlashbackCloudMaxPackages, "单次最多增量包数", false},
		{ArgFlashbackCloudPkgRetries, "增量包下载重试次数", false},
	}
}

func flashbackCloudVendorNames() []struct{ code, name, idKey, keyKey string } {
	return []struct{ code, name, idKey, keyKey string }{
		{"tencent", "腾讯云", ArgFlashbackTencentSecretID, ArgFlashbackTencentSecretKey},
		{"aliyun", "阿里云", ArgFlashbackAliyunAccessKeyID, ArgFlashbackAliyunAccessKeySecret},
		{"huawei", "华为云", ArgFlashbackHuaweiAccessKeyID, ArgFlashbackHuaweiSecretAccessKey},
		{"aws", "AWS", ArgFlashbackAWSAccessKeyID, ArgFlashbackAWSSecretAccessKey},
	}
}

func IsFlashbackSecretArg(key string) bool {
	key = strings.TrimSpace(key)
	for _, m := range flashbackCloudArgMetas() {
		if m.Key == key {
			return m.Secret
		}
	}
	return false
}

func IsFlashbackCloudArg(key string) bool {
	key = strings.TrimSpace(key)
	for _, m := range flashbackCloudArgMetas() {
		if m.Key == key {
			return true
		}
	}
	return false
}

func IsMaskedSecret(v string) bool { return isMaskedSecret(v) }

func flashbackCloudSettingDefaults() []GlobalArg {
	return []GlobalArg{
		{Key: ArgFlashbackTencentSecretID, Value: "", Description: "腾讯云 SecretId"},
		{Key: ArgFlashbackTencentSecretKey, Value: "", Description: "腾讯云 SecretKey"},
		{Key: ArgFlashbackTencentRegion, Value: "", Description: "腾讯云默认 Region，如 ap-guangzhou"},
		{Key: ArgFlashbackTencentRegionMap, Value: "", Description: "腾讯云 Region 映射 JSON（可选）"},
		{Key: ArgFlashbackAliyunAccessKeyID, Value: "", Description: "阿里云 AccessKey ID"},
		{Key: ArgFlashbackAliyunAccessKeySecret, Value: "", Description: "阿里云 AccessKey Secret"},
		{Key: ArgFlashbackHuaweiAccessKeyID, Value: "", Description: "华为云 AccessKey ID"},
		{Key: ArgFlashbackHuaweiSecretAccessKey, Value: "", Description: "华为云 Secret Access Key"},
		{Key: ArgFlashbackAWSAccessKeyID, Value: "", Description: "AWS AccessKey ID"},
		{Key: ArgFlashbackAWSSecretAccessKey, Value: "", Description: "AWS Secret Access Key"},
		{Key: ArgFlashbackCloudLookbackHours, Value: "2", Description: "云日志回溯小时数"},
		{Key: ArgFlashbackCloudDownloadMbps, Value: "16", Description: "云日志下载限速 Mbps"},
		{Key: ArgFlashbackCloudAPIIntervalMS, Value: "400", Description: "云 API 间隔毫秒"},
		{Key: ArgFlashbackCloudMaxPackages, Value: "48", Description: "单次最多增量包数"},
		{Key: ArgFlashbackCloudPkgRetries, Value: "3", Description: "增量包下载重试次数"},
	}
}

func (d *DB) EnsureFlashbackCloudSettings() error {
	for _, a := range flashbackCloudSettingDefaults() {
		var exists int
		_ = d.QueryRow(`SELECT COUNT(1) FROM tbl_global_args WHERE key=?`, a.Key).Scan(&exists)
		if exists > 0 {
			continue
		}
		if err := d.SetGlobalArg(a.Key, a.Value, a.Description); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) GetFlashbackCloudSettings() FlashbackCloudSettings {
	_ = d.EnsureFlashbackCloudSettings()
	out := FlashbackCloudSettings{
		Vendors: make([]FlashbackCloudVendorStatus, 0, 4),
		Args:    make([]FlashbackCloudArgItem, 0, len(flashbackCloudArgMetas())),
	}
	for _, v := range flashbackCloudVendorNames() {
		id := strings.TrimSpace(d.argOr(v.idKey, ""))
		key := strings.TrimSpace(d.argOr(v.keyKey, ""))
		out.Vendors = append(out.Vendors, FlashbackCloudVendorStatus{
			Vendor: v.code, Name: v.name, IDKey: v.idKey, KeyKey: v.keyKey,
			Configured: id != "" && key != "",
		})
	}
	for _, m := range flashbackCloudArgMetas() {
		val := d.argOr(m.Key, "")
		src := "db"
		if val == "" {
			src = ""
		}
		if m.Secret {
			if val != "" {
				val = "********"
			}
		}
		out.Args = append(out.Args, FlashbackCloudArgItem{
			Key: m.Key, Value: val, Description: m.Description, Secret: m.Secret, Source: src,
		})
	}
	return out
}

func (d *DB) SaveFlashbackCloudSettings(items []FlashbackCloudArgItem) error {
	allowed := map[string]flashbackCloudArgMeta{}
	for _, m := range flashbackCloudArgMetas() {
		allowed[m.Key] = m
	}
	for _, item := range items {
		key := strings.TrimSpace(item.Key)
		meta, ok := allowed[key]
		if !ok {
			continue
		}
		val := item.Value
		if meta.Secret && (strings.TrimSpace(val) == "" || isMaskedSecret(val)) {
			continue
		}
		desc := strings.TrimSpace(item.Description)
		if desc == "" {
			desc = meta.Description
		}
		if err := d.SetGlobalArg(key, val, desc); err != nil {
			return err
		}
	}
	return nil
}
