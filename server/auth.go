package main

import (
	"crypto/pbkdf2"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
)

const pbkdfIter = 200000

func hashPw(pw, saltHex string, iter int) string {
	salt, _ := hex.DecodeString(saltHex)
	k, _ := pbkdf2.Key(sha256.New, pw, salt, iter, 32)
	return hex.EncodeToString(k)
}

func (a *Account) setPassword(pw string) {
	a.Salt = rid(16)
	a.Iter = pbkdfIter
	a.Hash = hashPw(pw, a.Salt, a.Iter)
}

func (a *Account) checkPassword(pw string) bool {
	return subtle.ConstantTimeCompare([]byte(hashPw(pw, a.Salt, a.Iter)), []byte(a.Hash)) == 1
}

var userRe = regexp.MustCompile(`^[a-z0-9._]{3,32}$`)

func checkNewPassword(pw string) error {
	if len(pw) < 8 {
		return errors.New("Mật khẩu ít nhất 8 ký tự")
	}
	var l, d bool
	for _, r := range pw {
		if unicode.IsLetter(r) {
			l = true
		}
		if unicode.IsDigit(r) {
			d = true
		}
	}
	if !l || !d {
		return errors.New("Mật khẩu cần có cả chữ và số")
	}
	return nil
}

func tempPassword() string {
	const letters = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz"
	const digits = "23456789"
	b := make([]byte, 10)
	r, _ := hex.DecodeString(rid(10))
	for i := range b {
		if i%3 == 2 {
			b[i] = digits[int(r[i])%len(digits)]
		} else {
			b[i] = letters[int(r[i])%len(letters)]
		}
	}
	return string(b)
}

func clientIP(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

// isLAN: yêu cầu đến trực tiếp từ máy trong mạng nội bộ (riêng tư, link-local hoặc chính máy server),
// không đi qua proxy/tunnel (các dịch vụ đó luôn thêm header chuyển tiếp). Dùng cho điều khiển màn hình.
func isLAN(r *http.Request) bool {
	for _, h := range []string{"X-Forwarded-For", "X-Real-Ip", "Forwarded", "Cf-Connecting-Ip", "True-Client-Ip", "X-Forwarded-Host"} {
		if r.Header.Get(h) != "" {
			return false
		}
	}
	ip := net.ParseIP(clientIP(r))
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast())
}

func isLoopback(r *http.Request) bool {
	ip := net.ParseIP(clientIP(r))
	return ip != nil && ip.IsLoopback()
}

// ---------- phiên đăng nhập ----------

type ctxKey struct{}

func (s *Server) accountByID(id string) *Account {
	for _, a := range s.st.Accounts {
		if a.ID == id {
			return a
		}
	}
	return nil
}

func (s *Server) accountByUser(u string) *Account {
	for _, a := range s.st.Accounts {
		if a.User == u {
			return a
		}
	}
	return nil
}

func (s *Server) newSession(w http.ResponseWriter, accID string) {
	t := rid(32)
	s.mu.Lock()
	s.sess[t] = &Session{AccID: accID, Exp: time.Now().Add(12 * time.Hour)}
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "mam_sess", Value: t, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 12 * 3600})
}

// whoami trả tài khoản từ cookie (bản sao để dùng ngoài khóa).
func (s *Server) whoami(r *http.Request) *Account {
	c, err := r.Cookie("mam_sess")
	if err != nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	se := s.sess[c.Value]
	if se == nil || time.Now().After(se.Exp) {
		delete(s.sess, c.Value)
		return nil
	}
	a := s.accountByID(se.AccID)
	if a == nil {
		return nil
	}
	cp := *a
	return &cp
}

func (s *Server) dropSessionsOf(accID string) {
	for k, v := range s.sess {
		if v.AccID == accID {
			delete(s.sess, k)
		}
	}
}

type handlerFn func(w http.ResponseWriter, r *http.Request, me *Account)

