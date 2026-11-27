package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

// Đại ca: "không cần báo lỗi camera không nhận tài khoản, tao chỉ cần biết có hoạt động hay
// không, hoạt động mà bị treo". Camera từ chối tài khoản vẫn trả lời trên mạng nên nó ĐANG
// HOẠT ĐỘNG — không phải một tình trạng riêng, và tuyệt đối không được xếp vào đám treo.
func TestCameraKhongNhanTaiKhoanVanLaHoatDong(t *testing.T) {
	f := setup(t)
	f.s.mu.Lock()
	f.s.st.Devices = append(f.s.st.Devices, &NetDev{
		ID: "cam-saitk", Name: "Camera tường rào", IP: "172.20.8.203", Kind: "camera",
		Up: true, Checked: time.Now(), Ports: []int{554, 80, 8000}, SaiTK: true,
	})
	rows, ok := f.s.exportRows("devices", "", &Account{Role: "super"})
	f.s.mu.Unlock()
	if !ok {
		t.Fatal("exportRows trả về không ok")
	}
	var tt string
	for _, r := range rows[1:] {
		if r[1] == "172.20.8.203" {
			tt = r[6]
		}
	}
	if tt != "Hoạt động" {
		t.Fatalf("tình trạng = %q, muốn %q", tt, "Hoạt động")
	}
}

// Cùng một định nghĩa "treo" phải dùng cho CẢ nhãn, bộ lọc, cách xếp và con số KPI trong
// Console. Sửa mỗi cái nhãn mà quên con số thì thẻ đếm trên đầu trang vẫn báo sai.
func TestConsoleDungChungMotDinhNghiaTreo(t *testing.T) {
	b, err := os.ReadFile("web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)
	if !strings.Contains(js, "const devTreo = d => d.vid && !d.video && !d.saiTK && !d.khongDo;") {
		t.Fatal("thiếu devTreo: định nghĩa treo phải nằm một chỗ")
	}
	for _, can := range []string{
		"d.done && d.up && devTreo(d));",       // bộ lọc
		"!d.up ? 0 : devTreo(d) ? 1",           // xếp theo mức hỏng
		"d.done && d.up && devTreo(d)).length", // con số KPI
	} {
		if !strings.Contains(js, can) {
			t.Fatalf("app.js không dùng devTreo ở: %s", can)
		}
	}
	if strings.Contains(js, "'Camera không nhận tài khoản'") {
		t.Fatal("vẫn còn nhãn 'Camera không nhận tài khoản' trong Console")
	}
}
