//go:build !windows

package main

// Chỉ Windows có nhật ký sự kiện kiểu này; macOS/Linux chưa đọc.
func scanEvents(since int64) ([]SysEvent, bool) { return nil, false }
