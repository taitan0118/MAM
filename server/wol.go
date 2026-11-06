package main

import (
	"errors"
	"net"
	"strings"
)

// sendWOL gửi gói Wake-on-LAN (UDP broadcast, cổng 9 và 7) tới địa chỉ MAC, từ TỪNG card mạng (gắn đúng IP nguồn),
// mỗi gói gửi 3 lần. Chỉ gửi broadcast chung thì máy có nhiều card (VPN, VMware...) thường đi nhầm card.
// ip: địa chỉ IP cuối cùng của máy đích (có thể rỗng). Máy ở dải mạng khác với nơi gửi (vd server 172.20.20.x,
// máy 172.20.5.x) thì broadcast trong mạng của mình không tới nơi: gửi thêm thẳng tới IP cũ của máy (router còn nhớ
// MAC thì chuyển được, thường vài giờ sau khi tắt) và tới địa chỉ broadcast của dải /24 bên đó (router phải cho phép).
func sendWOL(mac, ip string) error {
	hw, err := net.ParseMAC(strings.ReplaceAll(mac, "-", ":"))
	if err != nil || len(hw) != 6 {
		return errors.New("MAC không hợp lệ")
	}
	pkt := make([]byte, 0, 102)
	for i := 0; i < 6; i++ {
		pkt = append(pkt, 0xFF)
	}
	for i := 0; i < 16; i++ {
		pkt = append(pkt, hw...)
	}
	var last error
	sent := 0
	send := func(src net.IP, dst string) {
		for _, port := range []int{9, 7} {
			c, err := net.DialUDP("udp4", &net.UDPAddr{IP: src}, &net.UDPAddr{IP: net.ParseIP(dst), Port: port})
			if err != nil {
				last = err
				continue
			}
			for i := 0; i < 3; i++ {
				c.Write(pkt)
			}
			c.Close()
			sent++
		}
	}
	if ifs, err := net.Interfaces(); err == nil {
		for _, ifc := range ifs {
			if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
				continue
			}
			addrs, _ := ifc.Addrs()
			for _, a := range addrs {
				if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil {
					b := make(net.IP, 4)
					for i := 0; i < 4; i++ {
						b[i] = n.IP.To4()[i] | ^n.Mask[len(n.Mask)-4+i]
					}
					send(n.IP.To4(), b.String())
					send(n.IP.To4(), "255.255.255.255")
				}
			}
		}
	}
	if t := net.ParseIP(ip).To4(); t != nil {
		send(nil, t.String())
		send(nil, net.IPv4(t[0], t[1], t[2], 255).String())
	}
	if sent == 0 {
		send(nil, "255.255.255.255")
	}
	if sent > 0 {
		return nil
	}
	return last
}
