package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeRTSP: camera giả nói đúng RTSP. auth=true thì đòi Digest trước; frames = số gói hình phát ra.
// Trả "127.0.0.1:<cổng>".
func fakeRTSP(t *testing.T, frames int, auth bool) (string, int) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		br := bufio.NewReader(c)
		const sdp = "v=0\r\nm=audio 0 RTP/AVP 97\r\na=control:track2\r\nm=video 0 RTP/AVP 96\r\na=control:track1\r\n"
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			f := strings.Fields(line)
			if len(f) < 2 {
				return
			}
			method, uri, seq, hasAuth := f[0], f[1], "", false
			for {
				h, err := br.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimSpace(h) == "" {
					break
				}
				low := strings.ToLower(h)
				if strings.HasPrefix(low, "cseq:") {
					seq = strings.TrimSpace(h[5:])
				}
				if strings.HasPrefix(low, "authorization:") {
					hasAuth = true
				}
			}
			if auth && !hasAuth {
				fmt.Fprintf(c, "RTSP/1.0 401 Unauthorized\r\nCSeq: %s\r\nWWW-Authenticate: Digest realm=\"MAM\", nonce=\"abc123\"\r\n\r\n", seq)
				continue
			}
			switch method {
			case "DESCRIBE":
				fmt.Fprintf(c, "RTSP/1.0 200 OK\r\nCSeq: %s\r\nContent-Type: application/sdp\r\nContent-Length: %d\r\n\r\n%s", seq, len(sdp), sdp)
			case "SETUP":
				if !strings.HasSuffix(uri, "/track1") { // phải chọn đúng luồng hình trong SDP, không phải tiếng
					fmt.Fprintf(c, "RTSP/1.0 455 Wrong Track\r\nCSeq: %s\r\n\r\n", seq)
					continue
				}
				fmt.Fprintf(c, "RTSP/1.0 200 OK\r\nCSeq: %s\r\nSession: 999888;timeout=60\r\nTransport: RTP/AVP/TCP;unicast;interleaved=0-1\r\n\r\n", seq)
			case "PLAY":
				fmt.Fprintf(c, "RTSP/1.0 200 OK\r\nCSeq: %s\r\nSession: 999888\r\n\r\n", seq)
				go func() { // phát gói hình trên kênh 0, xen gói RTCP kênh 1 (không được tính vào kbps)
					pkt := make([]byte, 1200)
					for i := 0; i < frames; i++ {
						ch := byte(0)
						if i%10 == 9 {
							ch = 1
						}
						if _, err := c.Write(append([]byte{'$', ch, byte(len(pkt) >> 8), byte(len(pkt))}, pkt...)); err != nil {
							return
						}
						time.Sleep(5 * time.Millisecond)
					}
				}()
			default: // TEARDOWN: đóng luôn, khỏi ghi cùng lúc với bên đang phát hình
				return
			}
		}
	}()
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	p := 0
	fmt.Sscan(port, &p)
	return host, p
}

func TestRtspKbps(t *testing.T) {
	old := rtspMeasure
	rtspMeasure = 300 * time.Millisecond
	t.Cleanup(func() { rtspMeasure = old })

	for _, tc := range []struct {
		name string
		auth bool
		user string
	}{{"không cần mật khẩu", false, ""}, {"camera đòi Digest", true, "admin"}} {
		ip, port := fakeRTSP(t, 200, tc.auth)
		kb, err := rtspKbps(context.Background(), ip, port, "/Streaming/Channels/102", tc.user, "bimat")
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if kb <= 0 {
			t.Fatalf("%s: kbps = %d, phải lớn hơn 0", tc.name, kb)
		}
	}
}

func TestRtspKhongCoHinh(t *testing.T) {
	old := rtspMeasure
	rtspMeasure = 200 * time.Millisecond
	t.Cleanup(func() { rtspMeasure = old })
	ip, port := fakeRTSP(t, 0, false) // bắt tay xong nhưng không phát gói nào
	if kb, err := rtspKbps(context.Background(), ip, port, "", "", ""); err == nil || kb != 0 {
		t.Fatalf("camera không ra hình phải báo lỗi, nhận kbps=%d err=%v", kb, err)
	}
}

func TestRtspCongDong(t *testing.T) {
	if _, err := rtspKbps(context.Background(), "127.0.0.1", 1, "", "", ""); err == nil {
		t.Fatal("cổng đóng phải báo lỗi")
	}
}

func TestVideoURI(t *testing.T) {
	base := "rtsp://1.2.3.4:554/live"
	for _, tc := range [][2]string{
		{"m=video 0 RTP/AVP 96\na=control:track1", base + "/track1"},
		{"m=video 0 RTP/AVP 96\na=control:rtsp://1.2.3.4:554/khac", "rtsp://1.2.3.4:554/khac"},
		{"m=audio 0 RTP/AVP 97\na=control:a1\nm=video 0 RTP/AVP 96\na=control:v1", base + "/v1"},
		{"m=video 0 RTP/AVP 96", base}, // không khai control
		{"m=audio 0 RTP/AVP 97", base}, // chỉ có tiếng
		{"", base},                     // SDP rỗng
		{"m=video 0 x\na=control:*", base},
	} {
		if got := videoURI(base, tc[0]); got != tc[1] {
			t.Fatalf("videoURI(%q) = %q, cần %q", tc[0], got, tc[1])
		}
	}
}

func TestCheckDevGoCong(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	_, ps, _ := net.SplitHostPort(ln.Addr().String())
	p := 0
	fmt.Sscan(ps, &p)
	d := checkDev(context.Background(), NetDev{ID: "x", IP: "127.0.0.1", Kind: "switch", Port: p}, false)
	if !d.Up || d.Way != "port" || len(d.Ports) != 1 || d.Ports[0] != p {
		t.Fatalf("phải thấy sống qua cổng %d, nhận up=%v way=%q ports=%v", p, d.Up, d.Way, d.Ports)
	}
	if d.Since.IsZero() || d.Checked.IsZero() {
		t.Fatal("phải ghi mốc thời gian")
	}
}

