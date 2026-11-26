package main

import (
	"testing"
	"time"
)

// Một sự cố mạng đẻ ra vài trăm cảnh báo; phải đóng được cả nắm một lượt.
func TestDongNhieuCanhBao(t *testing.T) {
	f := setup(t)
	f.s.mu.Lock()
	for i := 0; i < 5; i++ {
		f.s.st.Alerts = append(f.s.st.Alerts, &Alert{
			ID: "a" + itoa(i), Machine: devAlertID("d" + itoa(i)), Name: "Camera " + itoa(i),
			Kind: "dev", Sev: "crit", Msg: "mất kết nối", Status: "open", Opened: time.Now(),
		})
	}
	f.s.mu.Unlock()

	code, _, _ := f.super.do("POST", "/api/alerts/nhieu", map[string]interface{}{"ids": []string{}}, nil)
	expect(t, code, 400, "không chọn gì thì phải báo lỗi")

	code, m, _ := f.super.do("POST", "/api/alerts/nhieu", map[string]interface{}{"ids": []string{"a0", "a1", "a2", "khong-co"}}, nil)
	expect(t, code, 200, "đóng nhiều")
	if n, _ := m["n"].(float64); n != 3 {
		t.Fatalf("phải đóng đúng 3 cảnh báo, báo %v", m["n"])
	}
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	for _, a := range f.s.st.Alerts {
		mong := "open"
		if a.ID == "a0" || a.ID == "a1" || a.ID == "a2" {
			mong = "closed"
		}
		if a.Status != mong {
			t.Fatalf("%s phải %s, đang %s", a.ID, mong, a.Status)
		}
		if mong == "closed" && (a.By != "admin" || !a.Giu) {
			t.Fatalf("%s phải ghi người đóng và giữ không mở lại", a.ID)
		}
	}
}

// Tài khoản Chỉ xem không được đóng hàng loạt.
func TestDongNhieuChanChiXem(t *testing.T) {
	f := setup(t)
	f.s.mu.Lock()
	f.s.st.Alerts = append(f.s.st.Alerts, &Alert{ID: "z1", Machine: devAlertID("d1"), Name: "Camera", Kind: "dev", Sev: "crit", Msg: "x", Status: "open", Opened: time.Now()})
	f.s.mu.Unlock()
	code, _, _ := f.view.do("POST", "/api/alerts/nhieu", map[string]interface{}{"ids": []string{"z1"}}, nil)
	expect(t, code, 403, "chỉ xem không được đóng")
}
