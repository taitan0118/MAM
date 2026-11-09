package main

import (
	"os/exec"
	"syscall"
	"unsafe"
)

var pGetDiskFree = syscall.NewLazyDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")

func diskFree(p string) (free, total uint64, err error) {
	u, err := syscall.UTF16PtrFromString(p)
	if err != nil {
		return
	}
	var avail, tot, all uint64
	r, _, e := pGetDiskFree.Call(uintptr(unsafe.Pointer(u)), uintptr(unsafe.Pointer(&avail)), uintptr(unsafe.Pointer(&tot)), uintptr(unsafe.Pointer(&all)))
	if r == 0 {
		return 0, 0, e
	}
	return avail, tot, nil
}

// không bật cửa sổ PowerShell đen khi server chạy như ứng dụng thường
func hideWindow(c *exec.Cmd) { c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }
