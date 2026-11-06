//go:build windows

package main

// Chụp màn hình và gửi chuột/phím thật — chỉ chạy trong tiến trình "ui" (phiên người dùng đang
// đăng nhập), vì tiến trình chính chạy quyền SYSTEM ở phiên 0 không có quyền vào desktop của họ.
import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32          = syscall.NewLazyDLL("user32.dll")
	gdi32           = syscall.NewLazyDLL("gdi32.dll")
	pGetDC          = user32.NewProc("GetDC")
	pReleaseDC      = user32.NewProc("ReleaseDC")
	pGetSysMetrics  = user32.NewProc("GetSystemMetrics")
	pSetCursorPos   = user32.NewProc("SetCursorPos")
	pMouseEvent     = user32.NewProc("mouse_event")
	pKeybdEvent     = user32.NewProc("keybd_event")
	pVkKeyScan      = user32.NewProc("VkKeyScanW")
	pSendInput      = user32.NewProc("SendInput")
	pCreateCompatDC = gdi32.NewProc("CreateCompatibleDC")
	pCreateDIB      = gdi32.NewProc("CreateDIBSection")
	pGdiFlush       = gdi32.NewProc("GdiFlush")
	pSelectObject   = gdi32.NewProc("SelectObject")
	pBitBlt         = gdi32.NewProc("BitBlt")
	pDeleteDC       = gdi32.NewProc("DeleteDC")
	pDeleteObject   = gdi32.NewProc("DeleteObject")
)

const (
	smXVirtual  = 76
	smYVirtual  = 77
	smCXVirtual = 78
	smCYVirtual = 79
	srcCopy     = 0x00CC0020
	captureBlt  = 0x40000000 // chụp cả cửa sổ trong suốt (layered)
)

type bitmapInfoHeader struct {
	Size          uint32
	Width, Height int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPPM, YPPM    int32
	ClrUsed       uint32
	ClrImportant  uint32
}

func virtualScreen() (x, y, w, h int32) {
	a, _, _ := pGetSysMetrics.Call(smXVirtual)
	b, _, _ := pGetSysMetrics.Call(smYVirtual)
	c, _, _ := pGetSysMetrics.Call(smCXVirtual)
	d, _, _ := pGetSysMetrics.Call(smCYVirtual)
	return int32(a), int32(b), int32(c), int32(d)
}

// captureInto chụp toàn bộ màn hình (mọi màn hình nếu nhiều) vào img (RGBA, đúng kích thước w×h).
func captureInto(img *image.RGBA, x, y, w, h int32) error {
	hdc, _, _ := pGetDC.Call(0)
	if hdc == 0 {
		return errors.New("GetDC lỗi")
	}
	defer pReleaseDC.Call(0, hdc)
	mdc, _, _ := pCreateCompatDC.Call(hdc)
	if mdc == 0 {
		return errors.New("CreateCompatibleDC lỗi")
	}
	defer pDeleteDC.Call(mdc)
	// DIB section: BitBlt ghi thẳng vào bộ nhớ ta đọc được, khỏi cần GetDIBits (hay lỗi trên một số máy/driver)
	var bi bitmapInfoHeader
	bi.Size = uint32(unsafe.Sizeof(bi))
	bi.Width, bi.Height = w, -h // âm: hàng trên cùng ở đầu bộ đệm, khỏi phải lật ảnh
	bi.Planes, bi.BitCount = 1, 32
	var bits unsafe.Pointer
	bmp, _, _ := pCreateDIB.Call(hdc, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 || bits == nil {
		return errors.New("CreateDIBSection lỗi")
	}
	defer pDeleteObject.Call(bmp)
	old, _, _ := pSelectObject.Call(mdc, bmp)
	defer pSelectObject.Call(mdc, old)
	if r, _, _ := pBitBlt.Call(mdc, 0, 0, uintptr(w), uintptr(h), hdc, uintptr(x), uintptr(y), srcCopy|captureBlt); r == 0 {
		return errors.New("BitBlt lỗi")
	}
	pGdiFlush.Call()
	buf := unsafe.Slice((*byte)(bits), int(w)*int(h)*4)
	pix := img.Pix
	for i := 0; i+3 < len(buf); i += 4 {
		pix[i], pix[i+1], pix[i+2], pix[i+3] = buf[i+2], buf[i+1], buf[i], 255
	}
	return nil
}

// encodeJPEG nén khung hình; khung quá nặng (màn hình rất lớn, nhiều chi tiết) thì hạ chất lượng để không vượt giới hạn.
func encodeJPEG(img *image.RGBA, quality int) ([]byte, error) {
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	if out.Len() > 1200000 && quality > 70 {
		out.Reset()
		if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 70}); err != nil {
			return nil, err
		}
	}
	return out.Bytes(), nil
}

