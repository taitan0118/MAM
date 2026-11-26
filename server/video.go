package main

// Xem video camera thời gian thực: đọc RTSP, KHÔNG giải mã, chỉ gói khung H.264 có sẵn vào fMP4 rồi
// đẩy thẳng cho trình duyệt phát bằng MSE. Không cần ffmpeg, CPU gần như không tốn vì không chuyển mã.
// Dùng lại bộ bắt tay RTSP ở rtsp.go (DESCRIBE/SETUP/PLAY, Digest, RTP lồng trong TCP).
//
// Giới hạn đã biết:
//   shortcut: chỉ nhận H.264. Camera chạy H.265 thì báo lỗi rõ để người dùng đổi luồng phụ, vì
//   Chrome không phát được H.265. Nâng cấp khi nào cần: thêm hộp hvcC và thẻ hvc1.
//   shortcut: bề ngang/cao ghi trong moov là số tạm, trình duyệt lấy kích thước thật từ SPS trong
//   luồng. Nâng cấp khi nào thấy hình méo: đọc SPS ra kích thước thật (phải giải mã exp-golomb).
//   shortcut: không gửi lệnh giữ phiên. Camera nào tự cắt phiên khi lâu không nhận lệnh thì luồng
//   sẽ đứt, trình duyệt nối lại. Nâng cấp khi nào thấy đứt đều đặn: gửi OPTIONS mỗi 30 giây.

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

const (
	mp4Nhip    = 90000            // nhịp đồng hồ của luồng hình, RTP H.264 luôn dùng mức này
	videoToiDa = 3                // bao nhiêu người được xem video cùng lúc
	videoLau   = 10 * time.Minute // cắt luồng bỏ quên; trình duyệt tự nối lại nếu còn mở
)

var videoDang int32 // số luồng đang chạy

// ===== đọc RTSP ra từng khung hình =====

type luongH264 struct {
	r        *rtspConn
	base     string
	sps, pps []byte
	au       [][]byte // các mảnh NAL của khung đang gom
	fu       []byte   // mảnh NAL bị cắt làm nhiều gói RTP
	key      bool
}

var errMaLa = errors.New("bộ mã lạ")

// Thiết bị nào chưa khai đường luồng thì thử các đường hay gặp, luồng PHỤ trước: luồng chính của
// Hikvision/Dahua phần lớn là H.265 mà Chrome không phát được, còn luồng phụ gần như luôn là H.264.
var duongPhu = []string{"/Streaming/Channels/102", "/cam/realmonitor?channel=1&subtype=1", "/h264/ch1/sub/av_stream", ""}

// moLuong mở luồng hình. path rỗng thì dò lần lượt các đường hay gặp.
// shortcut: dò lại mỗi lần xem, không nhớ đường đã được. Nâng cấp khi nào thấy chậm: ghi lại vào thiết bị.
func moLuong(ctx context.Context, ip string, port int, path, user, pass string) (*luongH264, error) {
	if path != "" {
		return moMot(ctx, ip, port, path, user, pass)
	}
	var dau, maLa error
	for _, p := range duongPhu {
		l, err := moMot(ctx, ip, port, p, user, pass)
		if err == nil {
			return l, nil
		}
		if errors.Is(err, errChuaKhai) || errors.Is(err, errSaiMatKhau) {
			return nil, err // sai tài khoản thì đường nào cũng sai, dò thêm vô ích
		}
		if errors.Is(err, errMaLa) {
			maLa = err
		}
		if dau == nil {
			dau = err
		}
	}
	if maLa != nil { // camera có hình, chỉ sai bộ mã: báo cái đó mới giúp được người dùng
		return nil, maLa
	}
	return nil, dau
}

