package main

// Quét dải mạng tìm thiết bị chưa khai (camera, máy chấm công, switch, router) rồi đoán sẵn loại cho
// người dùng tick. KHÔNG tự thêm vào danh sách: để máy tự thêm thì điện thoại, laptop khách, máy in
// cũng nhảy vào, rồi ai đó rời wifi là cảnh báo đỏ giữa đêm — danh sách theo dõi phải do người chốt.
//
// Quét CHẠY NỀN và Console hỏi tiến độ: server cắt mọi phản hồi dài quá 30 giây (WriteTimeout), mà
// quét vài VLAN thì mất vài phút.

import (
	"context"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Cổng dò khi quét: gộp cổng của thiết bị mạng với cổng máy Windows (để nhận ra máy tính mà bỏ qua).
var scanAllPorts = []int{80, 443, 554, 4370, 8000, 37777, 22, 23, 135, 445, 3389}

// Bước dò nhanh: địa chỉ nào không trả lời một trong mấy cổng này thì coi như trống, khỏi dò tiếp.
// Gồm cổng web, RTSP của camera, 4370 của máy chấm công và 445 của máy Windows.
var scanFirstPorts = []int{80, 445, 554, 443, 4370}

// goCua gõ lần lượt mấy cổng, trúng cổng nào là dừng.
func goCua(ctx context.Context, ip net.IP, ports []int, wait time.Duration) bool {
	for _, p := range ports {
		if ctx.Err() != nil {
			return false
		}
		if c, err := (&net.Dialer{Timeout: wait}).DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), itoa(p))); err == nil {
			c.Close()
			return true
		}
	}
	return false
}

const (
	scanMaxNets = 200              // tối đa bấy nhiêu dải một lượt (200 × 254 ≈ 50.000 địa chỉ)
	scanSockets = 512              // số kết nối dò cùng lúc cho cả lượt quét
	scanBudget  = 30 * time.Minute // cả lượt quét không được lâu hơn mức này
)

type scanHit struct {
	IP     string `json:"ip"`
	MAC    string `json:"mac"`
	Vendor string `json:"vendor"` // hãng đoán theo 3 byte đầu của MAC, rỗng nếu không biết
	Host   string `json:"host"`   // tên từ DNS ngược
	Ports  []int  `json:"ports"`
	Kind   string `json:"kind"`  // camera | attend | switch | router | other | pc
	Why    string `json:"why"`   // vì sao đoán vậy, hiện cho người dùng xem
	Taken  string `json:"taken"` // đã khai rồi: tên thiết bị; hoặc tên máy đã cài agent
}

// scanJob: tình trạng lượt quét đang chạy. Chỉ ở RAM, mất khi khởi động lại server.
type scanJob struct {
	Running bool      `json:"running"`
	Nets    []string  `json:"nets"`    // các dải sẽ quét
	Done    int       `json:"done"`    // số dải đã quét xong
	Now     string    `json:"now"`     // dải đang quét
	Scanned int       `json:"scanned"` // số địa chỉ đã dò
	Found   []scanHit `json:"found"`
	Total   int       `json:"total"` // tổng số đã thấy (Found có thể chỉ là phần mới, xem ?since=)
	Err     string    `json:"err"`
	By      string    `json:"by"`
	At      time.Time `json:"at"`
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

// parseCIDRs đọc danh sách dải người dùng nhập (cách nhau bằng dấu phẩy, xuống dòng hay khoảng trắng).
// Nhận mọi dải nội bộ: 10.x, 172.16–31.x, 192.168.x.
func parseCIDRs(s string) ([]*net.IPNet, string) {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == '\n' || r == '\r' || r == ' ' || r == '\t' })
	out, seen := []*net.IPNet{}, map[string]bool{}
	for _, p0 := range parts {
		dai, bad := traiRa(p0)
		if bad != "" {
			return nil, bad
		}
		for _, p := range dai {
			_, n, err := net.ParseCIDR(p)
			ones, bits := 0, 0
			if err == nil {
				ones, bits = n.Mask.Size()
			}
			if err != nil || bits != 32 || !privateNet(n) {
				return nil, "Dải không hợp lệ: " + clip(p, 40) + ". Chỉ quét dải nội bộ dạng 192.168.1.0/24, 10.0.0.0/24 hoặc 172.20.40.0/24"
			}
			if ones < 24 {
				return nil, "Dải quá lớn: " + n.String() + ". Mỗi dải tối đa 256 địa chỉ (từ /24 trở xuống)"
			}
			if !seen[n.String()] {
				seen[n.String()] = true
				out = append(out, n)
			}
			if len(out) > scanMaxNets {
				return nil, "Tối đa " + itoa(scanMaxNets) + " dải một lượt"
			}
		}
	}
	if len(out) == 0 {
		return nil, "Nhập ít nhất một dải mạng"
	}
	return out, ""
}

