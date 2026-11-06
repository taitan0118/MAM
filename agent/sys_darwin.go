//go:build darwin

package main

// Cài đặt thật cho macOS: số liệu qua sysctl/vm_stat/top, tiến trình qua ps, lệnh từ xa, chính sách.
import (
	"log"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func sysctlStr(key string) string { out, _ := sh("sysctl", "-n", key); return out }
func sysctlInt(key string) int64  { n, _ := strconv.ParseInt(sysctlStr(key), 10, 64); return n }

func osName() string {
	name, _ := sh("sw_vers", "-productName")
	ver, _ := sh("sw_vers", "-productVersion")
	if name == "" {
		return "macOS"
	}
	return strings.TrimSpace(name + " " + ver)
}

func osMetrics() Metrics {
	m := Metrics{}
	// CPU: lấy mẫu thứ 2 của `top` (mẫu đầu luôn bằng 0)
	if out, err := sh("top", "-l", "2", "-n", "0"); err == nil {
		lines := strings.Split(out, "\n")
		for i := len(lines) - 1; i >= 0; i-- {
			if strings.HasPrefix(lines[i], "CPU usage:") {
				if idx := strings.Index(lines[i], "% idle"); idx > 0 {
					seg := lines[i][:idx]
					if sp := strings.LastIndex(seg, " "); sp > 0 {
						if idle, e := strconv.ParseFloat(seg[sp+1:], 64); e == nil {
							m.CPU = 100 - idle
						}
					}
				}
				break
			}
		}
	}
	// RAM: vm_stat (số trang) so với hw.memsize
	total := sysctlInt("hw.memsize")
	if out, err := sh("vm_stat"); err == nil && total > 0 {
		pg := int64(4096)
		var active, wired, compressed int64
		for _, l := range strings.Split(out, "\n") {
			if strings.HasPrefix(l, "Mach Virtual Memory Statistics") && strings.Contains(l, "page size of") {
				f := strings.Fields(l)
				for i, w := range f {
					if w == "of" && i+1 < len(f) {
						if v, e := strconv.ParseInt(f[i+1], 10, 64); e == nil {
							pg = v
						}
					}
				}
			}
			num := func() int64 {
				v, _ := strconv.ParseInt(strings.TrimRight(strings.TrimSpace(strings.SplitN(l, ":", 2)[len(strings.SplitN(l, ":", 2))-1]), "."), 10, 64)
				return v
			}
			switch {
			case strings.HasPrefix(l, "Pages active:"):
				active = num()
			case strings.HasPrefix(l, "Pages wired down:"):
				wired = num()
			case strings.HasPrefix(l, "Pages occupied by compressor:"):
				compressed = num()
			}
		}
		used := (active + wired + compressed) * pg
		m.RAM = 100 * float64(used) / float64(total)
	}
	if bt := sysctlStr("kern.boottime"); bt != "" {
		// dạng: { sec = 1700000000, usec = 0 } ...
		if i := strings.Index(bt, "sec = "); i >= 0 {
			rest := bt[i+6:]
			if j := strings.IndexAny(rest, ", "); j > 0 {
				if s, e := strconv.ParseInt(rest[:j], 10, 64); e == nil {
					m.Uptime = time.Now().Unix() - s
				}
			}
		}
	}
	var st syscall.Statfs_t
	if syscall.Statfs("/", &st) == nil {
		bs := uint64(st.Bsize)
		m.Disks = append(m.Disks, Disk{"/", float64(st.Blocks*bs) / 1e9, float64(st.Bavail*bs) / 1e9})
	}
	m.User = activeUser()
	return m
}

// activeUser: người đang đăng nhập đồ họa ở console.
func activeUser() string {
	if out, err := sh("stat", "-f", "%Su", "/dev/console"); err == nil && out != "" && out != "root" {
		return out
	}
	if u := os.Getenv("SUDO_USER"); u != "" {
		return u
	}
	return os.Getenv("USER")
}

func osHw() map[string]interface{} {
	hw := map[string]interface{}{"os": osName(), "cpu": sysctlStr("machdep.cpu.brand_string"), "cores": int(sysctlInt("hw.ncpu")),
		"ramGB": int(sysctlInt("hw.memsize")/1024/1024/1024 + 0), "model": sysctlStr("hw.model"), "board": ""}
	if out, err := sh("ioreg", "-l"); err == nil {
		for _, l := range strings.Split(out, "\n") {
			if strings.Contains(l, "IOPlatformSerialNumber") {
				if i := strings.LastIndex(l, `"`); i > 0 {
					seg := l[:i]
					if j := strings.LastIndex(seg, `"`); j >= 0 {
						hw["serial"] = seg[j+1:]
					}
				}
				break
			}
		}
	}
	return hw
}

func osTemp() *float64 { return nil } // macOS không có nguồn nhiệt độ không cần cgo/driver riêng

func osProcs() ([]map[string]interface{}, error) {
	out, err := sh("ps", "-axo", "pid=,rss=,comm=")
	if err != nil {
		return nil, err
	}
	var all []map[string]interface{}
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) < 3 {
			continue
		}
		pid, _ := strconv.Atoi(f[0])
		rss, _ := strconv.Atoi(f[1])
		name := f[2]
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:]
		}
		all = append(all, map[string]interface{}{"pid": pid, "name": name, "mem": rss / 1024, "cpu": 0.0, "title": ""})
	}
	for i := 0; i < len(all); i++ {
		for j := i + 1; j < len(all); j++ {
			if all[j]["mem"].(int) > all[i]["mem"].(int) {
				all[i], all[j] = all[j], all[i]
			}
		}
		if i >= 79 {
			break
		}
	}
	if len(all) > 80 {
		all = all[:80]
	}
	return all, nil
}

