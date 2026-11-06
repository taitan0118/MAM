package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAgentBinaryVersion(t *testing.T) {
	s, _ := newSrv(t)
	if s.agentBinary("amd64") != nil {
		t.Skip("có sẵn mam-agent.exe cạnh file test")
	}
	os.WriteFile(filepath.Join(s.dir, "mam-agent.exe"), []byte("MZ....MAMVER=9.9.9...."), 0o644)
	b := s.agentBinary("amd64")
	if b == nil || b.ver != "9.9.9" {
		t.Fatalf("không đọc được phiên bản agent: %+v", b)
	}
	if s.updateFor("1.1.0", "amd64") == nil {
		t.Fatal("máy cũ phải được báo cập nhật")
	}
	if s.updateFor("9.9.9", "amd64") != nil {
		t.Fatal("máy đã mới nhất không cần cập nhật")
	}
}
