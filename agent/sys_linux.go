//go:build linux

package main

// Cài đặt thật cho Linux: số liệu từ /proc, phần cứng, tiến trình, lệnh từ xa, chính sách.
import (
	"bufio"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func osName() string {
	if b, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(l, "PRETTY_NAME=") {
				return strings.Trim(l[len("PRETTY_NAME="):], `"`)
			}
		}
	}
	return "Linux"
}

var lastIdle, lastTotal uint64

func osMetrics() Metrics {
	m := Metrics{}
	if f, err := os.Open("/proc/stat"); err == nil {
		sc := bufio.NewScanner(f)
		if sc.Scan() {
			p := strings.Fields(sc.Text())
			var tot, idle uint64
			for i, x := range p[1:] {
				v, _ := strconv.ParseUint(x, 10, 64)
				tot += v
				if i == 3 {
					idle = v
				}
			}
			if lastTotal > 0 && tot > lastTotal {
				m.CPU = 100 * (1 - float64(idle-lastIdle)/float64(tot-lastTotal))
			}
			lastIdle, lastTotal = idle, tot
		}
		f.Close()
	}
	var total, avail float64
	if f, err := os.Open("/proc/meminfo"); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			p := strings.Fields(sc.Text())
			if len(p) >= 2 {
				v, _ := strconv.ParseFloat(p[1], 64)
				switch p[0] {
				case "MemTotal:":
					total = v
				case "MemAvailable:":
					avail = v
				}
			}
		}
		f.Close()
	}
	if total > 0 {
		m.RAM = 100 * (1 - avail/total)
	}
	if b, err := os.ReadFile("/proc/uptime"); err == nil {
		fv, _ := strconv.ParseFloat(strings.Fields(string(b))[0], 64)
		m.Uptime = int64(fv)
	}
	var st syscall.Statfs_t
	if syscall.Statfs("/", &st) == nil {
		bs := uint64(st.Bsize)
		m.Disks = append(m.Disks, Disk{"/", float64(st.Blocks*bs) / 1e9, float64(st.Bavail*bs) / 1e9})
	}
	m.User = activeUser()
	return m
}

// activeUser: người đang đăng nhập đồ họa (ai đang ngồi máy), lấy từ `who`.
func activeUser() string {
	out, _ := sh("who")
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) >= 2 && (strings.Contains(l, ":0") || strings.Contains(l, "seat0") || strings.HasPrefix(f[1], "tty")) {
			return f[0]
		}
	}
	if u := os.Getenv("SUDO_USER"); u != "" {
		return u
	}
	return os.Getenv("USER")
}

func osHw() map[string]interface{} {
	hw := map[string]interface{}{"os": osName(), "cores": 0, "cpu": "", "ramGB": 0}
	if b, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		cores := 0
		for _, l := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(l, "processor") {
				cores++
			} else if strings.HasPrefix(l, "model name") && hw["cpu"] == "" {
				if i := strings.Index(l, ":"); i > 0 {
					hw["cpu"] = strings.TrimSpace(l[i+1:])
				}
			}
		}
		hw["cores"] = cores
	}
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(l, "MemTotal:") {
				if kb, e := strconv.ParseFloat(strings.Fields(l)[1], 64); e == nil {
					hw["ramGB"] = int(kb/1024/1024 + 0.5)
				}
			}
		}
	}
	hw["model"], _ = readFirst("/sys/devices/virtual/dmi/id/product_name", "/sys/firmware/devicetree/base/model")
	hw["board"], _ = readFirst("/sys/devices/virtual/dmi/id/board_name")
	hw["serial"], _ = readFirst("/sys/devices/virtual/dmi/id/product_serial")
	return hw
}

func readFirst(paths ...string) (string, bool) {
	for _, p := range paths {
		if b, err := os.ReadFile(p); err == nil {
			s := strings.TrimRight(strings.TrimSpace(string(b)), "\x00")
			if s != "" {
				return s, true
			}
		}
	}
	return "", false
}

func osTemp() *float64 {
	es, _ := os.ReadDir("/sys/class/thermal")
	for _, e := range es {
		if strings.HasPrefix(e.Name(), "thermal_zone") {
			if b, err := os.ReadFile("/sys/class/thermal/" + e.Name() + "/temp"); err == nil {
				if v, e := strconv.ParseFloat(strings.TrimSpace(string(b)), 64); e == nil && v > 0 {
					t := v / 1000
					return &t
				}
			}
		}
	}
	return nil
}

