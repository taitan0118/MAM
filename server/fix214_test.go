package main

import (
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// Nâng server xong mà trình duyệt đang mở vẫn chạy app.js cũ thì đại ca tưởng bản mới không
// sửa gì. Trang chủ phải gắn số phiên bản vào các tệp tĩnh để trình duyệt lấy bản mới.
func TestTrangChuGanSoPhienBanVaoTepTinh(t *testing.T) {
	f := setup(t)
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	f.s.routes().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("trang chủ trả %d", w.Code)
	}
	body := w.Body.String()
	for _, can := range []string{"/app.js?v=" + Version, "/style.css?v=" + Version, "/i18n.js?v=" + Version} {
		if !strings.Contains(body, can) {
			t.Fatalf("trang chủ thiếu %s", can)
		}
	}
}

// Máy bị chấm "Nghiêm trọng" thì thẻ sức khỏe phải nói luôn vì sao (thẻ chỉ vẽ CPU/RAM/nhiệt,
// máy đỏ vì ổ đĩa sắp đầy trông như không có lý do gì).
func TestTheSucKhoeNoiRoLyDo(t *testing.T) {
	b, err := os.ReadFile("web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)
	for _, can := range []string{
		"ổ đĩa chỉ còn trống {0}%",
		"nhiệt {0}°C, ngưỡng {1}°C",
		"class: 'tile-vi'",
	} {
		if !strings.Contains(js, can) {
			t.Fatalf("app.js thiếu: %s", can)
		}
	}
	// thả cảm xúc không được vẽ lại ồn ào: render(true) giữ nguyên chỗ đang đọc
	if !strings.Contains(js, "'/tin', body); render(true);") {
		t.Fatal("app.js: thả cảm xúc vẫn gọi render() thường, khung chat sẽ nhảy xuống đáy")
	}
}
