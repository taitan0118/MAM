//go:build windows

package main

import (
	"errors"
	"os"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	mpr           = syscall.NewLazyDLL("mpr.dll")
	pWNetAdd      = mpr.NewProc("WNetAddConnection2W")
	pWNetCancel   = mpr.NewProc("WNetCancelConnection2W")
	riServiceName = "MAMCaiDatAgent"
	riFile        = `Temp\MAM-Agent-Cai.exe` // trong %SystemRoot% của máy đích (\\IP\ADMIN$)
	riPS1         = `Temp\MAM-Agent.ps1`     // agent PowerShell cho Windows 7/8
)

type netResource struct {
	Scope, Type, DisplayType, Usage uint32
	LocalName, RemoteName           *uint16
	Comment, Provider               *uint16
}

func wnetAdd(remote, user, pass string) syscall.Errno {
	r, _ := syscall.UTF16PtrFromString(remote)
	nr := netResource{RemoteName: r}
	var u, p *uint16 // để trống: truyền NULL để Windows dùng tài khoản đang chạy server (pass-through)
	if user != "" {
		u, _ = syscall.UTF16PtrFromString(user)
		p, _ = syscall.UTF16PtrFromString(pass)
	}
	ret, _, _ := pWNetAdd.Call(uintptr(unsafe.Pointer(&nr)), uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(u)), 0)
	return syscall.Errno(ret)
}

func wnetCancel(remote string) {
	r, _ := syscall.UTF16PtrFromString(remote)
	pWNetCancel.Call(uintptr(unsafe.Pointer(r)), 0, 1)
}

// riErr đổi mã lỗi Windows sang lời dễ hiểu cho người quản trị (tiếng Việt, Console dịch).
func riErr(step string, e syscall.Errno) error {
	switch e {
	case 1326:
		return errors.New("Sai tài khoản hoặc mật khẩu")
	case 5:
		return errors.New("Tài khoản không có quyền quản trị từ xa trên máy này (dùng tài khoản Administrator gốc, tài khoản quản trị domain, hoặc bật LocalAccountTokenFilterPolicy)")
	case 53, 64, 67, 1231, 1203, 121:
		return errors.New("Không kết nối được tới máy (máy tắt, hoặc tường lửa chặn chia sẻ file cổng 445)")
	case 1219:
		return errors.New("Server đang có kết nối khác tới máy này bằng tài khoản khác, thử lại sau ít phút")
	case 1331:
		return errors.New("Tài khoản đang bị vô hiệu hóa")
	case 1909:
		return errors.New("Tài khoản đang bị khóa do nhập sai nhiều lần")
	case 1330, 1907:
		return errors.New("Mật khẩu của tài khoản đã hết hạn hoặc cần đổi")
	}
	return errors.New(step + ": " + e.Error())
}

