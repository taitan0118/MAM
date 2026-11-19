package main

// Đo luồng hình camera IP bằng RTSP: bắt tay DESCRIBE/SETUP/PLAY rồi đếm byte RTP nhận được trong
// vài giây. Kéo RTP ngay trong kết nối TCP đang mở (interleaved) nên không cần mở cổng UDP, chạy
// được qua tường lửa. Chỉ dùng thư viện chuẩn.
//
// Mục đích: cổng 554 mở chỉ chứng minh camera còn sống; có byte hình mới chứng minh camera còn ra
// hình. KHÔNG biết được đầu ghi có lưu lại hay không, cũng không biết ống kính có bị che hay không.

import (
	"bufio"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// đo 3 giây là đủ biết có hình hay không; là biến để test hạ xuống cho nhanh
var rtspMeasure = 3 * time.Second

const rtspBudget = 12 * time.Second // cả lượt đo (bắt tay + đo) không lâu hơn mức này

type rtspConn struct {
	c                    net.Conn
	br                   *bufio.Reader
	seq                  int
	user, pass           string
	scheme, realm, nonce string // cách xác thực camera đòi, lấy từ phản hồi 401
	session              string
}

// rtspAlive hỏi một câu OPTIONS xem đầu kia có thật là camera không. Cổng 554 mở chưa chắc là camera,
// nhưng trả lời đúng RTSP thì gần như chắc. Không cần tài khoản.
func rtspAlive(ctx context.Context, ip string, port int) bool {
	c, err := (&net.Dialer{Timeout: 1500 * time.Millisecond}).DialContext(ctx, "tcp", net.JoinHostPort(ip, itoa(port)))
	if err != nil {
		return false
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	r := &rtspConn{c: c, br: bufio.NewReader(c)}
	code, _, _, err := r.send("OPTIONS", "rtsp://"+net.JoinHostPort(ip, itoa(port)), nil)
	// 401 (đòi mật khẩu) cũng là camera, miễn là nó nói được tiếng RTSP
	return err == nil && code > 0
}

// rtspKbps trả tốc độ bit của luồng hình (kbps). 0 kèm lỗi = không lấy được hình.
func rtspKbps(ctx context.Context, ip string, port int, path, user, pass string) (int, error) {
	if port <= 0 {
		port = 554
	}
	addr := net.JoinHostPort(ip, itoa(port))
	c, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return 0, err
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(rtspBudget))
	if !strings.HasPrefix(path, "/") && path != "" {
		path = "/" + path
	}
	base := "rtsp://" + addr + path
	r := &rtspConn{c: c, br: bufio.NewReader(c), user: user, pass: pass}

	code, _, sdp, err := r.do("DESCRIBE", base, "Accept: application/sdp")
	if err != nil {
		return 0, err
	}
	if code != 200 {
		return 0, fmt.Errorf("DESCRIBE %d", code)
	}
	code, hdr, _, err := r.do("SETUP", videoURI(base, string(sdp)), "Transport: RTP/AVP/TCP;unicast;interleaved=0-1")
	if err != nil {
		return 0, err
	}
	if code != 200 {
		return 0, fmt.Errorf("SETUP %d", code)
	}
	r.session = strings.TrimSpace(strings.SplitN(hdr["session"], ";", 2)[0])
	if code, _, _, err = r.do("PLAY", base); err != nil {
		return 0, err
	}
	if code != 200 {
		return 0, fmt.Errorf("PLAY %d", code)
	}

	n, err := r.countRTP(rtspMeasure)
	r.do("TEARDOWN", base) // dọn phiên cho camera, lỗi cũng không sao
	if n == 0 {
		if err == nil {
			err = errors.New("không có dữ liệu hình")
		}
		return 0, err
	}
	return int(float64(n) * 8 / rtspMeasure.Seconds() / 1000), nil
}

// countRTP đếm byte trên kênh 0 (RTP hình) trong khoảng d; bỏ qua kênh 1 (RTCP).
func (r *rtspConn) countRTP(d time.Duration) (int64, error) {
	r.c.SetDeadline(time.Now().Add(d + 2*time.Second))
	var n int64
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		b, err := r.br.ReadByte()
		if err != nil {
			return n, err
		}
		if b != '$' { // phản hồi chữ chen vào giữa luồng: bỏ tới hết khối
			r.skipText()
			continue
		}
		var h [3]byte
		if _, err := io.ReadFull(r.br, h[:]); err != nil {
			return n, err
		}
		sz := int64(h[1])<<8 | int64(h[2])
		if _, err := io.CopyN(io.Discard, r.br, sz); err != nil {
			return n, err
		}
		if h[0] == 0 {
			n += sz
		}
	}
	return n, nil
}

// do gửi một lệnh RTSP, tự gửi lại kèm xác thực khi camera trả 401.
func (r *rtspConn) do(method, uri string, extra ...string) (int, map[string]string, []byte, error) {
	code, hdr, body, err := r.send(method, uri, extra)
	if err != nil || code != 401 || r.user == "" {
		return code, hdr, body, err
	}
	r.parseAuth(hdr["www-authenticate"])
	if r.scheme == "" {
		return code, hdr, body, nil
	}
	return r.send(method, uri, extra)
}

