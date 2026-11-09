package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type cl struct {
	t   *testing.T
	c   *http.Client
	url string
}

func newSrv(t *testing.T) (*Server, string) {
	s, err := NewServer(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.routes())
	t.Cleanup(ts.Close)
	return s, ts.URL
}

func client(t *testing.T, url string) *cl {
	j, _ := cookiejar.New(nil)
	return &cl{t, &http.Client{Jar: j}, url}
}

func (c *cl) do(method, path string, body interface{}, hdr map[string]string) (int, map[string]interface{}, []byte) {
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, c.url+path, rd)
	if method != "GET" {
		req.Header.Set("X-MAM", "1")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.c.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	var m map[string]interface{}
	json.Unmarshal(buf.Bytes(), &m)
	return resp.StatusCode, m, buf.Bytes()
}

func expect(t *testing.T, got, want int, what string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: mã %d, cần %d", what, got, want)
	}
}

type fixture struct {
	url               string
	s                 *Server
	super, tech, view *cl
	m1, m2            string // id máy: m1 thuộc Tầng 2, m2 thuộc Kho
	tok1, tok2        string
}

func setup(t *testing.T) *fixture {
	s, url := newSrv(t)
	f := &fixture{url: url, s: s}
	f.super = client(t, url)
	code, m, _ := f.super.do("GET", "/api/status", nil, nil)
	expect(t, code, 200, "status")
	if m["setup"] != true {
		t.Fatal("phải cần cài đặt lần đầu")
	}
	code, _, _ = f.super.do("POST", "/api/setup", map[string]string{"name": "Admin Gốc", "user": "admin", "password": "Matkhau2026"}, nil)
	expect(t, code, 200, "setup")
	for _, g := range []string{"Tầng 2", "Kho"} {
		code, _, _ = f.super.do("POST", "/api/groups", map[string]string{"name": g}, nil)
		expect(t, code, 200, "tạo nhóm")
	}
	for _, a := range []map[string]interface{}{
		{"name": "Phúc Kỹ Thuật", "user": "phuc", "password": "Tam12345", "role": "tech", "scope": []string{"Tầng 2"}},
		{"name": "Bảo Xem", "user": "bao", "password": "Xem12345", "role": "view"},
	} {
		code, _, _ = f.super.do("POST", "/api/accounts", a, nil)
		expect(t, code, 200, "tạo tài khoản")
	}
	login := func(u, p string) *cl {
		c := client(t, url)
		code, m, _ := c.do("POST", "/api/login", map[string]string{"user": u, "password": p}, nil)
		expect(t, code, 200, "login "+u)
		if m["me"].(map[string]interface{})["mustChange"] != true {
			t.Fatal("tài khoản mới phải bị bắt đổi mật khẩu")
		}
		return c
	}
	f.tech, f.view = login("phuc", "Tam12345"), login("bao", "Xem12345")
	for c, p := range map[*cl][2]string{f.tech: {"Tam12345", "Phuc99999"}, f.view: {"Xem12345", "Bao999999"}} {
		code, _, _ = c.do("POST", "/api/me/password", map[string]string{"current": p[0], "new": p[1]}, nil)
		expect(t, code, 200, "đổi mật khẩu lần đầu")
	}
	enroll := func(name, mac, grp string) (string, string) {
		code, m, _ := client(t, url).do("POST", "/api/agent/enroll", map[string]interface{}{"name": name, "os": "Windows 11", "mac": mac, "ip": "10.0.0.5", "hw": map[string]interface{}{"cpu": "Test CPU"}}, nil)
		expect(t, code, 200, "enroll")
		id := m["id"].(string)
		code, _, _ = f.super.do("PUT", "/api/machines/"+id+"/group", map[string]string{"group": grp}, nil)
		expect(t, code, 200, "xếp nhóm")
		return id, m["token"].(string)
	}
	f.m1, f.tok1 = enroll("PC-KT-01", "AA:BB:CC:00:00:01", "Tầng 2")
	f.m2, f.tok2 = enroll("POS-KHO-01", "AA:BB:CC:00:00:02", "Kho")
	return f
}

func report(t *testing.T, url, tok string, cpu float64, temp float64) []interface{} {
	tt := temp
	code, m, _ := client(t, url).do("POST", "/api/agent/report", map[string]interface{}{"cpu": cpu, "ram": 40, "temp": &tt, "uptime": 100, "disks": []map[string]interface{}{{"name": "C:", "totalGB": 500, "freeGB": 300}}, "user": "ketoan"}, map[string]string{"Authorization": "Bearer " + tok})
	expect(t, code, 200, "report")
	c, _ := m["commands"].([]interface{})
	return c
}

func TestSetupOnlyOnce(t *testing.T) {
	f := setup(t)
	code, _, _ := client(t, f.url).do("POST", "/api/setup", map[string]string{"name": "Kẻ lạ", "user": "hacker", "password": "Hack12345"}, nil)
	expect(t, code, 409, "setup lần 2")
}

func TestPasswordRules(t *testing.T) {
	_, url := newSrv(t)
	c := client(t, url)
	for _, pw := range []string{"abc", "abcdefgh", "12345678"} {
		code, _, _ := c.do("POST", "/api/setup", map[string]string{"name": "Ad", "user": "admin", "password": pw}, nil)
		expect(t, code, 400, "mật khẩu yếu "+pw)
	}
}

func TestNeedLoginAndCSRF(t *testing.T) {
	f := setup(t)
	code, _, _ := client(t, f.url).do("GET", "/api/machines", nil, nil)
	expect(t, code, 401, "chưa đăng nhập")
	req, _ := http.NewRequest("POST", f.url+"/api/groups", strings.NewReader(`{"name":"X1"}`))
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	// đăng nhập rồi gửi POST thiếu X-MAM
	cc := &cl{t, c, f.url}
	cc.do("POST", "/api/login", map[string]string{"user": "admin", "password": "Matkhau2026"}, nil)
	resp, _ := c.Do(req)
	expect(t, resp.StatusCode, 403, "thiếu X-MAM")
}

func TestRolePermissions(t *testing.T) {
	f := setup(t)
	// Chỉ xem: không ra lệnh, không đổi gì
	code, _, _ := f.view.do("POST", "/api/machines/"+f.m1+"/command", map[string]interface{}{"type": "lock"}, nil)
	expect(t, code, 403, "view ra lệnh")
	code, _, _ = f.view.do("GET", "/api/machines", nil, nil)
	expect(t, code, 200, "view xem máy")
	// Kỹ thuật viên: không quản lý tài khoản, nhóm, cài đặt
	code, _, _ = f.tech.do("GET", "/api/accounts", nil, nil)
	expect(t, code, 403, "tech xem tài khoản")
	code, _, _ = f.tech.do("POST", "/api/groups", map[string]string{"name": "Moi"}, nil)
	expect(t, code, 403, "tech tạo nhóm")
	code, _, _ = f.tech.do("PUT", "/api/settings", map[string]interface{}{"temp": 80, "cpu": 90, "disk": 10, "offline": 5}, nil)
	expect(t, code, 403, "tech đổi cài đặt")
	code, _, _ = f.tech.do("DELETE", "/api/machines/"+f.m1, nil, nil)
	expect(t, code, 403, "tech thu hồi máy")
	// phạm vi nhóm
	report(t, f.url, f.tok1, 10, 50)
	report(t, f.url, f.tok2, 10, 50)
	code, _, _ = f.tech.do("POST", "/api/machines/"+f.m1+"/command", map[string]interface{}{"type": "message", "args": map[string]string{"text": "xin chào"}}, nil)
	expect(t, code, 200, "tech ra lệnh máy trong nhóm")
	code, _, _ = f.tech.do("POST", "/api/machines/"+f.m2+"/command", map[string]interface{}{"type": "lock"}, nil)
	expect(t, code, 403, "tech ra lệnh máy ngoài nhóm")
	code, _, _ = f.tech.do("POST", "/api/machines/bulk", map[string]interface{}{"ids": []string{f.m1, f.m2}, "type": "lock"}, nil)
	expect(t, code, 200, "bulk có một máy hợp lệ")
	// agent nhận lệnh
	cmds := report(t, f.url, f.tok1, 10, 50)
	if len(cmds) != 2 {
		t.Fatalf("agent 1 cần nhận 2 lệnh, nhận %d", len(cmds))
	}
	if len(report(t, f.url, f.tok2, 10, 50)) != 0 {
		t.Fatal("agent 2 không được nhận lệnh nào")
	}
	// kỹ thuật viên không được tự nâng quyền / xóa nhóm / chuyển sang nhóm ngoài phạm vi
	code, _, _ = f.tech.do("PUT", "/api/machines/"+f.m1+"/group", map[string]string{"group": "Kho"}, nil)
	expect(t, code, 403, "tech chuyển máy sang nhóm ngoài phạm vi")
}

