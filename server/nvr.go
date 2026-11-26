package main

// Đầu ghi camera (NVR). Công ty có 23 dải camera, mỗi dải một mật khẩu — khai được đầu ghi thì về
// sau chỉ cần MỘT tài khoản cho cả dàn. Bản này mới làm phần KẾT NỐI VÀ ĐĂNG NHẬP: server gọi thật
// vào đầu ghi, đọc tên/model/số kênh, đăng nhập được mới lưu. Xem hình và tua lại qua đầu ghi là
// việc sau, và khi làm thì dùng chính tài khoản lưu ở đây.
//
// shortcut: chỉ nói chuyện được với đầu ghi Hikvision (ISAPI) — đúng hãng camera công ty đang dùng.
// Đầu ghi Dahua (cổng 37777) hay hãng khác thì nâng cấp khi nào gặp.

import (
	"context"
	"crypto/tls"
	"encoding/xml"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

const nvrWait = 8 * time.Second

// nvrTT: những gì đầu ghi tự khai sau khi đăng nhập được.
type nvrTT struct {
	Ten   string `json:"ten"`
	Model string `json:"model"`
	Ver   string `json:"ver"`
	Kenh  int    `json:"kenh"`
	Gan   int    `json:"gan"` // số camera trong MAM khớp được với một kênh của đầu ghi
}

// nvrKenh: một kênh camera của đầu ghi. IP là địa chỉ camera mà đầu ghi đang kéo hình về — nhờ nó
// mới biết camera nào trong MAM nằm ở kênh nào, khỏi bắt đại ca ngồi gõ tay số kênh cho 395 camera.
type nvrKenh struct {
	ID int
	IP string
}

// thuNVR đăng nhập thật vào đầu ghi. Đọc được thông tin máy mới coi là thành công — 200 suông
// không đủ, trang đăng nhập cũng trả 200.
func thuNVR(ctx context.Context, ip string, port int, user, pass string) (*nvrTT, []nvrKenh, error) {
	if net.ParseIP(ip) == nil {
		return nil, nil, errors.New("IP không đúng dạng")
	}
	cl := &http.Client{
		Timeout:       nvrWait,
		Transport:     &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	sch := "http"
	if port == 443 {
		sch = "https"
	}
	goc := sch + "://" + net.JoinHostPort(ip, itoa(port))

	const duong = "/ISAPI/System/deviceInfo"
	b, err := isapiGET(ctx, cl, goc+duong, duong, user, pass)
	if err != nil {
		return nil, nil, err
	}
	var tin struct {
		Ten   string `xml:"deviceName"`
		Model string `xml:"model"`
		Ver   string `xml:"firmwareVersion"`
	}
	if xml.Unmarshal(b, &tin) != nil || (tin.Ten == "" && tin.Model == "") {
		return nil, nil, errors.New("đăng nhập được nhưng không đọc được thông tin đầu ghi: thiết bị này không nói ISAPI")
	}
	out := &nvrTT{Ten: strings.TrimSpace(tin.Ten), Model: strings.TrimSpace(tin.Model), Ver: strings.TrimSpace(tin.Ver)}
	ks := layKenh(ctx, cl, goc, user, pass)
	out.Kenh = len(ks)
	return out, ks, nil
}

// layKenh đọc danh sách kênh camera của đầu ghi. Không đọc được thì trả rỗng: biết tên máy là đã
// chứng minh đăng nhập được rồi, danh sách kênh chỉ để gắn camera và để tua lại.
func layKenh(ctx context.Context, cl *http.Client, goc, user, pass string) []nvrKenh {
	// Đầu ghi IP (NVR): mỗi kênh kéo hình từ một camera, có kèm IP camera đó.
	if b, err := isapiGET(ctx, cl, goc+"/ISAPI/ContentMgmt/InputProxy/channels", "/ISAPI/ContentMgmt/InputProxy/channels", user, pass); err == nil {
		var ds struct {
			K []struct {
				ID int    `xml:"id"`
				IP string `xml:"sourceInputPortDescriptor>ipAddress"`
			} `xml:"InputProxyChannel"`
		}
		if xml.Unmarshal(b, &ds) == nil && len(ds.K) > 0 {
			out := make([]nvrKenh, 0, len(ds.K))
			for _, k := range ds.K {
				out = append(out, nvrKenh{ID: k.ID, IP: strings.TrimSpace(k.IP)})
			}
			return out
		}
	}
	// Đầu ghi analog (DVR): có kênh nhưng không có IP, nên không gắn tự động được.
	// shortcut: gặp đầu ghi analog thì phải thêm chỗ gõ tay số kênh cho từng camera.
	if b, err := isapiGET(ctx, cl, goc+"/ISAPI/System/Video/inputs/channels", "/ISAPI/System/Video/inputs/channels", user, pass); err == nil {
		var ds struct {
			K []struct {
				ID int `xml:"id"`
			} `xml:"VideoInputChannel"`
		}
		if xml.Unmarshal(b, &ds) == nil {
			out := make([]nvrKenh, 0, len(ds.K))
			for _, k := range ds.K {
				out = append(out, nvrKenh{ID: k.ID})
			}
			return out
		}
	}
	return nil
}

// Tua lại qua đầu ghi: đầu ghi Hikvision phát lại bằng chính RTSP, đường /Streaming/tracks/<kênh><track>
// với mốc giờ UTC. Track 02 là luồng phụ (gần như luôn H.264, Chrome phát được), track 01 là luồng
// chính (phần lớn H.265 nên trình duyệt chịu) — nên thử luồng phụ trước, y như lúc xem trực tiếp.
const (
	nvrRtsp   = 554              // cổng RTSP của đầu ghi; chưa gặp đầu ghi nào đổi nên chưa cho khai
	nvrTuaLau = 30 * time.Minute // xin đầu ghi phát một đoạn dài bấy nhiêu tính từ mốc đại ca chọn
)

func hikGio(t time.Time) string { return t.UTC().Format("20060102T150405Z") }

// moKenh mở luồng TRỰC TIẾP của một kênh đầu ghi. Đây là chỗ làm cho MAM giống iVMS-4200: khai một
// đầu ghi là xem được cả dàn camera của nó bằng ĐÚNG MỘT tài khoản, khỏi phải khai mật khẩu riêng
// cho từng dải. Luồng phụ (02) trước vì nó là H.264, luồng chính (01) phần lớn H.265.
func moKenh(ctx context.Context, ip, user, pass string, ch int) (*luongH264, error) {
	var dau error
	for _, tr := range []int{2, 1} {
		l, err := moLuong(ctx, ip, nvrRtsp, fmt.Sprintf("/Streaming/Channels/%d%02d", ch, tr), user, pass)
		if err == nil {
			return l, nil
		}
		if errors.Is(err, errSaiMatKhau) || errors.Is(err, errChuaKhai) {
			return nil, err
		}
		if dau == nil {
			dau = err
		}
	}
	return nil, dau
}

// moTua mở luồng phát lại của một kênh đầu ghi từ mốc giờ t0.
func moTua(ctx context.Context, ip, user, pass string, ch int, t0 time.Time) (*luongH264, error) {
	var dau error
	for _, tr := range []int{2, 1} {
		duong := fmt.Sprintf("/Streaming/tracks/%d%02d?starttime=%s&endtime=%s", ch, tr, hikGio(t0), hikGio(t0.Add(nvrTuaLau)))
		l, err := moLuong(ctx, ip, nvrRtsp, duong, user, pass)
		if err == nil {
			return l, nil
		}
		if errors.Is(err, errSaiMatKhau) || errors.Is(err, errChuaKhai) {
			return nil, err
		}
		if dau == nil {
			dau = err
		}
	}
	return nil, dau
}

// hNvr: Console khai đầu ghi. POST = thử đăng nhập, đúng mới lưu (giống khai tài khoản riêng cho
// camera ở hDeviceTK: sai thì không ghi gì, khỏi phá cái đang chạy được). DELETE ?ip=… = bỏ một
// đầu ghi. Khai được nhiều đầu ghi: một cái 16 hay 32 kênh không ôm hết 395 camera.
func (s *Server) hNvr(w http.ResponseWriter, r *http.Request, me *Account) {
	if r.Method == "DELETE" {
		ip := strings.TrimSpace(r.URL.Query().Get("ip"))
		s.mu.Lock()
		st := &s.st.Settings
		con, ten := st.Nvrs[:0], ""
		for _, g := range st.Nvrs {
			if g.IP == ip {
				ten = g.Ten
				continue
			}
			con = append(con, g)
		}
		if ten == "" && len(con) == len(st.Nvrs) {
			s.mu.Unlock()
			jerr(w, 404, "Không có đầu ghi này")
			return
		}
		st.Nvrs = con
		for _, d := range s.st.Devices {
			if d.NvrID == ip { // bỏ đầu ghi thì camera của nó không còn gì để tua
				d.NvrID, d.NvrCh = "", 0
			}
		}
		s.save()
		s.mu.Unlock()
		s.auditBy(me, r, "Cài đặt", T("Xóa đầu ghi camera %s (%s)", ten, ip), "—", true)
		jok(w, map[string]bool{"ok": true})
		return
	}
	if r.Method != "POST" {
		jerr(w, 405, "Phương thức không hỗ trợ")
		return
	}
	var in struct {
		IP, User, Pass string
		Port           int
	}
	if err := readJSON(r, &in); err != nil {
		jerr(w, 400, "Dữ liệu không đọc được")
		return
	}
	in.IP, in.User = strings.TrimSpace(in.IP), strings.TrimSpace(in.User)
	if in.IP == "" || in.User == "" {
		jerr(w, 400, "Chưa nhập IP hoặc tài khoản đầu ghi")
		return
	}
	if in.Port <= 0 || in.Port > 65535 {
		in.Port = 80
	}
	// để trống mật khẩu = giữ mật khẩu đang lưu của chính đầu ghi đó, cùng tài khoản
	s.mu.RLock()
	for _, g := range s.st.Settings.Nvrs {
		if in.Pass == "" && g.IP == in.IP && g.User == in.User {
			in.Pass = g.Pass
		}
	}
	s.mu.RUnlock()

	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	tt, kenh, err := thuNVR(ctx, in.IP, in.Port, in.User, in.Pass)
	if err != nil {
		jerr(w, 400, TErr(err.Error()))
		return
	}

	s.mu.Lock()
	moi := DauGhi{IP: in.IP, Port: in.Port, User: in.User, Pass: in.Pass,
		Ten: strings.TrimSpace(tt.Ten + " " + tt.Model), Kenh: tt.Kenh, At: time.Now()}
	// Gắn camera trong MAM vào kênh đầu ghi bằng cách khớp IP: tự làm chứ không bắt gõ tay 395 lần.
	theoIP := map[string]int{}
	for _, k := range kenh {
		if k.IP != "" {
			theoIP[k.IP] = k.ID
		}
	}
	for _, d := range s.st.Devices {
		if ch := theoIP[d.IP]; ch > 0 {
			d.NvrID, d.NvrCh = moi.IP, ch
			moi.Gan++
		} else if d.NvrID == moi.IP {
			// đầu ghi này trước có kéo camera đó, giờ không còn: gỡ ra chứ đừng để kênh ma
			d.NvrID, d.NvrCh = "", 0
		}
	}
	tt.Gan = moi.Gan
	st := &s.st.Settings
	thay := false
	for i := range st.Nvrs {
		if st.Nvrs[i].IP == moi.IP { // khai lại đúng đầu ghi đó thì ghi đè, không đẻ thêm dòng
			st.Nvrs[i], thay = moi, true
		}
	}
	if !thay {
		st.Nvrs = append(st.Nvrs, moi)
	}
	s.save()
	s.mu.Unlock()
	s.auditBy(me, r, "Cài đặt", T("Khai đầu ghi camera %s (%s), gắn được %d camera", moi.Ten, moi.IP, moi.Gan), "—", true)
	jok(w, tt)
}

// dauGhiCua: đầu ghi đang giữ camera này, kèm số kênh. Không có thì trả rỗng. Gọi khi đang giữ s.mu.
func (s *Server) dauGhiCua(d *NetDev) (DauGhi, int) {
	if d.NvrID == "" || d.NvrCh == 0 {
		return DauGhi{}, 0
	}
	for _, g := range s.st.Settings.Nvrs {
		if g.IP == d.NvrID {
			return g, d.NvrCh
		}
	}
	return DauGhi{}, 0
}

// chuyenDauGhiCu: đầu ghi khai hồi bản 1.1.106–1.1.110 nằm ở mấy ô lẻ, chuyển vào danh sách rồi
// xóa ô cũ. Chạy khi đang giữ s.mu lúc khởi động. Trả về true nếu có chuyển.
func (s *Server) chuyenDauGhiCu() bool {
	st := &s.st.Settings
	if st.NvrIP == "" {
		return false
	}
	co := false
	for _, g := range st.Nvrs {
		if g.IP == st.NvrIP {
			co = true
		}
	}
	if !co {
		st.Nvrs = append(st.Nvrs, DauGhi{IP: st.NvrIP, Port: st.NvrPort, User: st.NvrUser, Pass: st.NvrPass,
			Ten: st.NvrTen, Kenh: st.NvrKenh, At: st.NvrAt})
		// camera đã gắn kênh hồi đó chưa biết thuộc đầu ghi nào, lúc ấy chỉ có một cái
		for _, d := range s.st.Devices {
			if d.NvrCh > 0 && d.NvrID == "" {
				d.NvrID = st.NvrIP
			}
		}
	}
	st.NvrIP, st.NvrPort, st.NvrUser, st.NvrPass, st.NvrTen, st.NvrKenh, st.NvrAt = "", 0, "", "", "", 0, time.Time{}
	return true
}
