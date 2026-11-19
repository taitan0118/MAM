package main

// Thiết bị mạng không cài được agent: camera IP, máy chấm công, switch. Chỉ cần biết IP còn sống hay
// không; riêng camera đo thêm luồng hình (rtsp.go) để biết camera còn ra hình chứ không chỉ còn mở cổng.
//
// Nhóm của thiết bị là nhóm riêng, không dùng chung với Nhóm máy: chỉ là chữ trên từng thiết bị, danh
// sách nhóm suy ra từ các giá trị đang có nên không cần trang quản lý nhóm riêng.

import (
	"context"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

type NetDev struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	IP    string `json:"ip"`
	Kind  string `json:"kind"`  // camera | attend | switch | other
	Group string `json:"group"` // nhóm riêng của thiết bị mạng, rỗng = chưa phân nhóm
	Port  int    `json:"port"`  // 0 = thử các cổng mặc định theo loại; camera thì đây là cổng RTSP
	// Camera: đường RTSP + tài khoản để đo luồng hình. Để trống tài khoản = chỉ kiểm sống/chết.
	Path string `json:"path"`
	User string `json:"user"`
	Pass string `json:"pass"` // phải lưu thô vì mỗi lượt đo lại đăng nhập camera; API không trả ra
	MAC  string `json:"mac"`  // MAC lần kiểm đầu, để biết IP có bị thiết bị khác chiếm

	// Trạng thái lượt kiểm gần nhất: chỉ ở RAM, mất khi khởi động lại server (kiểm lại sau 1 phút).
	Up      bool      `json:"-"`
	Video   bool      `json:"-"`
	Kbps    int       `json:"-"`
	Ports   []int     `json:"-"`
	Way     string    `json:"-"` // rtsp | port | ping
	MACNow  string    `json:"-"`
	Checked time.Time `json:"-"`
	Since   time.Time `json:"-"` // từ lúc nào ở trạng thái Up hiện tại
	VidAt   time.Time `json:"-"` // lượt đo hình gần nhất
	Miss    int       `json:"-"` // số lượt liên tiếp không gọi được
	VidMiss int       `json:"-"` // số lượt đo hình liên tiếp không có hình
}

// devAlertID: cảnh báo của thiết bị mạng gắn vào một "máy" giả, giống cách cảnh báo máy chủ dùng __server.
func devAlertID(id string) string { return "__dev-" + id }

// devAlertAfter: chờ ngần này lượt liên tiếp mới báo, để một lần trượt mạng không làm kêu oan.
const devAlertAfter = 2

var devKinds = map[string]bool{"camera": true, "attend": true, "switch": true, "router": true, "led": true, "wifi": true, "other": true}

// devPorts: cổng thử theo loại. Cổng riêng của thiết bị để trước nên ít nhầm với máy khác chiếm IP.
var devPorts = map[string][]int{
	"camera": {554, 80, 8000, 37777},
	"attend": {4370, 80, 443},
	"switch": {80, 443, 22, 23},
	"led":    {6000, 5005, 10001, 8899, 80},
	"wifi":   {80, 443, 22, 8443},
	"router": {80, 443, 22, 23}, // trùng cổng với switch: nhìn cổng không phân biệt được, nên để người dùng khai loại
	"other":  {80, 443, 22},
}

const (
	// Mạng công ty thật có thể tới cả nghìn camera: quét mạng của đại ca ra 677 thiết bị đáng theo dõi.
	devMax      = 5000
	devEvery    = 60 * time.Second // kiểm sống/chết
	devVidEvery = 5 * time.Minute  // đo luồng hình: thưa hơn cho đỡ tốn băng thông
)

func devPortList(d NetDev) []int {
	if d.Port > 0 {
		return []int{d.Port}
	}
	if p := devPorts[d.Kind]; p != nil {
		return p
	}
	return devPorts["other"]
}

func privateIP(ip net.IP) bool {
	v := ip.To4()
	return v != nil && (v[0] == 10 || (v[0] == 172 && v[1]&0xf0 == 16) || (v[0] == 192 && v[1] == 168))
}

