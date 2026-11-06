//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const dnsSupported = true

// Mỗi card mạng: chỉ số, DNS đặt tay trong registry (rỗng = lấy từ DHCP) và DNS đang dùng lúc sao lưu.
type dnsBk struct {
	I int      `json:"i"`
	S string   `json:"s"`
	C []string `json:"c"`
}

func dnsBackupPath() string { return filepath.Join(exeDir(), "dns-backup.json") }
func dnsBackupExists() bool { _, err := os.Stat(dnsBackupPath()); return err == nil }

func loadDNSBackup() []dnsBk {
	var bk []dnsBk
	if b, err := os.ReadFile(dnsBackupPath()); err == nil {
		json.Unmarshal(b, &bk)
	}
	return bk
}

func psList(xs []string) string {
	q := make([]string, len(xs))
	for i, x := range xs {
		q[i] = "'" + strings.ReplaceAll(x, "'", "") + "'"
	}
	return "@(" + strings.Join(q, ",") + ")"
}

func staticList(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == ';' })
}

// dnsApply sao lưu DNS cũ (chỉ lần đầu với mỗi card) rồi đặt "127.0.0.1, ::1, <DNS cũ>". Trả về danh sách DNS cũ.
func dnsApply() ([]string, error) {
	var now []dnsBk
	err := psJSON(`@(foreach($a in @(Get-NetAdapter -ErrorAction SilentlyContinue | Where-Object { $_.Status -eq 'Up' })){ $ns=''; try { $ns=[string](Get-ItemProperty ('HKLM:\SYSTEM\CurrentControlSet\Services\Tcpip\Parameters\Interfaces\'+$a.InterfaceGuid) -ErrorAction Stop).NameServer } catch {}; @{ i=[int]$a.ifIndex; s=$ns; c=@(@((Get-DnsClientServerAddress -InterfaceIndex $a.ifIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue).ServerAddresses) | ForEach-Object { [string]$_ }) } })`, &now)
	if err != nil {
		return nil, err
	}
	if len(now) == 0 {
		return nil, errors.New("không thấy card mạng nào đang chạy")
	}
	bk := loadDNSBackup()
	have := map[int]int{}
	for i, b := range bk {
		have[b.I] = i
	}
	for _, n := range now {
		if _, ok := have[n.I]; !ok {
			var c []string
			for _, x := range n.C {
				if !isLoop(x) {
					c = append(c, x)
				}
			}
			n.C = c
			bk = append(bk, n)
			have[n.I] = len(bk) - 1
		}
	}
	b, _ := json.Marshal(bk)
	if err := os.WriteFile(dnsBackupPath(), b, 0o600); err != nil {
		return nil, err
	}
	var ups []string
	seen := map[string]bool{}
	script := ""
	for _, n := range now {
		o := bk[have[n.I]]
		orig := o.C
		if o.S != "" {
			orig = staticList(o.S)
		}
		list := append([]string{"127.0.0.1", "::1"}, orig...)
		script += "Set-DnsClientServerAddress -InterfaceIndex " + strconv.Itoa(n.I) + " -ServerAddresses " + psList(list) + " -ErrorAction SilentlyContinue;"
		for _, u := range orig {
			if !seen[u] {
				seen[u] = true
				ups = append(ups, u)
			}
		}
	}
	runPS(script+"Clear-DnsClientCache", 60*time.Second)
	return ups, nil
}

// dnsRestore trả DNS cũ cho mọi card đã đổi rồi xóa bản sao lưu.
func dnsRestore() {
	bk := loadDNSBackup()
	script := ""
	for _, b := range bk {
		if b.S == "" {
			script += "Set-DnsClientServerAddress -InterfaceIndex " + strconv.Itoa(b.I) + " -ResetServerAddresses -ErrorAction SilentlyContinue;"
		} else {
			script += "Set-DnsClientServerAddress -InterfaceIndex " + strconv.Itoa(b.I) + " -ServerAddresses " + psList(staticList(b.S)) + " -ErrorAction SilentlyContinue;"
		}
	}
	if script != "" {
		runPS(script+"Clear-DnsClientCache", 60*time.Second)
	}
	os.Remove(dnsBackupPath())
}