func (r *rtspConn) send(method, uri string, extra []string) (int, map[string]string, []byte, error) {
	r.seq++
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s RTSP/1.0\r\nCSeq: %d\r\nUser-Agent: MAM\r\n", method, uri, r.seq)
	if r.session != "" {
		fmt.Fprintf(&b, "Session: %s\r\n", r.session)
	}
	if a := r.authHdr(method, uri); a != "" {
		fmt.Fprintf(&b, "Authorization: %s\r\n", a)
	}
	for _, x := range extra {
		b.WriteString(x + "\r\n")
	}
	b.WriteString("\r\n")
	if _, err := r.c.Write([]byte(b.String())); err != nil {
		return 0, nil, nil, err
	}
	return r.read()
}

// read đọc một phản hồi RTSP; bỏ qua các khối RTP chen vào trước phản hồi.
func (r *rtspConn) read() (int, map[string]string, []byte, error) {
	for {
		b, err := r.br.Peek(1)
		if err != nil {
			return 0, nil, nil, err
		}
		if b[0] != '$' {
			break
		}
		var h [4]byte
		if _, err := io.ReadFull(r.br, h[:]); err != nil {
			return 0, nil, nil, err
		}
		if _, err := io.CopyN(io.Discard, r.br, int64(h[2])<<8|int64(h[3])); err != nil {
			return 0, nil, nil, err
		}
	}
	line, err := r.br.ReadString('\n')
	if err != nil {
		return 0, nil, nil, err
	}
	f := strings.Fields(line)
	if len(f) < 2 || !strings.HasPrefix(f[0], "RTSP/") {
		return 0, nil, nil, fmt.Errorf("phản hồi lạ: %.40q", line)
	}
	code, _ := strconv.Atoi(f[1])
	hdr := map[string]string{}
	for {
		line, err = r.br.ReadString('\n')
		if err != nil {
			return 0, nil, nil, err
		}
		if strings.TrimSpace(line) == "" {
			break
		}
		if i := strings.IndexByte(line, ':'); i > 0 {
			hdr[strings.ToLower(strings.TrimSpace(line[:i]))] = strings.TrimSpace(line[i+1:])
		}
	}
	var body []byte
	if n, _ := strconv.Atoi(hdr["content-length"]); n > 0 && n <= 1<<20 {
		body = make([]byte, n)
		if _, err := io.ReadFull(r.br, body); err != nil {
			return 0, nil, nil, err
		}
	}
	return code, hdr, body, nil
}

// skipText bỏ qua một khối chữ (phản hồi không mong đợi) tới dòng trống.
func (r *rtspConn) skipText() {
	for i := 0; i < 50; i++ {
		line, err := r.br.ReadString('\n')
		if err != nil || strings.TrimSpace(line) == "" {
			return
		}
	}
}

func (r *rtspConn) parseAuth(h string) {
	low := strings.ToLower(h)
	switch {
	case strings.HasPrefix(low, "digest"):
		r.scheme = "Digest"
	case strings.HasPrefix(low, "basic"):
		r.scheme = "Basic"
	default:
		return
	}
	r.realm, r.nonce = authField(h, "realm"), authField(h, "nonce")
}

func authField(h, k string) string {
	i := strings.Index(strings.ToLower(h), k+"=\"")
	if i < 0 {
		return ""
	}
	rest := h[i+len(k)+2:]
	if j := strings.IndexByte(rest, '"'); j >= 0 {
		return rest[:j]
	}
	return ""
}

func (r *rtspConn) authHdr(method, uri string) string {
	if r.user == "" {
		return ""
	}
	switch r.scheme {
	case "Basic":
		return "Basic " + base64.StdEncoding.EncodeToString([]byte(r.user+":"+r.pass))
	case "Digest":
		h := func(s string) string { x := md5.Sum([]byte(s)); return hex.EncodeToString(x[:]) }
		resp := h(h(r.user+":"+r.realm+":"+r.pass) + ":" + r.nonce + ":" + h(method+":"+uri))
		return fmt.Sprintf("Digest username=%q, realm=%q, nonce=%q, uri=%q, response=%q", r.user, r.realm, r.nonce, uri, resp)
	}
	return ""
}

// videoURI tìm đường của luồng hình trong bản mô tả SDP (a=control của phần m=video).
func videoURI(base, sdp string) string {
	video, ctrl := false, ""
	for _, line := range strings.Split(sdp, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "m="):
			if video && ctrl != "" {
				return joinCtrl(base, ctrl)
			}
			video, ctrl = strings.HasPrefix(line, "m=video"), ""
		case video && strings.HasPrefix(line, "a=control:"):
			ctrl = strings.TrimSpace(strings.TrimPrefix(line, "a=control:"))
		}
	}
	if video && ctrl != "" {
		return joinCtrl(base, ctrl)
	}
	return base
}

func joinCtrl(base, ctrl string) string {
	if ctrl == "" || ctrl == "*" {
		return base
	}
	if strings.HasPrefix(ctrl, "rtsp://") {
		return ctrl
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(ctrl, "/")
}
