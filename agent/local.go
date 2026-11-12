package main

// Cửa sổ giao tiếp trên máy client: agent (chạy ngầm) giữ hộp thư và phục vụ trang web tại 127.0.0.1;
// tiến trình "ui" trong phiên người dùng mở cửa sổ khi có tin mới.
import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"time"
)

const uiAddr = "127.0.0.1:47631"

//go:embed missing-intro.js
var introJS []byte

//go:embed ui.html
var uiHTML []byte

type inMsg struct {
	ID     int    `json:"id"`
	From   string `json:"from"`
	Text   string `json:"text"`
	At     int64  `json:"at"`
	Mine   bool   `json:"mine,omitempty"`   // tin do người ngồi máy gửi đi
	Kind   string `json:"kind,omitempty"`   // "" = tin nhắn | req = yêu cầu hỗ trợ | alert = cảnh báo máy
	FromID string `json:"fromId,omitempty"` // id tài khoản quản trị gửi (để lấy ảnh đại diện)
}

var inbox struct {
	sync.Mutex
	msgs   []inMsg
	seq    int
	read   int   // id lớn nhất đã được xem
	ping   int64 // lần cuối cửa sổ báo còn mở
	online bool
}

func addMsg(from, text string, mine bool) { addNotice("", from, text, mine) }

// addMsgFrom: như addMsg nhưng gắn id người gửi để cửa sổ hiện ảnh đại diện của họ.
func addMsgFrom(fromID, from, text string) {
	addNotice("", from, text, false)
	inbox.Lock()
	inbox.msgs[len(inbox.msgs)-1].FromID = fromID
	inbox.Unlock()
}

// addNotice: thêm một thông báo vào hộp thư; cửa sổ MAM sẽ tự bật lên, kêu "ting" và hiện nổi bật.
func addNotice(kind, from, text string, mine bool) {
	inbox.Lock()
	defer inbox.Unlock()
	inbox.seq++
	inbox.msgs = append(inbox.msgs, inMsg{ID: inbox.seq, From: from, Text: text, At: time.Now().Unix(), Mine: mine, Kind: kind})
	if len(inbox.msgs) > 100 {
		inbox.msgs = inbox.msgs[len(inbox.msgs)-100:]
	}
	if mine {
		inbox.read = inbox.seq
	}
}

func setOnline(v bool) { inbox.Lock(); inbox.online = v; inbox.Unlock() }

