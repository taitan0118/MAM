package main

import (
	"bytes"
	"image"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Ảnh đại diện: mỗi tài khoản (kind "u") hoặc máy (kind "m") một file JPEG 128x128 trong <dir>/avatars.
// Ảnh luôn được giải mã rồi mã hóa lại ở server, nên file lạ giả dạng ảnh và dữ liệu ẩn (EXIF) đều bị loại.
const avSize = 128

var (
	avatarDir string // đặt khi server khởi động
	reAvID    = regexp.MustCompile(`^[0-9a-f]{1,32}$`)
)

func avatarPath(kind, id string) string {
	if (kind != "u" && kind != "m") || !reAvID.MatchString(id) || avatarDir == "" {
		return ""
	}
	return filepath.Join(avatarDir, kind+"-"+id+".jpg")
}

// avatarVer: mốc thời gian của ảnh (0 = chưa có), dùng làm ?v= để trình duyệt tải lại khi đổi ảnh.
func avatarVer(kind, id string) int64 {
	if p := avatarPath(kind, id); p != "" {
		if fi, err := os.Stat(p); err == nil {
			return fi.ModTime().Unix()
		}
	}
	return 0
}

func removeAvatar(kind, id string) {
	if p := avatarPath(kind, id); p != "" {
		os.Remove(p)
	}
}

// squareJPEG cắt vuông ở giữa, thu về avSize rồi nén JPEG.
func squareJPEG(raw []byte) ([]byte, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width < 16 || cfg.Height < 16 || cfg.Width > 6000 || cfg.Height > 6000 {
		return nil, os.ErrInvalid
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	side := b.Dx()
	if b.Dy() < side {
		side = b.Dy()
	}
	x0, y0 := b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2
	dst := image.NewRGBA(image.Rect(0, 0, avSize, avSize))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src) // nền trắng cho ảnh PNG trong suốt
	// lấy mẫu trung bình theo ô (box filter): đủ mịn cho ảnh nhỏ, không cần thư viện ngoài
	for y := 0; y < avSize; y++ {
		for x := 0; x < avSize; x++ {
			sx0, sx1 := x0+x*side/avSize, x0+(x+1)*side/avSize
			sy0, sy1 := y0+y*side/avSize, y0+(y+1)*side/avSize
			if sx1 <= sx0 {
				sx1 = sx0 + 1
			}
			if sy1 <= sy0 {
				sy1 = sy0 + 1
			}
			var r, g, bl, a, n uint64
			for yy := sy0; yy < sy1; yy++ {
				for xx := sx0; xx < sx1; xx++ {
					cr, cg, cb, ca := img.At(xx, yy).RGBA()
					// trộn lên nền trắng
					r += uint64(cr) + uint64(0xffff-ca)
					g += uint64(cg) + uint64(0xffff-ca)
					bl += uint64(cb) + uint64(0xffff-ca)
					a++
					n++
				}
			}
			o := dst.PixOffset(x, y)
			dst.Pix[o], dst.Pix[o+1], dst.Pix[o+2], dst.Pix[o+3] = byte(r/a>>8), byte(g/a>>8), byte(bl/a>>8), 255
		}
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 82}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func saveAvatar(kind, id string, raw []byte) error {
	p := avatarPath(kind, id)
	if p == "" {
		return os.ErrInvalid
	}
	j, err := squareJPEG(raw)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(avatarDir, 0o700); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, j, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func serveAvatar(w http.ResponseWriter, r *http.Request, kind, id string) {
	p := avatarPath(kind, id)
	f, err := os.Open(p)
	if p == "" || err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	fi, _ := f.Stat()
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=86400") // URL có ?v= nên đổi ảnh là đổi URL
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", fi.ModTime(), f)
}

func readImage(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 3<<20))
	if err != nil || len(raw) == 0 {
		jerr(w, 400, "Ảnh quá lớn (tối đa 3 MB) hoặc không đọc được")
		return nil, false
	}
	return raw, true
}

// /api/avatar/{u|m}/{id}: GET xem (mọi tài khoản đã đăng nhập); POST đổi ảnh của chính mình; DELETE xóa ảnh của mình, Super admin xóa được của mọi người.
func (s *Server) hAvatar(w http.ResponseWriter, r *http.Request, me *Account) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/avatar/"), "/")
	if len(parts) != 2 {
		jerr(w, 404, "Không có ảnh")
		return
	}
	kind, id := parts[0], parts[1]
	switch r.Method {
	case "GET":
		serveAvatar(w, r, kind, id)
	case "POST":
		if kind != "u" || id != me.ID {
			jerr(w, 403, "Chỉ đổi được ảnh của chính mình")
			return
		}
		raw, ok := readImage(w, r)
		if !ok {
			return
		}
		if err := saveAvatar(kind, id, raw); err != nil {
			jerr(w, 400, "Không đọc được ảnh. Dùng ảnh JPG, PNG hoặc GIF")
			return
		}
		jok(w, map[string]interface{}{"ok": true, "av": avatarVer(kind, id)})
	case "DELETE":
		if !(kind == "u" && id == me.ID) && me.Role != "super" {
			jerr(w, 403, "Chỉ Super admin xóa ảnh của người khác")
			return
		}
		removeAvatar(kind, id)
		if me.ID != id {
			s.auditBy(me, r, "Ảnh đại diện", "Xóa ảnh đại diện ("+kind+"/"+id+")", id, false)
		}
		jok(w, map[string]bool{"ok": true})
	default:
		jerr(w, 405, "Phương thức không hỗ trợ")
	}
}

