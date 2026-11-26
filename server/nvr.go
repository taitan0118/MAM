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
// camera ở hDeviceTK: sai thì không ghi gì, khỏi phá cái đang chạy được). DELETE = xóa.
func (s *Server) hNvr(w http.ResponseWriter, r *http.Request, me *Account) {
	if r.Method == "DELETE" {
		s.mu.Lock()
		st := &s.st.Settings
		st.NvrIP, st.NvrPort, st.NvrUser, st.NvrPass, st.NvrTen, st.NvrKenh, st.NvrAt = "", 0, "", "", "", 0, time.Time{}
		for _, d := range s.st.Devices {
			d.NvrCh = 0 // bỏ đầu ghi thì không còn kênh nào để tua
		}
		s.save()
		s.mu.Unlock()
		s.auditBy(me, r, "Cài đặt", T("Xóa đầu ghi camera"), "—", true)
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
	// để trống mật khẩu = giữ mật khẩu đang lưu, nhưng chỉ khi vẫn là đúng đầu ghi đó
	s.mu.RLock()
	if in.Pass == "" && in.IP == s.st.Settings.NvrIP && in.User == s.st.Settings.NvrUser {
		in.Pass = s.st.Settings.NvrPass
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
	st := &s.st.Settings
	st.NvrIP, st.NvrPort, st.NvrUser, st.NvrPass = in.IP, in.Port, in.User, in.Pass
	st.NvrTen, st.NvrKenh, st.NvrAt = strings.TrimSpace(tt.Ten+" "+tt.Model), tt.Kenh, time.Now()
	ten := st.NvrTen
	// Gắn camera trong MAM vào kênh đầu ghi bằng cách khớp IP: tự làm chứ không bắt gõ tay 395 lần.
	theoIP := map[string]int{}
	for _, k := range kenh {
		if k.IP != "" {
			theoIP[k.IP] = k.ID
		}
	}
	for _, d := range s.st.Devices {
		if ch := theoIP[d.IP]; ch > 0 {
			d.NvrCh = ch
			tt.Gan++
		}
	}
	s.save()
	s.mu.Unlock()
	s.auditBy(me, r, "Cài đặt", T("Khai đầu ghi camera %s (%s)", ten, in.IP), "—", true)
	jok(w, tt)
}
