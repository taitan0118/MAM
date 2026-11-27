package main

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// raw gửi một thân yêu cầu THÔ (không phải JSON) bằng đúng phiên đăng nhập của client đó.
func (c *cl) raw(method, path string, body []byte) (int, http.Header, []byte) {
	c.t.Helper()
	req, _ := http.NewRequest(method, c.url+path, bytes.NewReader(body))
	if method != "GET" {
		req.Header.Set("X-MAM", "1")
	}
	resp, err := c.c.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, b
}

var pngNho = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 13, 'I', 'H', 'D', 'R', 0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0, 0x1f, 0x15, 0xc4, 0x89}

// Gửi file kèm: lưu ra tệp riêng, tin nhắn chỉ giữ tên và cỡ, có lệnh xuống máy.
func TestGuiFileKemTrongChat(t *testing.T) {
	f := setup(t)
	report(t, f.url, f.tok1, 10, 50)
	noi := []byte("mot it chu trong file log")

	code, _, b := f.super.raw("POST", "/api/chat/"+f.m1+"/tep?ten=loi.txt&text=anh%20xem%20gium", noi)
	expect(t, code, 200, "gửi file")
	if !strings.Contains(string(b), `"ten":"loi.txt"`) {
		t.Fatalf("phải trả về tên file, đang là %s", b)
	}
	l := tinCuoi(t, f, f.m1)
	id, _ := l["id"].(string)
	if l["tep"] != "loi.txt" || l["tepCo"] != float64(len(noi)) || l["text"] != "anh xem gium" {
		t.Fatalf("tin nhắn phải giữ tên, cỡ và lời nhắn, đang là %v", l)
	}
	if l["tepAnh"] == true {
		t.Fatal("file chữ không phải ảnh")
	}
	// chính file nằm riêng trên đĩa chứ không nằm trong trạng thái
	if bNoi, err := os.ReadFile(tepChatPath(id)); err != nil || !bytes.Equal(bNoi, noi) {
		t.Fatalf("file phải nằm đúng trên đĩa: %v", err)
	}
	// máy phải nhận được lệnh, kể cả còn nằm chờ
	f.s.mu.RLock()
	thay := false
	for _, c := range f.s.st.Commands {
		if c.Type == "chattep" && c.Args["mid"] == id && c.Args["ten"] == "loi.txt" {
			thay = true
		}
	}
	f.s.mu.RUnlock()
	if !thay {
		t.Fatal("phải có lệnh chattep xuống máy")
	}

	// tải về: file thường thì BẮT TẢI, không cho mở thẳng trên tên miền của server
	code, h, got := f.super.raw("GET", "/api/chat/"+f.m1+"/tep?tin="+id, nil)
	expect(t, code, 200, "tải file")
	if !bytes.Equal(got, noi) {
		t.Fatalf("nội dung tải về phải y nguyên, đang là %q", got)
	}
	if !strings.HasPrefix(h.Get("Content-Disposition"), "attachment") {
		t.Fatalf("file thường phải bắt tải về, đang là %q", h.Get("Content-Disposition"))
	}
	if h.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("phải có nosniff, không thì trình duyệt tự đoán loại rồi chạy mã trong file")
	}
}

// Ảnh thì cho xem thẳng, và nhận ra ảnh bằng BYTE THẬT chứ không tin đuôi tên.
func TestNhanRaAnhTheoByteThat(t *testing.T) {
	f := setup(t)
	report(t, f.url, f.tok1, 10, 50)

	code, _, _ := f.super.raw("POST", "/api/chat/"+f.m1+"/tep?ten=anh.txt", pngNho)
	expect(t, code, 200, "gửi ảnh đặt tên .txt")
	l := tinCuoi(t, f, f.m1)
	if l["tepAnh"] != true {
		t.Fatal("byte đầu là PNG thì phải nhận ra là ảnh, dù tên đặt .txt")
	}
	id, _ := l["id"].(string)
	_, h, _ := f.super.raw("GET", "/api/chat/"+f.m1+"/tep?tin="+id, nil)
	if !strings.HasPrefix(h.Get("Content-Disposition"), "inline") {
		t.Fatalf("ảnh thì cho xem thẳng, đang là %q", h.Get("Content-Disposition"))
	}

	// ngược lại: file HTML đặt tên .png thì KHÔNG được coi là ảnh, phải bắt tải về
	code, _, _ = f.super.raw("POST", "/api/chat/"+f.m1+"/tep?ten=hinh.png", []byte("<html><script>alert(1)</script></html>"))
	expect(t, code, 200, "gửi html đặt tên .png")
	l = tinCuoi(t, f, f.m1)
	if l["tepAnh"] == true {
		t.Fatal("file HTML đặt tên .png KHÔNG được coi là ảnh — cho mở thẳng là chạy mã trong Console")
	}
	id, _ = l["id"].(string)
	_, h, _ = f.super.raw("GET", "/api/chat/"+f.m1+"/tep?tin="+id, nil)
	if !strings.HasPrefix(h.Get("Content-Disposition"), "attachment") {
		t.Fatalf("phải bắt tải về, đang là %q", h.Get("Content-Disposition"))
	}
}

