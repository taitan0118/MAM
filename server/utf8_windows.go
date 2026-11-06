//go:build windows

package main

import "syscall"

// Console Windows mặc định dùng bảng mã OEM nên tiếng Việt/Trung bị lỗi chữ; đặt UTF-8 cho cửa sổ lệnh.
func init() {
	k := syscall.NewLazyDLL("kernel32.dll")
	k.NewProc("SetConsoleOutputCP").Call(65001)
	k.NewProc("SetConsoleCP").Call(65001)
}