func TestDevPortList(t *testing.T) {
	if got := devPortList(NetDev{Kind: "attend"}); got[0] != 4370 {
		t.Fatalf("máy chấm công phải thử cổng riêng 4370 trước, nhận %v", got)
	}
	if got := devPortList(NetDev{Kind: "camera", Port: 8899}); len(got) != 1 || got[0] != 8899 {
		t.Fatalf("đã chỉ rõ cổng thì chỉ thử cổng đó, nhận %v", got)
	}
	if got := devPortList(NetDev{Kind: "lung tung"}); len(got) == 0 {
		t.Fatal("loại lạ vẫn phải có cổng mặc định")
	}
}

func TestPrivateIP(t *testing.T) {
	for _, ip := range []string{"192.168.1.50", "10.0.0.5", "172.16.3.4", "172.31.0.1"} {
		if !privateIP(net.ParseIP(ip)) {
			t.Fatalf("%s phải là IP nội bộ", ip)
		}
	}
	for _, ip := range []string{"8.8.8.8", "172.32.0.1", "1.1.1.1", "::1"} {
		if privateIP(net.ParseIP(ip)) {
			t.Fatalf("%s không phải IP nội bộ", ip)
		}
	}
}

func TestMacRe(t *testing.T) {
	win := "Interface: 192.168.1.9 --- 0xd\r\n  Internet Address      Physical Address      Type\r\n  192.168.1.50          00-1a-2b-3c-4d-5e     dynamic\r\n"
	if got := strings.ToUpper(strings.ReplaceAll(string(macRe.Find([]byte(win))), "-", ":")); got != "00:1A:2B:3C:4D:5E" {
		t.Fatalf("đọc MAC kiểu Windows sai: %q", got)
	}
	nix := "192.168.1.50 dev eth0 lladdr 00:1a:2b:3c:4d:5e REACHABLE\n"
	if got := strings.ToUpper(string(macRe.Find([]byte(nix)))); got != "00:1A:2B:3C:4D:5E" {
		t.Fatalf("đọc MAC kiểu Linux sai: %q", got)
	}
	if macRe.Find([]byte("No ARP Entries Found")) != nil {
		t.Fatal("không có bản ghi thì phải trả rỗng")
	}
}

func TestDevicesAPI(t *testing.T) {
	f := setup(t)
	cam := map[string]interface{}{"name": "Camera cổng trước", "ip": "192.168.77.50", "kind": "camera", "group": "Camera tầng 1", "port": 554, "path": "/Streaming/Channels/102", "user": "admin", "pass": "bimat"}
	code, m, _ := f.super.do("POST", "/api/devices", cam, nil)
	expect(t, code, 200, "thêm camera")
	id, _ := m["id"].(string)
	if id == "" {
		t.Fatal("phải trả id thiết bị")
	}
	if m["hasPass"] != true {
		t.Fatal("phải báo là đã có mật khẩu")
	}
	if _, leaked := m["pass"]; leaked {
		t.Fatal("không được trả mật khẩu camera ra Console")
	}

	code, _, raw := f.view.do("GET", "/api/devices", nil, nil)
	expect(t, code, 200, "chỉ xem đọc được danh sách")
	var list []map[string]interface{}
	if json.Unmarshal(raw, &list) != nil || len(list) != 1 {
		t.Fatalf("danh sách phải có 1 thiết bị: %s", raw)
	}
	if g, _ := list[0]["groups"].([]interface{}); len(g) != 1 || g[0] != "Camera tầng 1" {
		t.Fatalf("nhóm sai: %v", list[0]["groups"])
	}

	// chỉ Super admin được thêm/sửa/xóa
	code, _, _ = f.tech.do("POST", "/api/devices", cam, nil)
	expect(t, code, 403, "kỹ thuật viên không được thêm thiết bị")
	code, _, _ = f.tech.do("DELETE", "/api/devices?id="+id, nil, nil)
	expect(t, code, 403, "kỹ thuật viên không được xóa thiết bị")
	code, _, _ = f.view.do("POST", "/api/devices/check", nil, nil)
	expect(t, code, 403, "chỉ xem không được bấm kiểm lại")

	// dữ liệu sai
	for _, bad := range []map[string]interface{}{
		{"name": "", "ip": "192.168.77.9", "kind": "switch"},
		{"name": "X", "ip": "8.8.8.8", "kind": "switch"},
		{"name": "X", "ip": "khong-phai-ip", "kind": "switch"},
		{"name": "X", "ip": "192.168.77.9", "kind": "may-bay"},
		{"name": "X", "ip": "192.168.77.9", "kind": "switch", "port": 70000},
		{"name": "Trùng IP", "ip": "192.168.77.50", "kind": "switch"},
	} {
		if code, _, _ = f.super.do("POST", "/api/devices", bad, nil); code != 400 {
			t.Fatalf("dữ liệu sai %v phải bị từ chối, nhận %d", bad, code)
		}
	}

	// sửa: để trống mật khẩu thì giữ mật khẩu cũ
	code, _, _ = f.super.do("PUT", "/api/devices", map[string]interface{}{"id": id, "name": "Camera cổng sau", "ip": "192.168.77.51", "kind": "camera", "group": "Camera tầng 2", "pass": ""}, nil)
	expect(t, code, 200, "sửa thiết bị")
	f.s.mu.RLock()
	d := f.s.st.Devices[0]
	grp := ""
	if g := nhomCua(d); len(g) > 0 {
		grp = g[0]
	}
	keptPass, name, ip := d.Pass, d.Name, d.IP
	f.s.mu.RUnlock()
	if keptPass != "bimat" {
		t.Fatalf("để trống mật khẩu phải giữ mật khẩu cũ, nhận %q", keptPass)
	}
	if name != "Camera cổng sau" || ip != "192.168.77.51" || grp != "Camera tầng 2" {
		t.Fatalf("sửa không vào: %q %q %q", name, ip, grp)
	}

	code, _, _ = f.super.do("DELETE", "/api/devices?id="+id, nil, nil)
	expect(t, code, 200, "xóa thiết bị")
	code, _, raw = f.super.do("GET", "/api/devices", nil, nil)
	expect(t, code, 200, "danh sách sau khi xóa")
	if json.Unmarshal(raw, &list); len(list) != 0 {
		t.Fatalf("phải rỗng sau khi xóa: %s", raw)
	}
	code, _, _ = f.super.do("DELETE", "/api/devices?id=khongco", nil, nil)
	expect(t, code, 404, "xóa thiết bị không tồn tại")
}