// traiRa mở cú pháp dải liên tiếp "172.20.5-200.0/24" thành từng dải một, để khỏi phải gõ 196 dòng.
// Chuỗi không có dấu gạch ở ô thứ ba thì trả lại nguyên.
func traiRa(p string) ([]string, string) {
	i := strings.IndexByte(p, '/')
	if i < 0 {
		return []string{p}, ""
	}
	o := strings.Split(p[:i], ".")
	if len(o) != 4 || !strings.Contains(o[2], "-") {
		return []string{p}, ""
	}
	hai := strings.SplitN(o[2], "-", 2)
	tu, err1 := strconv.Atoi(hai[0])
	den, err2 := strconv.Atoi(hai[1])
	if err1 != nil || err2 != nil || tu < 0 || den > 255 || tu > den {
		return nil, "Dải liên tiếp không hợp lệ: " + clip(p, 40) + ". Viết kiểu 172.20.5-200.0/24"
	}
	out := make([]string, 0, den-tu+1)
	for v := tu; v <= den; v++ {
		out = append(out, o[0]+"."+o[1]+"."+itoa(v)+"."+o[3]+p[i:])
	}
	return out, ""
}

// suggestScanCIDRs: các dải /24 đáng quét — dải của card mạng máy chủ, cộng dải của mọi máy đã cài
// agent (máy ở VLAN nào thì thường camera cũng ở VLAN đó). Gọi khi KHÔNG giữ s.mu.
func (s *Server) suggestScanCIDRs() []string {
	seen := map[string]bool{}
	add := func(ip net.IP) {
		v := ip.To4()
		if v == nil || !privateIP(v) {
			return
		}
		seen[net.IPv4(v[0], v[1], v[2], 0).String()+"/24"] = true
	}
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok {
			add(n.IP)
		}
	}
	s.mu.RLock()
	for _, m := range s.st.Machines {
		if !m.Revoked && m.IP != "" {
			add(net.ParseIP(m.IP))
		}
	}
	for _, d := range s.st.Devices {
		add(net.ParseIP(d.IP))
	}
	s.mu.RUnlock()
	out := []string{}
	for c := range seen {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return ipLess(strings.Split(out[i], "/")[0], strings.Split(out[j], "/")[0]) })
	return out
}

func (s *Server) hDeviceScan(w http.ResponseWriter, r *http.Request, me *Account) {
	if me.Role != "super" {
		jerr(w, 403, "Chỉ Super admin được làm việc này")
		return
	}
	if r.Method == "GET" {
		// ?since=N: chỉ lấy phần mới từ vị trí N, khỏi gửi lại cả nghìn dòng mỗi 2 giây
		from, _ := strconv.Atoi(r.URL.Query().Get("since"))
		s.scanMu.Lock()
		var view *scanJob
		if s.scan != nil {
			c := *s.scan
			c.Total = len(s.scan.Found)
			if from > 0 && from <= c.Total {
				c.Found = append([]scanHit{}, s.scan.Found[from:]...)
			} else {
				c.Found = append([]scanHit{}, s.scan.Found...)
			}
			view = &c
		}
		s.scanMu.Unlock()
		out := map[string]interface{}{"suggest": s.suggestScanCIDRs()}
		if view != nil {
			out["job"] = view
		}
		jok(w, out)
		return
	}
	var in struct{ CIDR string }
	if r.Method != "POST" || readJSON(r, &in) != nil {
		jerr(w, 400, "Dữ liệu không hợp lệ")
		return
	}
	nets, bad := parseCIDRs(in.CIDR)
	if bad != "" {
		jerr(w, 400, bad)
		return
	}
	s.scanMu.Lock()
	if s.scan != nil && s.scan.Running {
		s.scanMu.Unlock()
		jerr(w, 409, "Đang có một lượt quét chạy, chờ xong đã")
		return
	}
	names := make([]string, len(nets))
	for i, n := range nets {
		names[i] = n.String()
	}
	j := &scanJob{Running: true, Nets: names, Found: []scanHit{}, By: me.User, At: time.Now()}
	s.scan = j
	s.scanMu.Unlock()
	s.auditBy(me, r, "Thiết bị mạng", "Quét "+itoa(len(names))+" dải: "+clip(strings.Join(names, ", "), 200), "—", false)
	go s.runScan(j, nets)
	jok(w, map[string]interface{}{"started": true, "job": j})
}

