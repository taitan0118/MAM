package main

import (
	"context"
	"net"
	"testing"
)

// Lỗi đại ca thấy: Console báo "Camera không ra hình: 399" trong khi camera vẫn ra hình.
// Nguyên do: luồng hình chỉ đo 5 phút một lần, mà lượt nào không đo cũng xóa luôn kết quả đo cũ,
// nên suốt 4 phút kế mọi camera đều thành "sống nhưng không ra hình" — và mỗi lượt đo lại đẻ ra
// một cặp mốc "treo không ra hình" / "hoạt động" giả trong lịch sử.
func TestCheckDevGiuKetQuaDoHinh(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	_, cong, _ := net.SplitHostPort(ln.Addr().String())

	d := NetDev{IP: "127.0.0.1", Port: atoiHay(cong), Kind: "camera", User: "admin",
		Video: true, Kbps: 512} // lượt trước đo được hình
	out := checkDev(context.Background(), d, false)
	if !out.Up {
		t.Fatal("cổng đang mở mà báo mất kết nối")
	}
	if !out.Video || out.Kbps != 512 {
		t.Fatalf("lượt không đo hình phải giữ kết quả cũ, nhận video=%v kbps=%d", out.Video, out.Kbps)
	}
	if devTrang(out.Up, true, out.Video) != "up" {
		t.Fatal("phải vẫn là hoạt động, không được thành treo không ra hình")
	}
}

// Ngược lại: gọi không được thì phải bỏ kết quả đo cũ, không được bám vào "có hình" mãi.
func TestCheckDevMatKetNoiThiBoHinhCu(t *testing.T) {
	d := NetDev{IP: "192.0.2.1", Port: 9, Kind: "camera", User: "admin", Video: true, Kbps: 512}
	out := checkDev(context.Background(), d, false)
	if out.Up {
		t.Skip("máy này gọi được 192.0.2.1, bỏ qua")
	}
	if out.Video || out.Kbps != 0 {
		t.Fatalf("mất kết nối mà vẫn báo có hình: video=%v kbps=%d", out.Video, out.Kbps)
	}
}
