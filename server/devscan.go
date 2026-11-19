package main

// Quét dải mạng tìm thiết bị chưa khai (camera, máy chấm công, switch, router) rồi đoán sẵn loại cho
// người dùng tick. KHÔNG tự thêm vào danh sách: để máy tự thêm thì điện thoại, laptop khách, máy in
// cũng nhảy vào, rồi ai đó rời wifi là cảnh báo đỏ giữa đêm — danh sách theo dõi phải do người chốt.
//
// Quét CHẠY NỀN và Console hỏi tiến độ: server cắt mọi phản hồi dài quá 30 giây (WriteTimeout), mà
// quét vài VLAN thì mất vài phút.

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Cổng dò khi quét. Gồm cổng của thiết bị mạng, cổng máy in và cổng máy Windows — hai loại sau dò để
// NHẬN RA mà loại bỏ, chứ không phải để theo dõi.
var scanAllPorts = []int{
	80, 443, 8080, // trang quản lý
	554, 8000, 37777, 34567, // camera và đầu ghi
	4370,   // máy chấm công
	22, 23, // switch, router
	8443, 8291, // bộ điều khiển WiFi (UniFi…) và Winbox của MikroTik
	6000, 5005, 10001, 8899, // bảng LED và thiết bị điều khiển hay dùng mấy cổng này
	9100, 515, 631, // máy in
	135, 445, 3389, // máy tính Windows
}

