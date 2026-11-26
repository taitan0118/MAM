package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Chưa đo luồng hình lần nào (server vừa khởi động) thì không được nói "không ra hình".
func TestChuaDoThiKhongKetLuan(t *testing.T) {
	s := &Server{st: State{}}
	s.st.Settings.CamUser = "admin"
	d := &NetDev{ID: "c1", IP: "10.0.0.9", Kind: "camera", Up: true, Checked: time.Now()}
	if v := s.devView(d)["vid"].(bool); v {
		t.Fatal("chưa đo lần nào thì không được bật cờ đo hình")
	}
	d.VidAt = time.Now()
	if v := s.devView(d)["vid"].(bool); !v {
		t.Fatal("đo rồi thì phải bật cờ để Console nói đúng")
	}
}

// Đo hỏng thì phải NHỚ LÝ DO, Console mới nói được vì sao chứ không bắt người dùng đoán.
func TestNhoLyDoKhongRaHinh(t *testing.T) {
	ip, port := camChoi(t) // camera luôn từ chối tài khoản
	out := checkDev(context.Background(), NetDev{ID: "c1", IP: ip, Port: port, Kind: "camera", User: "admin", Pass: "sai"}, true)
	if out.VidLoi == "" {
		t.Fatal("phải nhớ lý do của lượt đo")
	}
	if !strings.Contains(out.VidLoi, "tài khoản") {
		t.Fatalf("lý do phải nói rõ chuyện tài khoản: %q", out.VidLoi)
	}
	s := &Server{st: State{}}
	s.st.Settings.CamUser = "admin"
	if got := s.devView(&out)["vidLoi"]; got != out.VidLoi {
		t.Fatalf("phải trả lý do cho Console, nhận %v", got)
	}
}

// Lượt đo bị cắt ngang vì kiểm hàng loạt hết giờ: coi như CHƯA ĐO, giữ nguyên kết quả cũ.
// Không có chỗ này thì mỗi lần khởi động lại server, mấy trăm camera đang chạy ngon đều bị
// gắn nhãn "sống nhưng không ra hình".
func TestDoBiCatNganThiGiuKetQuaCu(t *testing.T) {
	ip, port := camChoi(t)
	ctx, thoi := context.WithCancel(context.Background())
	thoi() // hết giờ ngay từ đầu
	cu := NetDev{ID: "c1", IP: ip, Port: port, Kind: "camera", User: "admin", Pass: "x",
		Video: true, Kbps: 1234, VidAt: time.Now().Add(-time.Minute), VidMiss: 0}
	out := checkDev(ctx, cu, true)
	if !out.Video || out.Kbps != 1234 {
		t.Fatalf("phải giữ nguyên kết quả đo cũ, đang là video=%v kbps=%d", out.Video, out.Kbps)
	}
	if out.VidMiss != 0 {
		t.Fatalf("không được tính là một lượt mất hình, VidMiss=%d", out.VidMiss)
	}
}
