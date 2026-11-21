package main

// Xem hình camera trong Console. Trình duyệt KHÔNG phát được RTSP, nên server lấy ảnh JPEG mà chính
// camera đã nén sẵn rồi chuyển thẳng cho Console — không giải mã video một chữ nào, không cần ffmpeg.
// Console thay ảnh mỗi giây: không mượt như đầu ghi, nhưng đủ biết camera đang quay gì, có bị che
// hay không. Muốn xem mượt thì mở phần mềm đầu ghi, MAM là phần mềm giám sát.
//
// shortcut: chỉ ảnh tĩnh làm mới, không phải luồng video; muốn mượt thì nâng lên MJPEG (giữ kết nối
// và phải tách khỏi WriteTimeout 30 giây của server).

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// snapPaths: đường ảnh JPEG của các hãng hay gặp, thử lần lượt. Hãng nào đúng thì nhớ lại
// (NetDev.SnapPath) để lần sau gọi thẳng một phát.
var snapPaths = []string{
	"/ISAPI/Streaming/channels/101/picture", // Hikvision (và mấy hãng OEM theo Hikvision)
	"/cgi-bin/snapshot.cgi?channel=1",       // Dahua
	"/onvif-http/snapshot?Profile_1",        // ONVIF chung
	"/images/snapshot.jpg",                  // Uniview
	"/axis-cgi/jpg/image.cgi",               // Axis
	"/cgi-bin/api.cgi?cmd=Snap&channel=0",   // Reolink
	"/snapshot.jpg",                         // một số camera đời cũ
	"/tmpfs/auto.jpg",                       // vài hãng Trung Quốc
}

const (
	snapMax   = 8 << 20          // ảnh quá 8 MB là bất thường, không nhận
	snapWait  = 6 * time.Second  // một lượt lấy ảnh
	snapCache = 900 * time.Millisecond // giữ ảnh vừa lấy: nhiều người cùng xem thì camera chỉ bị gọi một lần
)

// bộ nhớ đệm ảnh: khóa là id thiết bị.
type snapItem struct {
	anh []byte
	luc time.Time
}

var (
	snapMu  sync.Mutex
	snapHet = map[string]snapItem{}
)

// laAnh: dữ liệu nhận về có đúng là JPEG/PNG không. Camera chưa đăng nhập được thường trả về trang
// HTML báo lỗi với mã 200, nên phải nhìn mấy byte đầu chứ không tin mã trả về.
func laAnh(b []byte) bool {
	return len(b) > 128 && ((b[0] == 0xFF && b[1] == 0xD8) || // JPEG
		(b[0] == 0x89 && b[1] == 'P' && b[2] == 'N' && b[3] == 'G'))
}

// digestHTTP dựng phần Authorization cho HTTP Digest. Khác bản RTSP ở chỗ camera qua HTTP hay đòi
// qop=auth, lúc đó công thức có thêm nc và cnonce.
func digestHTTP(h, method, uri, user, pass string) string {
	realm, nonce := authField(h, "realm"), authField(h, "nonce")
	if realm == "" || nonce == "" {
		return ""
	}
	sum := func(s string) string { x := md5.Sum([]byte(s)); return hex.EncodeToString(x[:]) }
	ha1, ha2 := sum(user+":"+realm+":"+pass), sum(method+":"+uri)
	qop, opaque := authField(h, "qop"), authField(h, "opaque")
	out := fmt.Sprintf("Digest username=%q, realm=%q, nonce=%q, uri=%q", user, realm, nonce, uri)
	if strings.Contains(qop, "auth") {
		var r [8]byte
		rand.Read(r[:])
		cn := hex.EncodeToString(r[:])
		out += fmt.Sprintf(", qop=auth, nc=00000001, cnonce=%q, response=%q", cn,
			sum(ha1+":"+nonce+":00000001:"+cn+":auth:"+ha2))
	} else {
		out += fmt.Sprintf(", response=%q", sum(ha1+":"+nonce+":"+ha2))
	}
	if opaque != "" {
		out += fmt.Sprintf(", opaque=%q", opaque)
	}
	return out
}