func injectMouseMove(xr, yr float64) {
	x, y, w, h := virtualScreen()
	px := x + int32(xr*float64(w))
	py := y + int32(yr*float64(h))
	pSetCursorPos.Call(uintptr(px), uintptr(py))
}

func injectButton(down bool, button string) {
	var flag uintptr
	switch {
	case button == "middle" && down:
		flag = 0x0020
	case button == "middle" && !down:
		flag = 0x0040
	case button == "right" && down:
		flag = 0x0008
	case button == "right" && !down:
		flag = 0x0010
	case down:
		flag = 0x0002
	default:
		flag = 0x0004
	}
	pMouseEvent.Call(flag, 0, 0, 0, 0)
}

func injectWheel(delta float64) { pMouseEvent.Call(0x0800, 0, 0, uintptr(int32(delta)), 0) }

// namedVK: các phím đặc biệt gửi được dù không gõ được một ký tự Unicode đơn.
var namedVK = map[string]uint16{
	"Enter": 0x0D, "Backspace": 0x08, "Tab": 0x09, "Escape": 0x1B, "Esc": 0x1B,
	"Space": 0x20, "Delete": 0x2E, "Home": 0x24, "End": 0x23, "PageUp": 0x21, "PageDown": 0x22,
	"ArrowLeft": 0x25, "ArrowUp": 0x26, "ArrowRight": 0x27, "ArrowDown": 0x28,
}

// injectKey: gửi phím đặt tên ở trên, hoặc một ký tự đơn theo bố cục bàn phím hiện tại.
// shortcut: không gõ được dấu tiếng Việt (IME) hoặc tổ hợp phím (Ctrl+C...) — đủ cho gõ nhanh/điều hướng để hỗ trợ.
func injectKey(key string) {
	if vk, ok := namedVK[key]; ok {
		pKeybdEvent.Call(uintptr(vk), 0, 0, 0)
		pKeybdEvent.Call(uintptr(vk), 0, 2, 0)
		return
	}
	r := []rune(key)
	if len(r) != 1 {
		return
	}
	ret, _, _ := pVkKeyScan.Call(uintptr(r[0]))
	if int16(ret) == -1 {
		return
	}
	vk := byte(ret & 0xFF)
	shift := (ret>>8)&1 != 0
	if shift {
		pKeybdEvent.Call(0x10, 0, 0, 0)
	}
	pKeybdEvent.Call(uintptr(vk), 0, 0, 0)
	pKeybdEvent.Call(uintptr(vk), 0, 2, 0)
	if shift {
		pKeybdEvent.Call(0x10, 0, 2, 0)
	}
}

// codeVK: mã phím vật lý của trình duyệt (KeyboardEvent.code) -> virtual-key của Windows.
var codeVK = map[string]uint16{
	"Enter": 0x0D, "NumpadEnter": 0x0D, "Backspace": 0x08, "Tab": 0x09, "Escape": 0x1B, "Space": 0x20, "Delete": 0x2E, "Insert": 0x2D,
	"Home": 0x24, "End": 0x23, "PageUp": 0x21, "PageDown": 0x22, "ArrowLeft": 0x25, "ArrowUp": 0x26, "ArrowRight": 0x27, "ArrowDown": 0x28,
	"ShiftLeft": 0xA0, "ShiftRight": 0xA1, "ControlLeft": 0xA2, "ControlRight": 0xA3, "AltLeft": 0xA4, "AltRight": 0xA5,
	"MetaLeft": 0x5B, "MetaRight": 0x5C, "ContextMenu": 0x5D, "CapsLock": 0x14, "NumLock": 0x90, "ScrollLock": 0x91, "PrintScreen": 0x2C, "Pause": 0x13,
	"Minus": 0xBD, "Equal": 0xBB, "BracketLeft": 0xDB, "BracketRight": 0xDD, "Backslash": 0xDC, "Semicolon": 0xBA, "Quote": 0xDE,
	"Comma": 0xBC, "Period": 0xBE, "Slash": 0xBF, "Backquote": 0xC0,
	"NumpadAdd": 0x6B, "NumpadSubtract": 0x6D, "NumpadMultiply": 0x6A, "NumpadDivide": 0x6F, "NumpadDecimal": 0x6E,
}
var extendedVK = map[uint16]bool{0x2D: true, 0x2E: true, 0x24: true, 0x23: true, 0x21: true, 0x22: true, 0x25: true, 0x26: true, 0x27: true, 0x28: true, 0xA3: true, 0xA5: true, 0x5B: true, 0x5C: true, 0x5D: true, 0x6F: true, 0x2C: true}

