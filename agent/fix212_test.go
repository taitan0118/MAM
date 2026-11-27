//go:build windows

package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// Đại ca: "máy agent khởi động lên lại thì tầm 3-4 phút server mới nhận được". Máy đã vào tới màn
// hình mà agent chưa chạy nghĩa là tác vụ khởi động chưa tới lượt — phải có thêm lượt chạy lúc
// đăng nhập, và tác vụ phải chạy bằng quyền SYSTEM mới gửi báo cáo được.
func TestTacVuCoLuotChayLucDangNhap(t *testing.T) {
	for _, can := range []string{"<BootTrigger>", "<LogonTrigger>", "S-1-5-18"} {
		if !strings.Contains(taskXML, can) {
			t.Fatalf("taskXML thiếu %s", can)
		}
	}
}

// Đăng ký lại tác vụ chỉ dành cho bản đã cài thật: bản chạy tay hay bản thử mà cũng đăng ký thì
// tác vụ của máy sẽ trỏ sang file exe ở chỗ khác, máy mất agent sau khi xóa bản thử đó.
func TestChiDangKyLaiTacVuChoBanDaCai(t *testing.T) {
	daCai := filepath.Join(installDir(), "mam-agent.exe")
	if !taskNeedsFix(daCai, 0) {
		t.Fatal("bản đã cài có tác vụ cũ thì phải đăng ký lại")
	}
	if taskNeedsFix(daCai, taskDefVer) {
		t.Fatal("đã đăng ký đúng phiên bản rồi thì đừng làm lại mỗi lần chạy")
	}
	if taskNeedsFix(`C:\thu\mam-agent.exe`, 0) {
		t.Fatal("bản chạy tay không được sửa tác vụ của máy")
	}
}