func osProcs() ([]map[string]interface{}, error) {
	es, _ := os.ReadDir("/proc")
	var out []map[string]interface{}
	for _, e := range es {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		comm, _ := os.ReadFile("/proc/" + e.Name() + "/comm")
		mem := 0
		if b, err := os.ReadFile("/proc/" + e.Name() + "/statm"); err == nil {
			if f := strings.Fields(string(b)); len(f) > 1 {
				n, _ := strconv.Atoi(f[1])
				mem = n * 4096 >> 20
			}
		}
		out = append(out, map[string]interface{}{"pid": pid, "name": strings.TrimSpace(string(comm)), "mem": mem, "cpu": 0.0, "title": ""})
	}
	// chỉ giữ 80 tiến trình tốn RAM nhất cho gọn
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j]["mem"].(int) > out[i]["mem"].(int) {
				out[i], out[j] = out[j], out[i]
			}
		}
		if i >= 79 {
			break
		}
	}
	if len(out) > 80 {
		out = out[:80]
	}
	return out, nil
}

func osSmart() ([]map[string]interface{}, error) {
	var out []map[string]interface{}
	es, _ := os.ReadDir("/sys/block")
	for _, e := range es {
		n := e.Name()
		if strings.HasPrefix(n, "loop") || strings.HasPrefix(n, "ram") || strings.HasPrefix(n, "dm-") {
			continue
		}
		size := 0
		if b, err := os.ReadFile("/sys/block/" + n + "/size"); err == nil {
			s, _ := strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
			size = int(s * 512 / 1e9)
		}
		rot, _ := readFirst("/sys/block/" + n + "/queue/rotational")
		typ := "SSD"
		if rot == "1" {
			typ = "HDD"
		}
		model, _ := readFirst("/sys/block/" + n + "/device/model")
		out = append(out, map[string]interface{}{"name": strings.TrimSpace(model + " /dev/" + n), "type": typ, "bus": "", "health": "", "status": "", "sizeGB": size, "temp": nil, "wear": nil, "hours": nil, "readErr": nil, "writeErr": nil})
	}
	return out, nil
}

func osApplyPolicy(p *Policy) {
	if key := strings.Join(p.Sites, ","); key != applied.sites {
		if err := writeHosts(p.Sites); err == nil {
			applied.sites = key
		} else {
			log.Printf("hosts: %v", err)
		}
	}
	if len(p.Apps) > 0 && time.Since(applied.lastApps) > 8*time.Second {
		applied.lastApps = time.Now()
		want := map[string]bool{}
		for _, a := range p.Apps {
			want[strings.ToLower(strings.TrimSuffix(a, ".exe"))] = true
		}
		es, _ := os.ReadDir("/proc")
		for _, e := range es {
			if _, err := strconv.Atoi(e.Name()); err != nil {
				continue
			}
			comm, _ := os.ReadFile("/proc/" + e.Name() + "/comm")
			if want[strings.ToLower(strings.TrimSpace(string(comm)))] {
				osKill(e.Name())
			}
		}
	}
	if p.Hours != nil && !inWindow(time.Now(), p.Hours.From, p.Hours.To) && activeUser() != "" && time.Since(applied.lastLock) > 45*time.Second {
		applied.lastLock = time.Now()
		osRun("lock", nil)
	}
}

func osRun(typ string, args map[string]string) (bool, string) {
	var out string
	var err error
	switch typ {
	case "restart":
		out, err = sh("shutdown", "-r", "+1", "MAM: quản trị viên yêu cầu khởi động lại máy")
	case "shutdown":
		out, err = sh("shutdown", "-h", "+1", "MAM: quản trị viên yêu cầu tắt máy")
	case "lock":
		// khóa phiên đồ họa của người đang ngồi máy
		u := activeUser()
		if u == "" {
			return false, "Không có ai đang đăng nhập để khóa"
		}
		if _, e := sh("loginctl", "lock-sessions"); e == nil {
			return true, "Đã khóa màn hình"
		}
		out, err = runAsUser(u, "xdg-screensaver", "lock")
	default:
		return false, "Lệnh không hỗ trợ: " + typ
	}
	if err != nil {
		return false, "Lỗi: " + err.Error() + " " + out
	}
	return true, "Đã thực hiện"
}

// runAsUser chạy lệnh GUI trong phiên của người đang ngồi máy (agent chạy quyền root).
func runAsUser(user string, name string, args ...string) (string, error) {
	full := append([]string{"-u", user, "env", "DISPLAY=:0", "XDG_RUNTIME_DIR=/run/user/$(id -u " + user + ")", name}, args...)
	out, err := exec.Command("sudo", full...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
