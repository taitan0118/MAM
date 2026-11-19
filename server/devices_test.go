package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
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
	if list[0]["group"] != "Camera tầng 1" {
		t.Fatalf("nhóm sai: %v", list[0]["group"])
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
	keptPass, name, ip, grp := d.Pass, d.Name, d.IP, d.Group
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
	if len(st.Devices) != 1 || st.Devices[0].Name != "Switch tầng 1" || st.Devices[0].Group != "Switch" {
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