func moMot(ctx context.Context, ip string, port int, path, user, pass string) (*luongH264, error) {
	if port <= 0 {
		port = 554
	}
	addr := net.JoinHostPort(ip, itoa(port))
	c, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	if path != "" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	l := &luongH264{r: &rtspConn{c: c, br: bufio.NewReaderSize(c, 64<<10), user: user, pass: pass}, base: "rtsp://" + addr + path}
	c.SetDeadline(time.Now().Add(15 * time.Second))

	code, _, sdp, err := l.r.do("DESCRIBE", l.base, "Accept: application/sdp")
	if err != nil {
		c.Close()
		return nil, err
	}
	if code == 401 {
		c.Close()
		if user == "" {
			return nil, errChuaKhai
		}
		return nil, errSaiMatKhau
	}
	if code != 200 {
		c.Close()
		return nil, fmt.Errorf("DESCRIBE %d", code)
	}
	ten, sps, pps := doSDP(string(sdp))
	if ten == "" {
		c.Close()
		return nil, errors.New("camera không khai luồng hình nào")
	}
	if ten != "H264" {
		c.Close()
		return nil, fmt.Errorf("camera đang phát %s, trình duyệt chỉ xem được H.264 — vào web camera đổi luồng phụ sang H.264: %w", ten, errMaLa)
	}
	l.sps, l.pps = sps, pps

	code, hdr, _, err := l.r.do("SETUP", videoURI(l.base, string(sdp)), "Transport: RTP/AVP/TCP;unicast;interleaved=0-1")
	if err != nil {
		c.Close()
		return nil, err
	}
	if code != 200 {
		c.Close()
		return nil, fmt.Errorf("SETUP %d", code)
	}
	l.r.session = strings.TrimSpace(strings.SplitN(hdr["session"], ";", 2)[0])
	if code, _, _, err = l.r.do("PLAY", l.base); err != nil {
		c.Close()
		return nil, err
	}
	if code != 200 {
		c.Close()
		return nil, fmt.Errorf("PLAY %d", code)
	}
	return l, nil
}

func (l *luongH264) dong() {
	l.r.c.SetDeadline(time.Now().Add(2 * time.Second))
	l.r.do("TEARDOWN", l.base) // trả phiên cho camera; lỗi cũng không sao
	l.r.c.Close()
}

// docAU đọc tới khi gom đủ một khung hình (access unit).
func (l *luongH264) docAU() ([][]byte, uint32, bool, error) {
	for {
		b, err := l.r.br.ReadByte()
		if err != nil {
			return nil, 0, false, err
		}
		if b != '$' { // phản hồi chữ chen vào giữa luồng
			l.r.skipText()
			continue
		}
		var h [3]byte
		if _, err := io.ReadFull(l.r.br, h[:]); err != nil {
			return nil, 0, false, err
		}
		sz := int(h[1])<<8 | int(h[2])
		if h[0] != 0 || sz < 12 { // kênh 1 là RTCP, không phải hình
			if _, err := io.CopyN(io.Discard, l.r.br, int64(sz)); err != nil {
				return nil, 0, false, err
			}
			continue
		}
		p := make([]byte, sz)
		if _, err := io.ReadFull(l.r.br, p); err != nil {
			return nil, 0, false, err
		}
		if au, ts, key, xong := l.nhan(p); xong {
			return au, ts, key, nil
		}
	}
}

// nhan bóc một gói RTP; trả khung hình khi gặp bit đánh dấu hết khung.
func (l *luongH264) nhan(p []byte) ([][]byte, uint32, bool, bool) {
	mark := p[1]&0x80 != 0
	ts := binary.BigEndian.Uint32(p[4:8])
	off := 12 + int(p[0]&0x0F)*4
	if p[0]&0x10 != 0 { // có phần mở rộng
		if len(p) < off+4 {
			return nil, 0, false, false
		}
		off += 4 + 4*int(binary.BigEndian.Uint16(p[off+2:off+4]))
	}
	if off >= len(p) {
		return nil, 0, false, false
	}
	pay := p[off:]
	switch typ := pay[0] & 0x1F; {
	case typ >= 1 && typ <= 23: // một NAL gọn trong một gói
		l.them(pay)
	case typ == 24: // STAP-A: nhiều NAL nhỏ ghép chung một gói
		for q := pay[1:]; len(q) >= 2; {
			n := int(q[0])<<8 | int(q[1])
			if n == 0 || len(q) < 2+n {
				break
			}
			l.them(q[2 : 2+n])
			q = q[2+n:]
		}
	case typ == 28: // FU-A: một NAL bị cắt ra nhiều gói
		if len(pay) < 3 {
			return nil, 0, false, false
		}
		fu := pay[1]
		if fu&0x80 != 0 { // mảnh đầu: dựng lại byte đầu của NAL
			l.fu = []byte{(pay[0] & 0xE0) | (fu & 0x1F)}
		}
		if l.fu == nil { // vào giữa chừng, bỏ cho tới NAL sau
			return nil, 0, false, false
		}
		l.fu = append(l.fu, pay[2:]...)
		if fu&0x40 != 0 { // mảnh cuối
			l.them(l.fu)
			l.fu = nil
		}
	}
	if mark && len(l.au) > 0 {
		au, key := l.au, l.key
		l.au, l.key = nil, false
		return au, ts, key, true
	}
	return nil, 0, false, false
}

