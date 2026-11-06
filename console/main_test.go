package main

import (
	"strings"
	"testing"
)

func TestFileURL(t *testing.T) {
	if got := fileURL("/home/a b/MAM-Console/connect.html"); got != "file:///home/a%20b/MAM-Console/connect.html" {
		t.Fatal(got)
	}
	if !strings.Contains(connectHTML, "{{VER}}") {
		t.Fatal("trang kết nối thiếu chỗ điền phiên bản")
	}
}