// runScan quét lần lượt từng dải, cập nhật tiến độ để Console hỏi lại.
func (s *Server) runScan(j *scanJob, nets []*net.IPNet) {
	ctx, cancel := context.WithTimeout(context.Background(), scanBudget)
	defer cancel()
	defer func() {
		s.scanMu.Lock()
		j.Running, j.Now = false, ""
		s.scanMu.Unlock()
	}()

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

	// Quét vài dải cùng lúc, dùng chung một hạn mức socket: quét từng dải một thì 196 dải mất hàng giờ.
	sem := make(chan struct{}, scanSockets)
	netSem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for _, n := range nets {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		netSem <- struct{}{}
		s.scanMu.Lock()
		j.Now = n.String()
		s.scanMu.Unlock()
		go func(n *net.IPNet) {
			defer wg.Done()
			defer func() { <-netSem }()
			hits, scanned := scanSubnet(ctx, n, taken, sem)
			s.scanMu.Lock()
			// chỉ nối thêm, không sắp lại: Console lấy kết quả theo vị trí (?since=) nên thứ tự phải
			// giữ nguyên; sắp xếp để Console tự làm lúc vẽ.
			j.Found = append(j.Found, hits...)
			j.Scanned += scanned
			j.Done++
			s.scanMu.Unlock()
		}(n)
	}
	wg.Wait()
	if ctx.Err() != nil {
		s.scanMu.Lock()
		j.Err = "Hết thời gian, chưa quét xong"
		s.scanMu.Unlock()
	}
}

// scanSubnet dò hết một dải /24, trả các địa chỉ có trả lời và số địa chỉ đã dò.
func scanSubnet(ctx context.Context, n *net.IPNet, taken map[string]string, sem chan struct{}) ([]scanHit, int) {
	ones, _ := n.Mask.Size()
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
	var mu sync.Mutex
	res := []scanHit{}
	var wg sync.WaitGroup
	for _, ip := range ips {
		wg.Add(1)
		sem <- struct{}{}
		go func(ip net.IP) {
			defer wg.Done()
			defer func() { <-sem }()
			// Hai bước: gõ vài cổng hay gặp nhất cho nhanh, im lặng thì bỏ qua luôn. Hầu hết địa chỉ
			// trong một dải là trống, nên bước này cắt phần lớn công việc.
			if !goCua(ctx, ip, scanFirstPorts, 200*time.Millisecond) {
				return
			}
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
			rtspOK, tuKhai := false, ""
			for _, p := range open {
				if p == 554 {
					rtspOK, tuKhai = rtspAlive(ctx, ip.String(), 554)
				}
			}
			dctx, dcancel := context.WithTimeout(ctx, time.Second) // mạng không có DNS thì đừng ngồi đợi
			host := ""
			if names, err := (&net.Resolver{}).LookupAddr(dctx, ip.String()); err == nil && len(names) > 0 {
				host = strings.TrimSuffix(names[0], ".")
			}
			dcancel()
			kind, why := guessDevKind(open, rtspOK, ip[3] == 1 || ip[3] == 254)
			mac := ""
			if kind != "pc" { // chỉ cần MAC cho thứ có thể thêm vào; đọc ARP là chạy một tiến trình, đắt
				mac = arpMAC(ctx, ip.String())
			}
			hang := vendorOf(mac)
			if hang == "" { // bảng mã hãng chỉ có ít hãng; camera tự khai tên thì tin camera
				hang = tuKhai
			}
			mu.Lock()
			res = append(res, scanHit{IP: ip.String(), MAC: mac, Vendor: hang, Host: host, Ports: open, Kind: kind, Why: why, Taken: taken[ip.String()]})
			mu.Unlock()
		}(ip)
	}
	wg.Wait()
	return res, len(ips)
}
