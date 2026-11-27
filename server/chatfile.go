package main

// File gửi kèm trong chat (ảnh hoặc file bất kỳ).
//
// Nguyên tắc: KHÔNG nhét file vào trạng thái. Cả trạng thái MAM là một cục JSON ghi lại mỗi lần
// save — nhét vài chục MB vào đó là mỗi lần ai nhắn một câu, server lại ghi cả đống byte ảnh xuống
// đĩa. Nên file nằm riêng từng tệp trong <dữ liệu>/chat-files, trong tin nhắn chỉ giữ tên và cỡ.
//
// Tên tệp trên đĩa LUÔN là <id dòng chat>.bin, không bao giờ lấy theo tên người gửi đặt: tên người
// gửi đặt có thể là "..\\..\\config.json" hay "x.ps1". Tên thật chỉ dùng lúc trả về cho người tải.

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// chatTepMax: giới hạn một file, đại ca đặt 200 MB. Đọc theo dòng thẳng xuống đĩa nên bộ nhớ server
// không phình theo cỡ file — máy này không có pagefile, ôm 200 MB vào RAM là chết.
const chatTepMax = 200 << 20

// chatTepTong: TỔNG dung lượng cả thư mục file chat. Giới hạn mỗi file không cứu được chuyện nhiều
// người gửi mỗi người vài chục tấm ảnh — chặn từng file mà không chặn tổng là vẫn đầy ổ như thường.
// Quá mức này thì dọn file CŨ NHẤT trước, và dòng chat được đánh dấu "đã dọn" chứ không im lặng
// biến mất: người xem phải biết là đã từng có file ở đó.
const chatTepTong = 20 << 30 // 20 GB

var (
	chatDir    string // đặt lúc khởi động, cạnh avatarDir
	reTinID    = regexp.MustCompile(`^[0-9a-f]{1,32}$`)
	anhXemDuoc = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}
)

// tepChatPath: đường tệp của file đính kèm một dòng chat. id lạ thì trả rỗng.
func tepChatPath(id string) string {
	if chatDir == "" || !reTinID.MatchString(id) {
		return ""
	}
	return filepath.Join(chatDir, id+".bin")
}