// authed bọc handler: cần đăng nhập, mọi thao tác ghi cần header X-MAM (chống CSRF),
// tài khoản đang bị bắt đổi mật khẩu thì chỉ dùng được vài đường.
func (s *Server) authed(minRole string, fn handlerFn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		me := s.whoami(r)
		if me == nil {
			jerr(w, 401, "Chưa đăng nhập")
			return
		}
		if r.Method != "GET" && r.Header.Get("X-MAM") != "1" {
			jerr(w, 403, "Thiếu header X-MAM")
			return
		}
		if me.MustChange && r.URL.Path != "/api/me" && r.URL.Path != "/api/me/password" && r.URL.Path != "/api/logout" {
			jerr(w, 403, "Bạn cần đổi mật khẩu trước")
			return
		}
		if minRole == "super" && me.Role != "super" {
			jerr(w, 403, "Chỉ Super admin được làm việc này")
			return
		}
		if minRole == "write" && me.Role == "view" {
			jerr(w, 403, "Tài khoản Chỉ xem không có quyền thao tác")
			return
		}
		fn(w, r, me)
	}
}

func jerr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func jok(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

func jsonDecode(rd io.Reader, v interface{}) error { return json.NewDecoder(rd).Decode(v) }

func readJSON(r *http.Request, v interface{}) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	return json.NewDecoder(r.Body).Decode(v)
}

func accView(a *Account) map[string]interface{} {
	return map[string]interface{}{"id": a.ID, "name": a.Name, "user": a.User, "role": a.Role, "scope": a.Scope, "mustChange": a.MustChange, "totp": a.TOTP != "", "av": avatarVer("u", a.ID), "lastLogin": a.LastLogin, "created": a.Created}
}

func (s *Server) auditBy(me *Account, r *http.Request, kind, msg, target string, sens bool) {
	s.audit(AuditEntry{User: me.User, Name: me.Name, Role: me.Role, IP: clientIP(r), Kind: kind, Msg: msg, Target: target, Sens: sens})
}

// ---------- các đường công khai ----------

func (s *Server) hStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	n := len(s.st.Accounts)
	s.mu.RUnlock()
	jok(w, map[string]interface{}{"setup": n == 0, "version": Version})
}

// Cài đặt lần đầu: chỉ chạy được khi chưa có tài khoản nào.
func (s *Server) hSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" || r.Header.Get("X-MAM") != "1" {
		jerr(w, 400, "Yêu cầu không hợp lệ")
		return
	}
	var in struct{ Name, User, Password string }
	if readJSON(r, &in) != nil {
		jerr(w, 400, "Dữ liệu không hợp lệ")
		return
	}
	in.User = strings.TrimSpace(in.User)
	in.Name = strings.TrimSpace(in.Name)
	if len([]rune(in.Name)) < 2 {
		jerr(w, 400, "Nhập họ tên")
		return
	}
	if !userRe.MatchString(in.User) {
		jerr(w, 400, "Tài khoản 3 đến 32 ký tự: chữ thường, số, dấu . _")
		return
	}
	if err := checkNewPassword(in.Password); err != nil {
		jerr(w, 400, err.Error())
		return
	}
	s.mu.Lock()
	if len(s.st.Accounts) > 0 {
		s.mu.Unlock()
		jerr(w, 409, "Hệ thống đã được cài đặt")
		return
	}
	a := &Account{ID: rid(6), Name: in.Name, User: in.User, Role: "super", Created: time.Now(), LastLogin: time.Now()}
	a.setPassword(in.Password)
	s.st.Accounts = append(s.st.Accounts, a)
	s.save()
	s.mu.Unlock()
	s.audit(AuditEntry{User: a.User, Name: a.Name, Role: a.Role, IP: clientIP(r), Kind: "Tài khoản", Msg: "Cài đặt lần đầu: tạo Super admin " + a.User, Target: a.User, Sens: true})
	s.newSession(w, a.ID)
	jok(w, map[string]interface{}{"ok": true, "me": accView(a)})
}

