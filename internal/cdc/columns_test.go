package cdc

import (
	"context"
	"testing"
)

func TestListPGColumnsManyRequiresDB(t *testing.T) {
	_, err := ListPGColumnsMany(context.Background(), nil, "public", []string{"t"})
	if err == nil {
		t.Fatal("nil db should fail")
	}
}
