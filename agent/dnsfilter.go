package main

// Bộ lọc DNS: chặn web chắc hơn file hosts. Khi chính sách có web cần chặn, agent mở một máy chủ DNS nhỏ ở
// 127.0.0.1:53 (và [::1]:53) rồi đặt DNS của các card mạng thành "127.0.0.1, <DNS cũ>". Mọi tra cứu tên miền
// đi qua đây: tên bị chặn (kể cả mọi tên miền phụ) trả về 0.0.0.0, còn lại chuyển nguyên gói tới DNS cũ.
// Đường lui: DNS cũ vẫn đứng thứ hai nên agent có chết thì máy vẫn có mạng; hết web cần chặn hoặc gỡ agent thì trả DNS cũ.
import (
	"encoding/binary"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

var dnsF struct {
	sync.Mutex
	block    map[string]bool // tên miền (và tên miền phụ) bị chặn
	upstream []string        // DNS cũ, "ip:53"
	started  bool            // đã mở được cổng 53
	applied  bool            // đã đổi DNS hệ thống
	lastApp  time.Time
}

// Tên đi kèm: chặn trang chính mà không chặn các tên này thì video/ảnh vẫn tải, app vẫn chạy.
var siteExtras = map[string][]string{
	"youtube.com":   {"youtu.be", "googlevideo.com", "ytimg.com", "youtube-nocookie.com", "youtubei.googleapis.com", "yt3.ggpht.com"},
	"facebook.com":  {"fb.com", "fbcdn.net", "facebook.net", "fbsbx.com", "fb.me", "fb.watch"},
	"messenger.com": {"m.me"},
	"instagram.com": {"cdninstagram.com", "ig.me"},
	"tiktok.com":    {"tiktokcdn.com", "tiktokv.com", "tiktokcdn-us.com", "byteoversea.com", "ibytedtos.com", "muscdn.com"},
	"twitter.com":   {"x.com", "twimg.com", "t.co"},
	"x.com":         {"twitter.com", "twimg.com", "t.co"},
	"netflix.com":   {"nflxvideo.net", "nflximg.net", "nflxso.net", "nflxext.com"},
}

// Máy chủ "DNS mã hóa" (DoH/DoT) mà trình duyệt có thể tự dùng để lách bộ lọc: luôn chặn khi bộ lọc bật.
var dohHosts = []string{"dns.google", "dns.google.com", "cloudflare-dns.com", "one.one.one.one", "dns.quad9.net",
	"doh.opendns.com", "doh.360.cn", "dns.alidns.com", "doh.pub", "dot.pub", "dns.adguard.com", "dns.adguard-dns.com",
	"dns.nextdns.io", "doh.cleanbrowsing.org", "mask.icloud.com", "mask-h2.icloud.com", "mask-api.icloud.com"}

func normSite(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "http://")
	if i := strings.IndexAny(s, "/:?#"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSuffix(strings.TrimPrefix(s, "www."), ".")
}

func buildBlock(sites []string) map[string]bool {
	m := map[string]bool{}
	for _, s := range sites {
		if s = normSite(s); s == "" {
			continue
		}
		m[s] = true
		for _, x := range siteExtras[s] {
			m[x] = true
		}
	}
	if len(m) > 0 {
		for _, h := range dohHosts {
			m[h] = true
		}
	}
	return m
}

// blocked: name hoặc một tên miền cha của nó nằm trong danh sách.
func blocked(block map[string]bool, name string) bool {
	name = strings.TrimSuffix(strings.ToLower(name), ".")
	for name != "" {
		if block[name] {
			return true
		}
		i := strings.IndexByte(name, '.')
		if i < 0 {
			return false
		}
		name = name[i+1:]
	}
	return false
}

// parseQ đọc tên và loại của câu hỏi đầu tiên; qEnd là vị trí hết phần câu hỏi.
func parseQ(p []byte) (name string, qtype uint16, qEnd int, ok bool) {
	if len(p) < 12 || binary.BigEndian.Uint16(p[4:6]) == 0 {
		return
	}
	i, labels := 12, []string{}
	for i < len(p) {
		l := int(p[i])
		if l == 0 {
			i++
			break
		}
		if l&0xC0 != 0 || i+1+l > len(p) {
			return
		}
		labels = append(labels, string(p[i+1:i+1+l]))
		i += 1 + l
	}
	if i+4 > len(p) {
		return
	}
	return strings.Join(labels, "."), binary.BigEndian.Uint16(p[i : i+2]), i + 4, true
}

// blockAnswer: trả lời "0.0.0.0" (A), "::" (AAAA), các loại khác trả rỗng; tên canary của Firefox trả NXDOMAIN để tắt DoH tự động.
func blockAnswer(q []byte, qtype uint16, qEnd int, nx bool) []byte {
	r := make([]byte, 0, qEnd+32)
	r = append(r, q[:qEnd]...)
	flags := uint16(0x8180) | binary.BigEndian.Uint16(q[2:4])&0x0100 // QR, RA, giữ RD
	if nx {
		flags |= 3
	}
	binary.BigEndian.PutUint16(r[2:4], flags)
	binary.BigEndian.PutUint16(r[4:6], 1)
	binary.BigEndian.PutUint16(r[8:10], 0)
	binary.BigEndian.PutUint16(r[10:12], 0)
	var rdata []byte
	switch qtype {
	case 1:
		rdata = make([]byte, 4)
	case 28:
		rdata = make([]byte, 16)
	}
	if rdata == nil || nx {
		binary.BigEndian.PutUint16(r[6:8], 0)
		return r
	}
	binary.BigEndian.PutUint16(r[6:8], 1)
	r = append(r, 0xC0, 0x0C, byte(qtype>>8), byte(qtype), 0, 1, 0, 0, 0, 30, 0, byte(len(rdata)))
	return append(r, rdata...)
}

func dnsAnswer(q []byte, tcp bool) []byte {
	name, qtype, qEnd, ok := parseQ(q)
	dnsF.Lock()
	block, ups := dnsF.block, dnsF.upstream
	dnsF.Unlock()
	if ok {
		if strings.EqualFold(strings.TrimSuffix(name, "."), "use-application-dns.net") && len(block) > 0 {
			return blockAnswer(q, qtype, qEnd, true)
		}
		if blocked(block, name) {
			return blockAnswer(q, qtype, qEnd, false)
		}
	}
	for _, u := range ups {
		if r := forward(u, q, tcp); r != nil {
			return r
		}
	}
	return nil // không DNS nào trả lời: máy tự thử DNS thứ hai
}

func forward(up string, q []byte, tcp bool) []byte {
	network := "udp"
	if tcp {
		network = "tcp"
	}
	c, err := net.DialTimeout(network, up, 2*time.Second)
	if err != nil {
		return nil
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	if tcp {
		b := make([]byte, 2+len(q))
		binary.BigEndian.PutUint16(b, uint16(len(q)))
		copy(b[2:], q)
		if _, err := c.Write(b); err != nil {
			return nil
		}
		var l [2]byte
		if _, err := io.ReadFull(c, l[:]); err != nil {
			return nil
		}
		r := make([]byte, binary.BigEndian.Uint16(l[:]))
		if _, err := io.ReadFull(c, r); err != nil {
			return nil
		}
		return r
	}
	if _, err := c.Write(q); err != nil {
		return nil
	}
	buf := make([]byte, 4096)
	n, err := c.Read(buf)
	if err != nil {
		return nil
	}
	return buf[:n]
}

func serveUDP(pc net.PacketConn) {
	buf := make([]byte, 4096)
	for {
		n, addr, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}
		q := append([]byte(nil), buf[:n]...)
		go func() {
			if r := dnsAnswer(q, false); r != nil {
				pc.WriteTo(r, addr)
			}
		}()
	}
}

func serveTCP(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer c.Close()
			c.SetDeadline(time.Now().Add(10 * time.Second))
			var l [2]byte
			if _, err := io.ReadFull(c, l[:]); err != nil {
				return
			}
			q := make([]byte, binary.BigEndian.Uint16(l[:]))
			if _, err := io.ReadFull(c, q); err != nil {
				return
			}
			if r := dnsAnswer(q, true); r != nil {
				b := make([]byte, 2+len(r))
				binary.BigEndian.PutUint16(b, uint16(len(r)))
				copy(b[2:], r)
				c.Write(b)
			}
		}()
	}
}

