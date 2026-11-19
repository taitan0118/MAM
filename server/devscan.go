package main

// Quét dải mạng tìm thiết bị chưa khai (camera, máy chấm công, switch, router) rồi đoán sẵn loại cho
// người dùng tick. KHÔNG tự thêm vào danh sách: để máy tự thêm thì điện thoại, laptop khách, máy in
// cũng nhảy vào, rồi ai đó rời wifi là cảnh báo đỏ giữa đêm — danh sách theo dõi phải do người chốt.

import (
	"context"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Cổng dò khi quét: gộp cổng của thiết bị mạng với cổng máy Windows (để nhận ra máy tính mà bỏ qua).
var scanAllPorts = []int{80, 443, 554, 4370, 8000, 37777, 22, 23, 135, 445, 3389}

type scanHit struct {
	IP     string `json:"ip"`
	MAC    string `json:"mac"`
	Vendor string `json:"vendor"` // hãng đoán theo 3 byte đầu của MAC, rỗng nếu không biết
	Host   string `json:"host"`   // tên từ DNS ngược
	Ports  []int  `json:"ports"`
	Kind   string `json:"kind"`    // camera | attend | switch | router | other | pc
	Why    string `json:"why"`     // vì sao đoán vậy, hiện cho người dùng xem
	Taken  string `json:"taken"`   // đã khai rồi: tên thiết bị; hoặc tên máy đã cài agent
}

// ouiVendor: 3 byte đầu của MAC → hãng. Bảng ngắn, chỉ các hãng hay gặp ở văn phòng Việt Nam; hãng lạ
// thì để trống và đoán loại theo cổng. Đây chỉ là gợi ý cho người dùng, không dùng để quyết định gì.
var ouiVendor = map[string]string{
	"44:19:B6": "Hikvision", "4C:BD:8F": "Hikvision", "C0:56:E3": "Hikvision", "BC:AD:28": "Hikvision", "28:57:BE": "Hikvision",
	"3C:EF:8C": "Dahua", "4C:11:BF": "Dahua", "90:02:A9": "Dahua", "E0:50:8B": "Dahua", "14:A7:8B": "Dahua",
	"00:17:61": "ZKTeco", "48:EA:63": "Uniview", "00:40:8C": "Axis",
	"50:C7:BF": "TP-Link", "98:DA:C4": "TP-Link", "AC:84:C6": "TP-Link", "60:32:B1": "TP-Link", "C0:06:C3": "TP-Link",
	"24:A4:3C": "Ubiquiti", "78:8A:20": "Ubiquiti", "68:D7:9A": "Ubiquiti",
	"4C:5E:0C": "MikroTik", "48:8F:5A": "MikroTik", "2C:C8:1B": "MikroTik",
	"00:0B:86": "Aruba/HPE", "00:1D:AA": "DrayTek", "00:74:9C": "Ruijie", "00:11:32": "Synology",
}

// vendorOf tra 3 byte đầu của MAC (dạng AA:BB:CC) ra tên hãng.
func vendorOf(mac string) string {
	if len(mac) < 8 {
		return ""
	}
	return ouiVendor[mac[:8]]
}

// guessDevKind đoán loại thiết bị từ cổng mở, có trả lời RTSP hay không, và vị trí IP trong dải.
func guessDevKind(ports []int, rtspOK, lastInSubnet bool) (string, string) {
	has := map[int]bool{}
	for _, p := range ports {
		has[p] = true
	}
	switch {
	case rtspOK:
		return "camera", "trả lời đúng giao thức RTSP ở cổng 554"
	case has[4370]:
		return "attend", "mở cổng 4370, cổng riêng của máy chấm công"
	case has[37777] || has[8000]:
		return "camera", "mở cổng riêng của camera/đầu ghi"
	case has[554]:
		return "camera", "mở cổng 554 (RTSP)"
	case has[135] || has[445] || has[3389]:
		return "pc", "là máy tính Windows — nên cài agent thay vì khai làm thiết bị mạng"
	case lastInSubnet && (has[80] || has[443]):
		return "router", "ở địa chỉ cuối dải, thường là router"
	case (has[23] || has[22]) && (has[80] || has[443]):
		return "switch", "có trang quản lý và cổng telnet/SSH"
	case has[80] || has[443]:
		return "other", "chỉ có trang web quản lý, chưa đoán được là gì"
	}
	return "other", "có trả lời nhưng không rõ là thiết bị gì"
}

func (s *Server) hDeviceScan(w http.ResponseWriter, r *http.Request, me *Account) {
	if me.Role != "super" {
		jerr(w, 403, "Chỉ Super admin được làm việc này")
		return
	}
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
	if ones < 24 {
		jerr(w, 400, "Dải quá lớn: quét tối đa 256 địa chỉ (từ /24 trở xuống)")
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

	// IP đã khai làm thiết bị, và IP của máy đã cài agent: vẫn hiện nhưng đánh dấu để khỏi thêm trùng
	s.mu.RLock()
	taken := map[string]string{}
	for _, d := range s.st.Devices {
		taken[d.IP] = d.Name
	}
	for _, m := range s.st.Machines {
		if !m.Revoked && m.IP != "" {
			if _, ok := taken[m.IP]; !ok {
				taken[m.IP] = m.Name
			}
		}
	}
	s.mu.RUnlock()

	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	var mu sync.Mutex
	res := []scanHit{}
	sem := make(chan struct{}, 128)
	var wg sync.WaitGroup
	for _, ip := range ips {
		wg.Add(1)
		sem <- struct{}{}
		go func(ip net.IP) {
			defer wg.Done()
			defer func() { <-sem }()
			var open []int
			for _, p := range scanAllPorts {
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
			// cổng 554 mở chưa chắc là camera; hỏi một câu RTSP là biết chắc
			rtspOK := false
			for _, p := range open {
				if p == 554 {
					rtspOK = rtspAlive(ctx, ip.String(), 554)
				}
			}
			host := ""
			if names, err := (&net.Resolver{}).LookupAddr(ctx, ip.String()); err == nil && len(names) > 0 {
				host = strings.TrimSuffix(names[0], ".")
			}
			mac := arpMAC(ctx, ip.String())
			kind, why := guessDevKind(open, rtspOK, ip[3] == 1 || ip[3] == 254)
			mu.Lock()
			res = append(res, scanHit{IP: ip.String(), MAC: mac, Vendor: vendorOf(mac), Host: host, Ports: open, Kind: kind, Why: why, Taken: taken[ip.String()]})
			mu.Unlock()
		}(ip)
	}
	wg.Wait()
	sort.Slice(res, func(i, j int) bool { return ipLess(res[i].IP, res[j].IP) })
	s.auditBy(me, r, "Thiết bị mạng", "Quét dải "+n.String()+": thấy "+itoa(len(res))+" thiết bị", n.String(), false)
	jok(w, map[string]interface{}{"cidr": n.String(), "scanned": len(ips), "found": res, "partial": ctx.Err() != nil})
}
