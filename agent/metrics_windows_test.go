//go:build windows

package main

import (
	"strings"
	"testing"
	"time"
)

// PowerShell lỗi thì lỗi trả về phải kèm lời nó báo, không chỉ "exit status 1".
func TestRunPSStderr(t *testing.T) {
	_, err := runPS(`[Console]::Error.WriteLine('hong-o-day'); exit 1`, 60*time.Second)
	if err == nil || !strings.Contains(err.Error(), "exit status 1") || !strings.Contains(err.Error(), "hong-o-day") {
		t.Fatalf("lỗi thiếu lời PowerShell báo: %v", err)
	}
	if out, err := runPS(`Write-Output 'ok'`, 60*time.Second); err != nil || strings.TrimSpace(string(out)) != "ok" {
		t.Fatalf("lệnh chạy được mà báo lỗi: %q %v", out, err)
	}
}
