//go:build !windows

package main

// keepHook: chỉ Windows cần chặn phím Win ở mức hệ điều hành; hệ khác thoát ngay.
func keepHook() {}
