//go:build !windows

package main

// Dùng chung cho Linux và macOS: điều phối số liệu/phần cứng (chi tiết từng HĐH ở metrics_linux.go,
// metrics_darwin.go) và nhánh giả lập -sim để thử nghiệm.
import (
	"log"
	"math/rand"
	"os"
	"runtime"
	"strings"
	"time"
)

func setupLog() { log.SetOutput(os.Stdout) }

func collectMetrics() Metrics {
	if *simName != "" {
		return simMetrics()
	}
	return osMetrics()
}

func collectHw() map[string]interface{} {
	if *simName != "" {
		return simHw()
	}
	return osHw()
}

func collectTemp() *float64 {
	if *simName != "" {
		return simTemp()
	}
	return osTemp()
}

// runCommand: lệnh từ xa. restart/shutdown/lock khác nhau theo HĐH nên để osRun lo; message đã xử lý
// ở handleCommand trước khi tới đây.
func runCommand(typ string, args map[string]string) (bool, string) {
	return osRun(typ, args)
}

// doInstall/uninstall trên Unix làm bằng script cài dịch vụ (systemd/launchd) kèm trong bộ phát hành,
// không nhúng trong agent như Windows.
func doInstall(args []string) {
	log.Print(T("Trên Linux/macOS dùng script cài dịch vụ kèm theo (install-agent-*.sh)"))
}
func autoInstall(b *Bundle, silent bool) {
	log.Print(T("Trên Linux/macOS dùng script cài dịch vụ kèm theo (install-agent-*.sh)"))
}
func doUninstall() {
	log.Print(T("Trên Linux/macOS dùng script gỡ dịch vụ kèm theo (uninstall-agent-*.sh)"))
}

// ---------- giả lập -sim (chỉ dùng để thử nghiệm, không đọc máy thật) ----------

var simStart = time.Now()
var simBase float64

func simMetrics() Metrics {
	if simBase == 0 {
		simBase = 10 + rand.Float64()*30
	}
	cpu := simBase + rand.Float64()*20
	if strings.Contains(*simName, "HOT") {
		cpu = 95 + rand.Float64()*4
	}
	free := 120.0
	if strings.Contains(*simName, "FULL") {
		free = 20
	}
	return Metrics{CPU: cpu, RAM: 30 + rand.Float64()*30, Uptime: int64(time.Since(simStart).Seconds()) + 3600, Disks: []Disk{{"C:", 500, free}}, User: "nhanvien"}
}

func simHw() map[string]interface{} {
	h, _ := os.Hostname()
	return map[string]interface{}{"cpu": "CPU thử nghiệm", "cores": runtime.NumCPU(), "ramGB": 16, "os": osName(), "model": "Máy thử nghiệm " + h,
		"board": "Giả lập", "serial": "SIM-" + *simName,
		"disks": []map[string]interface{}{{"model": "Ổ thử nghiệm", "sizeGB": 500, "type": "SSD", "health": "OK", "tempC": nil}}}
}

func simTemp() *float64 {
	t := 45 + rand.Float64()*10
	if strings.Contains(*simName, "HOT") {
		t = 92
	}
	return &t
}