// Thiết bị còn lưu lại sau khi server khởi động lại; trạng thái sống/chết thì không (kiểm lại sau 1 phút).
func TestDevicesLuuVaoDB(t *testing.T) {
	f := setup(t)
	code, _, _ := f.super.do("POST", "/api/devices", map[string]interface{}{"name": "Switch tầng 1", "ip": "192.168.77.2", "kind": "switch", "group": "Switch"}, nil)
	expect(t, code, 200, "thêm switch")
	f.s.mu.Lock()
	f.s.st.Devices[0].Up = true
	f.s.save()
	f.s.mu.Unlock()

	var st State
	if _, ok, err := loadState(f.s.db, &st); err != nil || !ok {
		t.Fatalf("đọc lại DB: ok=%v err=%v", ok, err)
	}
	if len(st.Devices) != 1 || st.Devices[0].Name != "Switch tầng 1" || len(st.Devices[0].Groups) != 1 || st.Devices[0].Groups[0] != "Switch" {
		t.Fatalf("thiết bị phải còn trong DB: %+v", st.Devices)
	}
	if st.Devices[0].Up {
		t.Fatal("trạng thái sống/chết không được ghi vào DB")
	}
}

func TestDeviceLoaiRouter(t *testing.T) {
	f := setup(t)
	code, m, _ := f.super.do("POST", "/api/devices", map[string]interface{}{"name": "Router phòng máy", "ip": "192.168.77.254", "kind": "router", "group": "Mạng"}, nil)
	expect(t, code, 200, "thêm router")
	if m["kind"] != "router" {
		t.Fatalf("loại phải là router, nhận %v", m["kind"])
	}
	if p := devPortList(NetDev{Kind: "router"}); len(p) == 0 || p[0] != 80 {
		t.Fatalf("router phải có cổng mặc định bắt đầu bằng 80, nhận %v", p)
	}
}

// Thiết bị rớt phải mở cảnh báo (sau 2 lượt trượt liên tiếp), sống lại thì tự đóng.
func TestCanhBaoThietBiRot(t *testing.T) {
	f := setup(t)
	d := &NetDev{ID: "dev1", Name: "Camera kho", IP: "192.168.77.60", Kind: "camera", User: "admin", Pass: "x"}
	f.s.mu.Lock()
	f.s.st.Devices = append(f.s.st.Devices, d)
	f.s.mu.Unlock()

	open := func(kind string) *Alert {
		f.s.mu.RLock()
		defer f.s.mu.RUnlock()
		for _, a := range f.s.st.Alerts {
			if a.Machine == devAlertID(d.ID) && a.Kind == kind && a.Status == "open" {
				return a
			}
		}
		return nil
	}
	set := func(fn func()) {
		f.s.mu.Lock()
		defer f.s.mu.Unlock()
		fn()
		f.s.devAlerts(d)
	}

	set(func() { d.Up, d.Miss = false, 1 })
	if open("devdown") != nil {
		t.Fatal("trượt một lượt chưa được báo, phải chờ lượt thứ hai")
	}
	set(func() { d.Up, d.Miss = false, 2 })
	a := open("devdown")
	if a == nil {
		t.Fatal("trượt 2 lượt liên tiếp phải mở cảnh báo")
	}
	if !strings.Contains(a.Msg, "192.168.77.60") || !strings.Contains(a.Msg, "Camera kho") || a.Sev != "crit" {
		t.Fatalf("nội dung cảnh báo thiếu tên hoặc IP: %+v", a)
	}

	// kỹ thuật viên chỉ có phạm vi nhóm "Tầng 2" vẫn phải thấy cảnh báo thiết bị mạng
	code, _, raw := f.tech.do("GET", "/api/alerts?status=open", nil, nil)
	expect(t, code, 200, "tech đọc cảnh báo")
	if !strings.Contains(string(raw), "Camera kho") {
		t.Fatalf("tài khoản có phạm vi nhóm vẫn phải thấy cảnh báo thiết bị mạng: %s", raw)
	}

	set(func() { d.Up, d.Miss = true, 0 })
	if open("devdown") != nil {
		t.Fatal("thiết bị sống lại phải tự đóng cảnh báo")
	}

	// camera còn sống nhưng không ra hình: cảnh báo riêng, mức nhẹ hơn
	set(func() { d.Up, d.Miss, d.Video, d.VidMiss = true, 0, false, 2 })
	if v := open("devvideo"); v == nil || v.Sev != "warn" {
		t.Fatalf("camera không ra hình phải mở cảnh báo mức warn, nhận %+v", v)
	}
	set(func() { d.Video, d.VidMiss = true, 0 })
	if open("devvideo") != nil {
		t.Fatal("camera ra hình lại phải đóng cảnh báo")
	}

	// xóa thiết bị thì đóng hết cảnh báo của nó
	set(func() { d.Up, d.Miss = false, 9 })
	if open("devdown") == nil {
		t.Fatal("phải đang mở cảnh báo trước khi xóa")
	}
	f.s.mu.Lock()
	f.s.devAlertsClose(d)
	f.s.mu.Unlock()
	if open("devdown") != nil {
		t.Fatal("xóa thiết bị phải đóng cảnh báo của nó")
	}
}

func TestGuessDevKind(t *testing.T) {
	for _, c := range []struct {
		ports []int
		rtsp  bool
		cuoi  bool
		muon  string
	}{
		{[]int{554, 80}, true, false, "camera"},         // trả lời RTSP thật
		{[]int{554, 80}, false, false, "camera"},        // cổng 554 thôi cũng đoán camera
		{[]int{4370, 80}, false, false, "attend"},       // cổng riêng máy chấm công
		{[]int{37777}, false, false, "camera"},          // đầu ghi Dahua
		{[]int{135, 445, 3389}, false, false, "pc"},     // máy Windows, không phải thiết bị mạng
		{[]int{80, 443, 23}, false, false, "switch"},    // có telnet + trang quản lý
		{[]int{80, 443}, false, true, "router"},         // địa chỉ cuối dải
		{[]int{80}, false, false, "other"},              // chỉ có web
		{[]int{9999}, false, false, "other"},            // cổng lạ
		{[]int{4370, 135, 445}, false, false, "attend"}, // cổng riêng thắng cổng Windows
	} {
		got, why := guessDevKind(c.ports, c.rtsp, c.cuoi)
		if got != c.muon {
			t.Fatalf("cổng %v (rtsp=%v cuối dải=%v) → %q, cần %q", c.ports, c.rtsp, c.cuoi, got, c.muon)
		}
		if why == "" {
			t.Fatalf("cổng %v: phải nói được vì sao đoán vậy", c.ports)
		}
	}
}

