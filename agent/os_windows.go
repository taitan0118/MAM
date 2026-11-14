//go:build windows

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func psJSON(script string, v interface{}) error {
	b, err := runPS(`Write-Output (ConvertTo-MamJson (`+script+`))`, 60*time.Second)
	if err != nil {
		return err
	}
	return json.Unmarshal(bytes.TrimSpace(bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})), v)
}

func osProcs() ([]map[string]interface{}, error) {
	var out []map[string]interface{}
	err := psJSON(`@(Get-Process | Sort-Object WorkingSet64 -Descending | Select-Object -First 80 | ForEach-Object { @{ pid=[int]$_.Id; name=[string]$_.ProcessName; mem=[int]($_.WorkingSet64/1MB); cpu=[math]::Round([double]$_.CPU,1); title=[string]$_.MainWindowTitle } })`, &out)
	return out, err
}

func osKill(pid string) error {
	out, err := hidden("taskkill", "/F", "/PID", pid).CombinedOutput()
	if err != nil {
		return errors.New(strings.TrimSpace(string(out)))
	}
	return nil
}

func osSmart() ([]map[string]interface{}, error) {
	// Dùng chung Get-MamHw với phần Ổ cứng vật lý: cùng chỉ số S.M.A.R.T, khỏi hai bảng nói ngược nhau.
	var out []map[string]interface{}
	err := psJSON("@((Get-MamHw).disks)", &out)
	return out, err
}


var applied struct {
	usb      *bool
	sites    string
	doh      *bool
	lastApps time.Time
	lastLock time.Time
}

// setDohLock bật/tắt khóa "DNS bảo mật" của trình duyệt bằng chính sách registry, và chặn cổng DoT 853.
// Khóa (on=true) khi có web cần chặn, để trình duyệt buộc dùng DNS hệ thống nên file hosts mới chặn được.
func setDohLock(on bool) {
	chrome := `HKLM\SOFTWARE\Policies\Google\Chrome`
	edge := `HKLM\SOFTWARE\Policies\Microsoft\Edge`
	coccoc := `HKLM\SOFTWARE\Policies\CocCoc\CocCoc`
	ff := `HKLM\SOFTWARE\Policies\Mozilla\Firefox\DNSOverHTTPS`
	if on {
		for _, k := range []string{chrome, edge, coccoc} {
			hidden("reg", "add", k, "/v", "DnsOverHttpsMode", "/t", "REG_SZ", "/d", "off", "/f").Run()
			hidden("reg", "add", k, "/v", "BuiltInDnsClientEnabled", "/t", "REG_DWORD", "/d", "0", "/f").Run()
		}
		hidden("reg", "add", ff, "/v", "Enabled", "/t", "REG_DWORD", "/d", "0", "/f").Run()
		hidden("reg", "add", ff, "/v", "Locked", "/t", "REG_DWORD", "/d", "1", "/f").Run()
		hidden("netsh", "advfirewall", "firewall", "add", "rule", "name=MAM-Block-DoT", "dir=out", "action=block", "protocol=TCP", "remoteport=853").Run()
	} else {
		for _, k := range []string{chrome, edge, coccoc} {
			hidden("reg", "delete", k, "/v", "DnsOverHttpsMode", "/f").Run()
			hidden("reg", "delete", k, "/v", "BuiltInDnsClientEnabled", "/f").Run()
		}
		hidden("reg", "delete", ff, "/f").Run()
		hidden("netsh", "advfirewall", "firewall", "delete", "rule", "name=MAM-Block-DoT").Run()
	}
}

const hostsPath = `C:\Windows\System32\drivers\etc\hosts`
const hMark1, hMark2 = "# MAM-BEGIN", "# MAM-END"

func osApplyPolicy(p *Policy) {
	if applied.usb == nil || *applied.usb != p.USB {
		v := "3" // 3 = cho phép ổ USB lưu trữ, 4 = chặn
		if p.USB {
			v = "4"
		}
		if hidden("reg", "add", `HKLM\SYSTEM\CurrentControlSet\Services\USBSTOR`, "/v", "Start", "/t", "REG_DWORD", "/d", v, "/f").Run() == nil {
			b := p.USB
			applied.usb = &b
		}
	}
	if key := strings.Join(p.Sites, ","); key != applied.sites {
		if err := writeHosts(p.Sites); err == nil {
			applied.sites = key
			setURLBlock(p.Sites)
			hidden("ipconfig", "/flushdns").Run()
		} else {
			log.Printf("hosts: %v", err)
		}
	}
	// Có web cần chặn thì khóa DNS bảo mật của trình duyệt để hosts chắc chắn ăn; hết thì mở lại.
	if want := len(p.Sites) > 0; applied.doh == nil || *applied.doh != want {
		setDohLock(want)
		applied.doh = &want
	}
	dnsFilter(p.Sites) // bộ lọc DNS: chặn cả tên miền phụ, mọi trình duyệt
	if len(p.Apps) > 0 && time.Since(applied.lastApps) > 8*time.Second {
		applied.lastApps = time.Now()
		out, _ := hidden("tasklist", "/FO", "CSV", "/NH").Output()
		want := map[string]bool{}
		for _, a := range p.Apps {
			want[strings.ToLower(a)] = true
		}
		for _, line := range strings.Split(string(out), "\n") {
			f := strings.SplitN(strings.TrimSpace(line), `","`, 2)
			if len(f) == 2 {
				if n := strings.Trim(f[0], `"`); want[strings.ToLower(n)] {
					hidden("taskkill", "/F", "/IM", n).Run()
				}
			}
		}
	}
	if p.Hours != nil && !inWindow(time.Now(), p.Hours.From, p.Hours.To) && activeUser() != "" && time.Since(applied.lastLock) > 45*time.Second {
		applied.lastLock = time.Now()
		runCommand("lock", nil)
	}
}

