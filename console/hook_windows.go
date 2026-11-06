//go:build windows

package main

// Chặn phím Win (và tổ hợp Win+...) ở mức hệ điều hành khi cửa sổ điều khiển từ xa ("MAM · tên máy") đang được chọn:
// trình duyệt không chặn nổi Win+R, Win+D... nên máy đang điều khiển bị mở Run/ẩn cửa sổ. Ở đây phím được giữ lại,
// rồi trang điều khiển lấy qua cổng cục bộ 127.0.0.1:47640 và gửi sang máy bị điều khiển.
import (
	"encoding/json"
	"net"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const hookAddr = "127.0.0.1:47640"

var (
	u32      = syscall.NewLazyDLL("user32.dll")
	pSetHook = u32.NewProc("SetWindowsHookExW")
	pNext    = u32.NewProc("CallNextHookEx")
	pGetMsg  = u32.NewProc("GetMessageW")
	pFore    = u32.NewProc("GetForegroundWindow")
	pTitle   = u32.NewProc("GetWindowTextW")
	pEnum    = u32.NewProc("EnumWindows")
	pVisible = u32.NewProc("IsWindowVisible")
	pMutex   = syscall.NewLazyDLL("kernel32.dll").NewProc("CreateMutexW")
)

type kbdHook struct {
	vk, scan, flags, time uint32
	extra                 uintptr
}

type keyEv struct {
	T string `json:"t"`
	C string `json:"c"`
}

var (
	evMu    sync.Mutex
	evs     []keyEv
	evBase  int // số thứ tự của evs[0]
	winHeld bool
)

func pushEv(e keyEv) {
	evMu.Lock()
	evs = append(evs, e)
	if len(evs) > 500 {
		evBase += 250
		evs = evs[250:]
	}
	evMu.Unlock()
}

func title(h uintptr) string {
	buf := make([]uint16, 256)
	n, _, _ := pTitle.Call(h, uintptr(unsafe.Pointer(&buf[0])), 256)
	return syscall.UTF16ToString(buf[:n])
}

func viewerFocused() bool {
	h, _, _ := pFore.Call()
	return h != 0 && strings.HasPrefix(title(h), "MAM · ")
}

func vkCode(vk uint32) string {
	switch {
	case vk == 0x5B:
		return "MetaLeft"
	case vk == 0x5C:
		return "MetaRight"
	case vk >= 'A' && vk <= 'Z':
		return "Key" + string(rune(vk))
	case vk >= '0' && vk <= '9':
		return "Digit" + string(rune(vk))
	case vk >= 0x70 && vk <= 0x7B:
		return "F" + strconv.Itoa(int(vk-0x70)+1)
	}
	return map[uint32]string{0x09: "Tab", 0x0D: "Enter", 0x20: "Space", 0x1B: "Escape", 0x08: "Backspace", 0x2E: "Delete", 0x25: "ArrowLeft", 0x26: "ArrowUp",
		0x27: "ArrowRight", 0x28: "ArrowDown", 0x24: "Home", 0x23: "End", 0x21: "PageUp", 0x22: "PageDown", 0xA0: "ShiftLeft", 0xA1: "ShiftRight",
		0xA2: "ControlLeft", 0xA3: "ControlRight", 0xA4: "AltLeft", 0xA5: "AltRight", 0xBC: "Comma", 0xBE: "Period", 0xBF: "Slash", 0xBA: "Semicolon",
		0xDE: "Quote", 0xDB: "BracketLeft", 0xDD: "BracketRight", 0xDC: "Backslash", 0xC0: "Backquote", 0xBD: "Minus", 0xBB: "Equal"}[vk]
}

func hookProc(nCode int, wParam, lParam uintptr) uintptr {
	if nCode == 0 {
		k := (*kbdHook)(unsafe.Pointer(lParam))
		down := wParam == 0x100 || wParam == 0x104
		up := wParam == 0x101 || wParam == 0x105
		isWin := k.vk == 0x5B || k.vk == 0x5C
		if k.flags&0x10 == 0 && (down || up) { // 0x10 = phím do chương trình khác tạo ra, bỏ qua
			c := vkCode(k.vk)
			switch {
			case isWin && up && winHeld: // đã giữ phím Win lúc nãy thì luôn nhả, dù cửa sổ đã đổi
				winHeld = false
				pushEv(keyEv{"kup", c})
				return 1
			case isWin && down && viewerFocused():
				winHeld = true
				pushEv(keyEv{"kdown", c})
				return 1
			case winHeld && c != "":
				t := "kdown"
				if up {
					t = "kup"
				}
				pushEv(keyEv{t, c})
				return 1
			}
		}
	}
	r, _, _ := pNext.Call(0, uintptr(nCode), wParam, lParam)
	return r
}

func runHook() {
	runtime.LockOSThread()
	cb := syscall.NewCallback(hookProc)
	if h, _, _ := pSetHook.Call(13, cb, 0, 0); h == 0 {
		return
	}
	var msg [48]byte
	for {
		if r, _, _ := pGetMsg.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0); int32(r) <= 0 {
			return
		}
	}
}

func serveKeys() bool {
	ln, err := net.Listen("tcp", hookAddr)
	if err != nil {
		return false
	}
	mux := http.NewServeMux()
	cors := func(w http.ResponseWriter) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Private-Network", "true")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		w.Header().Set("Cache-Control", "no-store")
	}
	mux.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) { cors(w); w.Write([]byte(`{"ok":1}`)) })
	mux.HandleFunc("/k", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		if r.Method == "OPTIONS" {
			return
		}
		after, err := strconv.Atoi(r.URL.Query().Get("after"))
		if err != nil || after < 0 {
			evMu.Lock()
			after = evBase + len(evs) // lần đầu: chỉ lấy phím từ bây giờ
			evMu.Unlock()
		}
		for i := 0; i < 400; i++ { // giữ yêu cầu tối đa ~20 giây
			evMu.Lock()
			end := evBase + len(evs)
			if after < evBase {
				after = evBase
			}
			if after < end {
				out := append([]keyEv(nil), evs[after-evBase:]...)
				evMu.Unlock()
				json.NewEncoder(w).Encode(map[string]interface{}{"seq": end, "ev": out})
				return
			}
			evMu.Unlock()
			time.Sleep(50 * time.Millisecond)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"seq": after, "ev": []keyEv{}})
	})
	go http.Serve(ln, mux)
	return true
}

func mamWindow() bool {
	found := false
	cb := syscall.NewCallback(func(h, _ uintptr) uintptr {
		if v, _, _ := pVisible.Call(h); v != 0 && strings.HasPrefix(title(h), "MAM") {
			found = true
			return 0
		}
		return 1
	})
	pEnum.Call(cb, 0)
	return found
}

// keepHook chạy nền sau khi mở cửa sổ Console; tự thoát khi không còn cửa sổ "MAM" nào trong ~30 giây.
func keepHook() {
	name, _ := syscall.UTF16PtrFromString("MAMConsoleKeyHook")
	_, _, e := pMutex.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if e == syscall.Errno(183) { // đã có bản khác đang giữ phím
		return
	}
	if !serveKeys() {
		return
	}
	go runHook()
	start, gone := time.Now(), 0
	for {
		time.Sleep(5 * time.Second)
		if mamWindow() {
			gone = 0
		} else if gone++; gone >= 6 && time.Since(start) > time.Minute {
			return
		}
	}
}