// winMajor đọc phiên bản Windows của máy đích từ kernel32.dll qua ổ ADMIN$ (6 = Windows 7/8/8.1, 10 = Windows 10/11).
func winMajor(admin string) int {
	p := admin + `System32\kernel32.dll`
	n, err := windows.GetFileVersionInfoSize(p, nil)
	if err != nil || n == 0 {
		return 0
	}
	buf := make([]byte, n)
	if windows.GetFileVersionInfo(p, 0, n, unsafe.Pointer(&buf[0])) != nil {
		return 0
	}
	var fi *windows.VS_FIXEDFILEINFO
	var l uint32
	if windows.VerQueryValue(unsafe.Pointer(&buf[0]), `\`, unsafe.Pointer(&fi), &l) != nil || fi == nil {
		return 0
	}
	return int(fi.FileVersionMS >> 16)
}

// remoteInstall: kết nối \\IP\IPC$ bằng tài khoản quản trị, chép agent đã gắn cấu hình vào \\IP\ADMIN$\Temp,
// tạo dịch vụ chạy một lần (qua cổng 445) để chạy "agent /S" bằng quyền SYSTEM, rồi xóa dịch vụ.
// Windows 7/8 (agent Go không chạy được): chép MAM-Agent.ps1 và chạy "-Install" của agent PowerShell.
func remoteInstall(ip, user, pass string, pk riPack, step func(string)) error {
	ipc := `\\` + ip + `\IPC$`
	wnetCancel(ipc)
	e := wnetAdd(ipc, user, pass)
	if e == 1219 { // còn phiên cũ tới máy này: hủy rồi thử lại
		wnetCancel(`\\` + ip + `\ADMIN$`)
		wnetCancel(ipc)
		e = wnetAdd(ipc, user, pass)
	}
	if e != 0 {
		return riErr("Không đăng nhập được vào máy", e)
	}
	step("Đang chép agent…")
	admin := `\\` + ip + `\ADMIN$\`
	noAdmin := errors.New("Không chép được file lên ổ quản trị ẩn ADMIN$ của máy (ổ này bị tắt, hoặc tài khoản không đủ quyền)")
	exe := pk.exe64
	if _, err := os.Stat(admin + "SysWOW64"); err != nil {
		switch {
		case errors.Is(err, os.ErrNotExist):
			exe = pk.exe32 // không có SysWOW64: Windows 32-bit
		case errors.Is(err, os.ErrPermission):
			// Windows có thể cho vào bằng tài khoản khách khi tài khoản/mật khẩu sai: tới đây mới biết là bị từ chối
			return errors.New("Bị từ chối vào ổ quản trị ADMIN$ của máy: kiểm tra lại tài khoản và mật khẩu (phải là tài khoản quản trị của máy đó)")
		default:
			return noAdmin
		}
	}
	var run string
	if v := winMajor(admin); v > 0 && v < 10 {
		step("Máy dùng Windows 7/8: cài agent bản PowerShell…")
		if len(pk.ps1) == 0 {
			return errors.New("Máy dùng Windows 7/8 nhưng server chưa có MAM-Agent.ps1 (đặt cạnh mam-server)")
		}
		if err := os.WriteFile(admin+riPS1, pk.ps1, 0o644); err != nil {
			return noAdmin
		}
		run = `%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%SystemRoot%\` + riPS1 + `" -Install -Server "` + pk.url + `" -Key "` + pk.key + `" -Lang ` + pk.lang
	} else {
		if len(exe) == 0 {
			return errors.New("Máy dùng Windows 32-bit nhưng server chưa có mam-agent-x86.exe")
		}
		if err := os.WriteFile(admin+riFile, exe, 0o644); err != nil {
			return noAdmin
		}
		run = `"%SystemRoot%\` + riFile + `" /S`
	}
	step("Đang chạy cài đặt…")
	host, _ := syscall.UTF16PtrFromString(`\\` + ip)
	scm, err := windows.OpenSCManager(host, nil, windows.SC_MANAGER_CONNECT|windows.SC_MANAGER_CREATE_SERVICE)
	if err != nil {
		if en, ok := err.(syscall.Errno); ok {
			return riErr("Không mở được trình quản lý dịch vụ của máy", en)
		}
		return err
	}
	defer windows.CloseServiceHandle(scm)
	name, _ := syscall.UTF16PtrFromString(riServiceName)
	if old, err := windows.OpenService(scm, name, windows.SERVICE_ALL_ACCESS); err == nil { // còn từ lần cài trước
		windows.DeleteService(old)
		windows.CloseServiceHandle(old)
		time.Sleep(time.Second)
	}
	// cmd /c start: tách tiến trình cài ra khỏi dịch vụ, để trình quản lý dịch vụ báo lỗi thời gian cũng không giết nó
	cmd, _ := syscall.UTF16PtrFromString(`%SystemRoot%\System32\cmd.exe /c start "" ` + run)
	disp, _ := syscall.UTF16PtrFromString("MAM - cài đặt agent")
	svc, err := windows.CreateService(scm, name, disp, windows.SERVICE_ALL_ACCESS, windows.SERVICE_WIN32_OWN_PROCESS, windows.SERVICE_DEMAND_START, windows.SERVICE_ERROR_IGNORE, cmd, nil, nil, nil, nil, nil)
	if err != nil {
		if en, ok := err.(syscall.Errno); ok {
			return riErr("Không tạo được dịch vụ cài đặt trên máy", en)
		}
		return err
	}
	defer windows.CloseServiceHandle(svc)
	defer windows.DeleteService(svc)
	if err := windows.StartService(svc, 0, nil); err != nil {
		// cmd.exe không phải dịch vụ thật nên thường báo 1053/1067 dù lệnh cài đã chạy; chỉ quyền là lỗi thật
		if en, ok := err.(syscall.Errno); ok && en == 5 {
			return riErr("Không chạy được dịch vụ cài đặt", en)
		}
	}
	return nil
}

// remoteCleanup xóa file cài tạm trên máy đích (agent đã tự chép mình vào Program Files) và đóng kết nối.
func remoteCleanup(ip string) {
	for _, f := range []string{riFile, riPS1} {
		f = `\\` + ip + `\ADMIN$\` + f
		for i := 0; i < 10; i++ {
			// chỉ chờ khi file còn đang chạy (bị khóa); lỗi khác (không có quyền, đã xóa...) thì thôi
			if err := os.Remove(f); err == nil || !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
				break
			}
			time.Sleep(3 * time.Second)
		}
	}
	wnetCancel(`\\` + ip + `\ADMIN$`)
	wnetCancel(`\\` + ip + `\IPC$`)
}
