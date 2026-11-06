package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// TOTP theo RFC 6238 (SHA-1, 6 số, bước 30 giây): dùng được với Google Authenticator, Microsoft Authenticator, Authy…
var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

func newTOTPSecret() string {
	b := make([]byte, 20)
	rand.Read(b)
	return b32.EncodeToString(b)
}

func totpCode(secret string, step int64) string {
	key, err := b32.DecodeString(strings.ToUpper(secret))
	if err != nil {
		return ""
	}
	var c [8]byte
	binary.BigEndian.PutUint64(c[:], uint64(step))
	m := hmac.New(sha1.New, key)
	m.Write(c[:])
	h := m.Sum(nil)
	o := h[len(h)-1] & 15
	n := binary.BigEndian.Uint32(h[o:o+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", n%1000000)
}

// totpCheck nhận mã của bước hiện tại hoặc liền kề (lệch giờ ±30 giây); trả bước khớp, hoặc 0.
// last là bước đã dùng gần nhất: không nhận lại mã cũ (chống dùng lại mã vừa lộ).
func totpCheck(secret, code string, now time.Time, last int64) int64 {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != 6 {
		return 0
	}
	cur := now.Unix() / 30
	for d := int64(-1); d <= 1; d++ {
		st := cur + d
		if st > last && hmac.Equal([]byte(totpCode(secret, st)), []byte(code)) {
			return st
		}
	}
	return 0
}

// hMy2FA: /api/me/2fa/{start|enable|disable}. start tạo khóa chờ xác nhận; enable xác nhận bằng mã đầu tiên.
func (s *Server) hMy2FA(w http.ResponseWriter, r *http.Request, me *Account) {
	act := strings.TrimPrefix(r.URL.Path, "/api/me/2fa/")
	if r.Method != "POST" {
		jerr(w, 405, "Phương thức không hỗ trợ")
		return
	}
	var in struct{ Code, Password string }
	readJSON(r, &in)
	s.mu.Lock()
	a := s.accountByID(me.ID)
	switch act {
	case "start":
		if a.TOTP != "" {
			s.mu.Unlock()
			jerr(w, 400, "Đã bật xác thực hai bước")
			return
		}
		a.TOTPPending = newTOTPSecret()
		sec := a.TOTPPending
		s.mu.Unlock()
		uri := "otpauth://totp/" + url.PathEscape("MAM:"+a.User) + "?secret=" + sec + "&issuer=MAM"
		jok(w, map[string]string{"secret": sec, "uri": uri})
	case "enable":
		st := int64(0)
		if a.TOTPPending != "" {
			st = totpCheck(a.TOTPPending, in.Code, time.Now(), 0)
		}
		if st == 0 {
			s.mu.Unlock()
			jerr(w, 400, "Mã không đúng. Kiểm tra đồng hồ điện thoại rồi thử lại")
			return
		}
		a.TOTP, a.TOTPPending, a.TOTPLast = a.TOTPPending, "", st
		s.save()
		s.mu.Unlock()
		s.auditBy(me, r, "Tài khoản", "Bật xác thực hai bước", me.User, true)
		jok(w, map[string]bool{"ok": true})
	case "disable":
		cp := *a
		s.mu.Unlock()
		if cp.TOTP == "" || !cp.checkPassword(in.Password) {
			jerr(w, 400, "Sai mật khẩu")
			return
		}
		s.mu.Lock()
		if st := totpCheck(a.TOTP, in.Code, time.Now(), a.TOTPLast); st == 0 {
			s.mu.Unlock()
			jerr(w, 400, "Mã xác thực không đúng")
			return
		}
		a.TOTP, a.TOTPLast = "", 0
		s.save()
		s.mu.Unlock()
		s.auditBy(me, r, "Tài khoản", "Tắt xác thực hai bước", me.User, true)
		jok(w, map[string]bool{"ok": true})
	default:
		s.mu.Unlock()
		jerr(w, 404, "Không có thao tác này")
	}
}
