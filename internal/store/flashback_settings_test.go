package store

import "testing"

func TestIsFlashbackSecretArg(t *testing.T) {
	if !IsFlashbackSecretArg(ArgFlashbackTencentSecretID) {
		t.Fatal("tencent secret id should be secret")
	}
	if IsFlashbackSecretArg(ArgFlashbackTencentRegion) {
		t.Fatal("region should not be secret")
	}
	if !IsFlashbackCloudArg(ArgFlashbackAliyunAccessKeyID) {
		t.Fatal("aliyun ak should be cloud arg")
	}
}

func TestFlashbackRetiredRegionAndDownloadValidation(t *testing.T) {
	if !IsFlashbackRetiredRegionArg(ArgFlashbackTencentRegion) || !IsFlashbackRetiredRegionArg(ArgFlashbackTencentRegionMap) {
		t.Fatal("tencent region args should leave global args")
	}
	if IsFlashbackRetiredRegionArg(ArgFlashbackCloudDownloadMbps) || IsFlashbackSecretArg(ArgFlashbackCloudLookbackHours) {
		t.Fatal("download args stay editable")
	}
	if flashbackDownloadLegacyDescription(ArgFlashbackCloudLookbackHours) != "云日志回溯小时数" {
		t.Fatal("legacy description mismatch")
	}
	if err := ValidateFlashbackDownloadArg(ArgFlashbackCloudDownloadMbps, "0"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateFlashbackDownloadArg(ArgFlashbackCloudLookbackHours, "0"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateFlashbackDownloadArg(ArgFlashbackCloudAPIIntervalMS, "0"); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"-1", "abc", ""} {
		if err := ValidateFlashbackDownloadArg(ArgFlashbackCloudDownloadMbps, raw); err == nil {
			t.Fatalf("mbps %q should be rejected", raw)
		}
	}
	if err := ValidateFlashbackDownloadArg(ArgFlashbackCloudMaxPackages, "0"); err == nil {
		t.Fatal("package cap 0 should be rejected")
	}
	if err := ValidateFlashbackDownloadArg(ArgFlashbackCloudPkgRetries, "1"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateFlashbackDownloadArg("other_key", "nope"); err != nil {
		t.Fatal(err)
	}
}

func TestSaveFlashbackCloudSettingsKeepsMasked(t *testing.T) {
	if !IsMaskedSecret("********") {
		t.Fatal("stars should be masked")
	}
	if IsMaskedSecret("AKID123") {
		t.Fatal("real key should not be masked")
	}
}