func vkFromCode(code string) (uint16, bool) {
	if vk, ok := codeVK[code]; ok {
		return vk, true
	}
	switch {
	case len(code) == 4 && code[:3] == "Key" && code[3] >= 'A' && code[3] <= 'Z':
		return uint16(code[3]), true
	case len(code) == 6 && code[:5] == "Digit" && code[5] >= '0' && code[5] <= '9':
		return uint16(code[5]), true
	case len(code) == 7 && code[:6] == "Numpad" && code[6] >= '0' && code[6] <= '9':
		return 0x60 + uint16(code[6]-'0'), true
	case code[0] == 'F' && len(code) <= 3:
		if n, err := strconv.Atoi(code[1:]); err == nil && n >= 1 && n <= 12 {
			return 0x70 + uint16(n-1), true
		}
	}
	return 0, false
}

var heldKeys = map[uint16]bool{}

// injectVK: nhấn/nhả một phím theo VK, nhớ phím đang giữ để nhả hết khi phiên kết thúc (tránh kẹt Ctrl/Alt).
func injectVK(vk uint16, down bool) {
	var fl uintptr
	if extendedVK[vk] {
		fl |= 1
	}
	if !down {
		fl |= 2
		delete(heldKeys, vk)
	} else {
		heldKeys[vk] = true
	}
	pKeybdEvent.Call(uintptr(vk), 0, fl, 0)
}

func releaseHeldKeys() {
	for vk := range heldKeys {
		injectVK(vk, false)
	}
}

// kbInput khớp struct INPUT của Windows (kiểu bàn phím) cho cả 32 và 64 bit.
type kbInput struct {
	Type  uint32
	_     [unsafe.Sizeof(uintptr(0)) - 4]byte
	Vk    uint16
	Scan  uint16
	Flags uint32
	Time  uint32
	Extra uintptr
	_     [8]byte
}

// injectText gõ chuỗi Unicode (tiếng Việt, ký tự đặc biệt) bằng SendInput KEYEVENTF_UNICODE; tối đa 500 ký tự.
func injectText(s string) {
	n := 0
	for _, r := range s {
		if n++; n > 500 {
			return
		}
		units := []uint16{uint16(r)}
		if r > 0xFFFF {
			r -= 0x10000
			units = []uint16{uint16(0xD800 + (r >> 10)), uint16(0xDC00 + (r & 0x3FF))}
		}
		for _, u := range units {
			in := [2]kbInput{{Type: 1, Scan: u, Flags: 4}, {Type: 1, Scan: u, Flags: 4 | 2}}
			pSendInput.Call(2, uintptr(unsafe.Pointer(&in[0])), unsafe.Sizeof(in[0]))
		}
	}
}

var httpUI = &http.Client{Timeout: 5 * time.Second}