// /api/avatar-policy: Super admin bật/tắt việc máy tự đổi ảnh.
func (s *Server) hAvatarPolicy(w http.ResponseWriter, r *http.Request, me *Account) {
	if r.Method == "GET" {
		s.mu.RLock()
		defer s.mu.RUnlock()
		jok(w, map[string]bool{"machines": !s.st.Settings.NoMachineAvatar})
		return
	}
	if me.Role != "super" {
		jerr(w, 403, "Chỉ Super admin đổi cài đặt")
		return
	}
	var in struct{ Machines bool }
	if readJSON(r, &in) != nil {
		jerr(w, 400, "Dữ liệu không hợp lệ")
		return
	}
	s.mu.Lock()
	s.st.Settings.NoMachineAvatar = !in.Machines
	s.save()
	s.mu.Unlock()
	s.auditBy(me, r, "Cài đặt", "Cho phép máy tự đổi ảnh đại diện: "+strconv.FormatBool(in.Machines), "—", false)
	jok(w, map[string]bool{"ok": true})
}

// /api/agent/avatar: GET ?kind=u&id=… xem ảnh quản trị viên (mặc định ảnh của chính máy); POST đổi ảnh máy; DELETE xóa ảnh máy.
func (s *Server) hAgentAvatar(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	m := s.agentMachine(r)
	allow := !s.st.Settings.NoMachineAvatar
	s.mu.RUnlock()
	if m == nil {
		jerr(w, 401, "Token không hợp lệ")
		return
	}
	switch r.Method {
	case "GET":
		kind, id := r.URL.Query().Get("kind"), r.URL.Query().Get("id")
		if kind == "" {
			kind, id = "m", m.ID
		}
		if kind == "m" && id != m.ID { // máy chỉ xem được ảnh của mình và của quản trị viên
			http.NotFound(w, r)
			return
		}
		serveAvatar(w, r, kind, id)
	case "POST":
		if !allow {
			jerr(w, 403, "Quản trị viên không cho phép máy đổi ảnh")
			return
		}
		raw, ok := readImage(w, r)
		if !ok {
			return
		}
		if err := saveAvatar("m", m.ID, raw); err != nil {
			jerr(w, 400, "Không đọc được ảnh. Dùng ảnh JPG, PNG hoặc GIF")
			return
		}
		jok(w, map[string]interface{}{"ok": true, "av": time.Now().Unix()})
	case "DELETE":
		removeAvatar("m", m.ID)
		jok(w, map[string]bool{"ok": true})
	default:
		jerr(w, 405, "Phương thức không hỗ trợ")
	}
}
