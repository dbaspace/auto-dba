package handler

import "testing"

func TestDDLDataSourceIDs(t *testing.T) {
	fb := "primary-id"
	cases := []struct {
		name string
		raw  string
		want []string
	}{
		{"empty fallback", "", []string{fb}},
		{"json multi", `{"data_source_ids":["a","b","a"]}`, []string{"a", "b"}},
		{"json empty list", `{"data_source_ids":[]}`, []string{fb}},
		{"junk", `{not-json`, []string{fb}},
		{"blank entries", `{"data_source_ids":[""," x ","x"]}`, []string{"x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ddlDataSourceIDs(tc.raw, fb)
			if len(got) != len(tc.want) {
				t.Fatalf("len got=%v want=%v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got=%v want=%v", got, tc.want)
				}
			}
		})
	}
	if ddlDataSourceIDs("", "") != nil {
		t.Fatal("both empty should be nil")
	}
}
