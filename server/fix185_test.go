package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Cắt bớt cảnh báo không bao giờ được bỏ cái đang mở: bỏ nhầm thì lượt kiểm sau dò không thấy nữa
// nên mở thêm một cái y hệt — đó chính là lỗi "cảnh báo cứ lặp lại".
func TestCatBotCanhBaoGiuCaiDangMo(t *testing.T) {
	f := setup(t)
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	f.s.st.Alerts = nil
	// 3 cái mở nằm ngay đầu danh sách (cũ nhất) — đúng chỗ kiểu cắt cũ sẽ xén mất
	for i := 0; i < 3; i++ {
		f.s.st.Alerts = append(f.s.st.Alerts, &Alert{ID: rid(5), Machine: "m-mo", Kind: "k" + itoa(i), Status: "open"})
	}
	for i := 0; i < 2500; i++ {
		f.s.st.Alerts = append(f.s.st.Alerts, &Alert{ID: rid(5), Machine: "m-dong", Kind: "cu", Status: "closed"})
	}
	f.s.catBotCanhBao()

	mo := 0
	for _, a := range f.s.st.Alerts {
		if a.Status == "open" {
			mo++
		}
	}
	if mo != 3 {
		t.Fatalf("cắt bớt làm mất cảnh báo đang mở: còn %d / 3", mo)
	}
	if n := len(f.s.st.Alerts); n != 1500 {
		t.Fatalf("phải rút về 1500 dòng (3 cái mở + 1497 cái đã đóng), đang có %d", n)
	}
	// dưới ngưỡng thì không đụng gì
	truoc := len(f.s.st.Alerts)
	f.s.catBotCanhBao()
	if len(f.s.st.Alerts) != truoc {
		t.Fatalf("dưới 2000 thì không được cắt nữa: %d -> %d", truoc, len(f.s.st.Alerts))
	}
}

// Lượt kiểm bị hủy giữa chừng (Console ngắt, quá hạn 30 giây) không được kết luận là thiết bị rớt:
// kết luận lúc đó là ghi một mốc rớt giả vào lịch sử.
func TestCheckDevBiHuyKhongBaoRot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cu := NetDev{ID: "d1", IP: "192.168.77.61", Kind: "camera", Up: true, Miss: 0, Checked: time.Now().Add(-time.Minute)}
	ra := checkDev(ctx, cu, false)
	if !ra.Up || ra.Miss != 0 {
		t.Fatalf("lượt kiểm bị hủy phải giữ nguyên trạng thái cũ, nhận được Up=%v Miss=%d", ra.Up, ra.Miss)
	}
	if !ra.Checked.Equal(cu.Checked) {
		t.Fatal("lượt kiểm bị hủy không được tính là đã kiểm")
	}
}

// IP bị thiết bị khác chiếm: cổng vẫn mở nên vẫn xanh, chỉ MAC là lộ ra — phải mở cảnh báo.
func TestCanhBaoMACDoi(t *testing.T) {
	f := setup(t)
	d := &NetDev{ID: "dmac", Name: "Camera cổng", IP: "192.168.77.62", Kind: "camera",
		Up: true, MAC: "AA:BB:CC:00:00:01", MACNow: "AA:BB:CC:00:00:01"}
	f.s.mu.Lock()
	f.s.st.Devices = append(f.s.st.Devices, d)
	f.s.devAlerts(d)
	f.s.mu.Unlock()

	mo := func() *Alert {
		f.s.mu.RLock()
		defer f.s.mu.RUnlock()
		for _, a := range f.s.st.Alerts {
			if a.Machine == devAlertID(d.ID) && a.Kind == "devmac" && a.Status == "open" {
				return a
			}
		}
		return nil
	}
	if mo() != nil {
		t.Fatal("MAC chưa đổi thì không được báo")
	}
	f.s.mu.Lock()
	d.MACNow = "AA:BB:CC:00:00:99"
	f.s.devAlerts(d)
	f.s.mu.Unlock()
	a := mo()
	if a == nil {
		t.Fatal("MAC đổi phải mở cảnh báo")
	}
	if !strings.Contains(a.Msg, "192.168.77.62") || a.Sev != "warn" {
		t.Fatalf("nội dung cảnh báo MAC sai: %+v", a)
	}

	// qua router thì ARP trả rỗng — không được vì thế mà báo IP bị chiếm
	f.s.mu.Lock()
	d2 := &NetDev{ID: "dmac2", Name: "Camera xa", IP: "192.168.77.63", Kind: "camera",
		Up: true, MAC: "AA:BB:CC:00:00:01", MACNow: ""}
	f.s.st.Devices = append(f.s.st.Devices, d2)
	f.s.devAlerts(d2)
	f.s.mu.Unlock()
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	for _, a := range f.s.st.Alerts {
		if a.Machine == devAlertID(d2.ID) && a.Kind == "devmac" {
			t.Fatal("không đọc được MAC (qua router) thì không được báo IP bị chiếm")
		}
	}
}

// Console chia hai tab nên server phải lọc loại TRƯỚC khi cắt 300 dòng, nếu không tab thiết bị mạng
// có thể trống trơn dù cảnh báo vẫn còn.
func TestAlertsLocTheoLoai(t *testing.T) {
	f := setup(t)
	d := &NetDev{ID: "dl", Name: "Camera lọc", IP: "192.168.77.64", Kind: "camera", Up: false, Miss: 9}
	f.s.mu.Lock()
	f.s.st.Devices = append(f.s.st.Devices, d)
	f.s.devAlerts(d)
	// 400 cảnh báo máy tính MỚI HƠN: kiểu cũ cắt 300 dòng là cảnh báo thiết bị bị đẩy ra ngoài
	for i := 0; i < 400; i++ {
		f.s.st.Alerts = append(f.s.st.Alerts, &Alert{ID: rid(5), Machine: "may-it", Name: "PC" + itoa(i),
			Kind: "cpu", Sev: "warn", Msg: "CPU cao", Opened: time.Now(), Status: "open"})
	}
	f.s.mu.Unlock()

	code, _, raw := f.super.do("GET", "/api/alerts?loai=mang&status=open", nil, nil)
	expect(t, code, 200, "đọc cảnh báo tab thiết bị mạng")
	if !strings.Contains(string(raw), "Camera lọc") {
		t.Fatalf("tab thiết bị mạng phải thấy cảnh báo thiết bị dù có 400 cảnh báo máy mới hơn: %s", clip(string(raw), 200))
	}
	if strings.Contains(string(raw), "cpu") {
		t.Fatal("tab thiết bị mạng không được lẫn cảnh báo máy tính")
	}
	code, _, raw = f.super.do("GET", "/api/alerts?loai=it&status=open", nil, nil)
	expect(t, code, 200, "đọc cảnh báo tab IT")
	if strings.Contains(string(raw), "Camera lọc") {
		t.Fatal("tab IT không được lẫn cảnh báo thiết bị mạng")
	}
}
