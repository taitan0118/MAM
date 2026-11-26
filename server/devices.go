package main

// Thiết bị mạng không cài được agent: camera IP, máy chấm công, switch. Chỉ cần biết IP còn sống hay
// không; riêng camera đo thêm luồng hình (rtsp.go) để biết camera còn ra hình chứ không chỉ còn mở cổng.
//
// Nhóm của thiết bị là nhóm riêng, không dùng chung với Nhóm máy: chỉ là chữ trên từng thiết bị, danh
// sách nhóm suy ra từ các giá trị đang có nên không cần trang quản lý nhóm riêng.

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type NetDev struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Tên tiếng Trung lấy nguyên từ file gốc; Console để tiếng Trung thì hiện tên này.
	NameZH string `json:"nameZh,omitempty"`
	IP     string `json:"ip"`
	Kind   string `json:"kind"` // camera | attend | switch | other
	// Một thiết bị có thể thuộc NHIỀU nhóm: camera khu Hải Quan vừa nằm trong danh sách Hải Quan
	// vừa nằm trong danh sách chung của công ty, phải đếm được ở cả hai chỗ.
	Groups []string `json:"groups"`
	Group  string   `json:"group,omitempty"` // kiểu cũ một nhóm; chỉ còn để đọc dữ liệu bản cũ
	Port   int      `json:"port"`            // 0 = thử các cổng mặc định theo loại; camera thì đây là cổng RTSP
	// Camera: đường RTSP + tài khoản để đo luồng hình. Để trống tài khoản = chỉ kiểm sống/chết.
	Path string `json:"path"`
	User string `json:"user"`
	Pass string `json:"pass"` // phải lưu thô vì mỗi lượt đo lại đăng nhập camera; API không trả ra
	MAC  string `json:"mac"`  // MAC lần kiểm đầu, để biết IP có bị thiết bị khác chiếm
	// Đường ảnh JPEG đã dò ra được của camera này, để lần sau khỏi thử lại cả danh sách.
	SnapPath string `json:"snapPath,omitempty"`

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
	SaiTK   bool      `json:"-"` // camera từ chối tài khoản: chưa biết có hình hay không
}

// devAlertID: cảnh báo của thiết bị mạng gắn vào một "máy" giả, giống cách cảnh báo máy chủ dùng __server.
func devAlertID(id string) string { return "__dev-" + id }

// DevEvent: một mốc đổi trạng thái của thiết bị mạng, để biết nó rớt lúc nào, sống lại lúc nào.
// Mỗi mốc một dòng riêng trong mam.db (id không đổi) nên chỉ ghi một lần, không ghi đè mỗi phút.
type DevEvent struct {
	ID  string    `json:"id"` // <id thiết bị>/<thời điểm>
	Dev string    `json:"dev"`
	Up  bool      `json:"up"`
	At  time.Time `json:"at"`
	// Trạng thái cụ thể: "up" | "down" | "novideo". Thiếu (dữ liệu cũ) thì suy từ Up.
	// Có nó mới phân biệt được thiết bị MẤT KẾT NỐI với camera còn sống mà TREO không ra hình.
	Trang string `json:"trang,omitempty"`
}

// devTrang: trạng thái để ghi vào lịch sử.
func devTrang(up, doHinh, coHinh bool) string {
	if !up {
		return "down"
	}
	if doHinh && !coHinh {
		return "novideo"
	}
	return "up"
}

const devLogMax = 50000 // giữ chừng này mốc; đầy thì bỏ phần cũ nhất

// mocCuoi: trạng thái của mốc gần nhất đã ghi cho thiết bị, rỗng nếu chưa có mốc nào.
// Gọi khi đang giữ s.mu.
func (s *Server) mocCuoi(id string) string {
	for i := len(s.st.DevLog) - 1; i >= 0; i-- {
		if e := s.st.DevLog[i]; e.Dev == id {
			return mocTrang(e)
		}
	}
	return ""
}

func mocTrang(e *DevEvent) string {
	if e.Trang != "" {
		return e.Trang
	}
	if e.Up { // dữ liệu ghi trước 1.1.90 chỉ có Up
		return "up"
	}
	return "down"
}

