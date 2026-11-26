package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// camChoi: camera luôn từ chối tài khoản (401 Basic) — đúng kiểu 22 dải mạng của đại ca.
func camChoi(t *testing.T) (string, int) {
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
					if _, err := br.ReadString('\n'); err != nil {
						return
					}
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
					fmt.Fprintf(c, "RTSP/1.0 401 Unauthorized\r\nCSeq: %s\r\nWWW-Authenticate: Basic realm=\"x\"\r\n\r\n", seq)
				}
			}()
		}
	}()
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	p := 0
	fmt.Sscan(port, &p)
	return host, p
}

func TestKbpsBaoSaiTaiKhoan(t *testing.T) {
	ip, port := camChoi(t)
	_, err := rtspKbps(context.Background(), ip, port, "", "admin", "sai")
	if !errors.Is(err, errSaiMatKhau) {
		t.Fatalf("phải báo sai tài khoản, nhận: %v", err)
	}
	if _, err := rtspKbps(context.Background(), ip, port, "", "", ""); !errors.Is(err, errChuaKhai) {
		t.Fatalf("chưa khai tài khoản thì phải báo chưa khai, nhận: %v", err)
	}
}

// Camera từ chối tài khoản thì KHÔNG được tính là mất hình: không đếm VidMiss (nguồn của báo động
// và của mốc "treo" trong lịch sử), mà phải bật cờ SaiTK để Console nói đúng lý do.
func TestCheckDevKhongVuOanCameraSaiTaiKhoan(t *testing.T) {
	ip, port := camChoi(t)
	d := NetDev{ID: "c1", IP: ip, Port: port, Kind: "camera", User: "admin", Pass: "sai", VidMiss: 3}
	out := checkDev(context.Background(), d, true)
	if !out.SaiTK {
		t.Fatal("phải bật cờ sai tài khoản")
	}
	if out.VidMiss != 3 {
		t.Fatalf("không được đếm thêm lượt mất hình, VidMiss=%d", out.VidMiss)
	}
	if out.Video {
		t.Fatal("không vào được thì không được coi là có hình")
	}
	_ = time.Second
}