func (s *Server) hLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" || r.Header.Get("X-MAM") != "1" {
		jerr(w, 400, "Yêu cầu không hợp lệ")
		return
	}
	var in struct{ User, Password, Code string }
	if readJSON(r, &in) != nil {
		jerr(w, 400, "Dữ liệu không hợp lệ")
		return
	}
	key := strings.ToLower(in.User) + "|" + clientIP(r)
	s.mu.Lock()
	f := s.fails[key]
	if f != nil && time.Now().Before(f.until) {
		s.mu.Unlock()
		jerr(w, 429, "Sai quá nhiều lần, thử lại sau 1 phút")
		return
	}
	a := s.accountByUser(strings.TrimSpace(in.User))
	var okPw bool
	if a != nil {
		cp := *a
		s.mu.Unlock()
		okPw = cp.checkPassword(in.Password)
		s.mu.Lock()
	} else {
		hashPw(in.Password, "00", pbkdfIter) // giữ thời gian trả lời gần như nhau
	}
	if !okPw {
		if f == nil {
			f = &failRec{}
			s.fails[key] = f
		}
		f.n++
		if f.n >= 5 {
			f.until = time.Now().Add(time.Minute)
			f.n = 0
		}
		s.mu.Unlock()
		s.audit(AuditEntry{User: in.User, Name: in.User, Role: "—", IP: clientIP(r), Kind: "Đăng nhập", Msg: "Đăng nhập sai mật khẩu", Target: in.User, Sens: true})
		jerr(w, 401, "Sai tài khoản hoặc mật khẩu")
		return
	}
	if a.TOTP != "" { // đúng mật khẩu rồi mới đòi mã; sai mã tính như sai mật khẩu (khóa 1 phút sau 5 lần)
		if strings.TrimSpace(in.Code) == "" {
			s.mu.Unlock()
			jerr(w, 401, "2FA_REQUIRED")
			return
		}
		st := totpCheck(a.TOTP, in.Code, time.Now(), a.TOTPLast)
		if st == 0 {
			if f == nil {
				f = &failRec{}
				s.fails[key] = f
			}
			if f.n++; f.n >= 5 {
				f.until = time.Now().Add(time.Minute)
				f.n = 0
			}
			s.mu.Unlock()
			s.audit(AuditEntry{User: in.User, Name: in.User, Role: "—", IP: clientIP(r), Kind: "Đăng nhập", Msg: "Đăng nhập sai mã xác thực hai bước", Target: in.User, Sens: true})
			jerr(w, 401, "Mã xác thực không đúng")
			return
		}
		a.TOTPLast = st
	}
	delete(s.fails, key)
	a.LastLogin = time.Now()
	s.save()
	cp := *a
	s.mu.Unlock()
	s.newSession(w, cp.ID)
	s.audit(AuditEntry{User: cp.User, Name: cp.Name, Role: cp.Role, IP: clientIP(r), Kind: "Đăng nhập", Msg: "Đăng nhập vào console", Target: "—"})
	jok(w, map[string]interface{}{"ok": true, "me": accView(&cp)})
}

func (s *Server) hLogout(w http.ResponseWriter, r *http.Request, me *Account) {
	if c, err := r.Cookie("mam_sess"); err == nil {
		s.mu.Lock()
		delete(s.sess, c.Value)
		s.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: "mam_sess", Value: "", Path: "/", MaxAge: -1})
	jok(w, map[string]bool{"ok": true})
}

func (s *Server) hMe(w http.ResponseWriter, r *http.Request, me *Account) {
	jok(w, accView(me))
}

