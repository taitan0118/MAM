package main

import (
	"strings"
	"testing"
	"time"
)

func canhBaoMo(id, kind string) *Alert {
	return &Alert{ID: id, Machine: devAlertID("d" + id), Name: "Camera " + id, Kind: kind,
		Sev: "warn", Msg: "x", Status: "open", Opened: time.Now().Add(-48 * time.Hour)}
}

// Đính chính cảnh báo "không ra hình" của bản cũ: đóng đúng loại đó, ghi rõ lý do, và KHÔNG
// đụng tới cảnh báo mất kết nối — sự cố thật phải để người xác nhận.
func TestDonCanhBaoHinhGia(t *testing.T) {
	s := &Server{st: State{Alerts: []*Alert{
		canhBaoMo("1", "devvideo"),
		canhBaoMo("2", "devvideo"),
		canhBaoMo("3", "devdown"),
		canhBaoMo("4", "devmac"),
	}}}
	s.st.Alerts[1].Status = "closed" // đã đóng sẵn thì không đếm lại
	if n := s.donCanhBaoHinhGia(); n != 1 {
		t.Fatalf("phải đính chính 1 cảnh báo, làm %d", n)
	}
	for _, a := range s.st.Alerts {
		switch a.Kind {
		case "devvideo":
			if a.Status != "closed" {
				t.Fatal("cảnh báo không ra hình phải được đóng")
			}
		default:
			if a.Status != "open" {
				t.Fatalf("KHÔNG được đụng cảnh báo %s", a.Kind)
			}
		}
	}
	a := s.st.Alerts[0]
	if !strings.Contains(a.By, "hệ thống") || !strings.Contains(a.By, "1.1.102") {
		t.Fatalf("phải ghi rõ ai đóng và vì sao: %q", a.By)
	}
	if a.Giu {
		t.Fatal("không được đặt cờ giữ: camera hỏng thật thì phải báo lại được")
	}
	if a.Closed.IsZero() {
		t.Fatal("phải ghi giờ đóng")
	}
}

// Chỉ chạy đúng một lần: khởi động lại server không được đóng oan cảnh báo thật về sau.
func TestDonCanhBaoHinhGiaChiMotLan(t *testing.T) {
	s := &Server{st: State{Alerts: []*Alert{canhBaoMo("1", "devvideo")}}}
	if n := s.donCanhBaoHinhGia(); n != 1 {
		t.Fatalf("lượt đầu phải dọn 1, làm %d", n)
	}
	s.st.Alerts = append(s.st.Alerts, canhBaoMo("9", "devvideo")) // cảnh báo mới, thật
	if n := s.donCanhBaoHinhGia(); n != 0 {
		t.Fatalf("lượt sau không được đụng gì, làm %d", n)
	}
	if s.st.Alerts[1].Status != "open" {
		t.Fatal("cảnh báo mới phải còn mở")
	}
}
