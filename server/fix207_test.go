package main

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// dauGhiGia: đầu ghi Hikvision giả, chỉ cho vào đúng một tài khoản (Basic) và khai 16 kênh.
func dauGhiGia(t *testing.T, user, pass string) (string, int) {
	dung := "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != dung {
			w.Header().Set("WWW-Authenticate", `Basic realm="x"`)
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/ISAPI/System/deviceInfo":
			fmt.Fprint(w, `<?xml version="1.0"?><DeviceInfo><deviceName>Embedded Net DVR</deviceName><model>DS-7616NI-K2</model><firmwareVersion>V4.30.085</firmwareVersion></DeviceInfo>`)
		case "/ISAPI/ContentMgmt/InputProxy/channels":
			var b strings.Builder
			b.WriteString(`<?xml version="1.0"?><InputProxyChannelList>`)
			for i := 0; i < 16; i++ {
				b.WriteString(`<InputProxyChannel><id>` + itoa(i+1) + `</id></InputProxyChannel>`)
			}
			b.WriteString(`</InputProxyChannelList>`)
			fmt.Fprint(w, b.String())
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	p := 0
	fmt.Sscan(port, &p)
	return host, p
}

// Khai đầu ghi: sai mật khẩu thì KHÔNG ghi gì, đúng mới lưu kèm tên và số kênh đọc được.
func TestKhaiDauGhiDungMoiLuu(t *testing.T) {
	f := setup(t)
	ip, port := dauGhiGia(t, "admin", "dung123")

	code, _, _ := f.super.do("POST", "/api/nvr", map[string]interface{}{"ip": ip, "port": port, "user": "admin", "pass": "sai"}, nil)
	expect(t, code, 400, "sai mật khẩu đầu ghi")
	f.s.mu.RLock()
	if f.s.st.Settings.NvrIP != "" {
		t.Fatal("sai mật khẩu thì không được lưu gì cả")
	}
	f.s.mu.RUnlock()

	code, m, _ := f.super.do("POST", "/api/nvr", map[string]interface{}{"ip": ip, "port": port, "user": "admin", "pass": "dung123"}, nil)
	expect(t, code, 200, "đúng mật khẩu đầu ghi")
	if m["kenh"] != float64(16) {
		t.Fatalf("phải đọc ra 16 kênh, đang là %v", m["kenh"])
	}
	f.s.mu.RLock()
	st := f.s.st.Settings
	f.s.mu.RUnlock()
	if st.NvrIP != ip || st.NvrUser != "admin" || st.NvrPass != "dung123" || st.NvrPort != port {
		t.Fatalf("phải lưu đúng đầu ghi, đang là %q:%d %q", st.NvrIP, st.NvrPort, st.NvrUser)
	}
	if !strings.Contains(st.NvrTen, "DS-7616NI-K2") || st.NvrKenh != 16 || st.NvrAt.IsZero() {
		t.Fatalf("phải nhớ tên/số kênh/lúc đăng nhập, đang là %q %d %v", st.NvrTen, st.NvrKenh, st.NvrAt)
	}

	// GET /api/settings không được trả mật khẩu đầu ghi về Console
	code, m, _ = f.super.do("GET", "/api/settings", nil, nil)
	expect(t, code, 200, "đọc cài đặt")
	if m["nvrPass"] != "" {
		t.Fatalf("không được trả mật khẩu đầu ghi về client, đang là %v", m["nvrPass"])
	}
}

// Lưu ngưỡng cảnh báo (PUT cả cục Settings) KHÔNG được xóa mất đầu ghi đã khai.
func TestLuuCaiDatKhongXoaDauGhi(t *testing.T) {
	f := setup(t)
	f.s.mu.Lock()
	f.s.st.Settings.NvrIP, f.s.st.Settings.NvrPort = "172.20.1.10", 80
	f.s.st.Settings.NvrUser, f.s.st.Settings.NvrPass = "admin", "bimat"
	f.s.st.Settings.NvrTen, f.s.st.Settings.NvrKenh = "Net DVR DS-7616", 16
	f.s.mu.Unlock()

	code, _, _ := f.super.do("PUT", "/api/settings", map[string]interface{}{
		"temp": 85, "cpu": 90, "disk": 10, "offline": 5, "offlineDays": 0}, nil)
	expect(t, code, 200, "lưu ngưỡng cảnh báo")

	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	st := f.s.st.Settings
	if st.NvrIP != "172.20.1.10" || st.NvrPass != "bimat" || st.NvrUser != "admin" || st.NvrPort != 80 {
		t.Fatalf("lưu cài đặt khác không được đụng tới đầu ghi, đang là %q:%d %q/%q", st.NvrIP, st.NvrPort, st.NvrUser, st.NvrPass)
	}
	if st.NvrTen != "Net DVR DS-7616" || st.NvrKenh != 16 {
		t.Fatalf("phải giữ cả tên và số kênh, đang là %q %d", st.NvrTen, st.NvrKenh)
	}
}

// Xóa đầu ghi thì xóa sạch cả mật khẩu.
func TestXoaDauGhi(t *testing.T) {
	f := setup(t)
	f.s.mu.Lock()
	f.s.st.Settings.NvrIP, f.s.st.Settings.NvrUser, f.s.st.Settings.NvrPass = "172.20.1.10", "admin", "bimat"
	f.s.st.Settings.NvrKenh = 16
	f.s.mu.Unlock()

	code, _, _ := f.super.do("DELETE", "/api/nvr", nil, nil)
	expect(t, code, 200, "xóa đầu ghi")
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	st := f.s.st.Settings
	if st.NvrIP != "" || st.NvrUser != "" || st.NvrPass != "" || st.NvrKenh != 0 {
		t.Fatalf("phải xóa sạch, đang là %q %q %q %d", st.NvrIP, st.NvrUser, st.NvrPass, st.NvrKenh)
	}
}

// Chỉ super admin được khai đầu ghi.
func TestKhaiDauGhiChanTech(t *testing.T) {
	f := setup(t)
	code, _, _ := f.tech.do("POST", "/api/nvr", map[string]interface{}{"ip": "172.20.1.10", "user": "admin", "pass": "x"}, nil)
	expect(t, code, 403, "tech không được khai đầu ghi")
}
