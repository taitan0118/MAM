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
}

// thuNVR đăng nhập thật vào đầu ghi. Đọc được thông tin máy mới coi là thành công — 200 suông
// không đủ, trang đăng nhập cũng trả 200.
func thuNVR(ctx context.Context, ip string, port int, user, pass string) (*nvrTT, error) {
	if net.ParseIP(ip) == nil {
		return nil, errors.New("IP không đúng dạng")
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
		return nil, err
	}
	var tin struct {
		Ten   string `xml:"deviceName"`
		Model string `xml:"model"`
		Ver   string `xml:"firmwareVersion"`
	}
	if xml.Unmarshal(b, &tin) != nil || (tin.Ten == "" && tin.Model == "") {
		return nil, errors.New("đăng nhập được nhưng không đọc được thông tin đầu ghi: thiết bị này không nói ISAPI")
	}
	out := &nvrTT{Ten: strings.TrimSpace(tin.Ten), Model: strings.TrimSpace(tin.Model), Ver: strings.TrimSpace(tin.Ver)}
	out.Kenh = demKenh(ctx, cl, goc, user, pass)
	return out, nil
}

// demKenh đếm số kênh camera đầu ghi đang nhận. Không đọc được thì trả 0: biết tên máy là đã
// chứng minh đăng nhập được rồi, số kênh chỉ để đại ca nhìn cho chắc.
func demKenh(ctx context.Context, cl *http.Client, goc, user, pass string) int {
	for _, d := range []string{"/ISAPI/ContentMgmt/InputProxy/channels", "/ISAPI/System/Video/inputs/channels"} {
		b, err := isapiGET(ctx, cl, goc+d, d, user, pass)
		if err != nil {
			continue
		}
		var ds struct {
			Proxy []struct{} `xml:"InputProxyChannel"`
			Vao   []struct{} `xml:"VideoInputChannel"`
		}
		if xml.Unmarshal(b, &ds) != nil {
			continue
		}
		if n := len(ds.Proxy) + len(ds.Vao); n > 0 {
			return n
		}
	}
	return 0
}

// hNvr: Console khai đầu ghi. POST = thử đăng nhập, đúng mới lưu (giống khai tài khoản riêng cho
// camera ở hDeviceTK: sai thì không ghi gì, khỏi phá cái đang chạy được). DELETE = xóa.
func (s *Server) hNvr(w http.ResponseWriter, r *http.Request, me *Account) {
	if r.Method == "DELETE" {
		s.mu.Lock()
		st := &s.st.Settings
		st.NvrIP, st.NvrPort, st.NvrUser, st.NvrPass, st.NvrTen, st.NvrKenh, st.NvrAt = "", 0, "", "", "", 0, time.Time{}
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
	tt, err := thuNVR(ctx, in.IP, in.Port, in.User, in.Pass)
	if err != nil {
		jerr(w, 400, TErr(err.Error()))
		return
	}

	s.mu.Lock()
	st := &s.st.Settings
	st.NvrIP, st.NvrPort, st.NvrUser, st.NvrPass = in.IP, in.Port, in.User, in.Pass
	st.NvrTen, st.NvrKenh, st.NvrAt = strings.TrimSpace(tt.Ten+" "+tt.Model), tt.Kenh, time.Now()
	ten := st.NvrTen
	s.save()
	s.mu.Unlock()
	s.auditBy(me, r, "Cài đặt", T("Khai đầu ghi camera %s (%s)", ten, in.IP), "—", true)
	jok(w, tt)
}