const (
	// 1024 dải = 4 lớp /16, đủ cho công ty dùng nhiều lớp mạng (172.20.x, 172.24.x, 192.168.x…).
	// Mỗi 256 dải mất khoảng 9 phút nên quét hết 1024 dải là gần nửa tiếng; chạy nền nên không sao.
	scanMaxNets = 1024
	scanSockets = 768 // số kết nối dò cùng lúc cho cả lượt quét
	scanWait    = 300 * time.Millisecond
	scanBudget  = 2 * time.Hour // cả lượt quét không được lâu hơn mức này
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

// cổng trang quản lý thử lần lượt; là biến để test đổi được
var webPorts = []int{80, 8080, 443}

var reTitle = regexp.MustCompile(`(?is)<title[^>]*>([^<]{1,60})`)

// webKhai hỏi trang quản lý của thiết bị xem nó tự khai là gì. Chỉ gọi cho thứ chưa đoán được loại
// (vài chục cái trong cả lượt quét) nên không làm chậm. Thiết bị mạng hay khai tên ở phần đầu
// "Server:" — thử thật trên mạng công ty ra HUAWEI, MoxaHttp, GoAhead-Webs, Embedthis-http.
func webKhai(ctx context.Context, ip string, open []int) string {
	c, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cl := &http.Client{
		Timeout:       3 * time.Second,
		Transport:     &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	for _, p := range webPorts {
		if !hasPort(open, p) {
			continue
		}
		u := "http://" + net.JoinHostPort(ip, itoa(p)) + "/"
		if p == 443 {
			u = "https://" + ip + "/"
		}
		req, err := http.NewRequestWithContext(c, "GET", u, nil)
		if err != nil {
			continue
		}
		resp, err := cl.Do(req)
		if err != nil {
			continue
		}
		sv := strings.TrimSpace(resp.Header.Get("Server"))
		var body []byte
		if sv == "" {
			body, _ = io.ReadAll(io.LimitReader(resp.Body, 16<<10))
		}
		resp.Body.Close()
		if sv != "" {
			return cut(sv, 40)
		}
		if m := reTitle.FindSubmatch(body); m != nil {
			if ti := strings.TrimSpace(string(m[1])); ti != "" && !tieuDeRac(ti) {
				return cut(ti, 40)
			}
		}
	}
	return ""
}

// theoTenKhai sửa lại loại khi chính thiết bị khai tên đủ rõ. Trả loại rỗng nếu tên không nói lên gì.
// Chỉ nhận những chữ đủ đặc trưng, không đoán bừa theo tên hãng (Huawei vừa làm switch vừa làm AP).
func theoTenKhai(ten string) (string, string) {
	t := strings.ToLower(ten)
	switch {
	case t == "":
		return "", ""
	case strings.Contains(t, "unifi") || strings.Contains(t, "ruckus") || strings.Contains(t, "access point") ||
		strings.Contains(t, "wireless") || strings.Contains(t, "wlan") || strings.Contains(t, "airos"):
		return "wifi", "trang quản lý tự khai là thiết bị WiFi"
	case strings.Contains(t, "nport") || strings.Contains(t, "moxa"):
		return "other", "là bộ chuyển cổng COM sang mạng (Moxa), thường nối bảng LED, cân, máy đọc mã"
	}
	return "", ""
}

// tieuDeRac: tiêu đề trang chỉ là câu báo lỗi của web, không phải tên thiết bị.
var reRac = regexp.MustCompile(`(?i)^\s*(\d{3}\b|not found|forbidden|unauthorized|bad request|error|index of)`)

func tieuDeRac(s string) bool { return reRac.MatchString(s) }

func hasPort(ps []int, p int) bool {
	for _, x := range ps {
		if x == p {
			return true
		}
	}
	return false
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
	case has[37777] || has[8000] || has[34567]:
		return "camera", "mở cổng riêng của camera/đầu ghi"
	case has[554]:
		return "camera", "mở cổng 554 (RTSP)"
	case has[9100] || has[515] || has[631]:
		return "printer", "là máy in mạng — không phải thứ cần theo dõi ở đây"
	case has[8291]:
		return "router", "mở cổng 8291 (Winbox) — thiết bị MikroTik"
	case has[8443] && (has[80] || has[443]):
		return "wifi", "mở cổng 8443, hay gặp ở bộ điều khiển WiFi"
	case has[135] || has[445] || has[3389]:
		return "pc", "là máy tính Windows — nên cài agent thay vì khai làm thiết bị mạng"
	case has[6000] || has[5005] || has[10001] || has[8899]:
		return "led", "mở cổng hay gặp ở bảng LED và bộ điều khiển"
	case lastInSubnet && (has[80] || has[443]):
		return "router", "ở địa chỉ cuối dải, thường là router"
	case (has[23] || has[22]) && (has[80] || has[443] || has[8080]):
		return "switch", "có trang quản lý và cổng telnet/SSH"
	case has[80] || has[443] || has[8080]:
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

// suggestAllCIDRs: "quét hết" — mọi lớp /16 mà công ty đang dùng, vì VLAN thường chia theo ô thứ ba.
// Lấy từ card mạng máy chủ, IP của mọi agent và IP thiết bị đã khai; công ty dùng nhiều lớp (172.20.x
// lẫn 172.24.x) thì ra đủ cả. Trả rỗng nếu không biết lớp nào.
func (s *Server) suggestAllCIDRs() []string {
	seen, out := map[string]bool{}, []string{}
	add := func(ip net.IP) {
		v := ip.To4()
		if v == nil || !privateIP(v) {
			return
		}
		k := itoa(int(v[0])) + "." + itoa(int(v[1])) + ".0-255.0/24"
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
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
	sort.Strings(out)
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
		out := map[string]interface{}{"suggest": s.suggestScanCIDRs(), "all": s.suggestAllCIDRs()}
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
			// Dò đủ mọi cổng cho từng địa chỉ. Đã thử cách nhanh hơn là gõ vài cổng hay gặp trước, im
			// lặng thì bỏ qua — nhưng thiết bị chỉ mở cổng lạ (đầu ghi 37777, bảng LED, switch chỉ có
			// telnet) bị sót hẳn. Thà chậm thêm vài phút còn hơn đếm thiếu.
			var open []int
			for _, p := range scanAllPorts {
				if ctx.Err() != nil {
					return
				}
				c, err := (&net.Dialer{Timeout: scanWait}).DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), itoa(p)))
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
			// Hỏi thẳng trang quản lý xem nó tự khai là gì. Làm cho mọi thiết bị mạng (trừ camera,
			// máy tính, máy in — mấy thứ đó chỉ trả chuỗi vô nghĩa) vì chính tên tự khai mới sửa được
			// chỗ đoán sai: bộ chuyển cổng Moxa NPort có web + telnet nên dễ bị xếp nhầm thành switch.
			if hang == "" && kind != "camera" && kind != "pc" && kind != "printer" {
				hang = webKhai(ctx, ip.String(), open)
				if k2, w2 := theoTenKhai(hang); k2 != "" {
					kind, why = k2, w2
				}
			}
			mu.Lock()
			res = append(res, scanHit{IP: ip.String(), MAC: mac, Vendor: hang, Host: host, Ports: open, Kind: kind, Why: why, Taken: taken[ip.String()]})
			mu.Unlock()
		}(ip)
	}
	wg.Wait()
	return res, len(ips)
}
