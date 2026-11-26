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
	// Xếp theo mức hay gặp ở Việt Nam: hãng đầu danh sách trúng thì dò xong trong một nhịp.
	"/ISAPI/Streaming/channels/101/picture", // Hikvision + hãng OEM theo Hikvision (đo thật trên camera công ty)
	"/cgi-bin/snapshot.cgi?channel=1",       // Dahua, Amcrest, Tiandy, Lorex
	"/cgi-bin/snapshot.cgi",                 // Dahua đời cũ, không tham số
	"/onvif-http/snapshot?Profile_1",        // ONVIF chung (nhiều hãng nhỏ)
	"/onvif/snapshot",                       // ONVIF kiểu khác
	"/images/snapshot.jpg",                  // Uniview
	"/cgi-bin/api.cgi?cmd=Snap&channel=0",   // Reolink
	"/axis-cgi/jpg/image.cgi",               // Axis
	"/stw-cgi/video.cgi?msubmenu=snapshot&action=view", // Hanwha / Samsung Techwin
	"/cgi-bin/viewer/video.jpg",             // Vivotek
	"/cgi-bin/CGIProxy.fcgi?cmd=snapPicture2", // Foscam
	"/SnapshotJPEG?Resolution=640x480",      // Panasonic
	"/image/jpeg.cgi",                       // D-Link
	"/cgi/jpg/jpeg.cgi",                     // TRENDnet
	"/snapshot/view0.jpg",                   // Grandstream
	"/record/current.jpg",                   // Mobotix
	"/snap.jpg",                             // Bosch và vài hãng khác
	"/jpg/image.jpg",                        // Mobotix / ACTi / nhiều hãng nhỏ
	"/ISAPI/Streaming/channels/1/picture",   // vài model Hikvision đánh số kênh khác
	"/Streaming/channels/101/picture",       // Hikvision firmware cũ, chưa có tiền tố ISAPI
	"/media/cam0/still.jpg",                 // vài hãng theo chuẩn mở
	"/image.jpg",                            // chung
	"/snapshot.jpg",                         // chung, camera đời cũ
	"/tmpfs/auto.jpg",                       // vài hãng Trung Quốc
	"/tmpfs/snap.jpg",                       // biến thể của trên
}

const (
	snapMax = 8 << 20 // ảnh quá 8 MB là bất thường, không nhận
	// Đã biết đường đúng thì cho thong thả; còn đang dò giữa hơn hai chục đường thì mỗi đường chỉ
	// được 2,5 giây, và cả lượt dò không quá 20 giây — không thì camera câm ngốn hết hạn gọi.
	snapWait = 6 * time.Second
	snapDo   = 2500 * time.Millisecond
	snapCache  = 900 * time.Millisecond // nhiều người cùng xem thì camera chỉ bị gọi một lần
)

// snapBudget: hạn cho cả lượt dò. Là biến để test hạ xuống cho nhanh.
var snapBudget = 20 * time.Second

// bộ nhớ đệm ảnh: khóa là id thiết bị.
type snapItem struct {
	anh []byte
	luc time.Time
}

var (
	snapMu  sync.Mutex
	snapHet = map[string]snapItem{}
)

// Hai lỗi này nói đúng việc cần làm, nên phải được ưu tiên báo ra thay vì lỗi mạng của cổng khác:
// camera trả 401 ở ĐÚNG đường ảnh nghĩa là đường đã tìm ra, chỉ còn thiếu tài khoản.
var (
	errChuaKhai   = fmt.Errorf("camera đòi đăng nhập — chưa khai Tài khoản camera chung")
	errSaiMatKhau = fmt.Errorf("sai tài khoản hoặc mật khẩu camera")
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

// isapiGET gọi một đường HTTP của camera hoặc đầu ghi. Gặp 401 thì đăng nhập lại đúng kiểu nó đòi
// (Digest hay Basic) rồi gọi lần nữa. Trả về nguyên bytes: ảnh JPEG hay XML ISAPI đều dùng được.
func isapiGET(ctx context.Context, cl *http.Client, u, duong, user, pass string) ([]byte, error) {
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
		if user == "" {
			return nil, errChuaKhai
		}
		return nil, errSaiMatKhau
	}
	return b, nil
}

// layAnhURL gọi một đường ảnh và đòi đúng một tấm ảnh: trang web báo lỗi cũng trả 200 nên phải xem bytes.
func layAnhURL(ctx context.Context, cl *http.Client, u, duong, user, pass string) ([]byte, error) {
	b, err := isapiGET(ctx, cl, u, duong, user, pass)
	if err != nil {
		return nil, err
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
	web := map[int]bool{80: true, 8080: true, 443: true, 81: true, 8081: true}
	truoc, sau := []int{}, []int{}
	for _, p := range d.Ports {
		if snapBoCong[p] {
			continue
		}
		if web[p] { // cổng web thật thử trước; 8000 của Hikvision là cổng SDK, nói HTTP là nó cắt
			truoc = append(truoc, p)
		} else {
			sau = append(sau, p)
		}
	}
	ra := append(truoc, sau...)
	if len(ra) == 0 {
		ra = []int{80, 8080, 443}
	}
	return ra
}

// layAnh thử các cổng web và các đường ảnh cho tới khi ra ảnh thật. Trả thêm đường vừa dùng được
// để nhớ lại. Không giữ khóa.
func layAnh(ctx context.Context, d NetDev, user, pass string) ([]byte, string, error) {
	han := snapWait // đã nhớ đường thì gọi thẳng, cho thong thả
	if d.SnapPath == "" {
		han = snapDo
		var thoi context.CancelFunc
		ctx, thoi = context.WithTimeout(ctx, snapBudget)
		defer thoi()
	}
	cl := &http.Client{
		Timeout:       han,
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
			if cuoi == nil {
				cuoi = err
			}
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
			if err == errChuaKhai {
				return nil, "", err // đã tìm ra đúng đường, thử tiếp cũng vô ích
			}
			if cuoi == nil || err == errSaiMatKhau {
				cuoi = err
			}
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