// tenTepSach: tên file hiện cho người dùng. Bỏ đường dẫn và ký tự điều khiển; rỗng thì đặt tên chung.
func tenTepSach(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexAny(s, `/\`); i >= 0 {
		s = s[i+1:]
	}
	s = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, s)
	if n := []rune(s); len(n) > 120 {
		s = string(n[:120])
	}
	if s == "" || s == "." || s == ".." {
		s = "file"
	}
	return s
}

// nhanTep đọc thân yêu cầu thẳng xuống một tệp tạm trong chatDir. Trả về đường tệp tạm, số byte,
// và có phải ảnh xem thẳng được không. Quá giới hạn thì xóa tệp tạm rồi báo lỗi.
func nhanTep(w http.ResponseWriter, r *http.Request) (string, int64, bool, error) {
	if chatDir == "" {
		return "", 0, false, errors.New("chưa có chỗ lưu file")
	}
	// Server đặt ReadTimeout 30 giây cho MỌI yêu cầu. File 200 MB qua mạng chậm thì 30 giây không
	// đủ, không nới ra là file lớn luôn bị cắt giữa chừng mà chẳng hiểu vì sao.
	if rc := http.NewResponseController(w); rc != nil {
		rc.SetReadDeadline(time.Now().Add(30 * time.Minute))
	}
	if err := os.MkdirAll(chatDir, 0o700); err != nil {
		return "", 0, false, err
	}
	f, err := os.CreateTemp(chatDir, "up-*")
	if err != nil {
		return "", 0, false, err
	}
	tmp := f.Name()
	xoa := func() { f.Close(); os.Remove(tmp) }

	// 512 byte đầu để đoán loại: phải soi BYTE THẬT chứ không tin đuôi tên hay Content-Type người gửi khai
	dau := make([]byte, 512)
	nDau, _ := io.ReadFull(r.Body, dau)
	dau = dau[:nDau]
	if _, err := f.Write(dau); err != nil {
		xoa()
		return "", 0, false, err
	}
	// +1 để biết nó có vượt giới hạn hay không, chứ không âm thầm cắt cụt file
	n, err := io.Copy(f, io.LimitReader(r.Body, chatTepMax-int64(nDau)+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return "", 0, false, err
	}
	co := int64(nDau) + n
	if co == 0 {
		os.Remove(tmp)
		return "", 0, false, errors.New("file rỗng")
	}
	if co > chatTepMax {
		os.Remove(tmp)
		return "", 0, false, errors.New("file quá lớn")
	}
	return tmp, co, anhXemDuoc[strings.ToLower(strings.TrimSpace(strings.SplitN(http.DetectContentType(dau), ";", 2)[0]))], nil
}

// themTep gắn tệp tạm vào một dòng chat mới. Gọi khi đang giữ s.mu. Hỏng thì dòng chat không được
// tạo và tệp tạm bị xóa — thà không có tin nhắn còn hơn có tin trỏ vào file không tồn tại.
func (s *Server) themTep(machine, by, text, byID, tmp, ten string, co int64, anh bool, client bool) (*ChatLine, error) {
	l := s.addChat(machine, by, text, client, byID)
	p := tepChatPath(l.ID)
	if p == "" {
		s.st.Chat = s.st.Chat[:len(s.st.Chat)-1]
		os.Remove(tmp)
		return nil, errors.New("không đặt được tên tệp")
	}
	if err := os.Rename(tmp, p); err != nil {
		s.st.Chat = s.st.Chat[:len(s.st.Chat)-1]
		os.Remove(tmp)
		return nil, err
	}
	l.Tep, l.TepCo, l.TepAnh = ten, co, anh
	return l, nil
}

// traTep trả file về cho người tải. Ảnh thì cho xem thẳng, còn lại BẮT TẢI VỀ: một file .html hay
// .svg người ta gửi lên mà cho mở thẳng trên tên miền của server là mở cửa chạy mã trong Console.
func traTep(w http.ResponseWriter, r *http.Request, l *ChatLine) {
	p := tepChatPath(l.ID)
	f, err := os.Open(p)
	if p == "" || err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	fi, _ := f.Stat()
	// cùng lý do như lúc nhận: WriteTimeout 30 giây không đủ để đẩy một file lớn đi
	if rc := http.NewResponseController(w); rc != nil {
		rc.SetWriteDeadline(time.Now().Add(30 * time.Minute))
	}
	ten := tenTepSach(l.Tep)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=86400") // file không bao giờ đổi, id là một lần
	if l.TepAnh {
		w.Header().Set("Content-Type", "image/jpeg") // chỉ ảnh thường mới tới được đây; trình duyệt tự nhận đúng loại
		w.Header().Set("Content-Disposition", "inline; filename*=UTF-8''"+urlTen(ten))
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+urlTen(ten))
	}
	http.ServeContent(w, r, "", fi.ModTime(), f)
}

func urlTen(s string) string {
	return strings.ReplaceAll(mime.QEncoding.Encode("utf-8", s), " ", "%20")
}

func itoa64(v int64) string { return strconv.FormatInt(v, 10) }

// coTep: cỡ file cho người đọc. KB/MB theo 1024 như Windows hiện, khỏi lệch với cái đại ca thấy ở Explorer.
func coTep(n int64) string {
	switch {
	case n >= 1<<20:
		return strconv.FormatFloat(float64(n)/(1<<20), 'f', 1, 64) + " MB"
	case n >= 1<<10:
		return strconv.FormatInt((n+512)/(1<<10), 10) + " KB"
	default:
		return strconv.FormatInt(n, 10) + " B"
	}
}

// xoaTepChat xóa tệp của một dòng chat (dòng bị cắt bớt, hoặc tin bị thu hồi).
func xoaTepChat(id string) {
	if p := tepChatPath(id); p != "" {
		os.Remove(p)
	}
}

// donTepMoCoi: xóa tệp không còn dòng chat nào trỏ tới, và tệp tạm bỏ dở. Chạy lúc khởi động khi
// đang giữ s.mu. Không có bước này thì mỗi lần cắt bớt tin cũ là để lại rác vĩnh viễn trên đĩa.
func (s *Server) donTepMoCoi() int {
	if chatDir == "" {
		return 0
	}
	ds, err := os.ReadDir(chatDir)
	if err != nil {
		return 0
	}
	con := map[string]bool{}
	for _, c := range s.st.Chat {
		if c.Tep != "" {
			con[c.ID+".bin"] = true
		}
	}
	n := 0
	for _, e := range ds {
		if e.IsDir() || con[e.Name()] {
			continue
		}
		// tệp tạm của lượt gửi đang dở (server tắt giữa chừng) thì chỉ xóa khi đã cũ
		if strings.HasPrefix(e.Name(), "up-") {
			if fi, err := e.Info(); err == nil && time.Since(fi.ModTime()) < time.Hour {
				continue
			}
		}
		if os.Remove(filepath.Join(chatDir, e.Name())) == nil {
			n++
		}
	}
	return n
}

// gonTepCu: giữ tổng dung lượng file chat dưới chatTepTong bằng cách dọn file CŨ NHẤT trước.
// Gọi khi đang giữ s.mu, sau mỗi lượt nhận file. Trả về số file đã dọn và số byte thu lại.
//
// Cố ý KHÔNG xóa dòng chat: chỉ gỡ file ra và đánh dấu TepDon, để người xem còn thấy "đã từng có
// file tên này, bị dọn vì hết chỗ" — im lặng làm file bốc hơi là người ta tưởng mình chưa gửi.
func (s *Server) gonTepCu() (int, int64) {
	var tong int64
	for _, c := range s.st.Chat {
		if c.Tep != "" && !c.TepDon {
			tong += c.TepCo
		}
	}
	if tong <= chatTepTong {
		return 0, 0
	}
	n, thu := 0, int64(0)
	for _, c := range s.st.Chat { // s.st.Chat xếp từ cũ tới mới, nên duyệt xuôi là dọn cũ trước
		if tong <= chatTepTong {
			break
		}
		if c.Tep == "" || c.TepDon {
			continue
		}
		xoaTepChat(c.ID)
		tong -= c.TepCo
		thu += c.TepCo
		c.TepDon, c.TepCo, c.TepAnh = true, 0, false
		n++
	}
	return n, thu
}
