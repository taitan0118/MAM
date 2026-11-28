package main

import (
	"os"
	"strings"
	"testing"
)

// Đại ca muốn xem máy đã bật được bao nhiêu ngày/giờ/phút/giây kể từ lúc mở máy. Số liệu đã có
// sẵn (agent gửi uptime mỗi lượt báo), chỗ hiện là tab "Thời gian hoạt động" của từng máy.
func TestChiTietMayCoDongHoDaBatLienTuc(t *testing.T) {
	b, err := os.ReadFile("web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)
	for _, can := range []string{
		"function upDHMS(",            // ngày / giờ / phút / giây
		"t('{0} giây', g)",            // giây luôn hiện, để thấy nó chạy
		"function upDongHo(",          // thẻ đồng hồ
		"Date.parse(m.lastSeen) - m.uptime * 1000", // mốc mở máy
		"if (!dem.isConnected) return;",            // nhịp tự dừng khi trang vẽ lại, không chồng timer
		"upDongHo(may, r.nowMs)",                   // có gắn vào tab Thời gian hoạt động
	} {
		if !strings.Contains(js, can) {
			t.Fatalf("app.js thiếu: %s", can)
		}
	}
}