// donMocTreoGia bỏ các mốc "treo không ra hình" GIẢ mà lỗi sửa ở 1.1.98 đẻ ra: luồng hình chỉ đo 5
// phút một lần, mà lượt nào không đo cũng xóa kết quả đo cũ, nên cứ sau mỗi lượt đo lại sinh một cặp
// mốc "novideo" rồi "up" giả. Dấu nhận: một mốc novideo kẹp giữa hai mốc up và chỉ kéo dài chưa tới
// một chu kỳ đo. Bỏ xong thì mốc up phía sau thành trùng với mốc up phía trước, nên gộp luôn.
//
//	shortcut: một lần treo THẬT mà ngắn hơn một chu kỳ đo cũng bị dọn theo — không có cách nào phân
//	biệt, và treo ngắn như vậy thì cũng chưa tới mức phải báo. Nâng cấp khi nào ghi thêm lý do vào
//	từng mốc: lúc đó lọc theo lý do thay vì theo độ dài.
//
// Gọi khi đang giữ s.mu.
func (s *Server) donMocTreoGia() int {
	han := devVidEvery + devEvery // cho rộng ra một lượt kiểm, phòng server chạy trễ
	ke := make([]*DevEvent, len(s.st.DevLog))
	cuoi := map[string]int{}
	for i := len(s.st.DevLog) - 1; i >= 0; i-- { // mốc kế tiếp của CÙNG thiết bị
		if j, ok := cuoi[s.st.DevLog[i].Dev]; ok {
			ke[i] = s.st.DevLog[j]
		}
		cuoi[s.st.DevLog[i].Dev] = i
	}
	truoc := map[string]string{} // trạng thái của mốc đã GIỮ gần nhất, theo từng thiết bị
	giu := make([]*DevEvent, 0, len(s.st.DevLog))
	n := 0
	for i, e := range s.st.DevLog {
		tr := mocTrang(e)
		gia := tr == "novideo" && truoc[e.Dev] == "up" && ke[i] != nil &&
			mocTrang(ke[i]) == "up" && ke[i].At.Sub(e.At) < han
		if gia || tr == truoc[e.Dev] {
			n++
			continue
		}
		truoc[e.Dev] = tr
		giu = append(giu, e)
	}
	s.st.DevLog = giu
	return n
}

// ghiMoc ghi lại lúc thiết bị đổi trạng thái. Gọi khi đang giữ s.mu.
func (s *Server) ghiMoc(d *NetDev, trang string, at time.Time) {
	s.st.DevLog = append(s.st.DevLog, &DevEvent{
		ID: d.ID + "/" + strconv.FormatInt(at.UnixNano(), 36), Dev: d.ID, Up: trang != "down", At: at, Trang: trang,
	})
	if len(s.st.DevLog) > devLogMax {
		s.st.DevLog = append([]*DevEvent{}, s.st.DevLog[len(s.st.DevLog)-devLogMax*9/10:]...)
	}
}

// devAlertAfter: chờ ngần này lượt liên tiếp mới báo, để một lần trượt mạng không làm kêu oan.
const devAlertAfter = 2

// nhomCua trả danh sách nhóm của thiết bị, tự chuyển dữ liệu kiểu cũ (một nhóm) sang kiểu mới.
func nhomCua(d *NetDev) []string {
	if len(d.Groups) == 0 && d.Group != "" {
		d.Groups, d.Group = []string{d.Group}, ""
	}
	if d.Groups == nil {
		return []string{}
	}
	return d.Groups
}

// donNhom: bỏ khoảng trắng thừa, bỏ trùng, bỏ rỗng; tối đa 10 nhóm cho một thiết bị.
func donNhom(in []string) []string {
	out, seen := []string{}, map[string]bool{}
	for _, g := range in {
		g = strings.TrimSpace(clip(g, 40))
		if g == "" || seen[g] {
			continue
		}
		seen[g] = true
		out = append(out, g)
		if len(out) >= 10 {
			break
		}
	}
	return out
}

