package dbprobe

import "testing"

func TestCellSize(t *testing.T) {
	if cellSize(nil) != 0 {
		t.Fatal("nil")
	}
	if cellSize("abcd") != 4 {
		t.Fatal("string")
	}
	if cellSize(20) != 2 {
		t.Fatal("int")
	}
}