// layAnhURL gọi một đường ảnh. Gặp 401 thì đăng nhập lại đúng kiểu camera đòi rồi gọi lần nữa.
func layAnhURL(ctx context.Context, cl *http.Client, u, duong, user, pass string) ([]byte, error) {
	goi := func(auth string) (int, string, []byte, error) {
		req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
		if err != nil {
			return 0, "", nil, err
		}
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := cl.Do(req)
		if err != nil {
			return 0, "", nil, err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(io.LimitReader(resp.Body, snapMax))
		return resp.StatusCode, resp.Header.Get("WWW-Authenticate"), b, err
	}
	code, wa, b, err := goi("")
	if err != nil {
		return nil, err
	}
	if code == 401 && user != "" {
		auth := digestHTTP(wa, "GET", duong, user, pass)
		if !strings.HasPrefix(strings.ToLower(wa), "digest") {
			auth = "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
		}
		if auth == "" {
			return nil, fmt.Errorf("camera đòi đăng nhập kiểu lạ")
		}
		if code, _, b, err = goi(auth); err != nil {
			return nil, err
		}
	}
	if code == 401 {
		return nil, fmt.Errorf("sai tài khoản hoặc mật khẩu camera")
	}
	if !laAnh(b) {
		return nil, fmt.Errorf("không phải ảnh")
	}
	return b, nil
}

// snapCong: cổng để xin ảnh. Lấy chính những cổng đang mở của camera — nhiều camera đặt trang web
// ở cổng lạ chứ không phải 80. Bỏ cổng RTSP và cổng đầu ghi vì chúng không phục vụ ảnh.
var snapBoCong = map[int]bool{554: true, 37777: true, 34567: true}

func snapCong(d NetDev) []int {
	ra := []int{}
	for _, p := range d.Ports {
		if !snapBoCong[p] {
			ra = append(ra, p)
		}
	}
	if len(ra) == 0 {
		ra = []int{80, 8080, 443}
	}
	return ra
}

// layAnh thử các cổng web và các đường ảnh cho tới khi ra ảnh thật. Trả thêm đường vừa dùng được
// để nhớ lại. Không giữ khóa.
func layAnh(ctx context.Context, d NetDev, user, pass string) ([]byte, string, error) {
	cl := &http.Client{
		Timeout:       snapWait,
		Transport:     &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	ds := snapPaths
	if d.SnapPath != "" { // đã biết đường đúng thì thử nó trước
		ds = append([]string{d.SnapPath}, snapPaths...)
	}
	var cuoi error
	for _, p := range snapCong(d) {
		// gõ cửa trước: cổng đóng thì bỏ qua ngay, không ngồi chờ hết 8 đường
		c, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(d.IP, itoa(p)))
		if err != nil {
			cuoi = err
			continue
		}
		c.Close()
		sch := "http"
		if p == 443 {
			sch = "https"
		}
		for _, duong := range ds {
			if ctx.Err() != nil {
				return nil, "", ctx.Err()
			}
			u := sch + "://" + net.JoinHostPort(d.IP, itoa(p)) + duong
			b, err := layAnhURL(ctx, cl, u, duong, user, pass)
			if err == nil {
				return b, duong, nil
			}
			cuoi = err
		}
	}
	if cuoi == nil {
		cuoi = fmt.Errorf("không lấy được ảnh")
	}
	return nil, "", cuoi
}

// hDeviceSnap: Console xin một tấm ảnh của camera. Ảnh vừa lấy được giữ lại chưa đầy một giây nên
// mười người cùng xem cũng chỉ gọi camera một lần.
func (s *Server) hDeviceSnap(w http.ResponseWriter, r *http.Request, me *Account) {
	id := r.URL.Query().Get("id")
	s.mu.RLock()
	var d *NetDev
	for _, x := range s.st.Devices {
		if x.ID == id {
			d = x
		}
	}
	if d == nil {
		s.mu.RUnlock()
		jerr(w, 404, "Không có thiết bị này")
		return
	}
	cp := *d
	if cp.User == "" {
		cp.User, cp.Pass = s.st.Settings.CamUser, s.st.Settings.CamPass
	}
	s.mu.RUnlock()

	snapMu.Lock()
	it, ok := snapHet[id]
	snapMu.Unlock()
	if ok && time.Since(it.luc) < snapCache {
		snapRa(w, it.anh)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), snapWait*2)
	defer cancel()
	b, duong, err := layAnh(ctx, cp, cp.User, cp.Pass)
	if err != nil {
		jerr(w, 502, "Không lấy được hình: "+err.Error())
		return
	}
	snapMu.Lock()
	snapHet[id] = snapItem{anh: b, luc: time.Now()}
	if len(snapHet) > 200 { // đừng giữ ảnh của cả nghìn camera trong bộ nhớ
		for k, v := range snapHet {
			if time.Since(v.luc) > time.Minute {
				delete(snapHet, k)
			}
		}
	}
	snapMu.Unlock()
	if duong != "" && duong != d.SnapPath { // nhớ đường đúng cho lần sau khỏi dò lại
		s.mu.Lock()
		d.SnapPath = duong
		s.save()
		s.mu.Unlock()
	}
	snapRa(w, b)
}

func snapRa(w http.ResponseWriter, b []byte) {
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(b)
}
