package handler

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestExplainQueryErrTimeout(t *testing.T) {
	if got := explainQueryErr(context.DeadlineExceeded); got != queryTimeoutMessage {
		t.Fatalf("deadline: %s", got)
	}
	if got := explainQueryErr(errors.New("ERROR: canceling statement due to statement timeout (SQLSTATE 57014)")); got != queryTimeoutMessage {
		t.Fatalf("pg: %s", got)
	}
}

func TestExplainQueryErrBoolean(t *testing.T) {
	got := explainQueryErr(errors.New("ERROR: operator does not exist: boolean = integer (SQLSTATE 42883)"))
	if !strings.Contains(got, "boolean = integer") || !strings.Contains(got, "true 或 false") {
		t.Fatalf("got: %s", got)
	}
}

func TestExplainQueryErrOther(t *testing.T) {
	raw := "relation does not exist"
	if got := explainQueryErr(errors.New(raw)); got != raw {
		t.Fatalf("got: %s", got)
	}
}

func TestNormalizeClientRequestID(t *testing.T) {
	if normalizeClientRequestID("abc") != "" {
		t.Fatal("short")
	}
	if normalizeClientRequestID("not valid!") != "" {
		t.Fatal("symbol")
	}
	id := "6f88d772-3247-4ffe-ac3b-f9afebbdfa62"
	if normalizeClientRequestID(id) != id {
		t.Fatal(id)
	}
}
