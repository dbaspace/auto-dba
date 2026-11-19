package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
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
		{ArgFlashbackCloudLookbackHours, "云日志额外向前回溯的小时数。0 表示不再额外回溯，仍保留云上传前后 3 分钟。默认 2。", false},
		{ArgFlashbackCloudDownloadMbps, "云日志下载限速，单位 Mbps。0 表示不限速。默认 16。", false},
		{ArgFlashbackCloudAPIIntervalMS, "两次云 API 之间的等待毫秒数。0 表示不再额外等待。默认 400。", false},
		{ArgFlashbackCloudMaxPackages, "单次下载最多增量包个数，必须大于等于 1。默认 48。", false},
		{ArgFlashbackCloudPkgRetries, "每个增量包最多尝试次数，必须大于等于 1。默认 3。", false},
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

func IsFlashbackRetiredRegionArg(key string) bool {
	switch strings.TrimSpace(key) {
	case ArgFlashbackTencentRegion, ArgFlashbackTencentRegionMap:
		return true
	default:
		return false
	}
}

type flashbackDownloadArgSpec struct {
	Key     string
	Min     int
	Message string
}

func flashbackDownloadArgSpecs() []flashbackDownloadArgSpec {
	return []flashbackDownloadArgSpec{
		{ArgFlashbackCloudLookbackHours, 0, "云日志回溯小时数须为大于等于 0 的整数，0 表示不再额外回溯"},
		{ArgFlashbackCloudDownloadMbps, 0, "云日志下载限速须为大于等于 0 的整数，单位 Mbps，0 表示不限速"},
		{ArgFlashbackCloudAPIIntervalMS, 0, "云 API 间隔须为大于等于 0 的整数，单位毫秒，0 表示不再额外等待"},
		{ArgFlashbackCloudMaxPackages, 1, "单次增量包上限须为大于等于 1 的整数"},
		{ArgFlashbackCloudPkgRetries, 1, "增量包尝试次数须为大于等于 1 的整数"},
	}
}

func ValidateFlashbackDownloadArg(key, value string) error {
	key = strings.TrimSpace(key)
	var spec *flashbackDownloadArgSpec
	specs := flashbackDownloadArgSpecs()
	for i := range specs {
		if specs[i].Key == key {
			spec = &specs[i]
			break
		}
	}
	if spec == nil {
		return nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < spec.Min {
		return fmt.Errorf("%s", spec.Message)
	}
	return nil
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
		{Key: ArgFlashbackCloudLookbackHours, Value: "2", Description: "云日志额外向前回溯的小时数。0 表示不再额外回溯，仍保留云上传前后 3 分钟。默认 2。"},
		{Key: ArgFlashbackCloudDownloadMbps, Value: "16", Description: "云日志下载限速，单位 Mbps。0 表示不限速。默认 16。"},
		{Key: ArgFlashbackCloudAPIIntervalMS, Value: "400", Description: "两次云 API 之间的等待毫秒数。0 表示不再额外等待。默认 400。"},
		{Key: ArgFlashbackCloudMaxPackages, Value: "48", Description: "单次下载最多增量包个数，必须大于等于 1。默认 48。"},
		{Key: ArgFlashbackCloudPkgRetries, Value: "3", Description: "每个增量包最多尝试次数，必须大于等于 1。默认 3。"},
	}
}

func flashbackDownloadLegacyDescription(key string) string {
	switch key {
	case ArgFlashbackCloudLookbackHours:
		return "云日志回溯小时数"
	case ArgFlashbackCloudDownloadMbps:
		return "云日志下载限速 Mbps"
	case ArgFlashbackCloudAPIIntervalMS:
		return "云 API 间隔毫秒"
	case ArgFlashbackCloudMaxPackages:
		return "单次最多增量包数"
	case ArgFlashbackCloudPkgRetries:
		return "增量包下载重试次数"
	default:
		return ""
	}
}

func (d *DB) EnsureFlashbackCloudSettings() error {
	for _, a := range flashbackCloudSettingDefaults() {
		if IsFlashbackSecretArg(a.Key) || IsFlashbackRetiredRegionArg(a.Key) {
			continue
		}
		var desc string
		err := d.QueryRow(`SELECT description FROM tbl_global_args WHERE key=?`, a.Key).Scan(&desc)
		if errors.Is(err, sql.ErrNoRows) {
			if err := d.SetGlobalArg(a.Key, a.Value, a.Description); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		legacy := flashbackDownloadLegacyDescription(a.Key)
		if legacy == "" || strings.TrimSpace(desc) != legacy || strings.TrimSpace(desc) == a.Description {
			continue
		}
		val, err := d.GetGlobalArg(a.Key)
		if err != nil {
			return err
		}
		if err := d.SetGlobalArg(a.Key, val, a.Description); err != nil {
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
		if !ok || meta.Secret || IsFlashbackRetiredRegionArg(key) {
			continue
		}
		val := item.Value
		if err := ValidateFlashbackDownloadArg(key, val); err != nil {
			return err
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
