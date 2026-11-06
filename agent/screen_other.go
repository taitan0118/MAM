//go:build !windows

package main

// Xem/điều khiển màn hình trên Linux/macOS bằng công cụ hệ thống (không cgo).
//   Linux:  chụp bằng scrot | imagemagick(import) | ffmpeg; chuột/phím bằng xdotool
//   macOS:  chụp bằng screencapture; chuột/phím bằng cliclick (cài thêm) nếu có
// Thiếu công cụ thì vẫn xem/điều khiển ở mức có được, phần còn lại bỏ qua chứ không làm sập agent.
// CHƯA chạy thử trên máy thật — đại ca cần kiểm tra lại khi triển khai.
import (
	"bytes"
	"encoding/json"
	"image"
	_ "image/jpeg"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var httpUI = &http.Client{Timeout: 5 * time.Second}
var lastShot struct{ w, h int }

// captureScreenJPEG chụp màn hình ra JPEG bằng công cụ sẵn có; trả lỗi nếu không có công cụ nào.
func captureScreenJPEG() ([]byte, int, int, error) {
	f := filepath.Join(os.TempDir(), "mam-shot.jpg")
	defer os.Remove(f)
	var ok bool
	if runtime.GOOS == "darwin" {
		ok = tryRun("screencapture", "-x", "-t", "jpg", f)
	} else {
		ok = tryRun("scrot", "-o", "-q", "60", f) ||
			tryRun("import", "-window", "root", f) ||
			tryRun("ffmpeg", "-y", "-f", "x11grab", "-i", os.Getenv("DISPLAY")+":0.0", "-frames:v", "1", f)
	}
	if !ok {
		return nil, 0, 0, os.ErrNotExist
	}
	b, err := os.ReadFile(f)
	if err != nil {
		return nil, 0, 0, err
	}
	w, h := lastShot.w, lastShot.h
	if ic, _, e := image.DecodeConfig(bytes.NewReader(b)); e == nil {
		w, h = ic.Width, ic.Height
		lastShot.w, lastShot.h = w, h
	}
	return b, w, h, nil
}

func tryRun(name string, args ...string) bool {
	if _, err := exec.LookPath(name); err != nil {
		return false
	}
	return exec.Command(name, args...).Run() == nil
}

func runScreenCapture(stop chan struct{}) {
	warned := false
	tick := time.NewTicker(300 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			jpg, w, h, err := captureScreenJPEG()
			if err != nil {
				if !warned {
					warned = true
					addMsg("MAM", T("Máy này chưa cài công cụ chụp màn hình (Linux: scrot/imagemagick; macOS: cho phép quyền ghi màn hình)."), false)
				}
				continue
			}
			req, _ := http.NewRequest("POST", "http://"+uiAddr+"/api/screen/frame?w="+strconv.Itoa(w)+"&h="+strconv.Itoa(h), bytes.NewReader(jpg))
			resp, err := httpUI.Do(req)
			if err != nil {
				continue
			}
			var out struct{ Status string }
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			json.Unmarshal(b, &out)
			if out.Status != "active" {
				return
			}
		}
	}
}

func runScreenInject(stop chan struct{}) {
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			resp, err := httpUI.Get("http://" + uiAddr + "/api/screen/input")
			if err != nil {
				continue
			}
			var out struct {
				Status string
				Input  []screenInEv
			}
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()
			json.Unmarshal(b, &out)
			if out.Status != "active" {
				return
			}
			for _, ev := range out.Input {
				injectEvent(ev)
			}
		}
	}
}

// injectEvent tiêm một thao tác chuột/phím; px tính từ tỉ lệ 0..1 nhân kích thước ảnh chụp gần nhất.
func injectEvent(ev screenInEv) {
	px, py := int(ev.X*float64(lastShot.w)+0.5), int(ev.Y*float64(lastShot.h)+0.5)
	if runtime.GOOS == "darwin" {
		switch ev.Type {
		case "move":
			tryRun("cliclick", "m:"+itoa(px)+","+itoa(py))
		case "down":
			tryRun("cliclick", "dd:"+itoa(px)+","+itoa(py))
		case "up":
			tryRun("cliclick", "du:"+itoa(px)+","+itoa(py))
		case "key":
			tryRun("cliclick", "t:"+ev.Key)
		}
		return
	}
	switch ev.Type {
	case "move":
		tryRun("xdotool", "mousemove", itoa(px), itoa(py))
	case "down":
		tryRun("xdotool", "mousemove", itoa(px), itoa(py), "mousedown", mouseBtn(ev.Button))
	case "up":
		tryRun("xdotool", "mouseup", mouseBtn(ev.Button))
	case "wheel":
		b := "4" // lên
		if ev.Delta < 0 {
			b = "5"
		}
		tryRun("xdotool", "click", b)
	case "key":
		tryRun("xdotool", "key", xdoKey(ev.Key))
	case "kdown", "kup":
		if k := xdoCode(ev.Code); k != "" {
			tryRun("xdotool", map[string]string{"kdown": "keydown", "kup": "keyup"}[ev.Type], k)
		}
	case "text":
		tryRun("xdotool", "type", "--delay", "8", "--", ev.Text)
	}
}

func mouseBtn(b string) string {
	switch b {
	case "right":
		return "3"
	case "middle":
		return "2"
	}
	return "1"
}

// xdoCode: KeyboardEvent.code -> keysym xdotool (chữ, số, phím chức năng và các phím hay dùng).
func xdoCode(c string) string {
	m := map[string]string{"Enter": "Return", "NumpadEnter": "KP_Enter", "Backspace": "BackSpace", "Tab": "Tab", "Escape": "Escape", "Space": "space", "Delete": "Delete", "Insert": "Insert",
		"Home": "Home", "End": "End", "PageUp": "Prior", "PageDown": "Next", "ArrowLeft": "Left", "ArrowUp": "Up", "ArrowRight": "Right", "ArrowDown": "Down",
		"ShiftLeft": "Shift_L", "ShiftRight": "Shift_R", "ControlLeft": "Control_L", "ControlRight": "Control_R", "AltLeft": "Alt_L", "AltRight": "Alt_R", "MetaLeft": "Super_L", "MetaRight": "Super_R",
		"CapsLock": "Caps_Lock", "Minus": "minus", "Equal": "equal", "BracketLeft": "bracketleft", "BracketRight": "bracketright", "Backslash": "backslash", "Semicolon": "semicolon",
		"Quote": "apostrophe", "Comma": "comma", "Period": "period", "Slash": "slash", "Backquote": "grave"}
	if k, ok := m[c]; ok {
		return k
	}
	switch {
	case len(c) == 4 && c[:3] == "Key":
		return strings.ToLower(c[3:])
	case len(c) == 6 && c[:5] == "Digit":
		return c[5:]
	case len(c) >= 2 && len(c) <= 3 && c[0] == 'F':
		return c
	}
	return ""
}

// xdoKey đổi tên phím của trình duyệt (ev.key) sang tên keysym của xdotool cho các phím hay dùng.
func xdoKey(k string) string {
	switch k {
	case " ":
		return "space"
	case "Enter":
		return "Return"
	case "Escape":
		return "Escape"
	case "Backspace":
		return "BackSpace"
	case "Tab":
		return "Tab"
	case "ArrowUp":
		return "Up"
	case "ArrowDown":
		return "Down"
	case "ArrowLeft":
		return "Left"
	case "ArrowRight":
		return "Right"
	}
	return k // ký tự đơn (a, A, 1...) xdotool nhận trực tiếp
}

func itoa(n int) string { return strconv.Itoa(n) }