// Tên file do người gửi đặt không bao giờ được thành đường dẫn trên đĩa.
func TestTenFileKhongThanhDuongDan(t *testing.T) {
	for _, c := range []struct{ vao, ra string }{
		{`..\..\config.json`, "config.json"},
		{"/etc/passwd", "passwd"},
		{"  ", "file"},
		{"..", "file"},
		{"anh dep.png", "anh dep.png"},
	} {
		if got := tenTepSach(c.vao); got != c.ra {
			t.Fatalf("tenTepSach(%q) = %q, cần %q", c.vao, got, c.ra)
		}
	}
	// tên trên đĩa luôn dựng từ ID, không dính gì tới tên người gửi đặt
	chatDir = t.TempDir()
	if p := tepChatPath("../x"); p != "" {
		t.Fatalf("id lạ phải trả rỗng, đang là %q", p)
	}
	if p := tepChatPath("abc123"); filepath.Base(p) != "abc123.bin" {
		t.Fatalf("tên trên đĩa phải là <id>.bin, đang là %q", p)
	}
}

// Thu hồi tin có file thì xóa luôn file, không để lại trên đĩa.
func TestThuHoiXoaLuonFile(t *testing.T) {
	f := setup(t)
	report(t, f.url, f.tok1, 10, 50)
	code, _, _ := f.super.raw("POST", "/api/chat/"+f.m1+"/tep?ten=bimat.txt", []byte("noi dung rieng tu"))
	expect(t, code, 200, "gửi file")
	id, _ := tinCuoi(t, f, f.m1)["id"].(string)
	p := tepChatPath(id)
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("file phải có trước đã: %v", err)
	}

	code, _, _ = f.super.do("POST", "/api/chat/"+f.m1+"/tin", map[string]string{"id": id, "lam": "thu"}, nil)
	expect(t, code, 200, "thu hồi")
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("thu hồi phải xóa HẲN file, không chỉ ẩn tin nhắn")
	}
	l := tinCuoi(t, f, f.m1)
	if l["tep"] != nil || l["thu"] != true {
		t.Fatalf("tin phải sạch file và được đánh dấu đã thu hồi, đang là %v", l)
	}
	code, _, _ = f.super.raw("GET", "/api/chat/"+f.m1+"/tep?tin="+id, nil)
	expect(t, code, 404, "tải file đã thu hồi")
}

// Thư mục đầy thì dọn file CŨ NHẤT trước, và đánh dấu chứ không để nó im lặng biến mất.
func TestDayThiDonFileCuNhat(t *testing.T) {
	f := setup(t)
	f.s.mu.Lock()
	for i := 0; i < 3; i++ {
		l := f.s.addChat(f.m1, "x", "", false)
		l.Tep, l.TepCo = "f"+itoa(i)+".bin", chatTepTong/2 // ba file, mỗi file nửa mức cho phép
	}
	nDon, thu := f.s.gonTepCu()
	ds := append([]*ChatLine(nil), f.s.st.Chat...)
	f.s.mu.Unlock()
	if nDon != 1 || thu != chatTepTong/2 {
		t.Fatalf("phải dọn đúng 1 file cũ nhất, dọn %d (%d byte)", nDon, thu)
	}
	var don, con []string
	for _, c := range ds {
		if c.Tep == "" {
			continue
		}
		if c.TepDon {
			don = append(don, c.Tep)
		} else {
			con = append(con, c.Tep)
		}
	}
	if len(don) != 1 || don[0] != "f0.bin" {
		t.Fatalf("phải dọn file CŨ NHẤT (f0.bin), đang dọn %v", don)
	}
	if len(con) != 2 {
		t.Fatalf("hai file mới phải còn, đang còn %v", con)
	}
	// tên vẫn giữ: người xem phải biết là đã từng có file ở đó
	for _, c := range ds {
		if c.TepDon && c.Tep == "" {
			t.Fatal("dọn file thì vẫn phải giữ TÊN, không thì người ta tưởng chưa gửi gì")
		}
	}
}

// File không còn tin nhắn nào trỏ tới thì dọn lúc khởi động.
func TestDonFileMoCoi(t *testing.T) {
	f := setup(t)
	f.s.mu.Lock()
	l := f.s.addChat(f.m1, "x", "", false)
	l.Tep, l.TepCo = "con.txt", 3
	os.MkdirAll(chatDir, 0o700)
	os.WriteFile(tepChatPath(l.ID), []byte("abc"), 0o600)
	os.WriteFile(filepath.Join(chatDir, "mocoi0000.bin"), []byte("rac"), 0o600)
	n := f.s.donTepMoCoi()
	f.s.mu.Unlock()
	if n != 1 {
		t.Fatalf("phải dọn đúng 1 file mồ côi, dọn %d", n)
	}
	if _, err := os.Stat(tepChatPath(l.ID)); err != nil {
		t.Fatal("file đang có tin nhắn dùng thì KHÔNG được đụng tới")
	}
}