func TestAuditOwnOnly(t *testing.T) {
	f := setup(t)
	report(t, f.url, f.tok1, 10, 50)
	f.tech.do("POST", "/api/machines/"+f.m1+"/command", map[string]interface{}{"type": "restart"}, nil)
	_, _, b := f.super.do("GET", "/api/audit", nil, nil)
	var all []AuditEntry
	json.Unmarshal(b, &all)
	users := map[string]bool{}
	sens := false
	for _, e := range all {
		users[e.User] = true
		if strings.Contains(e.Msg, "Khởi động lại") && e.Sens {
			sens = true
		}
	}
	if !users["admin"] || !users["phuc"] || !sens {
		t.Fatalf("super phải thấy nhật ký mọi người và lệnh khởi động lại là nhạy cảm: %v %v", users, sens)
	}
	_, _, b = f.tech.do("GET", "/api/audit", nil, nil)
	var own []AuditEntry
	json.Unmarshal(b, &own)
	for _, e := range own {
		if e.User != "phuc" {
			t.Fatalf("tech thấy nhật ký của %s", e.User)
		}
	}
	if len(own) == 0 {
		t.Fatal("tech phải thấy nhật ký của chính mình")
	}
}

func TestResetPasswordFlow(t *testing.T) {
	f := setup(t)
	code, m, _ := f.super.do("POST", "/api/accounts/"+f.accID(t, "phuc")+"/reset", nil, nil)
	expect(t, code, 200, "reset")
	tp := m["tempPassword"].(string)
	if len(tp) != 10 {
		t.Fatal("mật khẩu tạm 10 ký tự")
	}
	// phiên cũ của phuc phải mất hiệu lực
	code, _, _ = f.tech.do("GET", "/api/machines", nil, nil)
	expect(t, code, 401, "phiên cũ")
	c := client(t, f.url)
	code, _, _ = c.do("POST", "/api/login", map[string]string{"user": "phuc", "password": "Phuc99999"}, nil)
	expect(t, code, 401, "mật khẩu cũ")
	code, mm, _ := c.do("POST", "/api/login", map[string]string{"user": "phuc", "password": tp}, nil)
	expect(t, code, 200, "mật khẩu tạm")
	if mm["me"].(map[string]interface{})["mustChange"] != true {
		t.Fatal("phải bị bắt đổi mật khẩu")
	}
	code, _, _ = c.do("GET", "/api/machines", nil, nil)
	expect(t, code, 403, "chưa đổi mật khẩu thì không dùng được")
	code, _, _ = c.do("POST", "/api/me/password", map[string]string{"current": tp, "new": "abc"}, nil)
	expect(t, code, 400, "mật khẩu mới yếu")
	code, _, _ = c.do("POST", "/api/me/password", map[string]string{"current": tp, "new": "Moi123456"}, nil)
	expect(t, code, 200, "đổi")
	code, _, _ = c.do("GET", "/api/machines", nil, nil)
	expect(t, code, 200, "dùng được sau khi đổi")
}

func (f *fixture) accID(t *testing.T, user string) string {
	_, _, b := f.super.do("GET", "/api/accounts", nil, nil)
	var l []map[string]interface{}
	json.Unmarshal(b, &l)
	for _, a := range l {
		if a["user"] == user {
			return a["id"].(string)
		}
	}
	t.Fatal("không có " + user)
	return ""
}

func TestLastSuperProtected(t *testing.T) {
	f := setup(t)
	id := f.accID(t, "admin")
	code, _, _ := f.super.do("DELETE", "/api/accounts/"+id, nil, nil)
	expect(t, code, 400, "xóa super duy nhất")
	code, _, _ = f.super.do("PUT", "/api/accounts/"+id, map[string]interface{}{"name": "Admin Gốc", "role": "tech"}, nil)
	expect(t, code, 400, "hạ quyền super duy nhất")
}

func TestLoginLockout(t *testing.T) {
	f := setup(t)
	c := client(t, f.url)
	for i := 0; i < 5; i++ {
		code, _, _ := c.do("POST", "/api/login", map[string]string{"user": "admin", "password": "sai"}, nil)
		expect(t, code, 401, "sai mật khẩu")
	}
	code, _, _ := c.do("POST", "/api/login", map[string]string{"user": "admin", "password": "Matkhau2026"}, nil)
	expect(t, code, 429, "bị khóa tạm")
}

func TestAlertsAndRevoke(t *testing.T) {
	f := setup(t)
	report(t, f.url, f.tok1, 10, 95) // nhiệt độ quá ngưỡng
	_, _, b := f.super.do("GET", "/api/alerts?status=open", nil, nil)
	var al []Alert
	json.Unmarshal(b, &al)
	if len(al) != 1 || al[0].Kind != "temp" {
		t.Fatalf("cần 1 cảnh báo nhiệt độ, có %d", len(al))
	}
	report(t, f.url, f.tok1, 10, 50) // hạ nhiệt: tự đóng
	_, _, b = f.super.do("GET", "/api/alerts?status=open", nil, nil)
	json.Unmarshal(b, &al)
	if len(al) != 0 {
		t.Fatal("cảnh báo phải tự đóng khi hết quá nhiệt")
	}
	// thu hồi token
	code, _, _ := f.super.do("DELETE", "/api/machines/"+f.m1, nil, nil)
	expect(t, code, 200, "thu hồi")
	tt := 1.0
	code, _, _ = client(t, f.url).do("POST", "/api/agent/report", map[string]interface{}{"cpu": 1, "temp": &tt}, map[string]string{"Authorization": "Bearer " + f.tok1})
	expect(t, code, 403, "token đã thu hồi (revoked)")
	code, _, _ = client(t, f.url).do("POST", "/api/agent/report", map[string]interface{}{"cpu": 1}, map[string]string{"Authorization": "Bearer khongco"})
	expect(t, code, 401, "token lạ (unknown)")
}

func TestUninstallPassword(t *testing.T) {
	f := setup(t)
	c := client(t, f.url)
	h := map[string]string{"Authorization": "Bearer " + f.tok2}
	code, _, _ := c.do("POST", "/api/agent/uninstall", map[string]string{"code": "bat-ky"}, h)
	expect(t, code, 409, "chưa đặt mật khẩu gỡ")
	code, _, _ = f.tech.do("PUT", "/api/uninstall-password", map[string]string{"password": "Go-MAM-2026"}, nil)
	expect(t, code, 403, "tech không đặt được")
	code, _, _ = f.super.do("PUT", "/api/uninstall-password", map[string]string{"password": "ngan"}, nil)
	expect(t, code, 400, "quá ngắn")
	code, _, _ = f.super.do("PUT", "/api/uninstall-password", map[string]string{"password": "Go-MAM-2026"}, nil)
	expect(t, code, 200, "super đặt mật khẩu gỡ")
	code, m, _ := f.super.do("GET", "/api/uninstall-password", nil, nil)
	if code != 200 || m["set"] != true {
		t.Fatalf("phải báo đã đặt: %d %v", code, m)
	}
	code, _, _ = c.do("POST", "/api/agent/uninstall", map[string]string{"code": "sai"}, h)
	expect(t, code, 403, "sai mật khẩu gỡ")
	code, _, _ = c.do("POST", "/api/agent/uninstall", map[string]string{"user": "admin", "password": "Matkhau2026"}, h)
	expect(t, code, 403, "tài khoản admin không còn dùng để gỡ")
	code, _, _ = c.do("POST", "/api/agent/uninstall", map[string]string{"code": "Go-MAM-2026"}, h)
	expect(t, code, 200, "gỡ bằng mật khẩu gỡ")
	code, _, _ = f.super.do("DELETE", "/api/uninstall-password", nil, nil)
	expect(t, code, 200, "xóa mật khẩu gỡ")
}

