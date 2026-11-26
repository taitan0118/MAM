package main

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"testing"
)

// camKhoa: camera chỉ cho vào đúng một tài khoản (Basic), luồng phụ là H.264.
func camKhoa(t *testing.T, user, pass string) (string, int) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	dung := "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
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
					if len(f) < 2 {
						return
					}
					method, uri, seq, auth := f[0], f[1], "", ""
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
							auth = strings.TrimSpace(h[len("authorization:"):])
						}
					}
					if auth != dung {
						fmt.Fprintf(c, "RTSP/1.0 401 Unauthorized\r\nCSeq: %s\r\nWWW-Authenticate: Basic realm=\"x\"\r\n\r\n", seq)
						continue
					}
					switch method {
					case "DESCRIBE":
						if !strings.HasSuffix(uri, "/Streaming/Channels/102") {
							fmt.Fprintf(c, "RTSP/1.0 404 Not Found\r\nCSeq: %s\r\n\r\n", seq)
							continue
						}
						sdp := "v=0\r\nm=video 0 RTP/AVP 96\r\na=rtpmap:96 H264/90000\r\na=control:track1\r\n"
						fmt.Fprintf(c, "RTSP/1.0 200 OK\r\nCSeq: %s\r\nContent-Type: application/sdp\r\nContent-Length: %d\r\n\r\n%s", seq, len(sdp), sdp)
					case "SETUP":
						fmt.Fprintf(c, "RTSP/1.0 200 OK\r\nCSeq: %s\r\nSession: 1;timeout=60\r\nTransport: RTP/AVP/TCP;unicast;interleaved=0-1\r\n\r\n", seq)
					case "PLAY":
						fmt.Fprintf(c, "RTSP/1.0 200 OK\r\nCSeq: %s\r\n\r\n", seq)
					default:
						return
					}
				}
			}()
		}
	}()
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	p := 0
	fmt.Sscan(port, &p)
	return host, p
}

// Nhập tài khoản ở khung xem camera: ĐÚNG mới được nhớ, và nhớ cho RIÊNG camera đó.
func TestKhaiTaiKhoanRiengChoCamera(t *testing.T) {
	f := setup(t)
	ip, port := camKhoa(t, "admin", "dung123")
	f.s.mu.Lock()
	f.s.st.Devices = append(f.s.st.Devices, &NetDev{ID: "cam1", Name: "Camera kho", IP: ip, Port: port, Kind: "camera", SaiTK: true})
	f.s.st.Settings.CamUser, f.s.st.Settings.CamPass = "admin", "chung999"
	f.s.mu.Unlock()

	code, _, _ := f.super.do("POST", "/api/devices/tai-khoan", map[string]string{"id": "cam1", "user": "admin", "pass": "sai"}, nil)
	expect(t, code, 400, "sai mật khẩu")
	f.s.mu.RLock()
	if f.s.st.Devices[len(f.s.st.Devices)-1].Pass != "" {
		t.Fatal("sai mật khẩu thì không được lưu gì cả")
	}
	f.s.mu.RUnlock()

	code, _, _ = f.super.do("POST", "/api/devices/tai-khoan", map[string]string{"id": "cam1", "user": "admin", "pass": "dung123"}, nil)
	expect(t, code, 200, "đúng mật khẩu")
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	d := f.s.st.Devices[len(f.s.st.Devices)-1]
	if d.User != "admin" || d.Pass != "dung123" {
		t.Fatalf("phải nhớ tài khoản cho camera đó, đang là %q/%q", d.User, d.Pass)
	}
	if d.SaiTK {
		t.Fatal("khai đúng rồi thì phải bỏ cờ sai tài khoản")
	}
	if f.s.st.Settings.CamPass != "chung999" {
		t.Fatal("KHÔNG được đụng tới tài khoản chung")
	}
}

// Chỉ super admin được khai tài khoản camera.
func TestKhaiTaiKhoanChanTech(t *testing.T) {
	f := setup(t)
	code, _, _ := f.tech.do("POST", "/api/devices/tai-khoan", map[string]string{"id": "x", "user": "admin", "pass": "x"}, nil)
	expect(t, code, 403, "tech không được khai tài khoản camera")
}
