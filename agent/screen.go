package main

// Xem/điều khiển màn hình khi được người ngồi máy đồng ý.
// Tiến trình chính (chạy quyền SYSTEM, phiên 0) chỉ relay giữa server và cửa sổ MAM; việc chụp
// màn hình và gửi chuột/phím thật phải làm từ tiến trình "ui" (chạy trong phiên người dùng, xem
// screen_windows.go) vì phiên 0 không có quyền truy cập desktop của người dùng.
import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type screenInEv struct {
	Type   string  `json:"type"` // move | down | up | wheel | key | kdown | kup | text
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Button string  `json:"button"`
	Delta  float64 `json:"delta"`
	Key    string  `json:"key"`
	Code   string  `json:"code"` // kdown | kup: mã phím vật lý của trình duyệt (KeyA, ControlLeft...)
	Text   string  `json:"text"` // text: chuỗi gõ vào máy (kể cả tiếng Việt)
}

var scr struct {
	sync.Mutex
	ID     string
	By     string
	ByUser string
	Status string // "" | pending | active | ended
	Reason string
	Input  []screenInEv
	Diag   string    // lý do chưa có hình (tiến trình ui báo lên hoặc ui không liên lạc)
	LastUI time.Time // lần cuối tiến trình ui trong phiên người dùng gọi vào
	Frames int       // số khung hình đã gửi lên server trong phiên này
}

var trustMu sync.Mutex // chỉ bảo vệ cfg.TrustedAdmins, tránh tranh chấp giữa goroutine report và local server

func isTrustedAdmin(u string) bool {
	if u == "" {
		return false
	}
	trustMu.Lock()
	defer trustMu.Unlock()
	for _, x := range cfg.TrustedAdmins {
		if x == u {
			return true
		}
	}
	return false
}

func addTrustedAdmin(u string) {
	if u == "" || isTrustedAdmin(u) {
		return
	}
	trustMu.Lock()
	cfg.TrustedAdmins = append(cfg.TrustedAdmins, u)
	saveConfig()
	trustMu.Unlock()
}

func removeTrustedAdmin(u string) {
	trustMu.Lock()
	out := cfg.TrustedAdmins[:0]
	for _, x := range cfg.TrustedAdmins {
		if x != u {
			out = append(out, x)
		}
	}
	cfg.TrustedAdmins = out
	saveConfig()
	trustMu.Unlock()
}

// announceScreen: server báo (qua report) có người xin xem màn hình máy này. Nếu admin đó đã được
// người ngồi máy chọn "luôn đồng ý" từ trước, tự cho phép luôn — nhưng vẫn luôn hiện thanh báo khi
// đang xem và nút Dừng ngay, không bao giờ xem ngầm mà máy không biết.
func announceScreen(id, by, byUser string, auto bool) {
	scr.Lock()
	if id == "" || scr.ID == id {
		scr.Unlock()
		return
	}
	trusted := auto || isTrustedAdmin(byUser) // auto: admin đã nhập đúng mật khẩu điều khiển từ xa
	addNoticeLater := scr.ID != id
	scr.ID, scr.By, scr.ByUser, scr.Status, scr.Reason, scr.Input = id, by, byUser, "pending", "", nil
	scr.Diag, scr.LastUI, scr.Frames = "", time.Time{}, 0
	scr.Unlock()
	if addNoticeLater && !trusted {
		addNotice("alert", "MAM", T("%s đang xin xem màn hình máy này. Hãy bấm Đồng ý hoặc Từ chối trong cửa sổ MAM.", by), false)
	}
	if trusted {
		grantScreen(id)
	}
}

// grantScreen: báo server cho phép phiên id, rồi bắt đầu lấy thao tác chuột/phím.
func grantScreen(id string) {
	resp, err := post("/api/agent/screen/consent", map[string]interface{}{"id": id, "allow": true}, cfg.Token)
	if err != nil || resp.code != 200 {
		return
	}
	scr.Lock()
	if scr.ID == id {
		scr.Status = "active"
	}
	scr.Unlock()
	go pollScreenInput(id)
}

// expirePendingScreen: server không còn báo phiên xin phép nữa (hết hạn/admin đã đóng): bỏ hộp thoại.
func expirePendingScreen() {
	scr.Lock()
	if scr.Status == "pending" {
		scr.Status, scr.Reason = "ended", "Yêu cầu đã hết hạn"
	}
	scr.Unlock()
}

func screenStateJSON() map[string]interface{} {
	scr.Lock()
	defer scr.Unlock()
	if scr.Status == "" {
		return nil
	}
	return map[string]interface{}{"id": scr.ID, "by": scr.By, "status": scr.Status, "reason": scr.Reason, "trusted": isTrustedAdmin(scr.ByUser)}
}