func TestSelfRepairCommands(t *testing.T) {
	f := setup(t)
	report(t, f.url, f.tok1, 10, 50)
	var code int
	for _, ty := range []string{"clean", "freeram"} {
		code, _, _ = f.tech.do("POST", "/api/machines/"+f.m1+"/command", map[string]interface{}{"type": ty}, nil)
		expect(t, code, 200, "tự sửa chữa "+ty)
		code, _, _ = f.view.do("POST", "/api/machines/"+f.m1+"/command", map[string]interface{}{"type": ty}, nil)
		expect(t, code, 403, "view không được "+ty)
	}
}

func TestScreenDiag(t *testing.T) {
	f := setup(t)
	report(t, f.url, f.tok1, 10, 50)
	h := map[string]string{"Authorization": "Bearer " + f.tok1}
	code, j, _ := f.super.do("POST", "/api/machines/"+f.m1+"/screen", map[string]string{}, nil)
	expect(t, code, 200, "xin xem màn hình")
	sid, _ := j["id"].(string)
	code, _, _ = f.super.do("POST", "/api/machines/"+f.m1+"/screen", map[string]string{}, map[string]string{"X-Forwarded-For": "8.8.8.8"})
	expect(t, code, 403, "qua proxy/Internet bị chặn")
	code, _, _ = f.super.do("POST", "/api/agent/screen/consent", map[string]interface{}{"id": sid, "allow": true}, h)
	expect(t, code, 200, "máy đồng ý")
	code, _, _ = f.super.do("GET", "/api/agent/screen/poll?id="+sid+"&diag=L%E1%BB%97i+ch%E1%BB%A5p+m%C3%A0n+h%C3%ACnh%3A+BitBlt", nil, h)
	expect(t, code, 200, "agent poll")
	code, j, _ = f.super.do("GET", "/api/machines/"+f.m1+"/screen/"+sid, nil, nil)
	expect(t, code, 200, "xem trạng thái")
	if d, _ := j["diag"].(string); d != "Lỗi chụp màn hình: BitBlt" {
		t.Fatalf("diag = %q", d)
	}
	// gói hình: một gói đầy đủ (key) rồi các gói cập nhật vùng; gói đầy đủ mới thay hết gói cũ
	frame := func(key bool) map[string]interface{} {
		_, j, _ := f.super.do("POST", "/api/agent/screen/frame", map[string]interface{}{"id": sid, "data": "QUJD", "w": 10, "h": 10, "key": key}, h)
		return j
	}
	frame(true)
	j = frame(false)
	if k, _ := j["key"].(bool); k {
		t.Fatal("chưa cần gửi lại hình đầy đủ")
	}
	frame(false)
	f.s.mu.Lock()
	sc := f.s.screens[sid]
	nu, ks := len(sc.Ups), sc.KeySeq
	f.s.mu.Unlock()
	if nu != 2 || ks != 1 {
		t.Fatalf("ups=%d keyseq=%d", nu, ks)
	}
	frame(true)
	f.s.mu.Lock()
	nu, ks = len(sc.Ups), sc.KeySeq
	f.s.mu.Unlock()
	if nu != 0 || ks != 4 {
		t.Fatalf("sau gói đầy đủ mới: ups=%d keyseq=%d", nu, ks)
	}
	for i := 0; i < 85; i++ {
		j = frame(false)
	}
	if k, _ := j["key"].(bool); !k {
		t.Fatal("quá nhiều gói cập nhật: phải xin lại hình đầy đủ")
	}
}

func TestAgentRename(t *testing.T) {
	f := setup(t)
	c := client(t, f.url)
	h := map[string]string{"Authorization": "Bearer " + f.tok2}
	code, _, _ := c.do("POST", "/api/agent/rename", map[string]string{"name": "  "}, h)
	expect(t, code, 400, "tên rỗng")
	code, _, _ = c.do("POST", "/api/agent/rename", map[string]string{"name": "Tên mới"}, map[string]string{"Authorization": "Bearer sai"})
	expect(t, code, 401, "token sai")
	code, _, _ = c.do("POST", "/api/agent/rename", map[string]string{"name": "Kế toán 02"}, h)
	expect(t, code, 200, "đổi tên")
	found := false
	for _, m := range f.s.st.Machines {
		if m.Name == "Kế toán 02" {
			found = true
		}
	}
	if !found {
		t.Fatal("tên mới chưa lưu")
	}
}

func TestPersistence(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewServer(dir)
	ts := httptest.NewServer(s.routes())
	c := client(t, ts.URL)
	c.do("POST", "/api/setup", map[string]string{"name": "Admin", "user": "admin", "password": "Matkhau2026"}, nil)
	c.do("POST", "/api/groups", map[string]string{"name": "Nhóm A"}, nil)
	ts.Close()
	s2, err := NewServer(dir)
	if err != nil || len(s2.st.Accounts) != 1 || len(s2.st.Groups) != 1 {
		t.Fatal("dữ liệu không được nạp lại")
	}
	if tp, err := s2.resetLocal("admin"); err != nil || len(tp) != 10 {
		t.Fatal("reset-admin tại máy server lỗi")
	}
	_ = time.Now
}

func TestWOLPacket(t *testing.T) {
	if err := sendWOL("zz", ""); err == nil {
		t.Fatal("MAC sai phải báo lỗi")
	}
}

func TestAgentDownload(t *testing.T) {
	f := setup(t)
	code, _, _ := f.tech.do("GET", "/api/agent-download", nil, nil)
	expect(t, code, 403, "tech tải agent")
	code, _, _ = f.super.do("GET", "/api/agent-download", nil, nil) // Host là 127.0.0.1: bị từ chối vì máy khác không tới được
	expect(t, code, 400, "tải bằng địa chỉ localhost")
	os.WriteFile(filepath.Join(f.s.dir, "mam-agent.exe"), []byte("MZ-fake"), 0o644)
	f.s.st.Settings.EnrollKey = "abc"
	req := httptest.NewRequest("GET", "/api/agent-download?lang=zh", nil)
	req.Host = "192.168.1.10:8080"
	rec := httptest.NewRecorder()
	f.s.hAgentDownload(rec, req, &Account{User: "admin", Name: "A", Role: "super"})
	expect(t, rec.Code, 200, "tải agent đã gắn địa chỉ")
	b := rec.Body.Bytes()
	if !bytes.HasPrefix(b, []byte("MZ-fake")) || !bytes.HasSuffix(b, []byte("MAMCFG01")) {
		t.Fatal("thiếu phần gắn cấu hình")
	}
	if !strings.Contains(string(b), `"server":"http://192.168.1.10:8080"`) || !strings.Contains(string(b), `"key":"abc"`) || !strings.Contains(string(b), `"lang":"zh"`) {
		t.Fatal("cấu hình gắn sai: " + string(b))
	}
}

