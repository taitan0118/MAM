package main

import (
	"os"
	"strings"
	"testing"
)

// Đại ca: "gửi được nhiều ảnh hoặc nhiều file cùng lúc, cho xem trước rồi khi nhấn gửi thì mới
// gửi, có dấu x ở hình hoặc file để xóa nó đi". Khay chờ gửi phải còn đủ ba thứ đó trong Console.
func TestConsoleCoKhayFileChoGui(t *testing.T) {
	b, err := os.ReadFile("web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)
	for _, can := range []string{
		"multiple: true",   // chọn được nhiều file một lượt
		"class: 'tep-khay'", // khay xem trước
		"chatTep.splice(i, 1)", // dấu × bỏ một file khỏi khay
		"if (chatTep.length) return guiHet();", // bấm Gửi mới gửi thật
	} {
		if !strings.Contains(js, can) {
			t.Fatalf("app.js thiếu: %s", can)
		}
	}
	css, err := os.ReadFile("web/style.css")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(css), ".tep-x{") {
		t.Fatal("style.css thiếu nút × của khay file")
	}
}
