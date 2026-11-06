//go:build !windows

package main

// Phần dùng chung cho Linux và macOS: chạy lệnh, tắt tiến trình, chặn web qua /etc/hosts.
import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// sh chạy một lệnh ngoài, trả output gộp (stdout+stderr) và lỗi. Thay cho hidden() của Windows.
func sh(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func osKill(pid string) error {
	n, err := strconv.Atoi(pid)
	if err != nil {
		return err
	}
	return syscall.Kill(n, syscall.SIGKILL)
}

func archName() string { return runtime.GOARCH }

func swapSelf(newFile string) error {
	// File đang chạy trên Unix ghi đè được bằng rename; dịch vụ (systemd/launchd) tự khởi động lại.
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return os.Rename(newFile, exe)
}

var applied struct {
	sites    string
	lastApps time.Time
	lastLock time.Time
}

const hostsPath = "/etc/hosts"
const hMark1, hMark2 = "# MAM-BEGIN", "# MAM-END"

// writeHosts ghi danh sách web bị chặn vào /etc/hosts, giữ nguyên phần còn lại. Giống bản Windows.
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
	return os.WriteFile(hostsPath, []byte(strings.Join(keep, "\n")+"\n"), 0o644)
}

func enableWOL() {}