func TestVendorOf(t *testing.T) {
	if v := vendorOf("44:19:B6:11:22:33"); v != "Hikvision" {
		t.Fatalf("MAC Hikvision → %q", v)
	}
	if v := vendorOf("00:17:61:AA:BB:CC"); v != "ZKTeco" {
		t.Fatalf("MAC ZKTeco → %q", v)
	}
	if v := vendorOf("DE:AD:BE:EF:00:01"); v != "" {
		t.Fatalf("hãng lạ phải trả rỗng, nhận %q", v)
	}
	if v := vendorOf(""); v != "" {
		t.Fatalf("không có MAC phải trả rỗng, nhận %q", v)
	}
}

// Quét: chỉ Super admin, chỉ dải nội bộ, tối đa /24.
func TestDeviceScanChanDauVao(t *testing.T) {
	f := setup(t)
	code, _, _ := f.tech.do("POST", "/api/devices/scan", map[string]string{"cidr": "192.168.9.0/24"}, nil)
	expect(t, code, 403, "kỹ thuật viên không được quét")
	for _, bad := range []string{"8.8.8.0/24", "192.168.0.0/16", "khong-phai-dai", ""} {
		if code, _, _ = f.super.do("POST", "/api/devices/scan", map[string]string{"cidr": bad}, nil); code != 400 {
			t.Fatalf("dải %q phải bị từ chối, nhận %d", bad, code)
		}
	}
	code, m, _ := f.super.do("GET", "/api/devices/scan", nil, nil)
	expect(t, code, 200, "lấy gợi ý dải mạng")
	if _, ok := m["suggest"]; !ok {
		t.Fatal("phải trả gợi ý dải mạng")
	}
}

func TestParseCIDRs(t *testing.T) {
	ns, bad := parseCIDRs("172.20.20.0/24, 172.20.40.0/24\n192.168.0.0/24 10.1.2.0/24")
	if bad != "" || len(ns) != 4 {
		t.Fatalf("4 dải nội bộ phải nhận hết: %d dải, lỗi %q", len(ns), bad)
	}
	if ns, _ := parseCIDRs("172.20.20.0/24, 172.20.20.0/24"); len(ns) != 1 {
		t.Fatalf("dải trùng phải gộp lại, nhận %d", len(ns))
	}
	for _, bad := range []string{"", "8.8.8.0/24", "192.168.0.0/16", "lung tung", "172.20.20.0/24, 8.8.8.0/24"} {
		if ns, msg := parseCIDRs(bad); msg == "" || ns != nil {
			t.Fatalf("%q phải bị từ chối", bad)
		}
	}
	nhieu := make([]string, scanMaxNets+1)
	for i := range nhieu {
		nhieu[i] = "10.0." + itoa(i) + ".0/24"
	}
	if _, msg := parseCIDRs(strings.Join(nhieu, ",")); msg == "" {
		t.Fatalf("quá %d dải phải bị từ chối", scanMaxNets)
	}
}