// checkDev kiểm một thiết bị: đo hình (camera có tài khoản) → gõ cổng TCP → ping. Không giữ khóa.
func checkDev(ctx context.Context, d NetDev, wantVid bool) NetDev {
	out := d
	out.Up, out.Video, out.Kbps, out.Way, out.Ports = false, false, 0, "", []int{}
	out.Checked = time.Now()
	if wantVid {
		out.VidAt = out.Checked
		if kb, err := rtspKbps(ctx, d.IP, d.Port, d.Path, d.User, d.Pass); err == nil && kb > 0 {
			out.Up, out.Video, out.Kbps, out.Way = true, true, kb, "rtsp"
			out.VidMiss = 0
		} else {
			out.VidMiss = d.VidMiss + 1
		}
	}
	for _, p := range devPortList(d) {
		if ctx.Err() != nil {
			break
		}
		c, err := (&net.Dialer{Timeout: 400 * time.Millisecond}).DialContext(ctx, "tcp", net.JoinHostPort(d.IP, itoa(p)))
		if err == nil {
			c.Close()
			out.Ports = append(out.Ports, p)
		}
	}
	if len(out.Ports) > 0 && !out.Up {
		out.Up, out.Way = true, "port"
	}
	if !out.Up && len(out.Ports) == 0 && pingOK(ctx, d.IP) {
		out.Up, out.Way = true, "ping"
	}
	if out.Up {
		out.MACNow = arpMAC(ctx, d.IP)
	}
	if out.Up {
		out.Miss = 0
	} else {
		out.Miss = d.Miss + 1
	}
	if out.Up != d.Up || d.Since.IsZero() {
		out.Since = out.Checked
	}
	return out
}

// pingOK: ping trên Windows trả mã 0 cả khi "Destination host unreachable", nên phải thấy TTL trong
// kết quả mới tính là sống.
func pingOK(ctx context.Context, ip string) bool {
	args := []string{"-c", "1", "-W", "1", ip}
	if runtime.GOOS == "windows" {
		args = []string{"-n", "1", "-w", "900", ip}
	}
	c, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	out, err := exec.CommandContext(c, "ping", args...).CombinedOutput()
	return err == nil && strings.Contains(strings.ToLower(string(out)), "ttl=")
}

var macRe = regexp.MustCompile(`(?i)\b(?:[0-9a-f]{2}[:-]){5}[0-9a-f]{2}\b`)

// arpMAC đọc MAC từ bảng ARP của hệ điều hành. Chỉ thấy thiết bị cùng dải mạng; qua router trả rỗng.
func arpMAC(ctx context.Context, ip string) string {
	c, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(c, "ip", "neigh", "show", ip)
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(c, "arp", "-a", ip)
	}
	out, _ := cmd.CombinedOutput()
	return strings.ToUpper(strings.ReplaceAll(string(macRe.Find(out)), "-", ":"))
}

// devLoop kiểm toàn bộ thiết bị mạng mỗi phút. Chạy ngay lượt đầu để Console có trạng thái sớm.
func (s *Server) devLoop() {
	for {
		s.checkDevices(context.Background(), nil)
		time.Sleep(devEvery)
	}
}

// checkDevices kiểm các thiết bị (ids rỗng = tất cả). Chụp danh sách dưới khóa, gọi mạng ngoài khóa,
// rồi ghi kết quả lại theo ID: không giữ s.mu trong lúc chờ mạng.
func (s *Server) checkDevices(ctx context.Context, ids map[string]bool) {
	s.mu.RLock()
	var list []NetDev
	for _, d := range s.st.Devices {
		if len(ids) == 0 || ids[d.ID] {
			list = append(list, *d)
		}
	}
	s.mu.RUnlock()
	if len(list) == 0 {
		return
	}
	res := make([]NetDev, len(list))
	// vài trăm thiết bị mà kiểm 16 cái một lúc thì một lượt lâu hơn cả chu kỳ 60 giây
	sem := make(chan struct{}, 128)
	var wg sync.WaitGroup
	now := time.Now()
	for i, d := range list {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, d NetDev) {
			defer wg.Done()
			defer func() { <-sem }()
			vid := d.Kind == "camera" && d.User != "" && (len(ids) > 0 || now.Sub(d.VidAt) >= devVidEvery)
			res[i] = checkDev(ctx, d, vid)
		}(i, d)
	}
	wg.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	by := map[string]NetDev{}
	for _, x := range res {
		by[x.ID] = x
	}
	for _, d := range s.st.Devices {
		x, ok := by[d.ID]
		if !ok || d.IP != x.IP { // đại ca vừa sửa IP trong lúc đang kiểm: bỏ kết quả cũ
			continue
		}
		d.Up, d.Video, d.Kbps, d.Ports, d.Way = x.Up, x.Video, x.Kbps, x.Ports, x.Way
		d.MACNow, d.Checked, d.Since, d.VidAt = x.MACNow, x.Checked, x.Since, x.VidAt
		d.Miss, d.VidMiss = x.Miss, x.VidMiss
		if d.MAC == "" && x.MACNow != "" { // lần đầu kiểm được: ghi nhớ để sau biết IP có bị chiếm
			d.MAC = x.MACNow
			s.save()
		}
		s.devAlerts(d)
	}
}

