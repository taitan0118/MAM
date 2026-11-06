//go:build windows

package uikit

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"
)

// Exec chạy một lệnh ẩn, trả về cả đầu ra lẫn lỗi.
func Exec(name string, args ...string) (string, error) {
	var b bytes.Buffer
	c := hide(exec.Command(name, args...))
	c.Stdout, c.Stderr = &b, &b
	err := c.Run()
	return b.String(), err
}

func IsAdmin() bool { _, err := Exec("net", "session"); return err == nil }

// Elevate chạy lại chính file này với quyền Administrator (hộp thoại UAC).
func Elevate() {
	exe, _ := os.Executable()
	verb, _ := syscall.UTF16PtrFromString("runas")
	f, _ := syscall.UTF16PtrFromString(exe)
	syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteW").Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(f)), 0, 0, 1)
}

// CmdResult là kết quả chạy một lệnh có truyền đầu vào chuẩn.
type CmdResult struct {
	Out string
	Err error
}

// ExecIn như Exec nhưng ghi sẵn nội dung vào đầu vào chuẩn của lệnh (dùng để đưa tài khoản/mật khẩu qua ống dẫn).
func ExecIn(stdin, name string, args ...string) CmdResult {
	var b bytes.Buffer
	c := hide(exec.Command(name, args...))
	c.Stdin, c.Stdout, c.Stderr = strings.NewReader(stdin), &b, &b
	err := c.Run()
	return CmdResult{b.String(), err}
}