// Quét chạy nền: bấm là trả lời ngay, rồi hỏi tiến độ; đang chạy thì không cho bấm lần nữa.
func TestDeviceScanChayNen(t *testing.T) {
	f := setup(t)
	code, _, _ := f.tech.do("POST", "/api/devices/scan", map[string]string{"cidr": "192.168.9.0/24"}, nil)
	expect(t, code, 403, "kỹ thuật viên không được quét")

	code, m, _ := f.super.do("POST", "/api/devices/scan", map[string]string{"cidr": "192.168.231.0/24"}, nil)
	expect(t, code, 200, "bắt đầu quét")
	if m["started"] != true {
		t.Fatalf("phải trả started=true: %v", m)
	}
	code, _, _ = f.super.do("POST", "/api/devices/scan", map[string]string{"cidr": "192.168.232.0/24"}, nil)
	expect(t, code, 409, "đang quét thì không cho bấm lần nữa")

	code, m, _ = f.super.do("GET", "/api/devices/scan", nil, nil)
	expect(t, code, 200, "hỏi tiến độ")
	if m["job"] == nil {
		t.Fatal("phải trả tình trạng lượt quét")
	}
	if _, ok := m["suggest"]; !ok {
		t.Fatal("phải trả gợi ý dải mạng")
	}
	// dải toàn địa chỉ chết nên quét xong trong vài giây
	for i := 0; i < 120; i++ {
		code, m, _ = f.super.do("GET", "/api/devices/scan", nil, nil)
		if j, _ := m["job"].(map[string]interface{}); j != nil && j["running"] == false {
			if j["scanned"].(float64) != 254 {
				t.Fatalf("phải dò đủ 254 địa chỉ, nhận %v", j["scanned"])
			}
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("quét một dải chết mà không xong trong 60 giây")
}

func TestTraiRaDaiLienTiep(t *testing.T) {
	ns, bad := parseCIDRs("172.20.5-8.0/24")
	if bad != "" || len(ns) != 4 {
		t.Fatalf("172.20.5-8.0/24 phải ra 4 dải, nhận %d (%s)", len(ns), bad)
	}
	if ns[0].String() != "172.20.5.0/24" || ns[3].String() != "172.20.8.0/24" {
		t.Fatalf("trải sai: %v … %v", ns[0], ns[3])
	}
	if ns, _ := parseCIDRs("172.20.5-5.0/24"); len(ns) != 1 {
		t.Fatalf("dải một phần tử phải ra 1, nhận %d", len(ns))
	}
	if ns, _ := parseCIDRs("172.20.5-200.0/24"); len(ns) != 196 {
		t.Fatalf("172.20.5-200.0/24 phải ra 196 dải, nhận %d", len(ns))
	}
	for _, bad := range []string{"172.20.200-5.0/24", "172.20.5-999.0/24", "172.20.a-b.0/24"} {
		if _, msg := parseCIDRs(bad); msg == "" {
			t.Fatalf("%q phải bị từ chối", bad)
		}
	}
	// cả một dải /16 (256 dải con) thì vẫn nhận, vì "quét hết mạng công ty" cần đúng chừng đó
	if ns, msg := parseCIDRs("10.0.0-255.0/24"); msg != "" || len(ns) != 256 {
		t.Fatalf("256 dải phải nhận hết: %d dải, lỗi %q", len(ns), msg)
	}
	// bốn lớp /16 là vừa đủ mức cho phép, lớp thứ năm thì quá
	nam := []string{"10.0.0-255.0/24", "10.1.0-255.0/24", "10.2.0-255.0/24", "10.3.0-255.0/24", "10.4.0-255.0/24"}
	if ns, msg := parseCIDRs(strings.Join(nam[:4], ",")); msg != "" || len(ns) != 1024 {
		t.Fatalf("4 lớp /16 phải nhận hết: %d dải, lỗi %q", len(ns), msg)
	}
	if _, msg := parseCIDRs(strings.Join(nam, ",")); msg == "" {
		t.Fatal("quá 1024 dải phải bị từ chối")
	}
}

func TestGuessDevKindLoaiMoi(t *testing.T) {
	for _, c := range []struct {
		ports []int
		muon  string
	}{
		{[]int{9100}, "printer"},      // máy in mạng
		{[]int{515, 80}, "printer"},   // máy in có cả trang web
		{[]int{9100, 445}, "printer"}, // máy in gắn máy tính: vẫn là máy in
		{[]int{6000, 80}, "led"},      // bảng LED
		{[]int{10001}, "led"},         // bộ điều khiển
		{[]int{34567}, "camera"},      // đầu ghi đời khác
		{[]int{8080, 23}, "switch"},   // trang quản lý ở cổng 8080
		{[]int{8080}, "other"},        // chỉ có web ở 8080
	} {
		if got, _ := guessDevKind(c.ports, false, false); got != c.muon {
			t.Fatalf("cổng %v → %q, cần %q", c.ports, got, c.muon)
		}
	}
	// thiết bị mạng thật vẫn phải thắng cổng máy tính/máy in
	if got, _ := guessDevKind([]int{554, 445, 9100}, true, false); got != "camera" {
		t.Fatalf("trả lời RTSP thì phải là camera, nhận %q", got)
	}
}

func TestDevKindsCoLed(t *testing.T) {
	if !devKinds["led"] {
		t.Fatal("phải khai được thiết bị loại bảng LED")
	}
	if p := devPortList(NetDev{Kind: "led"}); len(p) == 0 {
		t.Fatal("bảng LED phải có cổng mặc định để kiểm")
	}
}

// webKhai: thiết bị tự khai tên ở phần đầu Server:, không có thì lấy <title>.
func TestWebKhai(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Host[strings.LastIndexByte(r.Host, ':')+1:] {
		default:
			w.Header().Set("Server", "MoxaHttp/1.0")
			w.Write([]byte("<html><title>Input Password</title>"))
		}
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()
	_, ps, _ := net.SplitHostPort(strings.TrimPrefix(ts.URL, "http://"))
	port := 0
	fmt.Sscan(ps, &port)

	// cổng thật của máy chủ thử nghiệm không phải 80/8080/443 nên tạm mượn danh sách cổng
	old := webPorts
	webPorts = []int{port}
	t.Cleanup(func() { webPorts = old })

	if got := webKhai(context.Background(), "127.0.0.1", []int{port}); got != "MoxaHttp/1.0" {
		t.Fatalf("phải lấy được tên ở phần đầu Server:, nhận %q", got)
	}
	if got := webKhai(context.Background(), "127.0.0.1", []int{65000}); got != "" {
		t.Fatalf("cổng không mở phải trả rỗng, nhận %q", got)
	}
}

func TestTieuDeRac(t *testing.T) {
	for _, s := range []string{"403 Forbidden", "404 Not Found", "Not Found", "Forbidden", "Error", "Index of /", "  401 Unauthorized"} {
		if !tieuDeRac(s) {
			t.Fatalf("%q là câu báo lỗi, không phải tên thiết bị", s)
		}
	}
	for _, s := range []string{"Input Password", "DiskStation", "UPS Web/SNMP", "TL-SG1016DE", "Moxa NPort"} {
		if tieuDeRac(s) {
			t.Fatalf("%q là tên thiết bị, không được bỏ", s)
		}
	}
}

func TestTheoTenKhai(t *testing.T) {
	for _, c := range [][2]string{
		{"UniFi AP-AC-Pro", "wifi"},
		{"Ruckus Wireless", "wifi"},
		{"AirOS", "wifi"},
		{"Wireless Access Point", "wifi"},
		{"NPort Web Console", "other"},
		{"MoxaHttp/1.0", "other"},
		{"Quidway", ""}, // Huawei vừa làm switch vừa làm AP: không đoán bừa
		{"HuaWei12708", ""},
		{"", ""},
		{"GoAhead-Webs", ""},
	} {
		got, why := theoTenKhai(c[0])
		if got != c[1] {
			t.Fatalf("tên %q → loại %q, cần %q", c[0], got, c[1])
		}
		if got != "" && why == "" {
			t.Fatalf("tên %q: đổi loại thì phải nói được vì sao", c[0])
		}
	}
}

func TestGuessDevKindWifi(t *testing.T) {
	if got, _ := guessDevKind([]int{8291, 80}, false, false); got != "router" {
		t.Fatalf("cổng Winbox 8291 phải ra router, nhận %q", got)
	}
	if got, _ := guessDevKind([]int{8443, 443, 22}, false, false); got != "wifi" {
		t.Fatalf("cổng 8443 phải ra thiết bị WiFi, nhận %q", got)
	}
	if !devKinds["wifi"] {
		t.Fatal("phải khai được thiết bị loại WiFi")
	}
	if p := devPortList(NetDev{Kind: "wifi"}); len(p) == 0 {
		t.Fatal("thiết bị WiFi phải có cổng mặc định để kiểm")
	}
}

// Lớp mạng khác (172.24.x) vẫn là dải nội bộ hợp lệ, và quét được cả nhiều lớp một lượt.
func TestLopMangKhac(t *testing.T) {
	for _, c := range []string{"172.24.0.0/24", "172.16.5.0/24", "172.31.200.0/24", "10.50.3.0/24", "192.168.77.0/24"} {
		if ns, bad := parseCIDRs(c); bad != "" || len(ns) != 1 {
			t.Fatalf("%s phải nhận được: %q", c, bad)
		}
	}
	// hai lớp /16 trong cùng một lượt
	ns, bad := parseCIDRs("172.20.0-255.0/24, 172.24.0-255.0/24")
	if bad != "" || len(ns) != 512 {
		t.Fatalf("hai lớp /16 phải ra 512 dải, nhận %d (%s)", len(ns), bad)
	}
	// 172.32 không còn là dải nội bộ
	if _, bad := parseCIDRs("172.32.0.0/24"); bad == "" {
		t.Fatal("172.32.x không phải dải nội bộ, phải bị từ chối")
	}
}

// Lịch sử: ghi mốc khi đổi trạng thái, không ghi khi giữ nguyên.
func TestLichSuThietBi(t *testing.T) {
	f := setup(t)
	d := &NetDev{ID: "dv9", Name: "Camera kho", IP: "192.168.77.90", Kind: "camera"}
	f.s.mu.Lock()
	f.s.st.Devices = append(f.s.st.Devices, d)
	f.s.mu.Unlock()

	moc := func() int {
		f.s.mu.RLock()
		defer f.s.mu.RUnlock()
		n := 0
		for _, e := range f.s.st.DevLog {
			if e.Dev == d.ID {
				n++
			}
		}
		return n
	}
	ghi := func(up bool) {
		f.s.mu.Lock()
		f.s.ghiMoc(d, up, time.Now())
		f.s.mu.Unlock()
	}
	ghi(true)
	ghi(false)
	ghi(true)
	if moc() != 3 {
		t.Fatalf("phải ghi 3 mốc, nhận %d", moc())
	}

	code, m, _ := f.super.do("GET", "/api/devices/log?id=dv9", nil, nil)
	expect(t, code, 200, "đọc lịch sử")
	lg, _ := m["log"].([]interface{})
	if len(lg) != 3 {
		t.Fatalf("API phải trả 3 mốc, nhận %d", len(lg))
	}
	if first, _ := lg[0].(map[string]interface{}); first["up"] != true {
		t.Fatal("mốc mới nhất phải lên đầu")
	}
	code, _, _ = f.super.do("GET", "/api/devices/log?id=khongco", nil, nil)
	expect(t, code, 404, "thiết bị không có")

	// tài khoản camera chung: camera chưa khai riêng vẫn được đo hình
	f.s.mu.Lock()
	co := f.s.doHinh(d)
	f.s.st.Settings.CamUser, f.s.st.Settings.CamPass = "admin", "bimat"
	co2 := f.s.doHinh(d)
	f.s.mu.Unlock()
	if co {
		t.Fatal("chưa có tài khoản nào thì không đo hình được")
	}
	if !co2 {
		t.Fatal("có tài khoản camera chung thì phải đo hình")
	}
}

// Làm hàng loạt: xóa nhiều thiết bị, hoặc xếp cả lô vào một nhóm.
func TestDeviceBulk(t *testing.T) {
	f := setup(t)
	them := func(name, ip string) string {
		code, m, _ := f.super.do("POST", "/api/devices", map[string]interface{}{"name": name, "ip": ip, "kind": "camera"}, nil)
		expect(t, code, 200, "thêm "+name)
		return m["id"].(string)
	}
	a, b, c := them("C1", "192.168.88.1"), them("C2", "192.168.88.2"), them("C3", "192.168.88.3")

	code, _, _ := f.tech.do("POST", "/api/devices/bulk", map[string]interface{}{"ids": []string{a}, "action": "delete"}, nil)
	expect(t, code, 403, "kỹ thuật viên không được làm hàng loạt")
	code, _, _ = f.super.do("POST", "/api/devices/bulk", map[string]interface{}{"ids": []string{}, "action": "delete"}, nil)
	expect(t, code, 400, "không chọn thiết bị nào")
	code, _, _ = f.super.do("POST", "/api/devices/bulk", map[string]interface{}{"ids": []string{a}, "action": "lung-tung"}, nil)
	expect(t, code, 400, "việc lạ")

	// xếp nhóm cho hai cái
	code, m, _ := f.super.do("POST", "/api/devices/bulk", map[string]interface{}{"ids": []string{a, b}, "action": "group", "group": "Camera kho"}, nil)
	expect(t, code, 200, "xếp nhóm hàng loạt")
	if m["n"].(float64) != 2 {
		t.Fatalf("phải xếp 2 thiết bị, nhận %v", m["n"])
	}
	f.s.mu.RLock()
	nhom := map[string]string{}
	for _, d := range f.s.st.Devices {
		if g := nhomCua(d); len(g) > 0 {
			nhom[d.ID] = g[0]
		} else {
			nhom[d.ID] = ""
		}
	}
	f.s.mu.RUnlock()
	if nhom[a] != "Camera kho" || nhom[b] != "Camera kho" || nhom[c] != "" {
		t.Fatalf("xếp nhóm sai: %v", nhom)
	}

	// xóa hai cái, còn lại một
	code, m, _ = f.super.do("POST", "/api/devices/bulk", map[string]interface{}{"ids": []string{a, c}, "action": "delete"}, nil)
	expect(t, code, 200, "xóa hàng loạt")
	if m["n"].(float64) != 2 {
		t.Fatalf("phải xóa 2 thiết bị, nhận %v", m["n"])
	}
	code, _, raw := f.super.do("GET", "/api/devices", nil, nil)
	expect(t, code, 200, "danh sách sau khi xóa")
	var list []map[string]interface{}
	if json.Unmarshal(raw, &list); len(list) != 1 || list[0]["id"] != b {
		t.Fatalf("phải còn đúng C2: %s", raw)
	}
}

// Nhập hàng loạt từ file: bỏ dòng thiếu/sai, bỏ dòng trùng IP, chỉ lấy tên và IP.
func TestDeviceImport(t *testing.T) {
	f := setup(t)
	code, _, _ := f.super.do("POST", "/api/devices", map[string]interface{}{"name": "Da co", "ip": "172.20.5.1", "kind": "camera"}, nil)
	expect(t, code, 200, "thêm sẵn một cái")

	ds := []map[string]string{
		{"name": "Cam A", "ip": "172.20.5.10"},
		{"name": "Cam B", "ip": "172.20.5.11"},
		{"name": "Trùng", "ip": "172.20.5.1"},             // trùng với cái đã có
		{"name": "Trùng trong file", "ip": "172.20.5.10"}, // trùng ngay trong file
		{"name": "Thiếu IP", "ip": ""},
		{"name": "", "ip": "172.20.5.12"}, // thiếu tên
		{"name": "Ngoài mạng nội bộ", "ip": "8.8.8.8"},
		{"name": "IP bậy", "ip": "abc"},
	}
	code, m, _ := f.super.do("POST", "/api/devices/import", map[string]interface{}{"devices": ds, "kind": "camera", "group": "Camera Hải Quan"}, nil)
	expect(t, code, 200, "nhập từ file")
	// dòng trùng IP với thiết bị đã có giờ được GỘP NHÓM chứ không bỏ hẳn
	if m["them"].(float64) != 2 || m["gop"].(float64) != 1 || m["boQua"].(float64) != 5 {
		t.Fatalf("phải thêm 2 gộp 1 bỏ 5, nhận thêm=%v gộp=%v bỏ=%v", m["them"], m["gop"], m["boQua"])
	}
	f.s.mu.RLock()
	n, nhom := 0, ""
	for _, d := range f.s.st.Devices {
		if len(nhomCua(d)) > 0 && nhomCua(d)[0] == "Camera Hải Quan" {
			n++
			nhom = nhomCua(d)[0]
		}
	}
	f.s.mu.RUnlock()
	// 2 cái mới + 1 cái đã có được gộp thêm nhóm
	if n != 3 || nhom != "Camera Hải Quan" {
		t.Fatalf("phải có 3 thiết bị mang nhóm này, nhận %d", n)
	}
	code, _, _ = f.super.do("POST", "/api/devices/import", map[string]interface{}{"devices": ds, "kind": "may-bay"}, nil)
	expect(t, code, 400, "loại lạ")
	code, _, _ = f.tech.do("POST", "/api/devices/import", map[string]interface{}{"devices": ds, "kind": "camera"}, nil)
	expect(t, code, 403, "kỹ thuật viên không được nhập")
}

// Đọc .xlsx: tự dựng một file Excel tối thiểu trong bộ nhớ rồi đọc lại.
func TestDocXLSX(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	them := func(name, body string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	them("xl/workbook.xml", `<workbook><sheets><sheet name="DS TỔNG" sheetId="1"/><sheet name="Trống" sheetId="2"/></sheets></workbook>`)
	them("xl/sharedStrings.xml", `<sst><si><t>VỊ TRÍ</t></si><si><t>IP</t></si><si><t>Nhuom 1</t></si><si><t>Chinh Ly</t></si></sst>`)
	// B2 là số viết thẳng, các ô chữ dùng bảng chuỗi chung; D3 kiểu inlineStr
	them("xl/worksheets/sheet1.xml", `<worksheet><sheetData>`+
		`<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c></row>`+
		`<row r="2"><c r="A2" t="s"><v>2</v></c><c r="B2"><v>172.20.42.140</v></c></row>`+
		`<row r="3"><c r="A3" t="s"><v>3</v></c><c r="B3"><v>172.20.20.250</v></c><c r="D3" t="inlineStr"><is><t>ghi chú</t></is></c></row>`+
		`<row r="4"></row>`+
		`</sheetData></worksheet>`)
	them("xl/worksheets/sheet2.xml", `<worksheet><sheetData></sheetData></worksheet>`)
	zw.Close()

	sheets, err := docXLSX(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(sheets) != 1 { // sheet rỗng bị bỏ
		t.Fatalf("phải còn 1 sheet có dữ liệu, nhận %d", len(sheets))
	}
	s := sheets[0]
	if s.Name != "DS TỔNG" {
		t.Fatalf("tên sheet sai: %q", s.Name)
	}
	if len(s.Rows) != 3 { // dòng rỗng bị bỏ
		t.Fatalf("phải có 3 dòng, nhận %d: %v", len(s.Rows), s.Rows)
	}
	if s.Rows[0][0] != "VỊ TRÍ" || s.Rows[0][1] != "IP" {
		t.Fatalf("dòng tiêu đề sai: %v", s.Rows[0])
	}
	if s.Rows[1][0] != "Nhuom 1" || s.Rows[1][1] != "172.20.42.140" {
		t.Fatalf("dòng dữ liệu sai: %v", s.Rows[1])
	}
	// ô trống ở giữa phải giữ đúng vị trí cột
	if len(s.Rows[2]) != 4 || s.Rows[2][2] != "" || s.Rows[2][3] != "ghi chú" {
		t.Fatalf("ô trống không giữ đúng vị trí cột: %v", s.Rows[2])
	}
	if _, err := docXLSX([]byte("day khong phai file zip")); err == nil {
		t.Fatal("file hỏng phải báo lỗi")
	}
}

func TestCotSo(t *testing.T) {
	for _, c := range []struct {
		ref string
		n   int
	}{{"A1", 1}, {"B2", 2}, {"Z9", 26}, {"AA1", 27}, {"AB3", 28}, {"", 0}} {
		if got := cotSo(c.ref); got != c.n {
			t.Fatalf("cotSo(%q) = %d, cần %d", c.ref, got, c.n)
		}
	}
}

// Một thiết bị thuộc nhiều nhóm; nhập file trùng IP thì gộp nhóm chứ không bỏ qua.
func TestNhieuNhom(t *testing.T) {
	f := setup(t)
	// nhập lần đầu: nhóm tổng + nhóm con theo từng dòng
	ds := []map[string]string{
		{"name": "Cam 1", "ip": "172.20.11.230", "group": "Nhuộm 1"},
		{"name": "Cam 2", "ip": "172.20.30.232", "group": "Sợi 2"},
	}
	code, m, _ := f.super.do("POST", "/api/devices/import", map[string]interface{}{"devices": ds, "kind": "camera", "groups": []string{"Camera CTC"}}, nil)
	expect(t, code, 200, "nhập lần đầu")
	if m["them"].(float64) != 2 {
		t.Fatalf("phải thêm 2, nhận %v", m["them"])
	}
	nhom := func(ip string) []string {
		f.s.mu.RLock()
		defer f.s.mu.RUnlock()
		for _, d := range f.s.st.Devices {
			if d.IP == ip {
				return nhomCua(d)
			}
		}
		return nil
	}
	if g := nhom("172.20.11.230"); len(g) != 2 || g[0] != "Camera CTC" || g[1] != "Nhuộm 1" {
		t.Fatalf("phải thuộc cả nhóm tổng lẫn nhóm con, nhận %v", g)
	}

	// nhập danh sách thứ hai trùng IP: không thêm trùng nhưng GỘP thêm nhóm
	code, m, _ = f.super.do("POST", "/api/devices/import", map[string]interface{}{
		"devices": []map[string]string{{"name": "Cam 2 (HQ)", "ip": "172.20.30.232"}},
		"kind":    "camera", "groups": []string{"Camera Hải Quan"}}, nil)
	expect(t, code, 200, "nhập danh sách thứ hai")
	if m["them"].(float64) != 0 || m["gop"].(float64) != 1 {
		t.Fatalf("phải thêm 0 gộp 1, nhận thêm=%v gộp=%v", m["them"], m["gop"])
	}
	g := nhom("172.20.30.232")
	if len(g) != 3 || g[2] != "Camera Hải Quan" {
		t.Fatalf("camera Hải Quan phải vẫn thuộc nhóm tổng CTC và có thêm nhóm Hải Quan, nhận %v", g)
	}
	// gộp lần nữa cùng nhóm thì không đổi gì
	code, m, _ = f.super.do("POST", "/api/devices/import", map[string]interface{}{
		"devices": []map[string]string{{"name": "Cam 2 (HQ)", "ip": "172.20.30.232"}},
		"kind":    "camera", "groups": []string{"Camera Hải Quan"}}, nil)
	expect(t, code, 200, "nhập lại lần nữa")
	if m["gop"].(float64) != 0 || m["boQua"].(float64) != 1 {
		t.Fatalf("nhóm đã có thì không gộp nữa: gộp=%v bỏ=%v", m["gop"], m["boQua"])
	}
}

func TestDonNhomVaThemNhom(t *testing.T) {
	if g := donNhom([]string{" A ", "A", "", "B", "A "}); len(g) != 2 || g[0] != "A" || g[1] != "B" {
		t.Fatalf("phải bỏ trùng và bỏ rỗng, nhận %v", g)
	}
	nhieu := make([]string, 20)
	for i := range nhieu {
		nhieu[i] = "N" + itoa(i)
	}
	if g := donNhom(nhieu); len(g) != 10 {
		t.Fatalf("tối đa 10 nhóm, nhận %d", len(g))
	}
	d := &NetDev{Group: "Cũ"} // dữ liệu bản cũ một nhóm
	if g := nhomCua(d); len(g) != 1 || g[0] != "Cũ" {
		t.Fatalf("phải chuyển được nhóm kiểu cũ, nhận %v", g)
	}
	if !themNhom(d, []string{"Mới"}) || len(nhomCua(d)) != 2 {
		t.Fatalf("thêm nhóm hỏng: %v", nhomCua(d))
	}
	if themNhom(d, []string{"Mới"}) {
		t.Fatal("thêm nhóm đã có thì phải trả false")
	}
}

func TestKhoangVN(t *testing.T) {
	for _, c := range []struct {
		phut int
		muon string
	}{
		{0, "0 phút"}, {45, "45 phút"}, {59, "59 phút"},
		{60, "1 tiếng 0 phút"}, {166, "2 tiếng 46 phút"}, {1303, "21 tiếng 43 phút"},
		{1440, "1 ngày 0 giờ 0 phút"}, {1474, "1 ngày 0 giờ 34 phút"}, {2192, "1 ngày 12 giờ 32 phút"},
	} {
		if got := khoangVN(c.phut); got != c.muon {
			t.Fatalf("khoangVN(%d) = %q, cần %q", c.phut, got, c.muon)
		}
	}
}

// Cảnh báo không được lặp lại dù danh sách đã dài hơn tầm dò cũ (400 dòng).
func TestCanhBaoKhongLap(t *testing.T) {
	f := setup(t)
	m := &Machine{ID: "mx", Name: "Máy thử"}
	f.s.mu.Lock()
	// nhồi cho danh sách dài hơn 400 dòng đã đóng
	for i := 0; i < 500; i++ {
		f.s.st.Alerts = append(f.s.st.Alerts, &Alert{ID: rid(5), Machine: "khac", Kind: "disk", Status: "closed"})
	}
	f.s.cond(m, "patch", "warn", true, "Máy chưa cài bản vá Windows nào trong 2192 ngày")
	for i := 0; i < 300; i++ { // thêm nữa để cảnh báo vừa mở lùi ra ngoài tầm dò cũ
		f.s.st.Alerts = append(f.s.st.Alerts, &Alert{ID: rid(5), Machine: "khac", Kind: "disk", Status: "closed"})
	}
	f.s.cond(m, "patch", "warn", true, "Máy chưa cài bản vá Windows nào trong 2192 ngày")
	f.s.cond(m, "patch", "warn", true, "Máy chưa cài bản vá Windows nào trong 2192 ngày")
	mo := 0
	for _, a := range f.s.st.Alerts {
		if a.Machine == "mx" && a.Kind == "patch" && a.Status == "open" {
			mo++
		}
	}
	f.s.mu.Unlock()
	if mo != 1 {
		t.Fatalf("một máy một loại chỉ được một cảnh báo đang mở, nhận %d", mo)
	}
}

func TestDonCanhBaoTrung(t *testing.T) {
	f := setup(t)
	f.s.mu.Lock()
	for i := 0; i < 4; i++ {
		f.s.st.Alerts = append(f.s.st.Alerts, &Alert{ID: rid(5), Machine: "m1", Kind: "patch", Status: "open", Opened: time.Now()})
	}
	f.s.st.Alerts = append(f.s.st.Alerts, &Alert{ID: rid(5), Machine: "m1", Kind: "disk", Status: "open"})
	f.s.st.Alerts = append(f.s.st.Alerts, &Alert{ID: rid(5), Machine: "m2", Kind: "patch", Status: "open"})
	n := f.s.donCanhBaoTrung()
	mo := map[string]int{}
	for _, a := range f.s.st.Alerts {
		if a.Status == "open" {
			mo[a.Machine+"/"+a.Kind]++
		}
	}
	f.s.mu.Unlock()
	if n != 3 {
		t.Fatalf("phải đóng 3 cái trùng, nhận %d", n)
	}
	if mo["m1/patch"] != 1 || mo["m1/disk"] != 1 || mo["m2/patch"] != 1 {
		t.Fatalf("mỗi máy mỗi loại phải còn đúng 1: %v", mo)
	}
}