// devAlerts mở/đóng cảnh báo cho một thiết bị. Gọi khi đang giữ s.mu.
func (s *Server) devAlerts(d *NetDev) {
	m := &Machine{ID: devAlertID(d.ID), Name: d.Name}
	s.cond(m, "devdown", "crit", d.Miss >= devAlertAfter, d.Name+" ("+d.IP+") mất kết nối")
	// camera còn sống mà không kéo được hình: máy vẫn đó nhưng không quay, vẫn phải báo
	noVid := d.Up && d.Kind == "camera" && d.User != "" && d.VidMiss >= devAlertAfter
	s.cond(m, "devvideo", "warn", noVid, d.Name+" ("+d.IP+") còn sống nhưng không ra hình")
}

// devAlertsClose đóng mọi cảnh báo của một thiết bị (lúc xóa thiết bị). Gọi khi đang giữ s.mu.
func (s *Server) devAlertsClose(d *NetDev) {
	m := &Machine{ID: devAlertID(d.ID), Name: d.Name}
	s.cond(m, "devdown", "crit", false, "")
	s.cond(m, "devvideo", "warn", false, "")
}

// devView: dữ liệu trả cho Console. Không trả mật khẩu camera.
func devView(d *NetDev) map[string]interface{} {
	p := d.Ports
	if p == nil {
		p = []int{}
	}
	return map[string]interface{}{
		"id": d.ID, "name": d.Name, "ip": d.IP, "kind": d.Kind, "group": d.Group, "port": d.Port,
		"path": d.Path, "user": d.User, "hasPass": d.Pass != "", "mac": d.MAC, "macNow": d.MACNow,
		"up": d.Up, "video": d.Video, "kbps": d.Kbps, "ports": p, "way": d.Way,
		"checked": d.Checked, "since": d.Since, "vidAt": d.VidAt, "done": !d.Checked.IsZero(),
	}
}

