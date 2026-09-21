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

func TestSaveFlashbackCloudSettingsKeepsMasked(t *testing.T) {
	if !IsMaskedSecret("********") {
		t.Fatal("stars should be masked")
	}
	if IsMaskedSecret("AKID123") {
		t.Fatal("real key should not be masked")
	}
}
