package main

import (
	"encoding/binary"
	"image"
	"testing"
)

func TestChangedRects(t *testing.T) {
	cur := image.NewRGBA(image.Rect(0, 0, 200, 150))
	prev := append([]byte(nil), cur.Pix...)
	if rs, _ := changedRects(cur, prev); rs != nil {
		t.Fatal("ảnh giống nhau phải không có vùng đổi")
	}
	cur.Pix[(70*cur.Stride)+70*4] = 9 // một điểm ở ô (1,1)
	rs, frac := changedRects(cur, prev)
	if len(rs) != 1 || rs[0] != image.Rect(64, 64, 128, 128) || frac <= 0 || frac > 0.2 {
		t.Fatalf("rs=%v frac=%v", rs, frac)
	}
	for y := 0; y < 128; y++ { // hai hàng ô liền nhau cùng khoảng cột phải ghép thành một vùng
		cur.Pix[y*cur.Stride+130*4] = 5
	}
	rs, _ = changedRects(cur, prev)
	if len(rs) != 2 {
		t.Fatalf("rs=%v", rs)
	}
}

func TestBuildPacket(t *testing.T) {
	b := buildPacket(1920, 1080, []screenRect{{64, 128, 32, 16, []byte("abc")}})
	if int(binary.LittleEndian.Uint32(b)) != len(b)-4 {
		t.Fatal("độ dài gói sai")
	}
	if binary.LittleEndian.Uint16(b[4:]) != 1920 || binary.LittleEndian.Uint16(b[8:]) != 1 || binary.LittleEndian.Uint16(b[10:]) != 64 || string(b[len(b)-3:]) != "abc" {
		t.Fatal("nội dung gói sai")
	}
}