func (s *Server) hMyPassword(w http.ResponseWriter, r *http.Request, me *Account) {
	var in struct{ Current, New string }
	if readJSON(r, &in) != nil {
		jerr(w, 400, "Dữ liệu không hợp lệ")
		return
	}
	if err := checkNewPassword(in.New); err != nil {
		jerr(w, 400, err.Error())
		return
	}
	s.mu.Lock()
	a := s.accountByID(me.ID)
	cp := *a
	s.mu.Unlock()
	if !cp.checkPassword(in.Current) {
		jerr(w, 400, "Mật khẩu hiện tại không đúng")
		return
	}
	if in.New == in.Current {
		jerr(w, 400, "Mật khẩu mới phải khác mật khẩu cũ")
		return
	}
	s.mu.Lock()
	a.setPassword(in.New)
	a.MustChange = false
	s.save()
	s.mu.Unlock()
	s.auditBy(me, r, "Tài khoản", "Đổi mật khẩu của chính mình", me.User, true)
	jok(w, map[string]bool{"ok": true})
}

// ---------- quản lý tài khoản (Super admin) ----------

func validRole(r string) bool { return r == "super" || r == "tech" || r == "view" }

func (s *Server) hAccounts(w http.ResponseWriter, r *http.Request, me *Account) {
	switch r.Method {
	case "GET":
		s.mu.RLock()
		out := []map[string]interface{}{}
		for _, a := range s.st.Accounts {
			out = append(out, accView(a))
		}
		s.mu.RUnlock()
		jok(w, out)
	case "POST":
		var in struct {
			Name, User, Password, Role string
			Scope                      []string
		}
		if readJSON(r, &in) != nil {
			jerr(w, 400, "Dữ liệu không hợp lệ")
			return
		}
		in.User = strings.TrimSpace(in.User)
		in.Name = strings.TrimSpace(in.Name)
		if len([]rune(in.Name)) < 2 || !userRe.MatchString(in.User) || !validRole(in.Role) {
			jerr(w, 400, "Kiểm tra lại họ tên, tài khoản (chữ thường, số, . _) và vai trò")
			return
		}
		if err := checkNewPassword(in.Password); err != nil {
			jerr(w, 400, err.Error())
			return
		}
		s.mu.Lock()
		if s.accountByUser(in.User) != nil {
			s.mu.Unlock()
			jerr(w, 409, "Tài khoản đã tồn tại")
			return
		}
		a := &Account{ID: rid(6), Name: in.Name, User: in.User, Role: in.Role, Scope: cleanScope(in.Scope), Created: time.Now(), MustChange: true}
		a.setPassword(in.Password)
		s.st.Accounts = append(s.st.Accounts, a)
		s.save()
		s.mu.Unlock()
		s.auditBy(me, r, "Tài khoản", "Thêm tài khoản "+a.User+" ("+roleName(a.Role)+")", a.User, true)
		jok(w, accView(a))
	default:
		jerr(w, 405, "Sai phương thức")
	}
}

func cleanScope(sc []string) []string {
	out := []string{}
	for _, x := range sc {
		x = strings.TrimSpace(x)
		if x != "" {
			out = append(out, x)
		}
	}
	return out
}

func roleName(r string) string {
	return map[string]string{"super": "Super admin", "tech": "Kỹ thuật viên", "view": "Chỉ xem"}[r]
}

