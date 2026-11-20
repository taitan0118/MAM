package main

// Luật: đã có cảnh báo cho một lỗi thì không báo lại lỗi đó, trừ khi thiết bị/máy thật sự đổi
// trạng thái (lỗi hết rồi mới hỏng lại). Đo trên dữ liệu thật trước khi sửa: một máy 41 dòng cùng
// một tiến trình, một camera 26 dòng cùng một lần rớt lên rớt xuống.

import (
	"strings"
	"testing"
	"time"
)

func soMo(s *Server, machine, kind string) int {
	n := 0
	for _, a := range s.st.Alerts {
		if a.Machine == machine && a.Kind == kind && a.Status == "open" {
			n++
		}
	}
	return n
}

func soDong(s *Server, machine, kind string) int {
	n := 0
	for _, a := range s.st.Alerts {
		if a.Machine == machine && a.Kind == kind {
			n++
		}
	}
	return n
}

// Đóng tay một cảnh báo mà lỗi vẫn còn nguyên (máy vẫn thiếu bản vá, ổ vẫn đầy): không được mở lại.
func TestDongTayThiKhongBaoLai(t *testing.T) {
	s, _ := newSrv(t)
	m := &Machine{ID: "m1", Name: "PC-01"}
	s.st.Machines = append(s.st.Machines, m)

	s.cond(m, "disk", "warn", true, "Ổ C: chỉ còn 5% trống")
	if soDong(s, "m1", "disk") != 1 {
		t.Fatal("lần đầu phải mở một cảnh báo")
	}
	// đóng tay như khi đại ca bấm nút Đóng trên Console
	for _, a := range s.st.Alerts {
		a.Status, a.Closed, a.By, a.Giu = "closed", time.Now(), "admin", true
	}
	// lỗi vẫn y nguyên qua nhiều lượt kiểm
	for i := 0; i < 20; i++ {
		s.cond(m, "disk", "warn", true, "Ổ C: chỉ còn 5% trống")
	}
	if n := soDong(s, "m1", "disk"); n != 1 {
		t.Fatalf("đóng tay rồi mà lỗi y nguyên vẫn đẻ thêm %d dòng (phải đúng 1)", n)
	}

	// ổ được dọn: lỗi hết → bỏ dấu giữ
	s.cond(m, "disk", "warn", false, "")
	// ổ lại đầy: đây là chuyện MỚI, phải báo
	s.cond(m, "disk", "warn", true, "Ổ C: chỉ còn 4% trống")
	if n := soDong(s, "m1", "disk"); n != 2 {
		t.Fatalf("lỗi hết rồi hỏng lại thì phải báo: có %d dòng (cần 2)", n)
	}
	if soMo(s, "m1", "disk") != 1 {
		t.Fatal("chỉ được một cảnh báo đang mở")
	}
}

// Thiết bị chập chờn: rớt → có lại → rớt lại. Trước đây mỗi vòng đẻ một dòng mới (đo thật: 26 dòng
// cho Hành Chính 2#). Giờ phải ghi vào chính dòng đang mở.
func TestRotLaiGhiVaoDongCu(t *testing.T) {
	f := setup(t)
	d := &NetDev{ID: "dcc", Name: "Hành Chính 2#", IP: "192.168.77.70", Kind: "camera"}
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	f.s.st.Devices = append(f.s.st.Devices, d)
	id := devAlertID(d.ID)

	nhay := func() { // một vòng rớt → có lại
		d.Up, d.Miss = false, devAlertAfter
		f.s.devAlerts(d)
		d.Up, d.Miss = true, 0
		f.s.devAlerts(d)
	}
	for i := 0; i < 10; i++ {
		nhay()
	}
	if n := soDong(f.s, id, "devdown"); n != 1 {
		t.Fatalf("chập chờn 10 vòng đẻ ra %d dòng cảnh báo, phải đúng 1", n)
	}
	a := f.s.st.Alerts[0]
	if a.Status != "open" {
		t.Fatal("dòng đó phải còn mở, chờ người bấm Đã xử lý")
	}
	if a.Lan != 9 {
		t.Fatalf("phải đếm được 9 lần rớt lại, đang là %d", a.Lan)
	}
	// lần cuối là "có lại" nên đuôi phải là đã có lại, không được dính cả hai đuôi
	if !strings.Contains(a.Msg, daCoLai) || strings.Contains(a.Msg, roiLai) {
		t.Fatalf("đuôi nội dung sai: %q", a.Msg)
	}
	// đang rớt thì đuôi phải là rớt lại kèm số lần
	d.Up, d.Miss = false, devAlertAfter
	f.s.devAlerts(d)
	if !strings.Contains(a.Msg, roiLai) || !strings.Contains(a.Msg, "lần thứ 11") {
		t.Fatalf("đang rớt phải ghi rớt lại lần thứ 11: %q", a.Msg)
	}
	if n := soDong(f.s, id, "devdown"); n != 1 {
		t.Fatalf("vẫn phải đúng 1 dòng, đang có %d", n)
	}
}

