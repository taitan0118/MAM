//go:build windows

package main

// Biểu tượng MAM ở khay hệ thống (góc phải thanh tác vụ): bấm để mở cửa sổ MAM, bấm phải để chọn menu.
import (
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	user32t      = syscall.NewLazyDLL("user32.dll")
	shell32      = syscall.NewLazyDLL("shell32.dll")
	pRegClass    = user32t.NewProc("RegisterClassExW")
	pCreateWin   = user32t.NewProc("CreateWindowExW")
	pDefWinProc  = user32t.NewProc("DefWindowProcW")
	pGetMsg      = user32t.NewProc("GetMessageW")
	pTransMsg    = user32t.NewProc("TranslateMessage")
	pDispatch    = user32t.NewProc("DispatchMessageW")
	pRegWinMsg   = user32t.NewProc("RegisterWindowMessageW")
	pCreateMenu  = user32t.NewProc("CreatePopupMenu")
	pAppendMenu  = user32t.NewProc("AppendMenuW")
	pTrackMenu   = user32t.NewProc("TrackPopupMenu")
	pDestroyMenu = user32t.NewProc("DestroyMenu")
	pSetFg       = user32t.NewProc("SetForegroundWindow")
	pPostMsg     = user32t.NewProc("PostMessageW")
	pGetCursor   = user32t.NewProc("GetCursorPos")
	pLoadIcon    = user32t.NewProc("LoadIconW")
	pNotify      = shell32.NewProc("Shell_NotifyIconW")
	pExtractIcon = shell32.NewProc("ExtractIconW")
)

const (
	wmTray       = 0x0401 // WM_USER+1
	wmLButtonUp  = 0x0202
	wmLDblClk    = 0x0203
	wmRButtonUp  = 0x0205
	nimAdd       = 0
	nifMessage   = 1
	nifIcon      = 2
	nifTip       = 4
	menuOpenID   = 1
	trayMenuText = "Mở MAM"
)

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type notifyIcon struct {
	Size            uint32
	Wnd             uintptr
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            uintptr
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Version         uint32
	InfoTitle       [64]uint16
	InfoFlags       uint32
	Guid            [16]byte
	BalloonIcon     uintptr
}

var trayWnd, trayIcon, trayCreatedMsg uintptr

func trayText() (tip, open string) {
	switch uiLang {
	case "en":
		return "MAM · Missing App Monitor", "Open MAM"
	case "zh":
		return "MAM · Missing App Monitor", "打开 MAM"
	}
	return "MAM · Missing App Monitor", trayMenuText
}

func addTrayIcon() {
	nd := notifyIcon{Wnd: trayWnd, ID: 1, Flags: nifMessage | nifIcon | nifTip, CallbackMessage: wmTray, Icon: trayIcon}
	nd.Size = uint32(unsafe.Sizeof(nd))
	tip, _ := trayText()
	copy(nd.Tip[:127], syscall.StringToUTF16(tip))
	pNotify.Call(nimAdd, uintptr(unsafe.Pointer(&nd)))
}

func trayProc(hwnd, msg, wp, lp uintptr) uintptr {
	switch {
	case msg == wmTray:
		switch lp & 0xFFFF {
		case wmLButtonUp, wmLDblClk:
			go openWindow()
		case wmRButtonUp:
			var pt struct{ X, Y int32 }
			pGetCursor.Call(uintptr(unsafe.Pointer(&pt)))
			m, _, _ := pCreateMenu.Call()
			_, open := trayText()
			t, _ := syscall.UTF16PtrFromString(open)
			pAppendMenu.Call(m, 0, menuOpenID, uintptr(unsafe.Pointer(t)))
			pSetFg.Call(hwnd)                                                                        // bắt buộc để menu tự đóng khi bấm ra ngoài
			cmd, _, _ := pTrackMenu.Call(m, 0x0100|0x0002, uintptr(pt.X), uintptr(pt.Y), 0, hwnd, 0) // TPM_RETURNCMD | TPM_RIGHTBUTTON
			pPostMsg.Call(hwnd, 0, 0, 0)
			pDestroyMenu.Call(m)
			if cmd == menuOpenID {
				go openWindow()
			}
		}
		return 0
	case trayCreatedMsg != 0 && msg == trayCreatedMsg: // Explorer khởi động lại: gắn lại biểu tượng
		addTrayIcon()
		return 0
	}
	r, _, _ := pDefWinProc.Call(hwnd, msg, wp, lp)
	return r
}

// runTray chạy vòng lặp thông điệp của biểu tượng khay trong một luồng riêng (gọi bằng go).
func runTray() {
	runtime.LockOSThread()
	exe, _ := os.Executable()
	if p, err := syscall.UTF16PtrFromString(exe); err == nil {
		trayIcon, _, _ = pExtractIcon.Call(0, uintptr(unsafe.Pointer(p)), 0)
	}
	if trayIcon <= 1 { // không đọc được biểu tượng của file: dùng biểu tượng ứng dụng mặc định
		trayIcon, _, _ = pLoadIcon.Call(0, 32512)
	}
	cn, _ := syscall.UTF16PtrFromString("MAMTrayWnd")
	wc := wndClassEx{ClassName: cn, WndProc: syscall.NewCallback(trayProc)}
	wc.Size = uint32(unsafe.Sizeof(wc))
	pRegClass.Call(uintptr(unsafe.Pointer(&wc)))
	trayWnd, _, _ = pCreateWin.Call(0, uintptr(unsafe.Pointer(cn)), uintptr(unsafe.Pointer(cn)), 0, 0, 0, 0, 0, 0, 0, 0, 0)
	if trayWnd == 0 {
		return
	}
	tb, _ := syscall.UTF16PtrFromString("TaskbarCreated")
	trayCreatedMsg, _, _ = pRegWinMsg.Call(uintptr(unsafe.Pointer(tb)))
	addTrayIcon()
	var raw [64]byte // đủ chỗ cho struct MSG của cả 32 và 64 bit
	for {
		r, _, _ := pGetMsg.Call(uintptr(unsafe.Pointer(&raw[0])), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		pTransMsg.Call(uintptr(unsafe.Pointer(&raw[0])))
		pDispatch.Call(uintptr(unsafe.Pointer(&raw[0])))
	}
}