func atoiSafe(s string) int { n, _ := strconv.Atoi(s); return n }

// pollScreenInput: trong lúc phiên đang mở, hỏi server mỗi chút có thao tác chuột/phím admin gửi
// không, để tiến trình "ui" lấy về tiêm vào máy. Dừng khi phiên không còn "active".
func pollScreenInput(id string) {
	tick := time.NewTicker(120 * time.Millisecond)
	defer tick.Stop()
	start := time.Now()
	for range tick.C {
		scr.Lock()
		live := scr.Status == "active" && scr.ID == id
		diag := scr.Diag
		if diag == "" && time.Since(start) > 6*time.Second && scr.LastUI.Before(start) {
			diag = "Tiến trình MAM trong phiên người dùng không chạy hoặc không liên lạc được với agent"
			if !uiAlive() && kickUI() { // tự bật lại tiến trình đó rồi chờ nó chụp hình
				diag = "Tiến trình MAM trong phiên người dùng đã tắt, agent đang bật lại, đợi vài giây"
			}
		} else if diag == "" && time.Since(start) > 8*time.Second && scr.Frames == 0 {
			diag = "Tiến trình MAM trong phiên người dùng đang chạy nhưng chưa chụp hoặc chưa gửi được khung hình nào"
		}
		scr.Unlock()
		if !live {
			return
		}
		req, _ := http.NewRequest("GET", strings.TrimRight(cfg.Server, "/")+"/api/agent/screen/poll?id="+id+"&diag="+url.QueryEscape(diag), nil)
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
		resp, err := httpc.Do(req)
		if err != nil {
			continue
		}
		var out struct {
			Status string
			Input  []screenInEv
			Chat   []struct {
				By   string
				Text string
				Mine bool
			}
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		json.Unmarshal(b, &out)
		if out.Status != "active" {
			scr.Lock()
			scr.Status, scr.Reason = "ended", "Phiên đã kết thúc"
			scr.Unlock()
			return
		}
		if len(out.Input) > 0 {
			scr.Lock()
			scr.Input = append(scr.Input, out.Input...)
			scr.Unlock()
		}
		// Tin admin nhắn trong lúc xem màn hình: cho hiện vào đúng khung chat sẵn có của cửa sổ MAM.
		for _, c := range out.Chat {
			if c.Mine {
				addMsg(c.By, c.Text, false)
			}
		}
	}
}

// registerScreenRoutes: API cục bộ (127.0.0.1) mà cửa sổ MAM và tiến trình "ui" dùng.
func registerScreenRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/screen/consent", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Allow, Trust bool }
		if r.Method != "POST" || json.NewDecoder(http.MaxBytesReader(w, r.Body, 256)).Decode(&in) != nil {
			http.Error(w, "bad", 400)
			return
		}
		scr.Lock()
		id, byUser := scr.ID, scr.ByUser
		scr.Unlock()
		if id == "" {
			http.Error(w, "none", 404)
			return
		}
		resp, err := post("/api/agent/screen/consent", map[string]interface{}{"id": id, "allow": in.Allow}, cfg.Token)
		if err == nil && resp.code == 404 { // server đã hủy phiên (quá 60 giây hoặc admin đóng cửa sổ)
			expirePendingScreen()
			http.Error(w, "expired", 410)
			return
		}
		if err != nil || resp.code != 200 {
			http.Error(w, "server", 502)
			return
		}
		scr.Lock()
		if in.Allow {
			scr.Status = "active"
		} else {
			scr.Status, scr.Reason = "ended", "Bạn đã từ chối"
		}
		scr.Unlock()
		if in.Allow {
			if in.Trust {
				addTrustedAdmin(byUser)
			}
			go pollScreenInput(id)
		}
		w.Write([]byte("{}"))
	})
	// Bỏ "luôn đồng ý" với admin đang xem màn hình (hỏi lại từ phiên sau), dùng bởi nút trên thanh báo.
	mux.HandleFunc("/api/screen/untrust", func(w http.ResponseWriter, r *http.Request) {
		scr.Lock()
		byUser := scr.ByUser
		scr.Unlock()
		removeTrustedAdmin(byUser)
		w.Write([]byte("{}"))
	})
	mux.HandleFunc("/api/screen/stop", func(w http.ResponseWriter, r *http.Request) {
		scr.Lock()
		id := scr.ID
		scr.Status, scr.Reason = "ended", "Bạn đã bấm Dừng ngay"
		scr.Unlock()
		if id != "" {
			go post("/api/agent/screen/ended", map[string]interface{}{"id": id, "reason": "Người ngồi máy bấm Dừng ngay"}, cfg.Token)
		}
		w.Write([]byte("{}"))
	})
	// Dùng bởi tiến trình "ui": lấy khung hình vừa chụp gửi lên, trả lại trạng thái hiện tại.
	mux.HandleFunc("/api/screen/frame", func(w http.ResponseWriter, r *http.Request) {
		scr.Lock()
		id, live := scr.ID, scr.Status == "active"
		scr.LastUI = time.Now()
		if e := r.URL.Query().Get("err"); e != "" && live { // ui chụp lỗi: ghi lại để báo cho admin
			scr.Diag = "Lỗi chụp màn hình: " + e
		}
		scr.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Method != "POST" || !live {
			w.Write([]byte(`{"status":"ended"}`))
			return
		}
		if r.URL.Query().Get("err") != "" {
			w.Write([]byte(`{"status":"active"}`))
			return
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 3500000))
		fw, fh := atoiSafe(r.URL.Query().Get("w")), atoiSafe(r.URL.Query().Get("h"))
		key := r.URL.Query().Get("k") == "1"
		if r.URL.Query().Get("fmt") != "r" { // nguồn chụp cũ (Linux, macOS) gửi một ảnh JPEG nguyên màn hình: bọc thành gói một vùng
			body, key = wrapFullJPEG(body, fw, fh), true
		}
		data := base64.StdEncoding.EncodeToString(body)
		resp, err := post("/api/agent/screen/frame", map[string]interface{}{"id": id, "data": data, "w": fw, "h": fh, "key": key}, cfg.Token)
		if err == nil && resp.code != 200 { // server/proxy từ chối khung hình (vd quá lớn)
			scr.Lock()
			scr.Diag = "Server không nhận được khung hình (HTTP " + strconv.Itoa(resp.code) + ")"
			scr.Unlock()
			w.Write([]byte(`{"status":"active"}`)) // để ui thử lại; phiên chỉ kết thúc khi server báo
			return
		}
		if err != nil {
			scr.Lock()
			scr.Diag = "Không gửi được khung hình lên server"
			scr.Unlock()
			w.Write([]byte(`{"status":"active"}`)) // lỗi mạng tạm thời: để tiến trình ui thử lại, không dừng ngay
			return
		}
		var out struct {
			Status string
			Key    bool
		}
		json.Unmarshal(resp.body, &out)
		if out.Status == "active" {
			scr.Lock()
			scr.Diag, scr.Frames = "", scr.Frames+1
			scr.Unlock()
		}
		if out.Status != "active" {
			scr.Lock()
			scr.Status, scr.Reason = "ended", "Phiên đã kết thúc"
			scr.Unlock()
		}
		w.Write([]byte(`{"status":"` + out.Status + `","key":` + strconv.FormatBool(out.Key) + `}`))
	})
	// Dùng bởi tiến trình "ui": lấy các thao tác chuột/phím admin vừa gửi để tiêm vào máy.
	mux.HandleFunc("/api/screen/input", func(w http.ResponseWriter, r *http.Request) {
		scr.Lock()
		scr.LastUI = time.Now()
		in, status := scr.Input, scr.Status
		scr.Input = nil
		scr.Unlock()
		b, _ := json.Marshal(map[string]interface{}{"status": status, "input": in})
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
	})
}

// wrapFullJPEG bọc một ảnh JPEG nguyên màn hình thành gói hình một vùng.
// Gói: u32 độ dài phần còn lại | u16 W | u16 H | u16 số vùng | mỗi vùng: u16 x, y, w, h | u32 độ dài JPEG | JPEG. Số nguyên little endian.
func wrapFullJPEG(jpg []byte, w, h int) []byte {
	return buildPacket(w, h, []screenRect{{0, 0, w, h, jpg}})
}

type screenRect struct {
	x, y, w, h int
	jpg        []byte
}

func buildPacket(w, h int, rs []screenRect) []byte {
	n := 6
	for _, r := range rs {
		n += 12 + len(r.jpg)
	}
	b := make([]byte, 4, 4+n)
	binary.LittleEndian.PutUint32(b, uint32(n))
	u16 := func(v int) { b = binary.LittleEndian.AppendUint16(b, uint16(v)) }
	u16(w)
	u16(h)
	u16(len(rs))
	for _, r := range rs {
		u16(r.x)
		u16(r.y)
		u16(r.w)
		u16(r.h)
		b = binary.LittleEndian.AppendUint32(b, uint32(len(r.jpg)))
		b = append(b, r.jpg...)
	}
	return b
}