// themNhom thêm các nhóm vào thiết bị, giữ nguyên nhóm cũ. Trả true nếu có thay đổi.
func themNhom(d *NetDev, g []string) bool {
	cu := nhomCua(d)
	moi := donNhom(append(append([]string{}, cu...), g...))
	if len(moi) == len(cu) {
		return false
	}
	d.Groups = moi
	return true
}

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
	out.Up, out.Way, out.Ports = false, "", []int{}
	out.Checked = time.Now()
	// Luồng hình chỉ đo 5 phút một lần. Lượt nào KHÔNG đo thì phải giữ nguyên kết quả đo gần nhất:
	// xóa đi là suốt 4 phút kế mọi camera đều thành "sống nhưng không ra hình", và tệ hơn nữa là
	// ghi một mốc "treo không ra hình" giả vào lịch sử hoạt động sau mỗi lượt đo.
	if wantVid {
		out.Video, out.Kbps, out.SaiTK = false, 0, false
		out.VidAt = out.Checked
		kb, err := rtspKbps(ctx, d.IP, d.Port, d.Path, d.User, d.Pass)
		switch {
		case err == nil && kb > 0:
			out.Up, out.Video, out.Kbps, out.Way = true, true, kb, "rtsp"
			out.VidMiss = 0
		case errors.Is(err, errSaiMatKhau) || errors.Is(err, errChuaKhai):
			// Camera không cho vào thì KHÔNG biết nó có ra hình hay không. Tính là "mất hình" ở đây
			// là vu oan: báo động giả, ghi mốc treo giả, mà lỗi thật (sai tài khoản) thì không ai thấy.
			out.SaiTK = true
		default:
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
	// Lượt kiểm bị cắt giữa chừng (Console ngắt, quá hạn) thì mọi thứ đều "không gọi được" — kết luận
	// rớt lúc này là ghi mốc rớt giả vào lịch sử. Bỏ nguyên lượt, giữ trạng thái cũ.
	if !out.Up && ctx.Err() != nil {
		return d
	}
	if !out.Up {
		out.Video, out.Kbps = false, 0 // không gọi được thì chắc chắn không có hình
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
	cu, cp := s.st.Settings.CamUser, s.st.Settings.CamPass
	for _, d := range s.st.Devices {
		if len(ids) == 0 || ids[d.ID] {
			x := *d
			if x.Kind == "camera" && x.User == "" && cu != "" {
				x.User, x.Pass = cu, cp // tài khoản camera dùng chung: khỏi khai từng cái một
			}
			list = append(list, x)
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
			// chỉ ép đo hình khi đại ca bấm "Kiểm lại" đúng một thiết bị; kiểm hàng loạt mà ép đo
			// cả mấy trăm camera (12 giây mỗi cái) thì quá hạn 30 giây rồi bị cắt giữa chừng
			vid := d.Kind == "camera" && d.User != "" && (len(ids) == 1 || now.Sub(d.VidAt) >= devVidEvery)
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
	// một switch chết kéo cả chục camera rớt cùng lúc: lưu từng cái một thì mỗi lần lưu là ghi lại
	// toàn bộ dữ liệu, Console đứng hình. Gom lại lưu một lần.
	luu := false
	for _, d := range s.st.Devices {
		x, ok := by[d.ID]
		if !ok || d.IP != x.IP { // đại ca vừa sửa IP trong lúc đang kiểm: bỏ kết quả cũ
			continue
		}
		// So với MỐC GẦN NHẤT ĐÃ GHI, không so với trạng thái trong RAM: trạng thái RAM mất khi
		// khởi động lại server, nên kiểu cũ đẻ ra một mốc "Hoạt động" giả sau mỗi lần nâng cấp.
		if tr := devTrang(x.Up, s.doHinh(d), x.Video); tr != s.mocCuoi(d.ID) {
			s.ghiMoc(d, tr, x.Checked)
			luu = true
		}
		d.Up, d.Video, d.Kbps, d.Ports, d.Way = x.Up, x.Video, x.Kbps, x.Ports, x.Way
		d.MACNow, d.Checked, d.Since, d.VidAt = x.MACNow, x.Checked, x.Since, x.VidAt
		d.Miss, d.VidMiss = x.Miss, x.VidMiss
		if d.MAC == "" && x.MACNow != "" { // lần đầu kiểm được: ghi nhớ để sau biết IP có bị chiếm
			d.MAC = x.MACNow
			luu = true
		}
		s.devAlerts(d)
	}
	if luu {
		s.save()
	}
}

// doHinh: thiết bị này có đang được đo luồng hình không (là camera, và có tài khoản riêng hoặc dùng
// tài khoản camera chung). Gọi khi đang giữ s.mu.
func (s *Server) doHinh(d *NetDev) bool {
	return d.Kind == "camera" && (d.User != "" || s.st.Settings.CamUser != "")
}

// devAlerts mở/đóng cảnh báo cho một thiết bị. Gọi khi đang giữ s.mu.
func (s *Server) devAlerts(d *NetDev) {
	s.mocCanhBao(d, "devdown", "crit", d.Miss >= devAlertAfter, "mất kết nối")
	// camera còn sống mà không kéo được hình: máy vẫn đó nhưng không quay, vẫn phải báo
	noVid := d.Up && s.doHinh(d) && d.VidMiss >= devAlertAfter
	s.mocCanhBao(d, "devvideo", "warn", noVid, "không ra hình")
	// camera bị tháo, cắm máy khác vào đúng IP: cổng vẫn mở nên vẫn xanh, chỉ MAC là lộ ra.
	// ARP chỉ thấy thiết bị cùng dải mạng với máy chủ, qua router trả rỗng — nên phải có MACNow mới xét.
	s.mocCanhBao(d, "devmac", "warn", d.MAC != "" && d.MACNow != "" && d.MAC != d.MACNow, "IP đang là thiết bị khác (MAC đổi)")
}

// Chữ đánh dấu cảnh báo đã hết hỏng nhưng còn chờ người xác nhận, và chữ đánh dấu nó rớt lại.
const daCoLai = " · đã có lại lúc "
const roiLai = " · rớt lại lúc "

// mocCanhBao mở cảnh báo khi thiết bị hỏng, và KHÔNG tự đóng khi nó tốt lại — chỉ ghi thêm giờ phục
// hồi rồi để đó chờ bấm "Đã xử lý". Thiết bị rớt lúc nửa đêm rồi tự lên lại thì sáng ra vẫn thấy.
// Gọi khi đang giữ s.mu.
func (s *Server) mocCanhBao(d *NetDev, kind, sev string, hong bool, viec string) {
	var mo *Alert
	id := devAlertID(d.ID)
	for i := len(s.st.Alerts) - 1; i >= 0; i-- {
		if a := s.st.Alerts[i]; a.Machine == id && a.Kind == kind && a.Status == "open" {
			mo = a
			break
		}
	}
	gio := time.Now().Format("15:04:05 02/01")
	if !hong {
		s.boGiu(id, kind) // thiết bị tốt lại: lần rớt sau là chuyện mới
	}
	switch {
	case hong && mo == nil && s.dangGiu(id, kind):
		// đã bấm "Đã xử lý" mà thiết bị vẫn chưa sống lại lần nào: không báo lại cùng một lỗi
	case hong && mo == nil:
		s.st.Alerts = append(s.st.Alerts, &Alert{ID: rid(5), Machine: id, Name: d.Name, Kind: kind, Sev: sev,
			Msg: d.Name + " (" + d.IP + ") " + viec + " lúc " + gio, Opened: time.Now(), Status: "open"})
		s.catBotCanhBao()
		s.notify(s.st.Settings, "Cảnh báo: "+d.Name, d.Name+" ("+d.IP+") "+viec+" lúc "+gio)
	case hong && mo != nil && strings.Contains(mo.Msg, daCoLai):
		// Rớt lại sau khi đã có lại: GHI VÀO CHÍNH DÒNG NÀY, không đẻ dòng mới. Thiết bị chập chờn
		// trước đây đẻ ra hàng chục dòng y hệt (đo thật: Hành Chính 2# 26 dòng) — đúng cái đại ca
		// kêu là cảnh báo lặp. Cũng không nhắn lại: dòng cảnh báo vẫn đang mở, chưa ai xử lý.
		mo.Lan++
		mo.Msg = mo.Msg[:strings.Index(mo.Msg, daCoLai)] + roiLai + gio + " (lần thứ " + itoa(mo.Lan+1) + ")"
	case !hong && mo != nil && !strings.Contains(mo.Msg, daCoLai):
		if i := strings.Index(mo.Msg, roiLai); i >= 0 {
			mo.Msg = mo.Msg[:i] // bỏ đuôi "rớt lại" cũ, vẫn giữ một dòng duy nhất
		}
		mo.Msg += daCoLai + gio
		s.notify(s.st.Settings, "Đã có lại: "+d.Name, mo.Msg)
	}
}

// devAck đóng mọi cảnh báo đang mở của một thiết bị ("Đã xử lý"). Gọi khi đang giữ s.mu.
func (s *Server) devAck(d *NetDev, ai string) int {
	n, id := 0, devAlertID(d.ID)
	for _, a := range s.st.Alerts {
		if a.Machine == id && a.Status == "open" {
			a.Status, a.Closed, a.By = "closed", time.Now(), ai
			a.Giu = true // thiết bị chưa sống lại thì đừng báo lại y hệt
			n++
		}
	}
	return n
}

// soCanhBao: số cảnh báo đang mở của thiết bị. Gọi khi đang giữ s.mu.
func (s *Server) soCanhBao(d *NetDev) int {
	n, id := 0, devAlertID(d.ID)
	for _, a := range s.st.Alerts {
		if a.Machine == id && a.Status == "open" {
			n++
		}
	}
	return n
}

// devAlertsClose đóng mọi cảnh báo của một thiết bị (lúc xóa thiết bị). Gọi khi đang giữ s.mu.
func (s *Server) devAlertsClose(d *NetDev) {
	s.devAck(d, "tự động (xóa thiết bị)")
}

// devView: dữ liệu trả cho Console. Không trả mật khẩu camera. vid = có đang đo luồng hình không.
func (s *Server) devView(d *NetDev) map[string]interface{} {
	vid := s.doHinh(d)
	p := d.Ports
	if p == nil {
		p = []int{}
	}
	// giờ rớt và giờ có lại của lần gần nhất, để danh sách nói rõ chứ không chỉ "đã từng mất kết nối"
	var rot, lai interface{}
	for i := len(s.st.DevLog) - 1; i >= 0; i-- {
		e := s.st.DevLog[i]
		if e.Dev != d.ID {
			continue
		}
		if e.Up && lai == nil && rot == nil {
			lai = e.At
			continue
		}
		if !e.Up {
			rot = e.At
			break
		}
	}
	return map[string]interface{}{
		"id": d.ID, "name": d.Name, "nameZh": d.NameZH, "ip": d.IP, "kind": d.Kind, "groups": nhomCua(d), "port": d.Port,
		"vid":  vid,
		"path": d.Path, "user": d.User, "hasPass": d.Pass != "", "mac": d.MAC, "macNow": d.MACNow,
		"up": d.Up, "video": d.Video, "kbps": d.Kbps, "saiTK": d.SaiTK, "ports": p, "way": d.Way, "canhBao": s.soCanhBao(d),
		"rot": rot, "lai": lai,
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
			out = append(out, s.devView(d))
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
		in.Groups = donNhom(append(in.Groups, in.Group))
		in.Group = ""
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
				d.Name, d.IP, d.Kind, d.Groups, d.Port = in.Name, in.IP, in.Kind, in.Groups, in.Port
				d.Group = ""
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
		d := &NetDev{ID: rid(5), Name: in.Name, IP: in.IP, Kind: in.Kind, Groups: in.Groups, Port: in.Port, Path: in.Path, User: in.User, Pass: in.Pass}
		s.st.Devices = append(s.st.Devices, d)
		s.save()
		s.mu.Unlock()
		s.auditBy(me, r, "Thiết bị mạng", "Thêm thiết bị "+d.Name+" ("+d.IP+")", d.Name, false)
		go s.checkDevices(context.Background(), map[string]bool{d.ID: true})
		jok(w, s.devView(d))
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
		out = append(out, s.devView(d))
	}
	jok(w, out)
}

// hDeviceLog: lịch sử rớt / sống lại của một thiết bị, mới nhất lên đầu.
func (s *Server) hDeviceLog(w http.ResponseWriter, r *http.Request, me *Account) {
	id := r.URL.Query().Get("id")
	s.mu.RLock()
	defer s.mu.RUnlock()
	var d *NetDev
	for _, x := range s.st.Devices {
		if x.ID == id {
			d = x
		}
	}
	if d == nil {
		jerr(w, 404, "Không có thiết bị này")
		return
	}
	out := []map[string]interface{}{}
	for i := len(s.st.DevLog) - 1; i >= 0; i-- {
		e := s.st.DevLog[i]
		if e.Dev == id {
			tr := e.Trang
			if tr == "" { // mốc ghi trước 1.1.90
				tr = "down"
				if e.Up {
					tr = "up"
				}
			}
			out = append(out, map[string]interface{}{"up": e.Up, "at": e.At, "trang": tr})
		}
		if len(out) >= 500 {
			break
		}
	}
	jok(w, map[string]interface{}{"name": d.Name, "ip": d.IP, "up": d.Up, "since": d.Since, "log": out})
}

// hDeviceBulk: làm một việc cho nhiều thiết bị đã chọn — xóa, hoặc xếp vào một nhóm.
// Có cái này vì quét xong thêm cả trăm thiết bị thì sửa từng cái là không xong.
func (s *Server) hDeviceBulk(w http.ResponseWriter, r *http.Request, me *Account) {
	if me.Role != "super" {
		jerr(w, 403, "Chỉ Super admin được làm việc này")
		return
	}
	var in struct {
		IDs    []string
		Action string
		Group  string
		Groups []string
		Them   bool // true = thêm vào nhóm đang có, false = thay hết nhóm cũ
	}
	if r.Method != "POST" || readJSON(r, &in) != nil || len(in.IDs) == 0 {
		jerr(w, 400, "Dữ liệu không hợp lệ")
		return
	}
	if in.Action != "delete" && in.Action != "group" {
		jerr(w, 400, "Dữ liệu không hợp lệ")
		return
	}
	in.Groups = donNhom(append(in.Groups, in.Group))
	chon := map[string]bool{}
	for _, id := range in.IDs {
		chon[id] = true
	}
	s.mu.Lock()
	n, giu := 0, []*NetDev{}
	for _, d := range s.st.Devices {
		if !chon[d.ID] {
			giu = append(giu, d)
			continue
		}
		n++
		if in.Action == "delete" {
			s.devAlertsClose(d)
			continue // không giữ lại
		}
		if in.Them {
			themNhom(d, in.Groups)
		} else {
			d.Groups, d.Group = in.Groups, ""
		}
		giu = append(giu, d)
	}
	s.st.Devices = giu
	s.save()
	s.mu.Unlock()
	if in.Action == "delete" {
		s.auditBy(me, r, "Thiết bị mạng", "Xóa "+itoa(n)+" thiết bị", "—", false)
	} else {
		s.auditBy(me, r, "Thiết bị mạng", "Xếp "+itoa(n)+" thiết bị vào nhóm "+tenNhom(strings.Join(in.Groups, ", ")), "—", false)
	}
	jok(w, map[string]interface{}{"ok": true, "n": n})
}

// tenNhom: tên nhóm để ghi nhật ký, rỗng thì ghi rõ là chưa phân nhóm.
func tenNhom(s string) string {
	if s == "" {
		return "(chưa phân nhóm)"
	}
	return s
}

// devKindVN: tên loại thiết bị để xuất ra file.
var devKindVN = func(k string) string {
	m := map[string]string{"camera": "Camera", "attend": "Máy chấm công", "switch": "Switch", "router": "Router", "wifi": "Thiết bị WiFi", "led": "Bảng LED", "other": "Khác"}
	if v := m[k]; v != "" {
		return v
	}
	return k
}

// congMo: liệt kê cổng đang mở để ghi vào file xuất ra.
func congMo(p []int) string {
	if len(p) == 0 {
		return ""
	}
	v := make([]string, len(p))
	for i, x := range p {
		v[i] = itoa(x)
	}
	return "mở cổng " + strings.Join(v, ", ")
}

// gioPhut: thời điểm dạng dd/MM/yyyy HH:mm:ss, rỗng nếu chưa có.
func gioPhut(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("02/01/2006 15:04:05")
}

// hDeviceAck: bấm "Đã xử lý" — đóng mọi cảnh báo đang mở của một thiết bị.
func (s *Server) hDeviceAck(w http.ResponseWriter, r *http.Request, me *Account) {
	if r.Method != "POST" {
		jerr(w, 405, "Phương thức không hỗ trợ")
		return
	}
	if me.Role == "view" {
		jerr(w, 403, "Tài khoản Chỉ xem không có quyền thao tác")
		return
	}
	id := r.URL.Query().Get("id")
	s.mu.Lock()
	var d *NetDev
	for _, x := range s.st.Devices {
		if x.ID == id {
			d = x
		}
	}
	if d == nil {
		s.mu.Unlock()
		jerr(w, 404, "Không có thiết bị này")
		return
	}
	n := s.devAck(d, me.User)
	s.save()
	s.mu.Unlock()
	s.auditBy(me, r, "Thiết bị mạng", "Đã xử lý "+itoa(n)+" cảnh báo của "+d.Name, d.Name, false)
	jok(w, map[string]interface{}{"ok": true, "n": n})
}