func osSmart() ([]map[string]interface{}, error) {
	var out []map[string]interface{}
	txt, err := sh("diskutil", "info", "-plist", "/")
	_ = txt
	if err != nil {
		return out, nil
	}
	// giữ nhẹ: báo một mục cho ổ hệ thống, chi tiết SMART cần smartctl (không có sẵn)
	var st syscall.Statfs_t
	size := 0
	if syscall.Statfs("/", &st) == nil {
		size = int(float64(st.Blocks*uint64(st.Bsize)) / 1e9)
	}
	out = append(out, map[string]interface{}{"name": "Ổ hệ thống (/)", "type": "SSD", "bus": "", "health": "", "status": "", "sizeGB": size, "temp": nil, "wear": nil, "hours": nil, "readErr": nil, "writeErr": nil})
	return out, nil
}

func osApplyPolicy(p *Policy) {
	if key := strings.Join(p.Sites, ","); key != applied.sites {
		if err := writeHosts(p.Sites); err == nil {
			applied.sites = key
			sh("dscacheutil", "-flushcache")
		} else {
			log.Printf("hosts: %v", err)
		}
	}
	dnsFilter(p.Sites)
	if len(p.Apps) > 0 && time.Since(applied.lastApps) > 8*time.Second {
		applied.lastApps = time.Now()
		for _, a := range p.Apps {
			sh("pkill", "-i", "-f", strings.TrimSuffix(a, ".exe"))
		}
	}
	if p.Hours != nil && !inWindow(time.Now(), p.Hours.From, p.Hours.To) && time.Since(applied.lastLock) > 45*time.Second {
		applied.lastLock = time.Now()
		osRun("lock", nil)
	}
}

const cgSession = "/System/Library/CoreServices/Menu Extras/User.menu/Contents/Resources/CGSession"

func osRun(typ string, args map[string]string) (bool, string) {
	var out string
	var err error
	switch typ {
	case "restart":
		out, err = sh("shutdown", "-r", "+1", "MAM: khởi động lại máy")
	case "shutdown":
		out, err = sh("shutdown", "-h", "+1", "MAM: tắt máy")
	case "lock":
		out, err = sh(cgSession, "-suspend")
	default:
		return false, "Lệnh không hỗ trợ: " + typ
	}
	if err != nil {
		return false, "Lỗi: " + err.Error() + " " + out
	}
	return true, "Đã thực hiện"
}
