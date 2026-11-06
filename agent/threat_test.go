package main

import (
	"strings"
	"testing"
)

func TestInferCause(t *testing.T) {
	cases := []struct {
		r    rawThreat
		want string
	}{
		{rawThreat{Path: `C:\Users\a\Downloads\x.exe`, ZoneURL: "https://evil.test/a/x.exe"}, "evil.test"},
		{rawThreat{Path: `E:\setup.exe`, Drive: "Removable"}, "USB"},
		{rawThreat{Path: `\\SRV01\share\a.exe`}, "SRV01"},
		{rawThreat{Path: `C:\Users\a\AppData\Local\Microsoft\Windows\INetCache\Content.Outlook\AB\inv.exe`}, "email"},
		{rawThreat{Path: `C:\Windows\Temp\a.exe`, Process: `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`}, "script"},
		{rawThreat{Path: `C:\Users\a\Downloads\y.exe`}, "Downloads"},
		{rawThreat{Path: `C:\x\y.exe`}, "Chưa xác định"},
	}
	for _, c := range cases {
		if got := inferCause(c.r); !strings.Contains(got, c.want) {
			t.Errorf("%+v -> %q, cần chứa %q", c.r, got, c.want)
		}
	}
}

func TestToThreats(t *testing.T) {
	js := `[{"id":"v1","kind":"virus","name":"Trojan:X","path":"C:\\a\\b.exe","action":"Đã xử lý","at":5,"drive":"Fixed","zoneUrl":"","zoneRef":"","process":"","note":""},{"id":"s1","kind":"suspect","name":"S","note":"n","process":"p.exe"}]`
	l := toThreats([]byte(js))
	if len(l) != 2 || l[0].Kind != "virus" || l[1].Kind != "suspect" || l[1].Detail != "n" {
		t.Fatalf("%+v", l)
	}
	if one := toThreats([]byte(`{"id":"v2","name":"N"}`)); len(one) != 1 {
		t.Fatal("một phần tử không phải mảng")
	}
}
