package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestBundleRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.exe")
	js := []byte(`{"server":"http://192.168.1.10:8080","key":"k","lang":"vi"}`)
	n := make([]byte, 4)
	binary.LittleEndian.PutUint32(n, uint32(len(js)))
	data := append(append(append([]byte("MZ-fake-exe"), js...), n...), bundleMagic...)
	os.WriteFile(p, data, 0o644)
	b := readBundleFrom(p)
	if b == nil || b.Server != "http://192.168.1.10:8080" || b.Key != "k" {
		t.Fatalf("got %+v", b)
	}
	os.WriteFile(p, []byte("MZ-plain-exe-without-bundle"), 0o644)
	if readBundleFrom(p) != nil {
		t.Fatal("plain exe must have no bundle")
	}
}