// runScreenCapture: giống VNC, chỉ gửi những vùng màn hình vừa thay đổi (nén JPEG từng vùng) nên gõ phím, di chuột, mở menu
// gần như tức thì. Thay đổi lớn (xem phim, kéo cửa sổ to) thì gửi cả màn hình nén nhẹ, rồi khi đứng yên gửi lại bản nét.
func runScreenCapture(stop chan struct{}) {
	const qPart, qFast, qSharp = 85, 70, 92
	type pkt struct {
		b    []byte
		key  bool
		w, h int
	}
	out := make(chan pkt, 1)
	ended := make(chan struct{})
	var endOnce sync.Once
	var needKey int32
	go func() { // người gửi: thứ tự gói được giữ nguyên
		for {
			select {
			case <-stop:
				return
			case <-ended:
				return
			case p := <-out:
				k := "0"
				if p.key {
					k = "1"
				}
				req, _ := http.NewRequest("POST", "http://"+uiAddr+"/api/screen/frame?fmt=r&k="+k+"&w="+strconv.Itoa(p.w)+"&h="+strconv.Itoa(p.h), bytes.NewReader(p.b))
				resp, err := httpUI.Do(req)
				if err != nil {
					atomic.StoreInt32(&needKey, 1) // có thể mất gói: gửi lại hình đầy đủ
					continue
				}
				var st struct {
					Status string
					Key    bool
				}
				b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
				resp.Body.Close()
				json.Unmarshal(b, &st)
				if st.Status != "active" {
					endOnce.Do(func() { close(ended) })
					return
				}
				if st.Key {
					atomic.StoreInt32(&needKey, 1)
				}
			}
		}
	}()
	var lastErr string
	var img *image.RGBA
	var prev []byte
	unchanged := 0
	dirty := false // lần gửi cả màn hình gần nhất chưa ở mức nét
	tick := time.NewTicker(16 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ended:
			return
		case <-tick.C:
		}
		if len(out) == cap(out) { // người gửi còn bận: chưa chụp, để thay đổi dồn lại gửi một lần
			continue
		}
		x, y, w, h := virtualScreen()
		if w <= 0 || h <= 0 {
			continue
		}
		if img == nil || img.Rect.Dx() != int(w) || img.Rect.Dy() != int(h) { // lần đầu hoặc đổi độ phân giải
			img = image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
			prev = nil
		}
		if err := captureInto(img, x, y, w, h); err != nil {
			if err.Error() != lastErr { // log chỉ khi lỗi đổi
				lastErr = err.Error()
				log.Printf("chụp màn hình lỗi: %v", err)
			}
			// báo lỗi cho agent chính (kèm lời báo ui còn sống) để Console hiện lý do
			if r, e := httpUI.Post("http://"+uiAddr+"/api/screen/frame?err="+url.QueryEscape(err.Error()), "application/octet-stream", nil); e == nil {
				r.Body.Close()
			}
			continue
		}
		full := image.Rect(0, 0, int(w), int(h))
		var rects []image.Rectangle
		q := qPart
		switch {
		case prev == nil || atomic.SwapInt32(&needKey, 0) == 1: // hình đầy đủ đầu tiên, hoặc server/viewer xin lại
			rects, q = []image.Rectangle{full}, qFast
			dirty = true
		default:
			rs, frac := changedRects(img, prev)
			switch {
			case rs == nil:
				unchanged++
				if !dirty || unchanged < 4 { // chờ màn hình đứng yên ~0,06 giây rồi mới gửi bản nét
					continue
				}
				rects, q, dirty = []image.Rectangle{full}, qSharp, false
			case frac > 0.5 || len(rs) > 12: // đổi nhiều: gửi cả màn hình nén nhẹ
				unchanged = 0
				rects, q, dirty = []image.Rectangle{full}, qFast, true
			default:
				unchanged = 0
				rects = rs
			}
		}
		prev = append(prev[:0], img.Pix...)
		pk := make([]screenRect, 0, len(rects))
		for _, r := range rects {
			jpg, err := encodeJPEG(img.SubImage(r).(*image.RGBA), q)
			if err != nil {
				pk = nil
				break
			}
			pk = append(pk, screenRect{r.Min.X, r.Min.Y, r.Dx(), r.Dy(), jpg})
		}
		if pk == nil {
			prev = nil
			continue
		}
		isKey := len(rects) == 1 && rects[0] == full
		select {
		case out <- pkt{buildPacket(int(w), int(h), pk), isKey, int(w), int(h)}:
		case <-stop:
			return
		case <-ended:
			return
		}
	}
}

// runScreenInject: lấy thao tác chuột/phím admin gửi mỗi 40ms và tiêm vào máy, tới khi phiên kết thúc.
func runScreenInject(stop chan struct{}) {
	defer releaseHeldKeys()
	tick := time.NewTicker(40 * time.Millisecond)
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
				switch ev.Type {
				case "move":
					injectMouseMove(ev.X, ev.Y)
				case "down":
					injectMouseMove(ev.X, ev.Y)
					injectButton(true, ev.Button)
				case "up":
					injectButton(false, ev.Button)
				case "wheel":
					injectWheel(ev.Delta)
				case "key":
					injectKey(ev.Key)
				case "kdown", "kup":
					if vk, ok := vkFromCode(ev.Code); ok {
						injectVK(vk, ev.Type == "kdown")
					}
				case "text":
					injectText(ev.Text)
				}
			}
		}
	}
}
