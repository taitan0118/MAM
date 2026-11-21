package main

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// một tấm JPEG bé xíu: chỉ cần đúng hai byte đầu và đủ dài để laAnh nhận
func anhGia() []byte {
	b := make([]byte, 300)
	b[0], b[1] = 0xFF, 0xD8
	return b
}

// camGia dựng một camera giả: đòi Digest qop=auth, và chỉ trả ảnh ở ĐÚNG một đường.
func camGia(t *testing.T, duong, user, pass string, dem *int) (string, int) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RequestURI() != duong {
			http.NotFound(w, r)
			return
		}
		a := r.Header.Get("Authorization")
		if a == "" {
			w.Header().Set("WWW-Authenticate", `Digest realm="cam", qop="auth", nonce="abc123"`)
			w.WriteHeader(401)
			return
		}
		sum := func(s string) string { x := md5.Sum([]byte(s)); return hex.EncodeToString(x[:]) }
		cn := authField(a, "cnonce")
		mong := sum(sum(user+":cam:"+pass) + ":abc123:00000001:" + cn + ":auth:" + sum("GET:"+duong))
		if !strings.Contains(a, `response="`+mong+`"`) {
			w.WriteHeader(401)
			return
		}
		if dem != nil {
			*dem++
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(anhGia())
	}))
	t.Cleanup(srv.Close)
	u := strings.TrimPrefix(srv.URL, "http://")
	host, cong, _ := net.SplitHostPort(u)
	return host, atoiHay(cong)
}

func atoiHay(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}

// Dò đúng đường ảnh của hãng, qua được Digest qop=auth.
func TestLayAnhDoDuongVaDigest(t *testing.T) {
	duong := "/cgi-bin/snapshot.cgi?channel=1" // kiểu Dahua, nằm thứ hai trong danh sách
	ip, cong := camGia(t, duong, "admin", "bimat", nil)
	d := NetDev{IP: ip, Ports: []int{cong}, Kind: "camera"}
	b, ra, err := layAnh(context.Background(), d, "admin", "bimat")
	if err != nil {
		t.Fatal("phải lấy được ảnh:", err)
	}
	if !laAnh(b) {
		t.Fatal("dữ liệu trả về không phải ảnh")
	}
	if ra != duong {
		t.Fatalf("phải nhớ đường %q, nhận %q", duong, ra)
	}
}

// Nhớ đường rồi thì lần sau gọi thẳng, không dò lại cả danh sách.
func TestLayAnhNhoDuong(t *testing.T) {
	duong := "/tmpfs/auto.jpg" // nằm CUỐI danh sách: không nhớ thì phải dò rất nhiều lần
	dem := 0
	ip, cong := camGia(t, duong, "admin", "x", &dem)
	d := NetDev{IP: ip, Ports: []int{cong}, Kind: "camera", SnapPath: duong}
	if _, _, err := layAnh(context.Background(), d, "admin", "x"); err != nil {
		t.Fatal(err)
	}
	if dem != 1 {
		t.Fatalf("nhớ sẵn đường thì chỉ được lấy ảnh 1 lần, đã lấy %d lần", dem)
	}
}

// Sai mật khẩu thì phải báo lỗi, không được trả trang lỗi của camera ra như thể là ảnh.
func TestLayAnhSaiMatKhau(t *testing.T) {
	ip, cong := camGia(t, "/snapshot.jpg", "admin", "dung", nil)
	d := NetDev{IP: ip, Ports: []int{cong}, Kind: "camera"}
	if _, _, err := layAnh(context.Background(), d, "admin", "sai"); err == nil {
		t.Fatal("sai mật khẩu mà vẫn báo lấy được ảnh")
	}
}

// Camera trả về trang HTML với mã 200 (hay gặp khi chưa đăng nhập) thì KHÔNG được tính là ảnh.
func TestLaAnhChanHTML(t *testing.T) {
	if laAnh([]byte(strings.Repeat("<html>nope</html>", 20))) {
		t.Fatal("trang HTML không phải ảnh")
	}
	if laAnh(anhGia()[:10]) {
		t.Fatal("vài byte lẻ không phải ảnh")
	}
	if !laAnh(anhGia()) {
		t.Fatal("JPEG thật phải được nhận")
	}
}

// Cổng RTSP không phục vụ ảnh nên đừng thử; cổng web mới thử.
func TestSnapCong(t *testing.T) {
	got := snapCong(NetDev{Ports: []int{554, 80, 8000}})
	if len(got) != 2 || got[0] != 80 || got[1] != 8000 {
		t.Fatalf("phải lấy cổng web 80 và 8000, nhận %v", got)
	}
	if len(snapCong(NetDev{})) == 0 {
		t.Fatal("chưa biết cổng nào thì vẫn phải thử cổng web mặc định")
	}
}

// API: không phải super admin vẫn xem được hình, nhưng thiết bị lạ thì 404.
func TestSnapAPIThietBiLa(t *testing.T) {
	f := setup(t)
	code, _, _ := f.tech.do("GET", "/api/devices/snapshot?id=khongco", nil, nil)
	expect(t, code, 404, "thiết bị không có")
}
