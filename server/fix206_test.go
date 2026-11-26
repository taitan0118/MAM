package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Ba kết cục phải tách bạch: có hình, ĐO ĐƯỢC mà không có hình, và KHÔNG ĐO ĐƯỢC.
// Gộp cái thứ ba vào cái thứ hai là vu oan cho camera đang chạy.
func TestKhongGoiDuocThiKhongPhaiKhongRaHinh(t *testing.T) {
	// cổng đóng: gọi không tới
	_, err := rtspKbps(context.Background(), "127.0.0.1", 9, "", "admin", "x")
	if !errors.Is(err, errKhongDo) {
		t.Fatalf("gọi không tới phải là không đo được, nhận: %v", err)
	}
	out := checkDev(context.Background(), NetDev{ID: "c1", IP: "127.0.0.1", Port: 9, Kind: "camera", User: "admin", Pass: "x", VidMiss: 2}, true)
	if !out.KhongDo {
		t.Fatal("phải bật cờ chưa đo được")
	}
	if out.VidMiss != 2 {
		t.Fatalf("không được tính là mất hình, VidMiss=%d", out.VidMiss)
	}
	if out.VidLoi == "" {
		t.Fatal("phải nhớ lý do")
	}
}

// Camera nhận lệnh phát mà không gửi byte nào mới thật sự là không ra hình, và lúc đó MỚI được
// tính là một lượt mất hình.
func TestNhanLenhPhatMaKhongCoByteMoiLaKhongRaHinh(t *testing.T) {
	old := rtspMeasure
	rtspMeasure = 200 * time.Millisecond
	t.Cleanup(func() { rtspMeasure = old })
	ip, port := fakeRTSP(t, 0, false) // bắt tay xong nhưng không phát gói nào
	_, err := rtspKbps(context.Background(), ip, port, "", "", "")
	if err == nil || errors.Is(err, errKhongDo) {
		t.Fatalf("phải là lỗi không ra hình thật, nhận: %v", err)
	}
	ip2, port2 := fakeRTSP(t, 0, false)
	out := checkDev(context.Background(), NetDev{ID: "c2", IP: ip2, Port: port2, Kind: "camera", User: "admin", Pass: "x", VidMiss: 2}, true)
	if out.KhongDo {
		t.Fatal("đo được rồi thì không phải chưa đo được")
	}
	if out.VidMiss != 3 {
		t.Fatalf("đây mới là một lượt mất hình thật, VidMiss=%d", out.VidMiss)
	}
}