func (l *luongH264) them(n []byte) {
	if len(n) == 0 {
		return
	}
	switch n[0] & 0x1F {
	case 5:
		l.key = true // khung chính (IDR): chỗ trình duyệt bắt đầu xem được
	case 7:
		l.sps = n // camera gửi lại trong luồng thì dùng bản mới nhất
	case 8:
		l.pps = n
	}
	l.au = append(l.au, n)
}

// doSDP đọc tên bộ mã và bộ tham số SPS/PPS của luồng hình trong bản mô tả SDP.
func doSDP(sdp string) (ten string, sps, pps []byte) {
	video := false
	for _, ln := range strings.Split(sdp, "\n") {
		ln = strings.TrimSpace(ln)
		switch {
		case strings.HasPrefix(ln, "m="):
			video = strings.HasPrefix(ln, "m=video")
		case video && strings.HasPrefix(ln, "a=rtpmap:"):
			if i := strings.IndexByte(ln, ' '); i > 0 && ten == "" {
				ten = strings.ToUpper(strings.SplitN(ln[i+1:], "/", 2)[0])
			}
		case video && strings.HasPrefix(ln, "a=fmtp:"):
			for _, kv := range strings.Split(ln, ";") {
				kv = strings.TrimSpace(kv)
				if !strings.HasPrefix(strings.ToLower(kv), "sprop-parameter-sets=") {
					continue
				}
				for _, x := range strings.Split(kv[len("sprop-parameter-sets="):], ",") {
					b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(x))
					if err != nil || len(b) == 0 {
						continue
					}
					switch b[0] & 0x1F {
					case 7:
						sps = b
					case 8:
						pps = b
					}
				}
			}
		}
	}
	return
}

// ===== gói khung hình vào fMP4 =====

type goiMP4 struct {
	seq         uint32
	tong        uint64 // tổng thời gian đã phát, theo nhịp 90000
	truoc, buoc uint32
	daCo        bool
}

func mp4hop(typ string, phan ...[]byte) []byte {
	n := 8
	for _, p := range phan {
		n += len(p)
	}
	b := make([]byte, 0, n)
	b = append(b, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	b = append(b, typ...)
	for _, p := range phan {
		b = append(b, p...)
	}
	return b
}

func mp4u32(v uint32) []byte { return []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)} }
func mp4u16(v uint16) []byte { return []byte{byte(v >> 8), byte(v)} }
func mp4u64(v uint64) []byte { return append(mp4u32(uint32(v>>32)), mp4u32(uint32(v))...) }

// mp4matran là ma trận đơn vị, bắt buộc có trong tkhd và mvhd.
func mp4matran() []byte {
	b := make([]byte, 36)
	copy(b[0:], mp4u32(0x00010000))
	copy(b[16:], mp4u32(0x00010000))
	copy(b[32:], mp4u32(0x40000000))
	return b
}

func maH264(sps []byte) string {
	if len(sps) < 4 {
		return "avc1.42E01E"
	}
	return fmt.Sprintf("avc1.%02X%02X%02X", sps[1], sps[2], sps[3])
}

