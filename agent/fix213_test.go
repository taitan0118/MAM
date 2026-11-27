package main

import (
	"os"
	"strings"
	"testing"
)

// Cửa sổ MAM cũng phải gửi được nhiều file một lượt, xem trước trong khay và bỏ được từng cái.
// ui.html nhúng thẳng vào agent nên kiểm bằng cách đọc tệp.
func TestCuaSoMAMCoKhayFileChoGui(t *testing.T) {
	b, err := os.ReadFile("ui.html")
	if err != nil {
		t.Fatal(err)
	}
	h := string(b)
	for _, can := range []string{
		`id="tepIn" multiple`, // chọn nhiều file
		`<div id="khay">`,     // khay xem trước
		"function guiHet()",   // bấm Gửi mới gửi, gửi lần lượt từng file
		"choTep.splice(i,1)",  // dấu × bỏ một file
	} {
		if !strings.Contains(h, can) {
			t.Fatalf("ui.html thiếu: %s", can)
		}
	}
	// ảnh trong bong bóng phải vừa cửa sổ hẹp, đừng để tấm ảnh dọc chiếm hết màn hình
	if !strings.Contains(h, "max-width:230px;max-height:240px") {
		t.Fatal("ui.html: thiếu giới hạn kích thước ảnh trong bong bóng")
	}
}
