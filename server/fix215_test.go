package main

import (
	"os"
	"strings"
	"testing"
)

// Đại ca muốn xem ngay trong tab "Thời gian hoạt động": máy đã bật bao nhiêu ngày/giờ/phút/giây
// kể từ lúc mở máy, và tổng giờ bật hôm nay cũng phải chạy kèm phút với giây.
func TestTabThoiGianHoatDongCoDongHoChay(t *testing.T) {
	b, err := os.ReadFile("web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)
	for _, can := range []string{
		"function upDHMS(",                         // ngày / giờ / phút / giây
		"t('{0} giây', g)",                         // giây luôn hiện, để thấy nó chạy
		"function upChay(",                         // một chỗ duy nhất lo chuyện đếm tiếp
		"if (!o.isConnected) return;",              // nhịp tự chết khi trang vẽ lại, không chồng timer
		"Date.parse(m.lastSeen) - m.uptime * 1000", // mốc mở máy
		"upDongHo(may, r.nowMs)",                   // thẻ đồng hồ nằm trong tab
		"last && last[2] ? upChay(st.tot, r.nowMs)", // tổng giờ bật hôm nay cũng chạy
	} {
		if !strings.Contains(js, can) {
			t.Fatalf("app.js thiếu: %s", can)
		}
	}
	// không được quay lại setInterval cho mấy ô chạy từng giây trong tab này
	if strings.Contains(js, "setInterval(nhip") {
		t.Fatal("đừng dùng setInterval cho đồng hồ: trang tự vẽ lại mỗi 4 giây là chồng nhịp")
	}
}

// Nhịp đếm phải được HẸN GIỜ, đừng gọi thẳng: lúc tạo xong ô chưa nằm trong trang nên
// isConnected còn false, gọi thẳng là nhịp thoát ngay lượt đầu và con số đứng im (bản 1.1.121 bị vậy).
func TestDongHoThucSuChayChuKhongDungIm(t *testing.T) {
	b, err := os.ReadFile("web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)
	if strings.Contains(js, "\n  nhip();") {
		t.Fatal("gọi thẳng nhip() lúc ô chưa gắn vào trang: nhịp chết ngay, số không chạy")
	}
	if n := strings.Count(js, "setTimeout(nhip, 1000);"); n != 2 {
		t.Fatalf("cần đúng 2 chỗ hẹn nhịp (lượt đầu + lượt tiếp), đang có %d", n)
	}
}
