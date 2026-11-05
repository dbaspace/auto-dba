package store

import "testing"

func TestMissingCloudInstanceIDs_onlySuccessfulRegions(t *testing.T) {
	rows := []cloudInstanceRef{
		{ID: "bj-gone", CloudID: "pg-bj-old", Region: "ap-beijing"},
		{ID: "bj-keep", CloudID: "pg-bj-1", Region: "ap-beijing"},
		{ID: "fra-keep", CloudID: "pg-fra-1", Region: "eu-frankfurt"},
		{ID: "sg-keep", CloudID: "pg-sg-1", Region: "ap-singapore"},
	}
	missing := missingCloudInstanceIDs(rows, []string{"ap-beijing", "ap-singapore"}, []string{"pg-bj-1", "pg-sg-1"}, true)
	if len(missing) != 1 || missing[0] != "bj-gone" {
		t.Fatalf("missing=%v", missing)
	}
}
