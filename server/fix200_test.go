package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
)

// camDuong: camera giả trả SDP khác nhau tùy đường được hỏi. Đường nào không có trong bang thì 404.
// Nhận nhiều lượt kết nối vì bên gọi dò lần lượt từng đường.
func camDuong(t *testing.T, bang map[string]string) (string, int) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
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
					method, uri, seq := f[0], f[1], ""
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
					duong := uri
					if i := strings.Index(uri[7:], "/"); i >= 0 {
						duong = uri[7+i:]
					} else {
						duong = ""
					}
					switch method {
					case "DESCRIBE":
						ma, co := bang[duong]
						if !co {
							fmt.Fprintf(c, "RTSP/1.0 404 Not Found\r\nCSeq: %s\r\n\r\n", seq)
							continue
						}
						sdp := "v=0\r\nm=video 0 RTP/AVP 96\r\na=rtpmap:96 " + ma + "/90000\r\na=control:track1\r\n"
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

// Thiết bị không khai đường: luồng chính H.265 nhưng luồng phụ H.264 thì phải tự tìm ra luồng phụ.
// Đây là lý do thật khiến 404 camera trong mạng đại ca không ra video ở bản 1.1.99.
func TestMoLuongDoDuongPhu(t *testing.T) {
	ip, port := camDuong(t, map[string]string{
		"":                       "H265",
		"/Streaming/Channels/101": "H265",
		"/Streaming/Channels/102": "H264",
	})
	l, err := moLuong(context.Background(), ip, port, "", "", "")
	if err != nil {
		t.Fatalf("phải mở được luồng phụ H.264, lỗi: %v", err)
	}
	defer l.dong()
	if !strings.HasSuffix(l.base, "/Streaming/Channels/102") {
		t.Fatalf("phải chọn luồng phụ, đang chọn %s", l.base)
	}
}

// Camera H.265 ở mọi đường: phải báo đúng chuyện bộ mã, không phải lỗi 404 của đường dò cuối.
func TestMoLuongBaoH265(t *testing.T) {
	ip, port := camDuong(t, map[string]string{"": "H265", "/Streaming/Channels/102": "H265"})
	_, err := moLuong(context.Background(), ip, port, "", "", "")
	if err == nil || !errors.Is(err, errMaLa) {
		t.Fatalf("phải báo lỗi bộ mã, nhận: %v", err)
	}
	if !strings.Contains(err.Error(), "H265") {
		t.Fatalf("lỗi phải nói rõ camera đang phát gì: %v", err)
	}
}

// Đường đã khai trong thiết bị thì dùng đúng đường đó, không được tự dò sang đường khác.
func TestMoLuongTonTrongDuongDaKhai(t *testing.T) {
	ip, port := camDuong(t, map[string]string{"/rieng": "H264", "/Streaming/Channels/102": "H264"})
	l, err := moLuong(context.Background(), ip, port, "rieng", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer l.dong()
	if !strings.HasSuffix(l.base, "/rieng") {
		t.Fatalf("phải dùng đường đã khai, đang dùng %s", l.base)
	}
}