// startDNS mở cổng 53 trên 127.0.0.1 (bắt buộc) và [::1] (nếu được). Không mở được thì không đổi DNS hệ thống.
func startDNS() bool {
	pc, err := net.ListenPacket("udp", "127.0.0.1:53")
	if err != nil {
		log.Printf("bộ lọc DNS: không mở được cổng 53: %v", err)
		return false
	}
	go serveUDP(pc)
	if ln, err := net.Listen("tcp", "127.0.0.1:53"); err == nil {
		go serveTCP(ln)
	}
	if pc6, err := net.ListenPacket("udp", "[::1]:53"); err == nil {
		go serveUDP(pc6)
	}
	if ln6, err := net.Listen("tcp", "[::1]:53"); err == nil {
		go serveTCP(ln6)
	}
	return true
}

func isLoop(ip string) bool { p := net.ParseIP(ip); return p == nil || p.IsLoopback() }

// dnsFilter bật/tắt bộ lọc theo danh sách web cần chặn. Gọi mỗi lần áp chính sách (rẻ nếu không có gì đổi).
func dnsFilter(sites []string) {
	if !dnsSupported {
		return
	}
	block := buildBlock(sites)
	dnsF.Lock()
	dnsF.block = block
	want, applied, started := len(block) > 0, dnsF.applied, dnsF.started
	dnsF.Unlock()
	if !want {
		if applied || dnsBackupExists() {
			dnsRestore()
			dnsF.Lock()
			dnsF.applied = false
			dnsF.Unlock()
			log.Printf("bộ lọc DNS: đã tắt, trả DNS cũ")
		}
		return
	}
	if !started {
		if !startDNS() {
			return
		}
		dnsF.Lock()
		dnsF.started = true
		dnsF.Unlock()
	}
	dnsF.Lock()
	due := !dnsF.applied || time.Since(dnsF.lastApp) > 5*time.Minute // áp lại định kỳ: cắm mạng mới, đổi Wi-Fi
	dnsF.Unlock()
	if !due {
		return
	}
	ups, err := dnsApply()
	if err != nil {
		log.Printf("bộ lọc DNS: %v", err)
		return
	}
	var clean []string
	for _, u := range ups {
		if !isLoop(u) {
			clean = append(clean, net.JoinHostPort(u, "53"))
		}
	}
	if len(clean) == 0 {
		clean = []string{"8.8.8.8:53", "1.1.1.1:53"}
	}
	dnsF.Lock()
	dnsF.upstream, dnsF.applied, dnsF.lastApp = clean, true, time.Now()
	dnsF.Unlock()
}
