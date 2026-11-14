//go:build windows

package main

import (
	"strings"
	"testing"
)

// Tên Windows phải đọc được từ registry (chuỗi UTF-16), không còn chữ rác như khi chạy "cmd /c ver"
// trên máy có bảng mã khác, và phải nói rõ bản mấy.
func TestOsName(t *testing.T) {
	n := osName()
	t.Log("osName() =", n)
	if !strings.HasPrefix(n, "Windows ") {
		t.Fatalf("không bắt đầu bằng \"Windows \": %q", n)
	}
	if !strings.Contains(n, "(build ") {
		t.Errorf("thiếu số bản dựng: %q", n)
	}
	for _, c := range n {
		if c > 127 { // chữ rác do sai bảng mã
			t.Errorf("có ký tự ngoài ASCII, nghi sai bảng mã: %q", n)
			break
		}
	}
	if regStr(`SOFTWARE\Microsoft\Windows NT\CurrentVersion`, "KhongCoKhoaNay") != "" {
		t.Error("khóa không tồn tại mà vẫn trả về chuỗi")
	}
}