// dauGoi dựng đoạn mở đầu (ftyp + moov): mô tả luồng, gửi một lần trước mọi khung hình.
func (g *goiMP4) dauGoi(sps, pps []byte) []byte {
	cfg := []byte{1, sps[1], sps[2], sps[3], 0xFF, 0xE1}
	cfg = append(cfg, mp4u16(uint16(len(sps)))...)
	cfg = append(cfg, sps...)
	cfg = append(cfg, 1)
	cfg = append(cfg, mp4u16(uint16(len(pps)))...)
	cfg = append(cfg, pps...)

	const rong, cao = 1920, 1080 // số tạm, trình duyệt lấy kích thước thật từ SPS
	hinh := make([]byte, 0, 78)
	hinh = append(hinh, make([]byte, 6)...)  // reserved
	hinh = append(hinh, 0, 1)                // chỉ số mô tả dữ liệu
	hinh = append(hinh, make([]byte, 16)...) // pre_defined + reserved
	hinh = append(hinh, mp4u16(rong)...)
	hinh = append(hinh, mp4u16(cao)...)
	hinh = append(hinh, 0, 0x48, 0, 0, 0, 0x48, 0, 0) // 72 dpi ngang và dọc
	hinh = append(hinh, 0, 0, 0, 0)
	hinh = append(hinh, 0, 1)                // số khung trên một mẫu
	hinh = append(hinh, make([]byte, 32)...) // tên bộ nén, để trống
	hinh = append(hinh, 0, 0x18)             // 24 bit màu
	hinh = append(hinh, 0xFF, 0xFF)
	avc1 := mp4hop("avc1", hinh, mp4hop("avcC", cfg))

	trong := []byte{0, 0, 0, 0, 0, 0, 0, 0}
	stbl := mp4hop("stbl",
		mp4hop("stsd", []byte{0, 0, 0, 0}, mp4u32(1), avc1),
		mp4hop("stts", trong), mp4hop("stsc", trong),
		mp4hop("stsz", []byte{0, 0, 0, 0}, mp4u32(0), mp4u32(0)),
		mp4hop("stco", trong))
	minf := mp4hop("minf",
		mp4hop("vmhd", []byte{0, 0, 0, 1}, make([]byte, 8)),
		mp4hop("dinf", mp4hop("dref", []byte{0, 0, 0, 0}, mp4u32(1), mp4hop("url ", []byte{0, 0, 0, 1}))),
		stbl)
	mdia := mp4hop("mdia",
		mp4hop("mdhd", []byte{0, 0, 0, 0}, mp4u32(0), mp4u32(0), mp4u32(mp4Nhip), mp4u32(0), []byte{0x55, 0xC4, 0, 0}),
		mp4hop("hdlr", []byte{0, 0, 0, 0}, mp4u32(0), []byte("vide"), make([]byte, 12), []byte("MAM\x00")),
		minf)
	tkhd := mp4hop("tkhd", []byte{0, 0, 0, 7}, mp4u32(0), mp4u32(0), mp4u32(1), mp4u32(0), mp4u32(0),
		make([]byte, 8), make([]byte, 8), mp4matran(), mp4u32(rong<<16), mp4u32(cao<<16))
	moov := mp4hop("moov",
		mp4hop("mvhd", []byte{0, 0, 0, 0}, mp4u32(0), mp4u32(0), mp4u32(1000), mp4u32(0),
			mp4u32(0x00010000), []byte{1, 0}, []byte{0, 0}, make([]byte, 8), mp4matran(), make([]byte, 24), mp4u32(2)),
		mp4hop("trak", tkhd, mdia),
		mp4hop("mvex", mp4hop("trex", []byte{0, 0, 0, 0}, mp4u32(1), mp4u32(1), mp4u32(0), mp4u32(0), mp4u32(0))))
	return append(mp4hop("ftyp", []byte("isom"), mp4u32(512), []byte("isomiso2avc1mp41")), moov...)
}

// khung gói một khung hình thành một đoạn fMP4 (moof + mdat).
func (g *goiMP4) khung(nals [][]byte, ts uint32, key bool) []byte {
	var data []byte
	for _, n := range nals {
		data = append(data, mp4u32(uint32(len(n)))...)
		data = append(data, n...)
	}
	// Nhịp giữa hai khung lấy từ dấu thời gian RTP. Khung đầu chưa có gì để trừ nên tạm tính 25
	// hình/giây; sai số không dồn lại vì mỗi đoạn đều ghi lại mốc thời gian tuyệt đối (tfdt).
	if g.daCo {
		if d := ts - g.truoc; d > 0 && d < mp4Nhip*5 { // bỏ giá trị vô lý khi camera nhảy giờ
			g.buoc = d
		}
		g.tong += uint64(g.buoc)
	} else {
		g.daCo, g.buoc = true, mp4Nhip/25
	}
	g.truoc = ts
	g.seq++

	co := uint32(0x01010000) // khung thường: dựa vào khung khác, không vào xem từ đây được
	if key {
		co = 0x02000000
	}
	moof := mp4hop("moof",
		mp4hop("mfhd", []byte{0, 0, 0, 0}, mp4u32(g.seq)),
		mp4hop("traf",
			mp4hop("tfhd", []byte{0, 0x02, 0, 0}, mp4u32(1)), // mốc tính từ đầu moof
			mp4hop("tfdt", []byte{1, 0, 0, 0}, mp4u64(g.tong)),
			mp4hop("trun", []byte{0, 0, 0x07, 0x01}, mp4u32(1), mp4u32(0), mp4u32(g.buoc), mp4u32(uint32(len(data))), mp4u32(co))))
	// chỗ dữ liệu nằm cách đầu moof bao nhiêu byte; chỉ biết sau khi dựng xong moof
	copy(moof[len(moof)-16:], mp4u32(uint32(len(moof)+8)))
	return append(moof, mp4hop("mdat", data)...)
}

