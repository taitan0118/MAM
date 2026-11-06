//go:build windows

package uikit

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

func hide(c *exec.Cmd) *exec.Cmd {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return c
}

// openWindow thử mở như cửa sổ ứng dụng của Edge hoặc Chrome (có sẵn trên Windows 10/11), không được thì dùng trình duyệt mặc định.
func openWindow(url string) {
	var c []string
	for _, base := range []string{os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles"), os.Getenv("LocalAppData")} {
		if base == "" {
			continue
		}
		c = append(c, filepath.Join(base, "Microsoft", "Edge", "Application", "msedge.exe"), filepath.Join(base, "Google", "Chrome", "Application", "chrome.exe"))
	}
	prof, _ := os.MkdirTemp("", "mam-ui")
	for _, p := range c {
		if _, err := os.Stat(p); err == nil {
			if exec.Command(p, "--app="+url, "--window-size=860,760", "--user-data-dir="+prof, "--no-first-run", "--disable-extensions").Start() == nil {
				return
			}
		}
	}
	openDefault(url)
}

func openDefault(url string) {
	hide(exec.Command("rundll32", "url.dll,FileProtocolHandler", url)).Start()
}
