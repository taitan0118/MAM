package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// ghiURI: đầu ghi giả, chỉ ghi lại xem MAM hỏi đường nào rồi trả 404 cho xong.
func ghiURI(t *testing.T) (string, *[]string, *sync.Mutex) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	var mu sync.Mutex
	uris := []string{}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				br := bufio.NewReader(c)
				for {
					line, err := br.ReadString('\n')
					if err != nil {
						return
					}
					f := strings.Fields(line)
					seq := ""
					for {
						h, err := br.ReadString('\n')
						if err != nil {
							return
						}
						if strings.TrimSpace(h) == "" {
							break
						}
						if strings.HasPrefix(strings.ToLower(h), "cseq:") {
							seq = strings.TrimSpace(h[5:])
						}
					}
					if len(f) >= 2 {
						mu.Lock()
						uris = append(uris, f[1])
						mu.Unlock()
					}
					fmt.Fprintf(c, "RTSP/1.0 404 Not Found\r\nCSeq: %s\r\n\r\n", seq)
				}
			}()
		}
	}()
	return ln.Addr().String(), &uris, &mu
}

// Tua lại phải hỏi đúng đường phát lại của Hikvision, và thử LUỒNG PHỤ trước (Chrome không phát H.265).
func TestTuaHoiDungDuongPhatLai(t *testing.T) {
	addr, uris, mu := ghiURI(t)
	ip, _, _ := net.SplitHostPort(addr)
	// đầu ghi giả nghe ở cổng ngẫu nhiên, mà moTua luôn gọi 554 — nên dựng luôn một cái nghe hộ
	// bằng cách gọi thẳng moLuong với đường do moTua dựng thì không kiểm được thứ tự; thay vào đó
	// kiểm hàm dựng giờ và dựng đường, rồi gọi moTua vào cổng thật để xem thứ tự track.
	t0 := time.Date(2026, 11, 26, 7, 30, 0, 0, time.UTC)
	if g := hikGio(t0); g != "20261126T073000Z" {
		t.Fatalf("mốc giờ Hikvision phải là 20261126T073000Z, đang là %q", g)
	}

	ctx, thoi := context.WithTimeout(context.Background(), 8*time.Second)
	defer thoi()
	// gọi trực tiếp từng đường như moTua làm, vào cổng của đầu ghi giả
	p := 0
	_, ps, _ := net.SplitHostPort(addr)
	fmt.Sscan(ps, &p)
	for _, tr := range []int{2, 1} {
		duong := fmt.Sprintf("/Streaming/tracks/%d%02d?starttime=%s&endtime=%s", 3, tr, hikGio(t0), hikGio(t0.Add(nvrTuaLau)))
		moLuong(ctx, ip, p, duong, "admin", "x")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(*uris) < 2 {
		t.Fatalf("đầu ghi giả phải nhận ít nhất 2 lượt hỏi, mới có %d: %v", len(*uris), *uris)
	}
	if !strings.Contains((*uris)[0], "/Streaming/tracks/302?starttime=20261126T073000Z&endtime=20261126T080000Z") {
		t.Fatalf("lượt đầu phải hỏi luồng phụ của kênh 3 kèm mốc giờ, đang là %q", (*uris)[0])
	}
	if !strings.Contains((*uris)[1], "/Streaming/tracks/301?") {
		t.Fatalf("lượt sau mới tới luồng chính, đang là %q", (*uris)[1])
	}
}

// Chưa khai đầu ghi, hoặc camera không nằm trên đầu ghi, thì nói thẳng chứ không im lặng trả hình trực tiếp.
func TestTuaKhiChuaCoDauGhi(t *testing.T) {
	f := setup(t)
	f.s.mu.Lock()
	f.s.st.Devices = append(f.s.st.Devices, &NetDev{ID: "cam9", Name: "Camera kho", IP: "10.0.0.9", Port: 554, Kind: "camera"})
	f.s.mu.Unlock()

	code, m, _ := f.super.do("GET", "/api/devices/video?id=cam9&tua=2026-11-26T07:30", nil, nil)
	expect(t, code, 400, "chưa khai đầu ghi")
	if s, _ := m["error"].(string); !strings.Contains(s, "đầu ghi") {
		t.Fatalf("phải nói rõ là chưa khai đầu ghi, đang là %q", s)
	}

	// khai đầu ghi rồi nhưng camera không nằm trên đó
	f.s.mu.Lock()
	f.s.st.Settings.Nvrs = []DauGhi{{IP: "10.0.0.1", Port: 80, User: "admin", Pass: "x"}}
	f.s.mu.Unlock()
	code, _, _ = f.super.do("GET", "/api/devices/video?id=cam9&tua=2026-11-26T07:30", nil, nil)
	expect(t, code, 400, "camera không có trên đầu ghi")

	// có kênh rồi thì tới lượt kiểm giờ
	f.s.mu.Lock()
	for _, d := range f.s.st.Devices {
		if d.ID == "cam9" {
			d.NvrID, d.NvrCh = "10.0.0.1", 3
		}
	}
	f.s.mu.Unlock()
	code, _, _ = f.super.do("GET", "/api/devices/video?id=cam9&tua=hom-qua", nil, nil)
	expect(t, code, 400, "giờ sai dạng")
	code, _, _ = f.super.do("GET", "/api/devices/video?id=cam9&tua=2099-01-01T00:00", nil, nil)
	expect(t, code, 400, "giờ ở tương lai")
}

// Đăng nhập đầu ghi thì tự gắn camera vào kênh theo IP, xóa đầu ghi thì gỡ sạch.
func TestGanCameraVaoKenhDauGhi(t *testing.T) {
	f := setup(t)
	ip, port := dauGhiGiaKenh(t, "admin", "dung123", map[int]string{1: "10.0.0.9", 2: "10.0.0.8"})
	f.s.mu.Lock()
	f.s.st.Devices = append(f.s.st.Devices,
		&NetDev{ID: "cam9", Name: "Kho", IP: "10.0.0.9", Kind: "camera"},
		&NetDev{ID: "cam7", Name: "Sân", IP: "10.0.0.7", Kind: "camera"})
	f.s.mu.Unlock()

	code, m, _ := f.super.do("POST", "/api/nvr", map[string]interface{}{"ip": ip, "port": port, "user": "admin", "pass": "dung123"}, nil)
	expect(t, code, 200, "khai đầu ghi")
	if m["gan"] != float64(1) {
		t.Fatalf("chỉ 1 camera trong MAM khớp IP với kênh đầu ghi, báo %v", m["gan"])
	}
	f.s.mu.RLock()
	for _, d := range f.s.st.Devices {
		if d.ID == "cam9" && (d.NvrCh != 1 || d.NvrID != ip) {
			t.Fatalf("camera 10.0.0.9 phải nằm ở kênh 1 của đầu ghi %s, đang là %q/%d", ip, d.NvrID, d.NvrCh)
		}
		if d.ID == "cam7" && (d.NvrCh != 0 || d.NvrID != "") {
			t.Fatalf("camera không có trên đầu ghi thì phải để trống, đang là %q/%d", d.NvrID, d.NvrCh)
		}
	}
	f.s.mu.RUnlock()

	code, _, _ = f.super.do("DELETE", "/api/nvr?ip="+ip, nil, nil)
	expect(t, code, 200, "xóa đầu ghi")
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	for _, d := range f.s.st.Devices {
		if d.NvrCh != 0 {
			t.Fatalf("xóa đầu ghi thì phải gỡ hết kênh, %s còn %d", d.ID, d.NvrCh)
		}
	}
}

// dauGhiGiaKenh: đầu ghi giả khai sẵn mấy kênh, mỗi kênh kéo hình từ một IP camera.
func dauGhiGiaKenh(t *testing.T, user, pass string, kenh map[int]string) (string, int) {
	dung := "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != dung {
			w.Header().Set("WWW-Authenticate", `Basic realm="x"`)
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/ISAPI/System/deviceInfo":
			fmt.Fprint(w, `<?xml version="1.0"?><DeviceInfo><deviceName>Net DVR</deviceName><model>DS-7616NI-K2</model></DeviceInfo>`)
		case "/ISAPI/ContentMgmt/InputProxy/channels":
			var b strings.Builder
			b.WriteString(`<?xml version="1.0"?><InputProxyChannelList>`)
			for id, ip := range kenh {
				b.WriteString(`<InputProxyChannel><id>` + itoa(id) + `</id><sourceInputPortDescriptor><ipAddress>` + ip + `</ipAddress></sourceInputPortDescriptor></InputProxyChannel>`)
			}
			b.WriteString(`</InputProxyChannelList>`)
			fmt.Fprint(w, b.String())
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	host, ps, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	p := 0
	fmt.Sscan(ps, &p)
	return host, p
}

// Xem TRỰC TIẾP qua đầu ghi: đúng đường kênh Hikvision, luồng phụ trước (kiểu iVMS-4200 — khai một
// đầu ghi là coi được cả dàn camera của nó).
func TestXemTrucTiepQuaDauGhi(t *testing.T) {
	addr, uris, mu := ghiURI(t)
	ip, ps, _ := net.SplitHostPort(addr)
	p := 0
	fmt.Sscan(ps, &p)

	ctx, thoi := context.WithTimeout(context.Background(), 8*time.Second)
	defer thoi()
	for _, tr := range []int{2, 1} {
		moLuong(ctx, ip, p, fmt.Sprintf("/Streaming/Channels/%d%02d", 7, tr), "admin", "x")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(*uris) < 2 {
		t.Fatalf("đầu ghi giả phải nhận 2 lượt hỏi, mới có %d: %v", len(*uris), *uris)
	}
	if !strings.HasSuffix((*uris)[0], "/Streaming/Channels/702") {
		t.Fatalf("lượt đầu phải hỏi luồng phụ của kênh 7, đang là %q", (*uris)[0])
	}
	if !strings.HasSuffix((*uris)[1], "/Streaming/Channels/701") {
		t.Fatalf("lượt sau mới tới luồng chính, đang là %q", (*uris)[1])
	}
}