func writeHosts(sites []string) error {
	b, err := os.ReadFile(hostsPath)
	if err != nil {
		return err
	}
	var keep []string
	skip := false
	for _, l := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		switch {
		case strings.HasPrefix(l, hMark1):
			skip = true
		case strings.HasPrefix(l, hMark2):
			skip = false
		case !skip:
			keep = append(keep, l)
		}
	}
	for len(keep) > 0 && strings.TrimSpace(keep[len(keep)-1]) == "" {
		keep = keep[:len(keep)-1]
	}
	if len(sites) > 0 {
		keep = append(keep, "", hMark1)
		for _, s := range sites {
			keep = append(keep, "0.0.0.0 "+s, "0.0.0.0 www."+s, ":: "+s, ":: www."+s) // dòng IPv6: không có thì máy hỏi DNS bản ghi AAAA và vẫn vào được
		}
		keep = append(keep, hMark2)
	}
	return os.WriteFile(hostsPath, []byte(strings.Join(keep, "\r\n")+"\r\n"), 0o644)
}

// swapSelf: thay mam-agent.exe bằng file mới bằng một tác vụ chạy một lần (SYSTEM), vì file đang chạy không ghi đè được.
func archName() string { return runtime.GOARCH }

func swapSelf(newFile string) error {
	dir := exeDir()
	exe := filepath.Join(dir, "mam-agent.exe")
	script := filepath.Join(dir, "update.cmd")
	body := fmt.Sprintf("@echo off\r\nping 127.0.0.1 -n 3 >nul\r\ntaskkill /f /im mam-agent.exe >nul 2>&1\r\nping 127.0.0.1 -n 2 >nul\r\nmove /y \"%s\" \"%s\" >nul\r\nschtasks /run /tn \"MAM Agent\" >nul 2>&1\r\nschtasks /run /tn \"MAM Client\" >nul 2>&1\r\nschtasks /delete /tn \"MAM Update\" /f >nul 2>&1\r\ndel \"%%~f0\"\r\n", newFile, exe)
	if err := os.WriteFile(script, []byte(body), 0o600); err != nil {
		return err
	}
	if out, err := hidden("schtasks", "/create", "/tn", "MAM Update", "/tr", `cmd /c "`+script+`"`, "/sc", "once", "/st", "00:00", "/ru", "SYSTEM", "/rl", "highest", "/f").CombinedOutput(); err != nil {
		return fmt.Errorf("%v %s", err, out)
	}
	if err := hidden("schtasks", "/run", "/tn", "MAM Update").Run(); err != nil {
		return err
	}
	return nil
}

// setURLBlock: chặn thêm bằng chính sách trình duyệt (Chrome, Edge, Cốc Cốc). Chặn cả tên miền phụ (m.facebook.com...) mà file hosts không bắt được.
// Trình duyệt đọc lại chính sách sau ít phút hoặc khi mở lại.
func setURLBlock(sites []string) {
	for _, base := range []string{`HKLM\SOFTWARE\Policies\Google\Chrome`, `HKLM\SOFTWARE\Policies\Microsoft\Edge`, `HKLM\SOFTWARE\Policies\CocCoc\CocCoc`} {
		k := base + `\URLBlocklist`
		hidden("reg", "delete", k, "/f").Run()
		for i, s := range sites {
			hidden("reg", "add", k, "/v", strconv.Itoa(i+1), "/t", "REG_SZ", "/d", s, "/f").Run()
		}
	}
}

// enableWOL: bật "Wake on Magic Packet" cho card mạng và tắt Fast Startup (nếu không, máy tắt hẳn thường không dậy được bằng Wake-on-LAN).
// Phần BIOS/UEFI (Wake on LAN / Power on by PCI-E) vẫn phải bật tay một lần trên từng máy.
func enableWOL() {
	hidden("reg", "add", `HKLM\SYSTEM\CurrentControlSet\Control\Session Manager\Power`, "/v", "HiberbootEnabled", "/t", "REG_DWORD", "/d", "0", "/f").Run()
	runPS(`Get-NetAdapter -Physical -ErrorAction SilentlyContinue | ForEach-Object { try { Enable-NetAdapterPowerManagement -Name $_.Name -ErrorAction SilentlyContinue } catch {}; try { Set-NetAdapterPowerManagement -Name $_.Name -WakeOnMagicPacket Enabled -WakeOnPattern Enabled -ErrorAction Stop } catch {}; $n=$_.Name; foreach($k in '*WakeOnMagicPacket','S5WakeOnLan','*ModernStandbyWoLMagicPacket','WolShutdownLinkSpeed'){ try { $p=Get-NetAdapterAdvancedProperty -Name $n -RegistryKeyword $k -ErrorAction Stop; if($k -ne 'WolShutdownLinkSpeed'){ Set-NetAdapterAdvancedProperty -Name $n -RegistryKeyword $k -RegistryValue 1 -ErrorAction Stop } } catch {} } }`, 60*time.Second)
}
