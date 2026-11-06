//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

// workArea: vùng làm việc của màn hình chính (trừ thanh taskbar), để đặt cửa sổ ở góc phải dưới.
func workArea() (l, t, r, b int32) {
	var rc struct{ l, t, r, b int32 }
	ok, _, _ := syscall.NewLazyDLL("user32.dll").NewProc("SystemParametersInfoW").Call(0x0030, 0, uintptr(unsafe.Pointer(&rc)), 0)
	if ok == 0 || rc.r <= rc.l {
		return 0, 0, 1366, 728
	}
	return rc.l, rc.t, rc.r, rc.b
}

// focusWindow: đưa cửa sổ MAM đang mở (kể cả đang thu nhỏ) lên trước.
func focusWindow() {
	hidden("powershell", "-NoProfile", "-WindowStyle", "Hidden", "-Command",
		"$w=New-Object -ComObject WScript.Shell;[void]$w.AppActivate('MAM')").Run()
}

// openWindow mở trang giao tiếp như cửa sổ ứng dụng (Edge hoặc Chrome có sẵn trên Windows 10/11)
// đặt ở góc phải dưới màn hình. Nếu cửa sổ đang mở thì chỉ đưa lên trước.
func openWindow() {
	if httpOpen() {
		focusWindow()
		return
	}
	if !mayLaunch() {
		return
	}
	url := "http://" + uiAddr + "/"
	l, t, r, b := workArea()
	w, h := int32(400), int32(620)
	if h > b-t-20 {
		h = b - t - 20
	}
	x, y := r-w-12, b-h-12
	if x < l {
		x = l
	}
	pos := fmt.Sprintf("--window-position=%d,%d", x, y)
	size := fmt.Sprintf("--window-size=%d,%d", w, h)
	prof := filepath.Join(os.TempDir(), "mam-client-ui")
	for _, base := range []string{os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles"), os.Getenv("LocalAppData")} {
		if base == "" {
			continue
		}
		for _, p := range []string{filepath.Join(base, "Microsoft", "Edge", "Application", "msedge.exe"), filepath.Join(base, "Google", "Chrome", "Application", "chrome.exe")} {
			if _, err := os.Stat(p); err == nil {
				if exec.Command(p, "--app="+url, size, pos, "--user-data-dir="+prof, "--no-first-run", "--disable-extensions").Start() == nil {
					return
				}
			}
		}
	}
	hidden("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

// httpOpen: cửa sổ MAM có đang mở không (cửa sổ báo về agent mỗi 2 giây).
func httpOpen() bool {
	c := &http.Client{Timeout: 2 * time.Second}
	r, err := c.Get("http://" + uiAddr + "/api/state")
	if err != nil {
		return false
	}
	defer r.Body.Close()
	var st struct{ Open bool }
	json.NewDecoder(r.Body).Decode(&st)
	return st.Open
}

func hideConsole() {
	if w, _, _ := k32.NewProc("GetConsoleWindow").Call(); w != 0 {
		syscall.NewLazyDLL("user32.dll").NewProc("ShowWindow").Call(w, 0)
	}
}

// runUI: "ui open" mở cửa sổ ngay (từ biểu tượng ngoài màn hình); "ui" chạy nền trong phiên người dùng,
// hễ có tin chưa đọc mà cửa sổ chưa mở thì mở ra.
func runUI(args []string) {
	hideConsole()
	if len(args) > 0 && args[0] == "open" {
		openWindow()
		return
	}
	c := &http.Client{Timeout: 3 * time.Second}
	var curID string
	var curStop chan struct{}
	// Vừa đăng nhập/vừa cập nhật xong: chờ agent sẵn sàng. Cửa sổ MAM KHÔNG tự hiện; chỉ hiện khi có tin nhắn hoặc người dùng mở từ biểu tượng.
	for i := 0; i < 30; i++ {
		if r, err := c.Get("http://" + uiAddr + "/api/state"); err == nil {
			r.Body.Close()
			break
		}
		time.Sleep(time.Second)
	}
	go runTray() // biểu tượng MAM ở khay hệ thống
	lastSeq := -1
	for {
		wait := 2 * time.Second
		if r, err := c.Get("http://" + uiAddr + "/api/state?proc=1"); err == nil {
			var st struct {
				Unread int
				Seq    int
				Open   bool
				Screen *struct{ ID, Status, By string }
			}
			json.NewDecoder(r.Body).Decode(&st)
			r.Body.Close()
			// thông báo mới (tin nhắn, yêu cầu hỗ trợ, cảnh báo): đưa cửa sổ lên trước dù đang thu nhỏ
			if st.Seq != lastSeq {
				if lastSeq != -1 && st.Unread > 0 {
					openWindow()
				}
				lastSeq = st.Seq
			}
			switch {
			case st.Screen == nil && curID != "":
				close(curStop)
				curID = ""
			case st.Screen != nil:
				wait = 400 * time.Millisecond // trong lúc chờ đồng ý hoặc đang xem: theo dõi sát hơn
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

const clientTaskXML = `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo><Description>MAM - nhận tin nhắn từ quản trị viên</Description></RegistrationInfo>
  <Triggers><LogonTrigger><Enabled>true</Enabled></LogonTrigger></Triggers>
  <Principals><Principal id="Users"><GroupId>S-1-5-32-545</GroupId><RunLevel>LeastPrivilege</RunLevel></Principal></Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <Hidden>true</Hidden>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
  </Settings>
  <Actions Context="Users"><Exec><Command>"%s"</Command><Arguments>ui</Arguments></Exec></Actions>
</Task>`

// installClientUI: cho người dùng thường chạy được exe, giấu config.json (có token), tạo tác vụ mở cùng đăng nhập và biểu tượng "MAM" ngoài màn hình.
func installClientUI(dir, dst string) {
	hidden("icacls", dir, "/grant", "*S-1-5-32-545:(OI)(CI)RX").Run()
	hidden("icacls", filepath.Join(dir, "config.json"), "/inheritance:r", "/grant:r", "*S-1-5-18:F", "*S-1-5-32-544:F").Run()
	xml := filepath.Join(os.TempDir(), "mam-ui-task.xml")
	os.WriteFile(xml, utf16LE(fmt.Sprintf(clientTaskXML, dst)), 0o600)
	defer os.Remove(xml)
	hidden("schtasks", "/create", "/tn", "MAM Client", "/xml", xml, "/f").Run()
	hidden("schtasks", "/run", "/tn", "MAM Client").Run() // chạy ngay, không đợi đăng nhập lại
	ps := fmt.Sprintf(`$s=(New-Object -ComObject WScript.Shell).CreateShortcut([Environment]::GetFolderPath('CommonDesktopDirectory')+'\MAM.lnk');$s.TargetPath='%s';$s.Arguments='ui open';$s.IconLocation='%s,0';$s.Description='MAM';$s.Save()`, dst, dst)
	hidden("powershell", "-NoProfile", "-Command", ps).Run()
}

func removeClientUI() {
	hidden("schtasks", "/end", "/tn", "MAM Client").Run()
	hidden("schtasks", "/delete", "/tn", "MAM Client", "/f").Run()
	os.Remove(filepath.Join(os.Getenv("PUBLIC"), "Desktop", "MAM.lnk"))
}

func utf16LE(x string) []byte {
	u := utf16.Encode([]rune("\uFEFF" + x))
	b := make([]byte, 0, len(u)*2)
	for _, c := range u {
		b = append(b, byte(c), byte(c>>8))
	}
	return b
}

// startUIProc chạy tác vụ "MAM Client" (chạy dưới tên người dùng đang đăng nhập).
func startUIProc() bool { return hidden("schtasks", "/run", "/tn", "MAM Client").Run() == nil }