func TestMoveData(t *testing.T) {
	f := setup(t)
	old := f.s.dir
	f.s.backup()
	for _, bad := range []string{"relative/dir", filepath.Join(old, "sub"), filepath.Dir(old), old} {
		if _, err := f.s.moveData(bad); err == nil {
			t.Fatal("phải từ chối: " + bad)
		}
	}
	full := t.TempDir()
	os.WriteFile(filepath.Join(full, "x"), []byte("1"), 0o600)
	if _, err := f.s.moveData(full); err == nil {
		t.Fatal("thư mục không trống phải bị từ chối")
	}
	if b, _ := os.ReadFile(filepath.Join(full, "x")); string(b) != "1" {
		t.Fatal("không được đụng vào thư mục bị từ chối")
	}
	nd := filepath.Join(t.TempDir(), "MAMdata")
	code, _, _ := f.tech.do("POST", "/api/data-dir", map[string]string{"path": nd}, nil)
	expect(t, code, 403, "tech đổi thư mục dữ liệu")
	code, _, _ = f.super.do("POST", "/api/data-dir", map[string]string{"path": nd}, nil)
	expect(t, code, 200, "super đổi thư mục dữ liệu")
	if _, err := os.Stat(filepath.Join(nd, dbFile)); err != nil {
		t.Fatal("thiếu mam.db ở thư mục mới")
	}
	if g, _ := filepath.Glob(filepath.Join(nd, "backup", "state-*.json")); len(g) != 1 {
		t.Fatal("thiếu bản sao lưu")
	}
	if _, err := os.Stat(filepath.Join(old, dbFile)); err != nil {
		t.Fatal("dữ liệu cũ phải được giữ nguyên")
	}
	// ghi tiếp sau khi chuyển: vào thư mục mới
	code, _, _ = f.super.do("POST", "/api/groups", map[string]string{"name": "Sau chuyen"}, nil)
	expect(t, code, 200, "tạo nhóm sau chuyển")
	// khởi động lại từ thư mục -data cũ phải đi theo redirect.txt và thấy đủ dữ liệu
	d, err := resolveDir(old)
	if err != nil || d != nd {
		t.Fatalf("resolveDir = %q, %v", d, err)
	}
	s2, err := NewServer(d)
	if err != nil || len(s2.st.Accounts) != 3 || len(s2.st.Machines) != 2 {
		t.Fatalf("mất dữ liệu sau khi chuyển: %v", err)
	}
	if !strings.Contains(strings.Join(s2.st.Groups, ","), "Sau chuyen") {
		t.Fatal("dữ liệu mới không ghi vào thư mục mới")
	}
	if len(s2.readAudit(0)) == 0 {
		t.Fatal("mất nhật ký sau khi chuyển")
	}
	// thư mục đích biến mất (ổ chưa gắn): báo lỗi, không tạo dữ liệu trống
	os.RemoveAll(nd)
	if _, err := resolveDir(old); err == nil {
		t.Fatal("phải báo lỗi khi thư mục dữ liệu mới không còn")
	}
}

func TestXLSXAndSolution(t *testing.T) {
	var buf bytes.Buffer
	if err := writeXLSX(&buf, []xSheet{{"A", [][]string{{"Máy", "Trạng thái"}, {"PC<1>", "Đã xong"}}}}); err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil || len(z.File) < 6 {
		t.Fatalf("xlsx hỏng: %v %d", err, len(z.File))
	}
	if xcol(0) != "A" || xcol(26) != "AA" {
		t.Fatal("xcol sai")
	}
}

func TestSolutionSuggestAndDeviceTypes(t *testing.T) {
	f := setup(t)
	auth := map[string]string{"Authorization": "Bearer " + f.tok1}
	send := func(text, typ string) string {
		code, m, _ := client(t, f.url).do("POST", "/api/agent/request", map[string]interface{}{"text": text, "deviceType": typ, "user": "u"}, auth)
		expect(t, code, 200, "gửi yêu cầu")
		return m["id"].(string)
	}
	a := send("Máy in kẹt giấy khay hai", "Máy in")
	send("loại không có trong danh sách", "Máy bay")
	// đóng yêu cầu phải có cách xử lý
	code, _, _ := f.super.do("PUT", "/api/requests/"+a+"/status", map[string]string{"status": "done"}, nil)
	expect(t, code, 400, "đóng không có cách xử lý")
	code, _, _ = f.super.do("PUT", "/api/requests/"+a+"/status", map[string]string{"status": "done", "solution": "Lấy giấy kẹt, lau con lăn"}, nil)
	expect(t, code, 200, "đóng có cách xử lý")
	// yêu cầu mới giống cũ -> được gợi ý
	b := send("Máy in lại kẹt giấy", "Máy in")
	code, _, raw := f.super.do("GET", "/api/requests/"+b+"/suggest", nil, nil)
	expect(t, code, 200, "gợi ý")
	if !strings.Contains(string(raw), "lau con lăn") {
		t.Fatalf("thiếu gợi ý: %s", raw)
	}
	// loại thiết bị: chỉ super sửa, loại lạ bị bỏ
	code, _, _ = f.tech.do("PUT", "/api/device-types", map[string][]string{"types": {"Máy chiếu"}}, nil)
	expect(t, code, 403, "kỹ thuật viên sửa loại thiết bị")
	code, _, _ = f.super.do("PUT", "/api/device-types", map[string][]string{"types": {"Máy in", "Máy chiếu", "máy in"}}, nil)
	expect(t, code, 200, "super sửa loại thiết bị")
	code, _, raw = f.super.do("GET", "/api/requests?status=", nil, nil)
	expect(t, code, 200, "danh sách")
	if strings.Contains(string(raw), "Máy bay") {
		t.Fatal("loại không hợp lệ phải bị bỏ")
	}
	code, _, raw = f.super.do("GET", "/api/export/all.xlsx", nil, nil)
	if code != 200 || len(raw) < 100 || string(raw[:2]) != "PK" {
		t.Fatalf("xlsx lỗi: %d", code)
	}
}

func TestChatTwoWay(t *testing.T) {
	f := setup(t)
	report(t, f.url, f.tok1, 10, 50)
	code, _, _ := f.tech.do("POST", "/api/chat/"+f.m1, map[string]string{"text": "chào bạn"}, nil)
	if code != 200 {
		t.Fatalf("send %d", code)
	}
	code, _, _ = f.view.do("POST", "/api/chat/"+f.m1, map[string]string{"text": "x"}, nil)
	if code != 403 {
		t.Fatalf("viewer %d", code)
	}
	code, _, _ = f.tech.do("POST", "/api/chat/"+f.m1, map[string]string{"text": "  "}, nil)
	if code != 400 {
		t.Fatalf("empty %d", code)
	}
	f.s.mu.Lock()
	f.s.addChat(f.m1, "PC", "dạ em nghe", true)
	f.s.mu.Unlock()
	_, b, _ := f.tech.do("GET", "/api/chat?unread=1", nil, nil)
	if !strings.Contains(fmt.Sprint(b), "unread:1") {
		t.Fatalf("unread %s", b)
	}
	_, b, _ = f.tech.do("GET", "/api/chat/"+f.m1, nil, nil)
	if !strings.Contains(fmt.Sprint(b), "chào bạn") || !strings.Contains(fmt.Sprint(b), "dạ em nghe") {
		t.Fatalf("thread %s", b)
	}
	_, b, _ = f.tech.do("GET", "/api/chat?unread=1", nil, nil)
	if !strings.Contains(fmt.Sprint(b), "unread:0") {
		t.Fatalf("after read %s", b)
	}
}

func TestReportRange(t *testing.T) {
	f := setup(t)
	day := time.Now().Format("2006-01-02")
	code, m, _ := f.super.do("GET", "/api/report?from="+day+"&to="+day, nil, nil)
	expect(t, code, 200, "báo cáo 1 ngày")
	if m["days"] != float64(1) {
		t.Fatal("days", m["days"])
	}
	code, _, _ = f.super.do("GET", "/api/report?from=2020-01-01&to="+day, nil, nil)
	expect(t, code, 400, "quá 366 ngày")
}

func TestContactLabel(t *testing.T) {
	f := setup(t)
	code, m, _ := f.super.do("GET", "/api/contact-label", nil, nil)
	expect(t, code, 200, "đọc nhãn")
	if m["label"] != "SĐT/Zalo" {
		t.Fatal("mặc định", m["label"])
	}
	code, _, _ = f.super.do("PUT", "/api/contact-label", map[string]string{"label": "WeChat ID"}, nil)
	expect(t, code, 200, "đổi nhãn")
	_, m, _ = f.super.do("GET", "/api/contact-label", nil, nil)
	if m["label"] != "WeChat ID" {
		t.Fatal("sau khi đổi", m["label"])
	}
	code, _, _ = f.super.do("PUT", "/api/contact-label", map[string]string{"label": strings.Repeat("x", 31)}, nil)
	expect(t, code, 400, "quá dài")
}