func (s *Server) hDevices(w http.ResponseWriter, r *http.Request, me *Account) {
	switch r.Method {
	case "GET":
		s.mu.RLock()
		defer s.mu.RUnlock()
		out := []map[string]interface{}{} // [] chứ không null, nếu không Console lỗi khi vẽ danh sách
		for _, d := range s.st.Devices {
			out = append(out, devView(d))
		}
		jok(w, out)
	case "POST", "PUT":
		if me.Role != "super" {
			jerr(w, 403, "Chỉ Super admin được làm việc này")
			return
		}
		var in NetDev
		if readJSON(r, &in) != nil {
			jerr(w, 400, "Dữ liệu không hợp lệ")
			return
		}
		in.Name = strings.TrimSpace(clip(in.Name, 60))
		in.Group = strings.TrimSpace(clip(in.Group, 40))
		in.Path = strings.TrimSpace(clip(in.Path, 120))
		in.User = strings.TrimSpace(clip(in.User, 60))
		in.IP = strings.TrimSpace(in.IP)
		if in.Name == "" {
			jerr(w, 400, "Nhập tên thiết bị")
			return
		}
		ip := net.ParseIP(in.IP)
		if ip == nil || !privateIP(ip) {
			jerr(w, 400, "IP phải là địa chỉ nội bộ, dạng 192.168.1.50")
			return
		}
		in.IP = ip.String()
		if !devKinds[in.Kind] {
			jerr(w, 400, "Chọn loại thiết bị")
			return
		}
		if in.Port < 0 || in.Port > 65535 {
			jerr(w, 400, "Cổng phải từ 1 đến 65535")
			return
		}
		s.mu.Lock()
		for _, d := range s.st.Devices {
			if d.IP == in.IP && d.ID != in.ID {
				s.mu.Unlock()
				jerr(w, 400, "IP này đã có trong danh sách")
				return
			}
		}
		if r.Method == "PUT" {
			for _, d := range s.st.Devices {
				if d.ID != in.ID {
					continue
				}
				if d.IP != in.IP { // IP đổi thì MAC và trạng thái cũ không còn ý nghĩa
					d.MAC, d.MACNow, d.Up, d.Ports, d.Video, d.Kbps = "", "", false, nil, false, 0
					d.Checked, d.Since, d.VidAt = time.Time{}, time.Time{}, time.Time{}
				}
				d.Name, d.IP, d.Kind, d.Group, d.Port = in.Name, in.IP, in.Kind, in.Group, in.Port
				d.Path, d.User = in.Path, in.User
				if in.Pass != "" { // để trống = giữ mật khẩu cũ
					d.Pass = in.Pass
				}
				s.save()
				s.mu.Unlock()
				s.auditBy(me, r, "Thiết bị mạng", "Sửa thiết bị "+d.Name+" ("+d.IP+")", d.Name, false)
				go s.checkDevices(context.Background(), map[string]bool{d.ID: true})
				jok(w, map[string]bool{"ok": true})
				return
			}
			s.mu.Unlock()
			jerr(w, 404, "Không có thiết bị này")
			return
		}
		if len(s.st.Devices) >= devMax {
			s.mu.Unlock()
			jerr(w, 400, "Tối đa 5000 thiết bị mạng")
			return
		}
		d := &NetDev{ID: rid(5), Name: in.Name, IP: in.IP, Kind: in.Kind, Group: in.Group, Port: in.Port, Path: in.Path, User: in.User, Pass: in.Pass}
		s.st.Devices = append(s.st.Devices, d)
		s.save()
		s.mu.Unlock()
		s.auditBy(me, r, "Thiết bị mạng", "Thêm thiết bị "+d.Name+" ("+d.IP+")", d.Name, false)
		go s.checkDevices(context.Background(), map[string]bool{d.ID: true})
		jok(w, devView(d))
	case "DELETE":
		if me.Role != "super" {
			jerr(w, 403, "Chỉ Super admin được làm việc này")
			return
		}
		id := r.URL.Query().Get("id")
		s.mu.Lock()
		for i, d := range s.st.Devices {
			if d.ID != id {
				continue
			}
			s.st.Devices = append(s.st.Devices[:i], s.st.Devices[i+1:]...)
			s.devAlertsClose(d)
			s.save()
			s.mu.Unlock()
			s.auditBy(me, r, "Thiết bị mạng", "Xóa thiết bị "+d.Name+" ("+d.IP+")", d.Name, false)
			jok(w, map[string]bool{"ok": true})
			return
		}
		s.mu.Unlock()
		jerr(w, 404, "Không có thiết bị này")
	default:
		jerr(w, 405, "Phương thức không hỗ trợ")
	}
}

// hDeviceCheck: bấm "Kiểm lại" trên Console. Không id = kiểm tất cả, kèm đo hình ngay.
func (s *Server) hDeviceCheck(w http.ResponseWriter, r *http.Request, me *Account) {
	if r.Method != "POST" {
		jerr(w, 405, "Phương thức không hỗ trợ")
		return
	}
	if me.Role == "view" {
		jerr(w, 403, "Tài khoản Chỉ xem không có quyền thao tác")
		return
	}
	var ids map[string]bool
	if id := r.URL.Query().Get("id"); id != "" {
		ids = map[string]bool{id: true}
	} else {
		s.mu.RLock()
		ids = map[string]bool{}
		for _, d := range s.st.Devices {
			ids[d.ID] = true
		}
		s.mu.RUnlock()
		if len(ids) == 0 {
			jok(w, []map[string]interface{}{})
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	s.checkDevices(ctx, ids)
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []map[string]interface{}{}
	for _, d := range s.st.Devices {
		out = append(out, devView(d))
	}
	jok(w, out)
}

