package main

import (
	"context"
	"net"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var reHHMM = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)
var reDomain = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]{0,251}[a-z0-9])?$`)
var reProc = regexp.MustCompile(`^[A-Za-z0-9 ._()-]{1,60}$`)

// Các máy chủ "DNS bảo mật" (DNS-over-HTTPS) mà trình duyệt dùng để bỏ qua file hosts. Chặn chúng thì
// trình duyệt phải quay về DNS hệ thống, lúc đó chặn web bằng hosts mới có tác dụng.
var dohHosts = []string{
	"cloudflare-dns.com", "mozilla.cloudflare-dns.com", "chrome.cloudflare-dns.com", "one.one.one.one",
	"dns.google", "dns.google.com", "dns.quad9.net", "doh.opendns.com", "dns.nextdns.io", "doh.cleanbrowsing.org",
	"dns.adguard.com", "doh.dns.sb", "dns.cloudflare.com",
}

// expandBlock mở rộng danh sách web cần chặn: bỏ "www." đầu nếu có, thêm biến thể "m." (di động), và
// chèn các máy chủ DoH để trình duyệt không lách qua DNS bảo mật. Agent tự thêm biến thể "www." khi ghi hosts.
func expandBlock(sites []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(d string) {
		d = strings.ToLower(strings.TrimSpace(d))
		if d == "" || seen[d] {
			return
		}
		seen[d] = true
		out = append(out, d)
	}
	for _, s := range sites {
		d := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(s)), "www.")
		if d == "" {
			continue
		}
		add(d)
		add("m." + d)
	}
	if len(out) > 0 { // chỉ chặn DoH khi thật sự có web cần chặn
		for _, h := range dohHosts {
			add(h)
		}
	}
	return out
}

func (s *Server) policyOf(group string) *Policy {
	for _, p := range s.st.Policies {
		if p.Group == group {
			return p
		}
	}
	return nil
}

// policyFor: chính sách gửi cho agent trong mỗi lần báo cáo (luôn gửi, kể cả rỗng, để agent gỡ chính sách cũ). Giữ s.mu.
func (s *Server) policyFor(m *Machine) map[string]interface{} {
	out := map[string]interface{}{"usb": false, "sites": []string{}, "apps": []string{}, "hours": nil}
	p := s.policyOf(m.Group)
	if p == nil {
		return out
	}
	out["usb"] = p.USB
	if p.SitesOn {
		out["sites"] = expandBlock(p.Sites)
	}
	if p.AppsOn {
		out["apps"] = p.Apps
	}
	if p.HoursOn {
		out["hours"] = map[string]string{"from": p.From, "to": p.To}
	}
	return out
}

func cleanList(in []string, re *regexp.Regexp, lower bool) ([]string, bool) {
	seen, out := map[string]bool{}, []string{}
	for _, x := range in {
		x = strings.TrimSpace(x)
		if x == "" {
			continue
		}
		if lower {
			x = strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(x, "https://"), "http://"))
			x = strings.TrimSuffix(strings.SplitN(x, "/", 2)[0], ".")
		}
		if !re.MatchString(x) {
			return nil, false
		}
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
		if len(out) > 100 {
			return nil, false
		}
	}
	return out, true
}

func (s *Server) hPolicies(w http.ResponseWriter, r *http.Request, me *Account) {
	if r.Method == "GET" {
		s.mu.RLock()
		defer s.mu.RUnlock()
		out := s.st.Policies
		if out == nil {
			out = []*Policy{}
		}
		jok(w, out)
		return
	}
	if r.Method != "PUT" || me.Role != "super" {
		jerr(w, 403, "Chỉ Super admin đổi chính sách")
		return
	}
	var in Policy
	if readJSON(r, &in) != nil {
		jerr(w, 400, "Dữ liệu không hợp lệ")
		return
	}
	var ok1, ok2 bool
	if in.Sites, ok1 = cleanList(in.Sites, reDomain, true); !ok1 {
		jerr(w, 400, "Tên miền không hợp lệ (mỗi dòng một tên miền, ví dụ facebook.com)")
		return
	}
	if in.Apps, ok2 = cleanList(in.Apps, reProc, false); !ok2 {
		jerr(w, 400, "Tên tiến trình không hợp lệ (mỗi dòng một tên, ví dụ steam.exe)")
		return
	}
	if in.HoursOn && (!reHHMM.MatchString(in.From) || !reHHMM.MatchString(in.To) || in.From == in.To) {
		jerr(w, 400, "Giờ không hợp lệ (dạng 07:30, hai mốc phải khác nhau)")
		return
	}
	s.mu.Lock()
	if in.Group != "" && !s.groupExists(in.Group) {
		s.mu.Unlock()
		jerr(w, 400, "Nhóm không tồn tại")
		return
	}
	set := false
	for i, p := range s.st.Policies {
		if p.Group == in.Group {
			s.st.Policies[i], set = &in, true
		}
	}
	if !set {
		s.st.Policies = append(s.st.Policies, &in)
	}
	s.save()
	s.mu.Unlock()
	s.auditBy(me, r, "Chính sách", "Cập nhật chính sách nhóm "+nz(in.Group)+": USB "+onoff(in.USB)+", web "+onoff(in.SitesOn)+", ứng dụng "+onoff(in.AppsOn)+", giờ dùng "+onoff(in.HoursOn), nz(in.Group), true)
	jok(w, map[string]bool{"ok": true})
}

func onoff(b bool) string {
	if b {
		return "bật"
	}
	return "tắt"
}

// ---------- phát hiện máy trong LAN ----------

type found struct {
	IP    string `json:"ip"`
	Host  string `json:"host"`
	Ports []int  `json:"ports"`
	Agent string `json:"agent"` // tên máy đã cài agent, rỗng nếu chưa
	Kind  string `json:"kind"`
}

var scanPorts = []int{135, 445, 3389, 22, 80, 443, 5900, 62078}

func privateNet(n *net.IPNet) bool {
	ip := n.IP.To4()
	if ip == nil {
		return false
	}
	return ip[0] == 10 || (ip[0] == 172 && ip[1]&0xf0 == 16) || (ip[0] == 192 && ip[1] == 168)
}

func suggestCIDRs() []string {
	out := []string{}
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && privateNet(n) {
			ip := n.IP.To4()
			out = append(out, net.IPv4(ip[0], ip[1], ip[2], 0).String()+"/24")
		}
	}
	return out
}

func guessKind(p []int) string {
	has := map[int]bool{}
	for _, x := range p {
		has[x] = true
	}
	switch {
	case has[135] || has[445] || has[3389]:
		return "Windows"
	case has[62078]:
		return "iPhone/iPad"
	case has[22] && !has[80]:
		return "Linux/macOS"
	case has[80] || has[443]:
		return "Thiết bị mạng"
	}
	return "Khác"
}

func (s *Server) hDiscover(w http.ResponseWriter, r *http.Request, me *Account) {
	if r.Method == "GET" {
		jok(w, map[string]interface{}{"suggest": suggestCIDRs()})
		return
	}
	var in struct{ CIDR string }
	if r.Method != "POST" || readJSON(r, &in) != nil {
		jerr(w, 400, "Dữ liệu không hợp lệ")
		return
	}
	_, n, err := net.ParseCIDR(strings.TrimSpace(in.CIDR))
	ones, bits := 0, 0
	if err == nil {
		ones, bits = n.Mask.Size()
	}
	if err != nil || bits != 32 || !privateNet(n) {
		jerr(w, 400, "Chỉ quét dải mạng nội bộ dạng 192.168.1.0/24, 10.0.0.0/24 hoặc 172.16.0.0/24")
		return
	}
	if ones < 22 {
		jerr(w, 400, "Dải quá lớn: quét tối đa 1.024 địa chỉ (từ /22 trở xuống)")
		return
	}
	base := n.IP.To4()
	var ips []net.IP
	for i := 0; i < 1<<(32-ones); i++ {
		v := uint32(base[0])<<24 | uint32(base[1])<<16 | uint32(base[2])<<8 | uint32(base[3])
		v += uint32(i)
		ip := net.IPv4(byte(v>>24), byte(v>>16), byte(v>>8), byte(v)).To4()
		if ip[3] != 0 && ip[3] != 255 {
			ips = append(ips, ip)
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	var mu sync.Mutex
	res := []found{}
	sem := make(chan struct{}, 256)
	var wg sync.WaitGroup
	for _, ip := range ips {
		wg.Add(1)
		sem <- struct{}{}
		go func(ip net.IP) {
			defer wg.Done()
			defer func() { <-sem }()
			var open []int
			for _, p := range scanPorts {
				if ctx.Err() != nil {
					return
				}
				c, err := (&net.Dialer{Timeout: 350 * time.Millisecond}).DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), itoa(p)))
				if err == nil {
					c.Close()
					open = append(open, p)
				}
			}
			if len(open) == 0 {
				return
			}
			host := ""
			if names, err := (&net.Resolver{}).LookupAddr(ctx, ip.String()); err == nil && len(names) > 0 {
				host = strings.TrimSuffix(names[0], ".")
			}
			mu.Lock()
			res = append(res, found{IP: ip.String(), Host: host, Ports: open, Kind: guessKind(open)})
			mu.Unlock()
		}(ip)
	}
	wg.Wait()
	s.mu.RLock()
	byIP := map[string]string{}
	for _, m := range s.st.Machines {
		if !m.Revoked && m.IP != "" {
			byIP[m.IP] = m.Name
		}
	}
	s.mu.RUnlock()
	for i := range res {
		res[i].Agent = byIP[res[i].IP]
	}
	sort.Slice(res, func(i, j int) bool { return ipLess(res[i].IP, res[j].IP) })
	s.auditBy(me, r, "Phát hiện máy", "Quét dải "+n.String()+": thấy "+itoa(len(res))+" thiết bị", n.String(), false)
	jok(w, map[string]interface{}{"cidr": n.String(), "scanned": len(ips), "found": res, "partial": ctx.Err() != nil})
}

func ipLess(a, b string) bool {
	x, y := net.ParseIP(a).To4(), net.ParseIP(b).To4()
	for i := 0; i < 4; i++ {
		if x[i] != y[i] {
			return x[i] < y[i]
		}
	}
	return false
}