// Thu hồi máy rồi cài lại (token mới, cùng MAC): máy phải hiện lại trên danh sách và gửi báo cáo được.
func TestReenrollAfterRevoke(t *testing.T) {
	f := setup(t)
	code, _, _ := f.super.do("DELETE", "/api/machines/"+f.m1, nil, nil)
	expect(t, code, 200, "thu hồi")
	code, _, _ = client(t, f.url).do("POST", "/api/agent/report", map[string]interface{}{"cpu": 1.0}, map[string]string{"Authorization": "Bearer " + f.tok1})
	expect(t, code, 403, "token cũ bị từ chối")
	code, m, _ := client(t, f.url).do("POST", "/api/agent/enroll", map[string]interface{}{"name": "PC-KT-01", "os": "Windows 11", "mac": "AA:BB:CC:00:00:01", "ip": "10.0.0.5"}, nil)
	expect(t, code, 200, "đăng ký lại")
	tok, _ := m["token"].(string)
	code, _, _ = client(t, f.url).do("POST", "/api/agent/report", map[string]interface{}{"cpu": 5.0}, map[string]string{"Authorization": "Bearer " + tok})
	expect(t, code, 200, "token mới báo cáo được")
	_, list, raw := f.super.do("GET", "/api/machines", nil, nil)
	_ = list
	if !strings.Contains(string(raw), `"id":"`+m["id"].(string)+`"`) {
		t.Fatal("máy cài lại không hiện trong danh sách")
	}
}

func TestCSPAllowsBlobImages(t *testing.T) { // viewer remote hiện hình bằng blob: URL
	f := setup(t)
	resp, err := http.Get(f.url + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "img-src 'self' data: blob:") {
		t.Fatal("CSP thiếu blob: cho img-src")
	}
}

func TestRemotePassword(t *testing.T) {
	f := setup(t)
	code, _, _ := f.super.do("PUT", "/api/remote-password", map[string]string{"password": "short"}, nil)
	expect(t, code, 400, "ngắn hơn 8 ký tự")
	code, _, _ = f.super.do("PUT", "/api/remote-password", map[string]string{"password": "Remote2026"}, nil)
	expect(t, code, 200, "đặt mật khẩu")
	code, _, _ = f.super.do("PUT", "/api/remote-password", map[string]string{"password": "chitoanchu"}, nil)
	expect(t, code, 200, "chỉ chữ vẫn được")
	code, _, _ = f.super.do("PUT", "/api/remote-password", map[string]string{"password": "Remote2026"}, nil)
	expect(t, code, 200, "đặt lại")
	// máy phải online: gửi báo cáo trước
	report(t, f.url, f.tok1, 10, 50)
	code, _, _ = f.super.do("POST", "/api/machines/"+f.m1+"/screen", map[string]string{"password": "sai"}, nil)
	expect(t, code, 403, "sai mật khẩu")
	code, m, _ := f.super.do("POST", "/api/machines/"+f.m1+"/screen", map[string]string{"password": "Remote2026"}, nil)
	expect(t, code, 200, "đúng mật khẩu")
	_ = m
	_, rep, _ := client(t, f.url).do("POST", "/api/agent/report", map[string]interface{}{"cpu": 1.0}, map[string]string{"Authorization": "Bearer " + f.tok1})
	scr, _ := rep["screen"].(map[string]interface{})
	if scr == nil || scr["auto"] != true {
		t.Fatal("agent phải nhận phiên auto", rep["screen"])
	}
	// tắt mật khẩu: quay về xin phép (auto=false)
	f.super.do("POST", "/api/machines/"+f.m1+"/screen/"+m["id"].(string)+"/stop", map[string]string{}, nil)
	code, _, _ = f.super.do("PUT", "/api/remote-password", map[string]string{"password": ""}, nil)
	expect(t, code, 200, "tắt mật khẩu")
	code, _, _ = f.super.do("POST", "/api/machines/"+f.m1+"/screen", map[string]string{}, nil)
	expect(t, code, 200, "xin phép như cũ")
}

func TestPurge(t *testing.T) {
	f := setup(t)
	old := time.Now().AddDate(0, 0, -60)
	f.s.mu.Lock()
	f.s.st.Requests = append(f.s.st.Requests,
		&Request{ID: "a", Status: "done", Created: old}, &Request{ID: "b", Status: "new", Created: old}, &Request{ID: "c", Status: "done", Created: time.Now()})
	f.s.st.Alerts = append(f.s.st.Alerts, &Alert{ID: "x", Status: "closed", Opened: old}, &Alert{ID: "y", Status: "open", Opened: old})
	f.s.mu.Unlock()
	to := time.Now().AddDate(0, 0, -30).Format("2006-01-02")
	body := map[string]interface{}{"kinds": []string{"requests", "alerts"}, "to": to, "dry": true}
	code, m, _ := f.super.do("POST", "/api/purge", body, nil)
	expect(t, code, 200, "xem trước")
	c := m["counts"].(map[string]interface{})
	if c["requests"].(float64) != 1 || c["alerts"].(float64) != 1 {
		t.Fatal("đếm", c)
	}
	f.s.mu.RLock()
	nr := len(f.s.st.Requests)
	f.s.mu.RUnlock()
	if nr != 3 {
		t.Fatal("xem trước không được xóa", nr)
	}
	body["dry"] = false
	code, _, _ = f.super.do("POST", "/api/purge", body, nil)
	expect(t, code, 200, "xóa thật")
	f.s.mu.RLock()
	left := len(f.s.st.Requests)*10 + len(f.s.st.Alerts)
	f.s.mu.RUnlock()
	if left != 21 { // còn: yêu cầu b (chưa xong) + c (mới), cảnh báo y (đang mở)
		t.Fatal("còn lại", left)
	}
	code, _, _ = f.super.do("POST", "/api/purge", map[string]interface{}{"kinds": []string{"audit"}, "to": time.Now().Format("2006-01-02")}, nil)
	expect(t, code, 400, "nhật ký gần đây được bảo vệ")
	code, _, _ = f.super.do("POST", "/api/purge", map[string]interface{}{"kinds": []string{"nope"}, "to": to}, nil)
	expect(t, code, 400, "loại lạ")
}

// Lọc theo nhóm khi xuất báo cáo và kiểm kê.
func TestExportGroupFilter(t *testing.T) {
	f := setup(t)
	get := func(path string) string {
		code, _, raw := f.super.do("GET", path, nil, nil)
		expect(t, code, 200, path)
		return string(raw)
	}
	all := get("/api/export/inventory.csv")
	if !strings.Contains(all, "PC-KT-01") || !strings.Contains(all, "POS-KHO-01") {
		t.Fatal("không lọc phải có cả hai máy:", all)
	}
	kho := get("/api/export/inventory.csv?group=Kho")
	if strings.Contains(kho, "PC-KT-01") || !strings.Contains(kho, "POS-KHO-01") {
		t.Fatal("lọc Kho:", kho)
	}
	if m := get("/api/export/machines.csv?group=Kho"); strings.Contains(m, "PC-KT-01") || !strings.Contains(m, "POS-KHO-01") {
		t.Fatal("máy lọc Kho:", m)
	}
	if none := get("/api/export/inventory.csv?group=__none"); strings.Contains(none, "PC-KT-01") || strings.Contains(none, "POS-KHO-01") {
		t.Fatal("chưa phân nhóm phải rỗng:", none)
	}
	_, r, _ := f.super.do("GET", "/api/report?days=7&group=Kho", nil, nil)
	if r["machines"] != float64(1) {
		t.Fatal("báo cáo nhóm Kho:", r["machines"])
	}
	_, r, _ = f.super.do("GET", "/api/report?days=7", nil, nil)
	if r["machines"] != float64(2) {
		t.Fatal("báo cáo toàn bộ:", r["machines"])
	}
}

