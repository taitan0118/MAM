//go:build darwin

package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const dnsSupported = true

// Mỗi dịch vụ mạng (Wi-Fi, Ethernet...): DNS đặt tay (rỗng = lấy từ DHCP).
type dnsBk struct {
	Svc    string   `json:"svc"`
	Static []string `json:"static"`
}

func dnsBackupPath() string { return filepath.Join(exeDir(), "dns-backup.json") }
func dnsBackupExists() bool { _, err := os.Stat(dnsBackupPath()); return err == nil }

func netServices() []string {
	out, _ := sh("networksetup", "-listallnetworkservices")
	var s []string
	for i, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if i == 0 || l == "" || strings.HasPrefix(l, "*") {
			continue
		}
		s = append(s, l)
	}
	return s
}

// dhcpDNS: DNS hệ thống đang dùng (từ DHCP), đọc từ scutil --dns.
func dhcpDNS() []string {
	out, _ := sh("scutil", "--dns")
	var r []string
	seen := map[string]bool{}
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if !strings.HasPrefix(l, "nameserver[") {
			continue
		}
		if i := strings.Index(l, ":"); i > 0 {
			ip := strings.TrimSpace(l[i+1:])
			if !isLoop(ip) && !seen[ip] {
				seen[ip] = true
				r = append(r, ip)
			}
		}
	}
	return r
}

func flushDNS() { sh("dscacheutil", "-flushcache"); sh("killall", "-HUP", "mDNSResponder") }

func dnsApply() ([]string, error) {
	svcs := netServices()
	if len(svcs) == 0 {
		return nil, errors.New("không thấy dịch vụ mạng nào")
	}
	var bk []dnsBk
	if b, err := os.ReadFile(dnsBackupPath()); err == nil {
		json.Unmarshal(b, &bk)
	}
	have := map[string]int{}
	for i, b := range bk {
		have[b.Svc] = i
	}
	dh := dhcpDNS()
	for _, s := range svcs {
		if _, ok := have[s]; ok {
			continue
		}
		out, _ := sh("networksetup", "-getdnsservers", s)
		var st []string
		if !strings.Contains(out, "aren't any") {
			for _, f := range strings.Fields(out) {
				if !isLoop(f) {
					st = append(st, f)
				}
			}
		}
		bk = append(bk, dnsBk{s, st})
		have[s] = len(bk) - 1
	}
	b, _ := json.Marshal(bk)
	if err := os.WriteFile(dnsBackupPath(), b, 0o600); err != nil {
		return nil, err
	}
	var ups []string
	seen := map[string]bool{}
	for _, s := range svcs {
		orig := bk[have[s]].Static
		if len(orig) == 0 {
			orig = dh
		}
		sh("networksetup", append([]string{"-setdnsservers", s, "127.0.0.1", "::1"}, orig...)...)
		for _, u := range orig {
			if !seen[u] {
				seen[u] = true
				ups = append(ups, u)
			}
		}
	}
	flushDNS()
	return ups, nil
}

func dnsRestore() {
	var bk []dnsBk
	if b, err := os.ReadFile(dnsBackupPath()); err == nil {
		json.Unmarshal(b, &bk)
	}
	for _, b := range bk {
		if len(b.Static) == 0 {
			sh("networksetup", "-setdnsservers", b.Svc, "Empty")
		} else {
			sh("networksetup", append([]string{"-setdnsservers", b.Svc}, b.Static...)...)
		}
	}
	flushDNS()
	os.Remove(dnsBackupPath())
}