func (s *Server) hAccount(w http.ResponseWriter, r *http.Request, me *Account) {
	id := strings.TrimPrefix(r.URL.Path, "/api/accounts/")
	action := ""
	if i := strings.Index(id, "/"); i >= 0 {
		action = id[i+1:]
		id = id[:i]
	}
	s.mu.Lock()
	a := s.accountByID(id)
	if a == nil {
		s.mu.Unlock()
		jerr(w, 404, "Không có tài khoản này")
		return
	}
	supers := 0
	for _, x := range s.st.Accounts {
		if x.Role == "super" {
			supers++
		}
	}
	switch {
	case action == "reset2fa" && r.Method == "POST": // mất điện thoại: Super admin gỡ 2FA để người đó đăng nhập lại
		a.TOTP, a.TOTPLast = "", 0
		s.save()
		u := a.User
		s.mu.Unlock()
		s.auditBy(me, r, "Tài khoản", "Gỡ xác thực hai bước của "+u, u, true)
		jok(w, map[string]bool{"ok": true})
	case action == "reset" && r.Method == "POST":
		tp := tempPassword()
		a.setPassword(tp)
		a.MustChange = true
		s.dropSessionsOf(a.ID)
		s.save()
		u := a.User
		s.mu.Unlock()
		s.auditBy(me, r, "Tài khoản", "Đặt lại mật khẩu cho "+u+" · mật khẩu tạm, buộc đổi khi đăng nhập", u, true)
		jok(w, map[string]string{"tempPassword": tp})
	case action == "" && r.Method == "PUT":
		var in struct {
			Name, Role string
			Scope      []string
		}
		if readJSON(r, &in) != nil || len([]rune(strings.TrimSpace(in.Name))) < 2 || !validRole(in.Role) {
			s.mu.Unlock()
			jerr(w, 400, "Dữ liệu không hợp lệ")
			return
		}
		if a.Role == "super" && in.Role != "super" && supers <= 1 {
			s.mu.Unlock()
			jerr(w, 400, "Phải còn ít nhất 1 Super admin")
			return
		}
		old := a.Role
		a.Name = strings.TrimSpace(in.Name)
		a.Role = in.Role
		a.Scope = cleanScope(in.Scope)
		if old != in.Role {
			s.dropSessionsOf(a.ID)
		}
		s.save()
		u := a.User
		s.mu.Unlock()
		msg := "Sửa tài khoản " + u
		if old != in.Role {
			msg = "Đổi quyền " + u + " từ " + roleName(old) + " thành " + roleName(in.Role)
		}
		s.auditBy(me, r, "Tài khoản", msg, u, old != in.Role)
		jok(w, map[string]bool{"ok": true})
	case action == "" && r.Method == "DELETE":
		if a.Role == "super" && supers <= 1 {
			s.mu.Unlock()
			jerr(w, 400, "Phải còn ít nhất 1 Super admin")
			return
		}
		if a.ID == me.ID {
			s.mu.Unlock()
			jerr(w, 400, "Không tự xóa tài khoản đang dùng")
			return
		}
		u := a.User
		s.dropSessionsOf(a.ID)
		out := s.st.Accounts[:0]
		for _, x := range s.st.Accounts {
			if x != a {
				out = append(out, x)
			}
		}
		s.st.Accounts = out
		removeAvatar("u", a.ID)
		s.save()
		s.mu.Unlock()
		s.auditBy(me, r, "Tài khoản", "Xóa tài khoản "+u, u, true)
		jok(w, map[string]bool{"ok": true})
	default:
		s.mu.Unlock()
		jerr(w, 405, "Sai phương thức")
	}
}

// Khôi phục tại máy server: chỉ nhận từ chính máy này (127.0.0.1).
func (s *Server) hLocalReset(w http.ResponseWriter, r *http.Request) {
	if !isLoopback(r) || r.Method != "POST" {
		jerr(w, 403, "Chỉ chạy được tại máy server")
		return
	}
	var in struct{ User string }
	readJSON(r, &in)
	tp, err := s.resetLocal(in.User)
	if err != nil {
		jerr(w, 404, err.Error())
		return
	}
	jok(w, map[string]string{"tempPassword": tp})
}

func (s *Server) resetLocal(user string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.accountByUser(strings.TrimSpace(user))
	if a == nil {
		return "", errors.New("Không có tài khoản " + user)
	}
	tp := tempPassword()
	a.setPassword(tp)
	a.MustChange = true
	s.dropSessionsOf(a.ID)
	s.save()
	s.audit(AuditEntry{User: "system", Name: "Hệ thống", Role: "sys", IP: "—", Kind: "Tài khoản", Msg: "Khôi phục mật khẩu " + a.User + " tại máy server (mam-server reset-admin) · mật khẩu tạm, buộc đổi khi đăng nhập", Target: a.User, Sens: true})
	return tp, nil
}