func TestThreatAlert(t *testing.T) {
	s, _ := newSrv(t)
	m := &Machine{ID: "m1", Name: "PC-01"}
	s.st.Machines = append(s.st.Machines, m)
	th := []ThreatIn{{ID: "v1", Kind: "virus", Name: "Trojan:Win32/X", Detail: `C:\Users\a\Downloads\x.exe`, Cause: "Tải từ web, trang nguồn: evil.test", Status: "Đã xử lý (Quarantine)"}, {ID: "s1", Kind: "suspect", Name: "Tắt bảo vệ", Detail: "d"}}
	s.threatsIn(m, th)
	s.threatsIn(m, th) // báo lại cùng ID: không tạo thêm
	if len(s.st.Alerts) != 2 {
		t.Fatal("cần đúng 2 cảnh báo, có", len(s.st.Alerts))
	}
	a := s.st.Alerts[0]
	if a.Kind != "virus" || a.Sev != "crit" || !strings.Contains(a.Msg, "evil.test") || !strings.Contains(a.Msg, "Nguyên nhân có thể") {
		t.Fatal("nội dung cảnh báo virus:", a.Kind, a.Msg)
	}
	if s.st.Alerts[1].Kind != "suspect" {
		t.Fatal("loại nghi ngờ:", s.st.Alerts[1].Kind)
	}
}

func TestSchedule(t *testing.T) {
	f := setup(t)
	code, r, _ := f.super.do("POST", "/api/schedules", map[string]interface{}{"type": "lock", "time": "22:30", "group": "Kho", "days": []int{1, 2}}, nil)
	expect(t, code, 200, "tạo lịch")
	id, _ := r["id"].(string)
	if id == "" {
		t.Fatal("thiếu id lịch")
	}
	if c, _, _ := f.super.do("POST", "/api/schedules", map[string]interface{}{"type": "kill", "time": "22:30"}, nil); c != 400 {
		t.Fatal("lệnh kill không được hẹn giờ:", c)
	}
	if c, _, _ := f.view.do("POST", "/api/schedules", map[string]interface{}{"type": "lock", "time": "22:30"}, nil); c != 403 {
		t.Fatal("tài khoản xem không được tạo lịch:", c)
	}
	f.s.runSchedules(time.Date(2026, 10, 19, 22, 30, 5, 0, time.Local)) // thứ Hai, đúng giờ
	f.s.mu.RLock()
	lr := f.s.st.Schedules[0].LastRun
	f.s.mu.RUnlock()
	if lr != "2026-10-19" {
		t.Fatal("lịch chưa chạy:", lr)
	}
	f.s.runSchedules(time.Date(2026, 10, 20, 22, 31, 0, 0, time.Local)) // sai phút: không chạy
	f.s.mu.RLock()
	lr = f.s.st.Schedules[0].LastRun
	f.s.mu.RUnlock()
	if lr != "2026-10-19" {
		t.Fatal("chạy sai phút:", lr)
	}
	if c, _, _ := f.super.do("DELETE", "/api/schedules?id="+id, nil, nil); c != 200 {
		t.Fatal("xóa lịch:", c)
	}
}

func TestTOTP(t *testing.T) {
	// vectơ thử của RFC 6238 (SHA-1, thời điểm 59 giây): 8 số 94287082 -> 6 số 287082
	if c := totpCode("GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", 1); c != "287082" {
		t.Fatal("sai mã RFC 6238:", c)
	}
	sec := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	now := time.Unix(59, 0)
	st := totpCheck(sec, "287082", now, 0)
	if st != 1 {
		t.Fatal("mã đúng bị từ chối")
	}
	if totpCheck(sec, "287082", now, st) != 0 {
		t.Fatal("mã đã dùng vẫn được nhận lại")
	}
	if totpCheck(sec, "000000", now, 0) != 0 || totpCheck(sec, "12", now, 0) != 0 {
		t.Fatal("mã sai được nhận")
	}
}

func TestLogin2FA(t *testing.T) {
	f := setup(t)
	c := f.super
	_, r, _ := c.do("POST", "/api/me/2fa/start", nil, nil)
	sec, _ := r["secret"].(string)
	if sec == "" {
		t.Fatal("thiếu khóa 2FA")
	}
	if code, _, _ := c.do("POST", "/api/me/2fa/enable", map[string]string{"code": "000000"}, nil); code != 400 {
		t.Fatal("mã sai vẫn bật được 2FA:", code)
	}
	cur := time.Now().Unix() / 30
	code, _, _ := c.do("POST", "/api/me/2fa/enable", map[string]string{"code": totpCode(sec, cur)}, nil)
	expect(t, code, 200, "bật 2FA")
	c2 := client(t, f.url)
	code, _, raw := c2.do("POST", "/api/login", map[string]string{"user": "admin", "password": "Matkhau2026"}, nil)
	if code != 401 || !strings.Contains(string(raw), "2FA_REQUIRED") {
		t.Fatal("thiếu mã phải bị đòi:", code, string(raw))
	}
	code, _, _ = c2.do("POST", "/api/login", map[string]string{"user": "admin", "password": "Matkhau2026", "code": "123456"}, nil)
	expect(t, code, 401, "mã sai")
	code, _, _ = c2.do("POST", "/api/login", map[string]string{"user": "admin", "password": "Matkhau2026", "code": totpCode(sec, cur+1)}, nil)
	expect(t, code, 200, "mã đúng")
	// mã vừa dùng không dùng lại được
	code, _, _ = client(t, f.url).do("POST", "/api/login", map[string]string{"user": "admin", "password": "Matkhau2026", "code": totpCode(sec, cur+1)}, nil)
	expect(t, code, 401, "dùng lại mã")
}

