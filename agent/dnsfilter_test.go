package main

import (
	"encoding/binary"
	"net"
	"strings"
	"testing"
)

func query(name string, qtype uint16) []byte {
	q := []byte{0x12, 0x34, 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0}
	for _, l := range strings.Split(name, ".") {
		q = append(q, byte(len(l)))
		q = append(q, l...)
	}
	return append(q, 0, byte(qtype>>8), byte(qtype), 0, 1)
}

func TestDNSFilter(t *testing.T) {
	// DNS "gốc" giả: trả lại đúng gói hỏi với cờ QR để biết là đã chuyển tiếp
	pc, _ := net.ListenPacket("udp", "127.0.0.1:0")
	defer pc.Close()
	go func() {
		b := make([]byte, 512)
		for {
			n, a, err := pc.ReadFrom(b)
			if err != nil {
				return
			}
			b[2] |= 0x80
			pc.WriteTo(b[:n], a)
		}
	}()
	dnsF.block = buildBlock([]string{"https://www.Facebook.com/", "youtube.com"})
	dnsF.upstream = []string{pc.LocalAddr().String()}
	for _, n := range []string{"facebook.com", "m.facebook.com", "static.xx.fbcdn.net", "rr1.googlevideo.com", "www.youtube.com", "dns.google"} {
		r := dnsAnswer(query(n, 1), false)
		if r == nil || binary.BigEndian.Uint16(r[6:8]) != 1 || !net.IP(r[len(r)-4:]).Equal(net.IPv4zero) {
			t.Fatalf("%s phải bị chặn (0.0.0.0): %v", n, r)
		}
	}
	if r := dnsAnswer(query("m.facebook.com", 65), false); r == nil || binary.BigEndian.Uint16(r[6:8]) != 0 {
		t.Fatal("bản ghi HTTPS của tên bị chặn phải trả rỗng")
	}
	if r := dnsAnswer(query("use-application-dns.net", 1), false); r == nil || r[3]&0x0F != 3 {
		t.Fatal("canary Firefox phải NXDOMAIN")
	}
	q := query("google.com", 1)
	if r := dnsAnswer(q, false); r == nil || len(r) != len(q) || r[2]&0x80 == 0 {
		t.Fatal("tên không chặn phải được chuyển tới DNS gốc")
	}
	if blocked(dnsF.block, "notfacebook.com") {
		t.Fatal("notfacebook.com không được chặn nhầm")
	}
}