// Bấm "Đã xử lý" mà thiết bị vẫn chưa sống lại: không được báo lại cùng một lỗi.
func TestDaXuLyRoiVanRotThiKhongBaoLai(t *testing.T) {
	f := setup(t)
	d := &NetDev{ID: "dxl", Name: "Switch kho", IP: "192.168.77.71", Kind: "switch"}
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	f.s.st.Devices = append(f.s.st.Devices, d)
	id := devAlertID(d.ID)

	d.Up, d.Miss = false, devAlertAfter
	f.s.devAlerts(d)
	if f.s.devAck(d, "admin") != 1 {
		t.Fatal("Đã xử lý phải đóng đúng một cảnh báo")
	}
	for i := 0; i < 30; i++ { // thiết bị vẫn rớt suốt 30 lượt kiểm
		f.s.devAlerts(d)
	}
	if n := soDong(f.s, id, "devdown"); n != 1 {
		t.Fatalf("đã xử lý rồi mà vẫn rớt y nguyên lại đẻ %d dòng (phải 1)", n)
	}

	// thiết bị sống lại rồi rớt lần nữa: đây là chuyện mới, phải báo
	d.Up, d.Miss = true, 0
	f.s.devAlerts(d)
	d.Up, d.Miss = false, devAlertAfter
	f.s.devAlerts(d)
	if n := soDong(f.s, id, "devdown"); n != 2 {
		t.Fatalf("sống lại rồi rớt lần nữa phải báo: có %d dòng (cần 2)", n)
	}
}

// Virus/nghi ngờ đã đóng tay: Defender quét lại ra mã mới cho cùng file thì thôi, kể cả quá 24 giờ.
// Đo thật: một tiến trình sinh 41 dòng cảnh báo.
func TestNghiNgoDongTayThiThoiHan(t *testing.T) {
	s, _ := newSrv(t)
	m := &Machine{ID: "m9", Name: "VN10220"}
	s.st.Machines = append(s.st.Machines, m)
	moi := func(id string) []ThreatIn {
		return []ThreatIn{{ID: id, Kind: "suspect", Name: "Tiến trình đáng ngờ: x.exe", Detail: `C:\Temp\x.exe`}}
	}
	s.threatsIn(m, moi("t1"))
	if len(s.st.Alerts) != 1 {
		t.Fatal("lần đầu phải báo")
	}
	a := s.st.Alerts[0]
	a.Status, a.Closed, a.By, a.Giu = "closed", time.Now(), "admin", true
	// đã quá 24 giờ, Defender vẫn quét ra cùng file với mã mới mỗi lần
	a.Opened, a.Closed = time.Now().Add(-80*time.Hour), time.Now().Add(-70*time.Hour)
	for i := 0; i < 15; i++ {
		s.threatsIn(m, moi("t"+itoa(i+2)))
	}
	if len(s.st.Alerts) != 1 {
		t.Fatalf("đóng tay rồi mà cùng một tiến trình vẫn đẻ ra %d dòng (phải 1)", len(s.st.Alerts))
	}
	// file khác thì vẫn phải báo bình thường
	s.threatsIn(m, []ThreatIn{{ID: "k1", Kind: "suspect", Name: "Tiến trình đáng ngờ: y.exe", Detail: `C:\Temp\y.exe`}})
	if len(s.st.Alerts) != 2 {
		t.Fatalf("dấu hiệu khác thì vẫn phải báo: có %d dòng", len(s.st.Alerts))
	}
}