func tinyPNG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 120, 255})
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func TestAvatar(t *testing.T) {
	f := setup(t)
	post := func(c *cl, path string, body []byte) int {
		req, _ := http.NewRequest("POST", c.url+path, bytes.NewReader(body))
		req.Header.Set("X-MAM", "1")
		resp, err := c.c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	_, me, _ := f.super.do("GET", "/api/me", nil, nil)
	id, _ := me["id"].(string)
	if post(f.super, "/api/avatar/u/"+id, tinyPNG(300, 200)) != 200 {
		t.Fatal("tải ảnh lên thất bại")
	}
	code, _, raw := f.super.do("GET", "/api/avatar/u/"+id, nil, nil)
	expect(t, code, 200, "xem ảnh")
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width != 128 || cfg.Height != 128 {
		t.Fatal("ảnh phải được thu về 128x128 JPEG:", err, cfg)
	}
	if post(f.super, "/api/avatar/u/"+id, []byte("<html>không phải ảnh</html>")) != 400 {
		t.Fatal("file không phải ảnh vẫn được nhận")
	}
	if post(f.super, "/api/avatar/u/aaaa", tinyPNG(40, 40)) != 403 {
		t.Fatal("không được đổi ảnh người khác")
	}
	if c, _, _ := f.super.do("GET", "/api/avatar/u/..%2fstate", nil, nil); c == 200 {
		t.Fatal("đường dẫn lạ không được trả file")
	}
	// máy tự đổi ảnh bằng token, và bị chặn khi Super admin tắt
	req := func(method string, body []byte) int {
		rq, _ := http.NewRequest(method, f.url+"/api/agent/avatar", bytes.NewReader(body))
		rq.Header.Set("Authorization", "Bearer "+f.tok1)
		resp, err := http.DefaultClient.Do(rq)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if req("POST", tinyPNG(64, 64)) != 200 {
		t.Fatal("máy đổi ảnh thất bại")
	}
	if code, _, _ := f.super.do("PUT", "/api/avatar-policy", map[string]bool{"machines": false}, nil); code != 200 {
		t.Fatal("tắt quyền đổi ảnh:", code)
	}
	if req("POST", tinyPNG(64, 64)) != 403 {
		t.Fatal("máy vẫn đổi được ảnh khi bị tắt")
	}
	if code, _, _ := f.tech.do("DELETE", "/api/avatar/m/"+f.m1, nil, nil); code != 403 {
		t.Fatal("kỹ thuật viên không được xóa ảnh máy:", code)
	}
	if code, _, _ := f.super.do("DELETE", "/api/avatar/m/"+f.m1, nil, nil); code != 200 {
		t.Fatal("Super admin xóa ảnh máy:", code)
	}
	if avatarVer("m", f.m1) != 0 {
		t.Fatal("ảnh vẫn còn sau khi xóa")
	}
}

func TestAgentDownloads(t *testing.T) {
	f := setup(t)
	os.WriteFile(filepath.Join(f.s.dir, "mam-agent-linux-amd64"), []byte("ELF-giả"), 0o755)
	get := func(path, host string) (int, string) {
		rq, _ := http.NewRequest("GET", f.url+path, nil)
		if host != "" {
			rq.Host = host
		}
		resp, err := http.DefaultClient.Do(rq) // không đăng nhập: các đường /dl và /install công khai trong LAN
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var b bytes.Buffer
		b.ReadFrom(resp.Body)
		return resp.StatusCode, b.String()
	}
	if c, b := get("/dl/agent/linux-amd64", ""); c != 200 || b != "ELF-giả" {
		t.Fatal("tải agent linux:", c, b)
	}
	if c, _ := get("/dl/agent/mac-arm64", ""); c != 404 {
		t.Fatal("file chưa có phải 404:", c)
	}
	if c, _ := get("/dl/agent/..%2fstate.json", ""); c != 404 {
		t.Fatal("đường dẫn lạ phải 404:", c)
	}
	if c, b := get("/install/linux.sh", "192.168.1.5:7070"); c != 200 || !strings.Contains(b, `SRV="http://192.168.1.5:7070"`) || strings.Contains(b, "__SRV__") {
		t.Fatal("script linux:", c)
	}
	if c, _ := get("/install/mac.sh", ""); c != 400 {
		t.Fatal("localhost không được sinh script:", c)
	}
	_, have, _ := f.super.do("GET", "/api/agent-downloads", nil, nil)
	if have["linux-amd64"] != true || have["mac-arm64"] != false || have["android"] != false {
		t.Fatal("danh sách file:", have)
	}
	if c, _, _ := f.tech.do("GET", "/api/agent-downloads", nil, nil); c != 403 {
		t.Fatal("chỉ Super admin xem:", c)
	}
}

func TestTechScopeVisibility(t *testing.T) {
	f := setup(t)
	report(t, f.url, f.tok1, 10, 110)
	report(t, f.url, f.tok2, 10, 110)
	count := func(c *cl, path string) int {
		_, _, raw := c.do("GET", path, nil, nil)
		var a []interface{}
		json.Unmarshal(raw, &a)
		return len(a)
	}
	for _, p := range []string{"/api/machines", "/api/inventory", "/api/security"} {
		if n := count(f.super, p); n != 2 {
			t.Fatalf("super %s: %d", p, n)
		}
		if n := count(f.tech, p); n != 1 {
			t.Fatalf("tech %s phải thấy 1 máy, thấy %d", p, n)
		}
	}
	code, _, _ := f.tech.do("GET", "/api/machines/"+f.m2, nil, nil)
	expect(t, code, 404, "tech xem máy ngoài nhóm")
	code, _, _ = f.tech.do("GET", "/api/machines/"+f.m1, nil, nil)
	expect(t, code, 200, "tech xem máy trong nhóm")
	if n, ns := count(f.super, "/api/alerts"), count(f.tech, "/api/alerts"); ns == 0 || ns >= n {
		t.Fatalf("cảnh báo: super %d, tech %d", n, ns)
	}
	_, g, _ := f.tech.do("GET", "/api/groups", nil, nil)
	if gs := g["groups"].([]interface{}); len(gs) != 1 {
		t.Fatalf("tech chỉ thấy nhóm của mình: %v", gs)
	}
	_, ov, _ := f.tech.do("GET", "/api/overview", nil, nil)
	if ov["total"].(float64) != 1 {
		t.Fatalf("tổng quan tech: %v", ov["total"])
	}
	_, rp, _ := f.tech.do("GET", "/api/report?days=7", nil, nil)
	if b, _ := json.Marshal(rp); strings.Contains(string(b), "POS-KHO-01") || rp["machines"] != nil && rp["machines"].(float64) != 1 {
		t.Fatalf("báo cáo tech lộ máy ngoài nhóm: %s", b)
	}
	for _, p := range []string{"machines", "alerts", "inventory"} {
		_, _, raw := f.tech.do("GET", "/api/export/"+p+".csv", nil, nil)
		if strings.Contains(string(raw), "POS-KHO-01") {
			t.Fatalf("xuất %s lộ máy ngoài nhóm", p)
		}
		_, _, raw = f.tech.do("GET", "/api/export/"+p+".csv?group=Kho", nil, nil)
		if strings.Contains(string(raw), "POS-KHO-01") {
			t.Fatalf("xuất %s chọn nhóm Kho vẫn lộ", p)
		}
	}
	code, _, _ = f.tech.do("POST", "/api/schedules", map[string]interface{}{"type": "lock", "time": "22:00", "group": ""}, nil)
	expect(t, code, 403, "tech hẹn giờ cho mọi máy")
	code, _, _ = f.tech.do("POST", "/api/schedules", map[string]interface{}{"type": "lock", "time": "22:00", "group": "Tầng 2"}, nil)
	expect(t, code, 200, "tech hẹn giờ cho nhóm mình")
}

func TestUptimeSessions(t *testing.T) {
	f := setup(t)
	t0 := int64(1_000_000)
	f.s.mu.Lock()
	marks := [][2]int64{{t0, 30}, {t0 + 5, 35}, {t0 + 600, 630}, {t0 + 605, 635}, {t0 + 660, 20}}
	for _, x := range marks {
		f.s.upMark("x", x[0], x[1])
	}
	got := f.s.st.Uptime["x"]
	f.s.mu.Unlock()
	want := [][2]int64{{t0 - 30, t0 + 5}, {t0 + 600, t0 + 605}, {t0 + 640, t0 + 660}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("lần bật: %v, muốn %v", got, want)
	}

	report(t, f.url, f.tok1, 10, 50)
	report(t, f.url, f.tok2, 10, 50)
	ids := func(c *cl, q string) string {
		code, m, _ := c.do("GET", "/api/uptime"+q, nil, nil)
		expect(t, code, 200, "uptime"+q)
		out := ""
		for _, x := range m["machines"].([]interface{}) {
			mm := x.(map[string]interface{})
			out += mm["name"].(string) + fmt.Sprint(len(mm["s"].([]interface{}))) + " "
		}
		return out
	}
	if g := ids(f.super, ""); g != "PC-KT-011 POS-KHO-011 " {
		t.Fatalf("super thấy %q", g)
	}
	if g := ids(f.super, "?group=Kho"); g != "POS-KHO-011 " {
		t.Fatalf("lọc nhóm Kho: %q", g)
	}
	if g := ids(f.tech, ""); g != "PC-KT-011 " {
		t.Fatalf("tech chỉ thấy Tầng 2, lại thấy %q", g)
	}
	if code, _, _ := f.tech.do("GET", "/api/uptime?id="+f.m2, nil, nil); code != 404 {
		t.Fatalf("tech xem máy ngoài phạm vi: %d", code)
	}
	day := time.Now().Format("2006-01-02")
	code, _, raw := f.super.do("GET", "/api/export/uptime.csv?from="+day+"&to="+day, nil, nil)
	expect(t, code, 200, "xuất uptime")
	if s := string(raw); !strings.Contains(s, "PC-KT-01") || !strings.Contains(s, "Đang bật") {
		t.Fatalf("file xuất thiếu dữ liệu: %s", s)
	}
	if code, _, _ := f.super.do("GET", "/api/export/uptime.csv", nil, nil); code != 400 {
		t.Fatalf("thiếu ngày phải báo lỗi: %d", code)
	}
}

func TestMachineEvents(t *testing.T) {
	f := setup(t)
	now := time.Now().Unix()
	send := func(ev []map[string]interface{}) {
		code, _, _ := client(t, f.url).do("POST", "/api/agent/report", map[string]interface{}{"cpu": 1, "uptime": 100, "events": ev}, map[string]string{"Authorization": "Bearer " + f.tok1})
		expect(t, code, 200, "report kèm lỗi")
	}
	send([]map[string]interface{}{
		{"id": "Sys1", "at": now - 60, "log": "System", "src": "Microsoft-Windows-Kernel-Power", "eid": 41, "lvl": 1, "msg": "Tắt đột ngột"},
		{"id": "App2", "at": now - 30, "log": "Application", "src": "Application Error", "eid": 1000, "lvl": 2, "msg": "chrome.exe"},
		{"id": "Old3", "at": now - 200*86400, "src": "disk", "eid": 7, "lvl": 2}, // quá 180 ngày: bỏ
		{"id": "Warn4", "at": now - 10, "src": "x", "eid": 1, "lvl": 3},          // không phải lỗi: bỏ
	})
	send([]map[string]interface{}{{"id": "Sys1", "at": now - 60, "src": "Microsoft-Windows-Kernel-Power", "eid": 41, "lvl": 1}}) // gửi lại: bỏ trùng
	code, m, _ := f.super.do("GET", "/api/machines/"+f.m1+"/events", nil, nil)
	expect(t, code, 200, "xem lỗi")
	ev := m["events"].([]interface{})
	if len(ev) != 2 || ev[0].(map[string]interface{})["id"] != "App2" {
		t.Fatalf("lỗi lưu sai: %v", ev)
	}
	if code, _, _ := f.tech.do("GET", "/api/machines/"+f.m2+"/events", nil, nil); code != 404 {
		t.Fatalf("tech xem lỗi máy ngoài phạm vi: %d", code)
	}
	if code, _, _ := f.view.do("GET", "/api/machines/"+f.m1+"/events", nil, nil); code != 200 {
		t.Fatalf("view phải xem được lỗi máy trong phạm vi: %d", code)
	}
	// dọn: giữ mục trong hạn
	evPrune(f.s.dir)
	if l := evRead(f.s.dir, f.m1); len(l) != 2 {
		t.Fatalf("dọn xóa nhầm: %v", l)
	}
}

func TestMirrorBackup(t *testing.T) {
	// giữ mới nhất mỗi giờ (24), ngày (7), tuần (4), tháng (12)
	names := []string{}
	base := time.Date(2026, 11, 9, 10, 0, 0, 0, time.Local)
	for h := 0; h < 24*400; h++ { // 400 ngày, mỗi giờ một bản
		names = append(names, "mam-"+base.Add(-time.Duration(h)*time.Hour).Format("20060102-1504")+".zip")
	}
	keep := keepMirrors(names)
	if len(keep) < 40 || len(keep) > 47 || !keep[names[0]] {
		t.Fatalf("giữ %d bản, bản mới nhất giữ=%v", len(keep), keep[names[0]])
	}
	f := setup(t)
	dest := filepath.Join(t.TempDir(), "o-khac")
	code, m, _ := f.super.do("POST", "/api/backups/mirror", map[string]string{"dir": dest}, nil)
	expect(t, code, 200, "đặt thư mục sao lưu")
	if m["ok"] != true || m["file"] == "" {
		t.Fatalf("sao lưu chưa chạy: %v", m)
	}
	zs, _ := filepath.Glob(filepath.Join(dest, "mam-*.zip"))
	if len(zs) != 1 || verifyMirror(zs[0]) != nil {
		t.Fatalf("file sao lưu: %v", zs)
	}
	if code, _, _ := f.tech.do("POST", "/api/backups/mirror", map[string]bool{"run": true}, nil); code != 403 {
		t.Fatalf("tech không được sao lưu: %d", code)
	}
	if code, _, _ := f.super.do("POST", "/api/backups/mirror", map[string]string{"dir": "tuong-doi"}, nil); code != 400 {
		t.Fatalf("đường dẫn tương đối phải bị từ chối: %d", code)
	}
	// thư mục không ghi được: báo lỗi + mở cảnh báo máy chủ
	bad := filepath.Join(zs[0], "khong-the") // nằm trong một file nên không tạo được
	code, _, _ = f.super.do("POST", "/api/backups/mirror", map[string]string{"dir": bad}, nil)
	if code != 500 {
		t.Fatalf("thư mục hỏng phải báo lỗi: %d", code)
	}
	f.s.mu.RLock()
	open := false
	for _, a := range f.s.st.Alerts {
		open = open || (a.Machine == "__server" && a.Kind == "backup" && a.Status == "open")
	}
	f.s.mu.RUnlock()
	if !open {
		t.Fatal("thiếu cảnh báo sao lưu thất bại")
	}
	f.s.checkServerDisk() // không được treo/lỗi trên Linux
}

func TestRestore(t *testing.T) {
	f := setup(t)
	evAppend(f.s.dir, f.m1, []SysEvent{{ID: "Sys1", At: time.Now().Unix() - 60, Src: "disk", EID: 7, Lvl: 2}})
	dest := filepath.Join(t.TempDir(), "o-khac")
	code, m, _ := f.super.do("POST", "/api/backups/mirror", map[string]string{"dir": dest}, nil)
	expect(t, code, 200, "sao lưu")
	name := m["file"].(string)
	// sau khi sao lưu: thu hồi một máy, xóa lỗi
	expect(t, func() int { c, _, _ := f.super.do("DELETE", "/api/machines/"+f.m2, nil, nil); return c }(), 200, "thu hồi")
	os.Remove(evFile(f.s.dir, f.m1))
	if code, _, _ := f.tech.do("POST", "/api/restore?mirror="+name, nil, nil); code != 403 {
		t.Fatalf("tech không được khôi phục: %d", code)
	}
	code, _, _ = f.super.do("POST", "/api/restore?mirror="+name, nil, nil)
	expect(t, code, 200, "khôi phục")
	f.s.mu.RLock()
	rev := f.s.machineByID(f.m2).Revoked
	f.s.mu.RUnlock()
	if rev || len(evRead(f.s.dir, f.m1)) != 1 {
		t.Fatalf("khôi phục chưa đủ: máy 2 thu hồi=%v, lỗi=%v", rev, evRead(f.s.dir, f.m1))
	}
	if code, _, _ := f.super.do("GET", "/api/me", nil, nil); code != 401 {
		t.Fatalf("sau khôi phục phải đăng nhập lại: %d", code)
	}
	// máy chủ mới cài: nạp file zip không cần đăng nhập, rồi đăng nhập bằng tài khoản cũ
	zb, _ := os.ReadFile(filepath.Join(dest, name))
	_, url2 := newSrv(t)
	c2 := client(t, url2)
	req, _ := http.NewRequest("POST", url2+"/api/restore", bytes.NewReader(zb))
	req.Header.Set("X-MAM", "1")
	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("khôi phục trên máy mới: %v %v", err, res)
	}
	code, _, _ = c2.do("POST", "/api/login", map[string]string{"user": "admin", "password": "Matkhau2026"}, nil)
	expect(t, code, 200, "đăng nhập sau khôi phục")
	// đã có tài khoản thì không còn khôi phục tự do
	req, _ = http.NewRequest("POST", url2+"/api/restore", bytes.NewReader(zb))
	req.Header.Set("X-MAM", "1")
	if res, _ := http.DefaultClient.Do(req); res.StatusCode != 401 {
		t.Fatalf("khôi phục không đăng nhập phải bị chặn: %d", res.StatusCode)
	}
	if _, _, err := parseBackup([]byte("rác")); err == nil {
		t.Fatal("file rác phải bị từ chối")
	}
}

func TestNameSync(t *testing.T) {
	f := setup(t)
	h := map[string]string{"Authorization": "Bearer " + f.tok1}
	rep := func(host string) string {
		code, m, _ := client(t, f.url).do("POST", "/api/agent/report", map[string]interface{}{"cpu": 1, "host": host}, h)
		expect(t, code, 200, "report")
		return m["name"].(string)
	}
	if n := rep("DESKTOP-ABC"); n != "PC-KT-01" { // lần đầu chỉ ghi nhận tên Windows
		t.Fatalf("lần đầu không được đổi tên: %q", n)
	}
	if n := rep("KETOAN-02"); n != "KETOAN-02" { // đổi tên trong Windows: MAM đổi theo, agent nhận tên mới
		t.Fatalf("đổi tên Windows chưa theo: %q", n)
	}
	code, _, _ := f.super.do("PUT", "/api/machines/"+f.m1+"/rename", map[string]string{"name": "Kế toán 02"}, nil)
	expect(t, code, 200, "đổi tên trên Console")
	if n := rep("KETOAN-02"); n != "Kế toán 02" { // agent nhận tên do quản trị đặt, tên Windows không đè lại
		t.Fatalf("agent chưa nhận tên mới: %q", n)
	}
}