// ===== đường gọi =====

// hDeviceVideo đẩy video trực tiếp của một camera xuống trình duyệt dưới dạng fMP4 nối tiếp.
func (s *Server) hDeviceVideo(w http.ResponseWriter, r *http.Request, me *Account) {
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
	ghi, ch := s.dauGhiCua(d)
	soGhi := len(s.st.Settings.Nvrs)
	s.mu.RUnlock()

	// ?tua=2026-11-26T14:30 — xem lại từ mốc đó, lấy hình từ đầu ghi chứ không phải từ camera:
	// camera không lưu gì cả, chỉ đầu ghi mới có băng để tua.
	var t0 time.Time
	tua := r.URL.Query().Get("tua")
	if tua != "" {
		if soGhi == 0 {
			jerr(w, 400, "Chưa khai đầu ghi nên không tua lại được. Vào Cài đặt → Đầu ghi camera để khai.")
			return
		}
		if ch == 0 {
			jerr(w, 400, "Không đầu ghi nào kéo hình camera này về nên không có gì để tua lại")
			return
		}
		var e error
		if t0, e = time.ParseInLocation("2006-01-02T15:04", tua, time.Local); e != nil {
			jerr(w, 400, "Giờ muốn tua lại không đúng dạng")
			return
		}
		if t0.After(time.Now()) {
			jerr(w, 400, "Giờ muốn tua lại nằm ở tương lai")
			return
		}
	}

	if atomic.AddInt32(&videoDang, 1) > videoToiDa {
		atomic.AddInt32(&videoDang, -1)
		jerr(w, 503, "Đang có quá nhiều người xem video cùng lúc, thử lại sau")
		return
	}
	defer atomic.AddInt32(&videoDang, -1)

	ctx, thoi := context.WithTimeout(r.Context(), videoLau)
	defer thoi()
	var l *luongH264
	var err error
	quaGhi := false
	switch {
	case tua != "":
		l, err = moTua(ctx, ghi.IP, ghi.User, ghi.Pass, ch, t0)
		quaGhi = err == nil
	case ch > 0 && ghi.IP != "":
		// Camera nằm trên đầu ghi thì lấy hình QUA ĐẦU GHI bằng tài khoản đầu ghi: khai một đầu ghi
		// là xem được cả dàn camera của nó, khỏi khai mật khẩu riêng cho từng dải. Đầu ghi không
		// cho thì vẫn gọi thẳng camera như cũ — mất đường này còn đường kia, không bỏ trắng.
		if l, err = moKenh(ctx, ghi.IP, ghi.User, ghi.Pass, ch); err == nil {
			quaGhi = true
		} else if l2, e2 := moLuong(ctx, cp.IP, cp.Port, cp.Path, cp.User, cp.Pass); e2 == nil {
			l, err = l2, nil
		}
	default:
		l, err = moLuong(ctx, cp.IP, cp.Port, cp.Path, cp.User, cp.Pass)
	}
	if err != nil {
		jerr(w, 502, "Không xem được video: "+err.Error())
		return
	}
	defer l.dong()

	rc := http.NewResponseController(w)
	rc.SetWriteDeadline(time.Time{}) // luồng chạy lâu, bỏ hạn ghi 30 giây của server
	var g goiMP4
	daMo := false
	for ctx.Err() == nil {
		l.r.c.SetReadDeadline(time.Now().Add(10 * time.Second))
		nals, ts, key, err := l.docAU()
		if err != nil {
			return
		}
		if !daMo {
			if !key || l.sps == nil || l.pps == nil {
				continue // chờ khung chính đầu tiên, không thì trình duyệt không dựng được hình
			}
			w.Header().Set("Content-Type", "video/mp4")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-Codec", maH264(l.sps))
			if quaGhi {
				w.Header().Set("X-Qua", "daughi") // Console nói rõ hình đang đi qua đầu ghi
			}
			w.WriteHeader(200)
			if _, err := w.Write(g.dauGoi(l.sps, l.pps)); err != nil {
				return
			}
			daMo = true
		}
		if _, err := w.Write(g.khung(nals, ts, key)); err != nil {
			return
		}
		rc.Flush()
	}
}
