//go:build !windows

package main

// Cửa sổ giao tiếp (xin phép xem màn hình, thanh báo, nhắn tin) trên Linux/macOS: mở trang cục bộ
// 127.0.0.1 bằng trình duyệt sẵn có, và chạy vòng theo dõi phiên như bản Windows.
import (
	"encoding/json"
	"net/http"
	"os/exec"
	"runtime"
	"time"
)

func openWindow() {
	if !mayLaunch() {
		return
	}
	url := "http://" + uiAddr + "/"
	if runtime.GOOS == "darwin" {
		exec.Command("open", url).Start()
		return
	}
	for _, b := range []string{"xdg-open", "google-chrome", "chromium", "firefox"} {
		if _, err := exec.LookPath(b); err == nil {
			if b == "google-chrome" || b == "chromium" {
				exec.Command(b, "--app="+url, "--window-size=440,640").Start()
			} else {
				exec.Command(b, url).Start()
			}
			return
		}
	}
}

// runUI: chạy nền trong phiên người dùng. Có tin chưa đọc mà cửa sổ chưa mở thì mở ra; khi có phiên
// xem màn hình thì tự chụp/tiêm chuột-phím. Giống ui_windows.go.
func runUI(args []string) {
	if len(args) > 0 && args[0] == "open" {
		openWindow()
		return
	}
	c := &http.Client{Timeout: 3 * time.Second}
	var curID string
	var curStop chan struct{}
	for {
		wait := 2 * time.Second
		if r, err := c.Get("http://" + uiAddr + "/api/state?proc=1"); err == nil {
			var st struct {
				Unread int
				Open   bool
				Screen *struct{ ID, Status, By string }
			}
			json.NewDecoder(r.Body).Decode(&st)
			r.Body.Close()
			if st.Unread > 0 && !st.Open {
				openWindow()
				time.Sleep(8 * time.Second)
			}
			switch {
			case st.Screen == nil && curID != "":
				close(curStop)
				curID = ""
			case st.Screen != nil:
				wait = 400 * time.Millisecond
				if (st.Screen.Status == "pending" || st.Screen.Status == "active") && !st.Open {
					openWindow()
				}
				if st.Screen.Status == "active" && curID != st.Screen.ID {
					curID, curStop = st.Screen.ID, make(chan struct{})
					go runScreenCapture(curStop)
					go runScreenInject(curStop)
				} else if st.Screen.Status != "active" && curID != "" {
					close(curStop)
					curID = ""
				}
			}
		}
		time.Sleep(wait)
	}
}

// startUIProc: Linux/macOS tự mở lại tiến trình ui qua autostart/LaunchAgent khi đăng nhập; agent không tự bật được.
func startUIProc() bool { return false }