func startLocal() {
	ln, err := net.Listen("tcp", uiAddr)
	if err != nil {
		return // cổng bận (đã có agent khác): bỏ qua, agent vẫn báo cáo bình thường
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(uiHTML)
	})
	mux.HandleFunc("/missing-intro.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Write(introJS)
	})
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		inbox.Lock()
		defer inbox.Unlock()
		unread := 0
		for _, m := range inbox.msgs {
			if m.ID > inbox.read {
				unread++
			}
		}
		if r.URL.Query().Get("proc") == "1" { // tiến trình "ui" trong phiên người dùng còn sống
			uiBeat()
		}
		if r.URL.Query().Get("ui") == "1" { // cửa sổ đang mở
			inbox.ping = time.Now().Unix()
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"name": cfg.Name, "server": cfg.Server, "ver": Version, "online": inbox.online,
			"lang": uiLang, "unread": unread, "seq": inbox.seq, "open": time.Now().Unix()-inbox.ping < 6, "msgs": inbox.msgs, "screen": screenStateJSON(), "types": getDeviceTypes(), "contact": getContactLabel()})
	})
	// Chỉ cho mở MỘT cửa sổ: cửa sổ đang mở, hoặc vừa có lệnh mở trong 25 giây (trang chưa kịp báo "đang mở") thì từ chối.
	mux.HandleFunc("/api/launch", func(w http.ResponseWriter, r *http.Request) {
		inbox.Lock()
		now := time.Now().Unix()
		ok := now-inbox.ping >= 6 && now-launchAt >= 25
		if ok {
			launchAt = now
		}
		inbox.Unlock()
		json.NewEncoder(w).Encode(map[string]bool{"go": ok})
	})
	mux.HandleFunc("/api/read", func(w http.ResponseWriter, r *http.Request) {
		inbox.Lock()
		inbox.read = inbox.seq
		inbox.Unlock()
		w.Write([]byte("{}"))
	})
	// Ảnh đại diện: cửa sổ MAM chỉ nói chuyện với agent; agent mang token đi lấy/đổi ảnh trên server.
	mux.HandleFunc("/av/", func(w http.ResponseWriter, r *http.Request) {
		q := "/api/agent/avatar"
		if p := strings.TrimPrefix(r.URL.Path, "/av/"); strings.HasPrefix(p, "u/") {
			q += "?kind=u&id=" + url.QueryEscape(strings.TrimPrefix(p, "u/"))
		} else if p != "self" {
			http.NotFound(w, r)
			return
		}
		code, ct, body := rawReq("GET", q, nil)
		if code != 200 {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Cache-Control", "max-age=300")
		w.Write(body)
	})
	mux.HandleFunc("/api/avatar", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" && r.Method != "DELETE" {
			http.Error(w, "bad", 405)
			return
		}
		var body []byte
		if r.Method == "POST" {
			var err error
			if body, err = io.ReadAll(http.MaxBytesReader(w, r.Body, 3<<20)); err != nil || len(body) == 0 {
				http.Error(w, "bad", 400)
				return
			}
		}
		code, _, resp := rawReq(r.Method, "/api/agent/avatar", body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		w.Write(resp)
	})
	mux.HandleFunc("/api/reply", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Text string }
		if r.Method != "POST" || json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in) != nil {
			http.Error(w, "bad", 400)
			return
		}
		t := []rune(in.Text)
		if len(t) == 0 || len(t) > 300 {
			http.Error(w, "1-300", 400)
			return
		}
		if resp, err := post("/api/agent/reply", map[string]string{"text": in.Text}, cfg.Token); err != nil || resp.code != 200 {
			http.Error(w, "server", 502)
			return
		}
		addMsg("", in.Text, true)
		w.Write([]byte("{}"))
	})
	mux.HandleFunc("/api/rename", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Name, Code string }
		if r.Method != "POST" || json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&in) != nil {
			http.Error(w, "bad", 400)
			return
		}
		in.Name = strings.TrimSpace(in.Name)
		if n := len([]rune(in.Name)); n < 1 || n > 40 {
			http.Error(w, "1-40", 400)
			return
		}
		resp, err := post("/api/agent/rename", map[string]string{"name": in.Name, "code": in.Code}, cfg.Token)
		if err != nil {
			http.Error(w, "server", 502)
			return
		}
		if resp.code != 200 { // sai/thiếu mật khẩu gỡ: đưa lời server báo (đã dịch) lên cửa sổ
			var e struct{ Error string }
			json.Unmarshal(resp.body, &e)
			msg := "server"
			if e.Error != "" && resp.code != 401 {
				msg = uninstErr(e.Error)
			}
			http.Error(w, msg, resp.code)
			return
		}
		cfg.Name = in.Name
		saveConfig()
		w.Write([]byte("{}"))
	})
	// Thông tin máy cho cửa sổ MAM.
	mux.HandleFunc("/api/info", func(w http.ResponseWriter, r *http.Request) {
		ip, mac := localAddr()
		lastInfo.Lock()
		rep, hw := lastInfo.rep, lastInfo.hw
		lastInfo.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"name": cfg.Name, "os": osName(), "ip": ip, "mac": mac, "ver": Version, "arch": runtime.GOARCH,
			"server": cfg.Server, "rep": rep, "hw": hw})
	})
	// Tự sửa chữa do người ngồi máy bấm trong cửa sổ MAM (tiến trình này chạy SYSTEM nên làm được); mỗi lúc chỉ chạy một việc.
	var maintMu sync.Mutex
	mux.HandleFunc("/api/maint", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Do string }
		if r.Method != "POST" || json.NewDecoder(http.MaxBytesReader(w, r.Body, 256)).Decode(&in) != nil || (in.Do != "clean" && in.Do != "freeram") {
			http.Error(w, "bad", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if !maintMu.TryLock() {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "out": "Đang làm việc trước đó, đợi vài giây rồi bấm lại"})
			return
		}
		defer maintMu.Unlock()
		res := func() (res Result) {
			defer func() {
				if e := recover(); e != nil {
					log.Printf("tự sửa chữa %s lỗi: %v", in.Do, e)
					res = Result{Out: fmt.Sprint("Lỗi: ", e)}
				}
			}()
			return handleCommand(in.Do, nil)
		}()
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": res.OK, "out": res.Out})
	})
	registerReqRoutes(mux)
	registerScreenRoutes(mux)
	// chặn trang web ngoài gọi vào qua trình duyệt: chỉ nhận Host là 127.0.0.1
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != uiAddr {
			http.Error(w, "forbidden", 403)
			return
		}
		mux.ServeHTTP(w, r)
	}))
}
