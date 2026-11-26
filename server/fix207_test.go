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
	ghis := f.s.st.Settings.Nvrs
	f.s.mu.RUnlock()
	if len(ghis) != 1 {
		t.Fatalf("phải lưu đúng 1 đầu ghi, đang có %d", len(ghis))
	}
	g := ghis[0]
	if g.IP != ip || g.User != "admin" || g.Pass != "dung123" || g.Port != port {
		t.Fatalf("phải lưu đúng đầu ghi, đang là %q:%d %q", g.IP, g.Port, g.User)
	}
	if !strings.Contains(g.Ten, "DS-7616NI-K2") || g.Kenh != 16 || g.At.IsZero() {
		t.Fatalf("phải nhớ tên/số kênh/lúc đăng nhập, đang là %q %d %v", g.Ten, g.Kenh, g.At)
	}

	// khai lại đúng đầu ghi đó thì ghi đè, không đẻ thêm dòng
	code, _, _ = f.super.do("POST", "/api/nvr", map[string]interface{}{"ip": ip, "port": port, "user": "admin", "pass": "dung123"}, nil)
	expect(t, code, 200, "khai lại")
	f.s.mu.RLock()
	n := len(f.s.st.Settings.Nvrs)
	f.s.mu.RUnlock()
	if n != 1 {
		t.Fatalf("khai lại cùng một IP thì vẫn phải là 1 đầu ghi, đang có %d", n)
	}

	// GET /api/settings không được trả mật khẩu đầu ghi về Console, và phải giữ nguyên mật khẩu thật
	code, m, _ = f.super.do("GET", "/api/settings", nil, nil)
	expect(t, code, 200, "đọc cài đặt")
	ds, _ := m["nvrs"].([]interface{})
	if len(ds) != 1 {
		t.Fatalf("phải trả về 1 đầu ghi, đang là %v", m["nvrs"])
	}
	if p, _ := ds[0].(map[string]interface{})["pass"].(string); p != "" {
		t.Fatalf("không được trả mật khẩu đầu ghi về client, đang là %q", p)
	}
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	if f.s.st.Settings.Nvrs[0].Pass != "dung123" {
		t.Fatalf("xóa mật khẩu lúc trả về KHÔNG được đụng tới mật khẩu thật, đang là %q", f.s.st.Settings.Nvrs[0].Pass)
	}
}

// Lưu ngưỡng cảnh báo (PUT cả cục Settings) KHÔNG được xóa mất đầu ghi đã khai.
func TestLuuCaiDatKhongXoaDauGhi(t *testing.T) {
	f := setup(t)
	f.s.mu.Lock()
	f.s.st.Settings.Nvrs = []DauGhi{{IP: "172.20.1.10", Port: 80, User: "admin", Pass: "bimat", Ten: "Net DVR DS-7616", Kenh: 16}}
	f.s.mu.Unlock()

	code, _, _ := f.super.do("PUT", "/api/settings", map[string]interface{}{
		"temp": 85, "cpu": 90, "disk": 10, "offline": 5, "offlineDays": 0}, nil)
	expect(t, code, 200, "lưu ngưỡng cảnh báo")

	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	ghis := f.s.st.Settings.Nvrs
	if len(ghis) != 1 {
		t.Fatalf("lưu cài đặt khác không được làm mất đầu ghi, còn %d", len(ghis))
	}
	g := ghis[0]
	if g.IP != "172.20.1.10" || g.Pass != "bimat" || g.User != "admin" || g.Port != 80 {
		t.Fatalf("lưu cài đặt khác không được đụng tới đầu ghi, đang là %q:%d %q/%q", g.IP, g.Port, g.User, g.Pass)
	}
	if g.Ten != "Net DVR DS-7616" || g.Kenh != 16 {
		t.Fatalf("phải giữ cả tên và số kênh, đang là %q %d", g.Ten, g.Kenh)
	}
}

// Xóa một đầu ghi thì chỉ mất đúng cái đó, mấy cái kia còn nguyên.
func TestXoaDauGhi(t *testing.T) {
	f := setup(t)
	f.s.mu.Lock()
	f.s.st.Settings.Nvrs = []DauGhi{
		{IP: "172.20.1.10", User: "admin", Pass: "bimat", Kenh: 16},
		{IP: "172.20.1.11", User: "admin", Pass: "bimat2", Kenh: 32}}
	f.s.st.Devices = append(f.s.st.Devices,
		&NetDev{ID: "camA", IP: "10.0.0.1", Kind: "camera", NvrID: "172.20.1.10", NvrCh: 3},
		&NetDev{ID: "camB", IP: "10.0.0.2", Kind: "camera", NvrID: "172.20.1.11", NvrCh: 5})
	f.s.mu.Unlock()

	code, _, _ := f.super.do("DELETE", "/api/nvr?ip=172.20.1.99", nil, nil)
	expect(t, code, 404, "xóa đầu ghi không có thật")

	code, _, _ = f.super.do("DELETE", "/api/nvr?ip=172.20.1.10", nil, nil)
	expect(t, code, 200, "xóa đầu ghi")
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	ghis := f.s.st.Settings.Nvrs
	if len(ghis) != 1 || ghis[0].IP != "172.20.1.11" || ghis[0].Pass != "bimat2" {
		t.Fatalf("chỉ được xóa đúng cái đó, còn lại %v", ghis)
	}
	for _, d := range f.s.st.Devices {
		if d.ID == "camA" && (d.NvrID != "" || d.NvrCh != 0) {
			t.Fatalf("camera của đầu ghi vừa xóa phải được gỡ kênh, đang là %q/%d", d.NvrID, d.NvrCh)
		}
		if d.ID == "camB" && (d.NvrID != "172.20.1.11" || d.NvrCh != 5) {
			t.Fatalf("camera của đầu ghi khác phải còn nguyên, đang là %q/%d", d.NvrID, d.NvrCh)
		}
	}
}

// Đầu ghi khai từ bản chỉ cho một cái phải được chuyển vào danh sách, không mất.
func TestChuyenDauGhiCuSangDanhSach(t *testing.T) {
	f := setup(t)
	f.s.mu.Lock()
	st := &f.s.st.Settings
	st.NvrIP, st.NvrPort, st.NvrUser, st.NvrPass = "172.20.1.10", 80, "admin", "bimat"
	st.NvrTen, st.NvrKenh = "Net DVR", 16
	f.s.st.Devices = append(f.s.st.Devices, &NetDev{ID: "camA", IP: "10.0.0.1", Kind: "camera", NvrCh: 3})
	co := f.s.chuyenDauGhiCu()
	f.s.mu.Unlock()
	if !co {
		t.Fatal("phải báo là có chuyển")
	}
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	ghis := f.s.st.Settings.Nvrs
	if len(ghis) != 1 || ghis[0].IP != "172.20.1.10" || ghis[0].Pass != "bimat" || ghis[0].Kenh != 16 {
		t.Fatalf("phải chuyển nguyên đầu ghi cũ vào danh sách, đang là %v", ghis)
	}
	if f.s.st.Settings.NvrIP != "" {
		t.Fatal("chuyển xong phải xóa ô cũ, không thì lần sau chuyển lại lần nữa")
	}
	for _, d := range f.s.st.Devices {
		if d.ID == "camA" && d.NvrID != "172.20.1.10" {
			t.Fatalf("camera đã gắn kênh hồi đó phải thuộc về đầu ghi đó, đang là %q", d.NvrID)
		}
	}
	// chạy lại lần nữa thì không làm gì cả
	if f.s.chuyenDauGhiCu() {
		t.Fatal("chạy lại không được chuyển thêm lần nào")
	}
}

// Chỉ super admin được khai đầu ghi.
func TestKhaiDauGhiChanTech(t *testing.T) {
	f := setup(t)
	code, _, _ := f.tech.do("POST", "/api/nvr", map[string]interface{}{"ip": "172.20.1.10", "user": "admin", "pass": "x"}, nil)
	expect(t, code, 403, "tech không được khai đầu ghi")
}
