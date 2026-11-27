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
	// SID: id dòng chat bên server. Có nó mới thả cảm xúc / thu hồi / sửa đúng tin được. Thông báo
	// máy tự sinh (cảnh báo, yêu cầu hỗ trợ) không có SID nên không thao tác được — đúng vậy.
	SID string `json:"sid,omitempty"`
	// File gửi kèm: chính file nằm trên server, đây chỉ giữ tên và cỡ để vẽ ra cửa sổ
	Tep    string `json:"tep,omitempty"`
	TepCo  int64  `json:"tepCo,omitempty"`
	TepAnh bool   `json:"tepAnh,omitempty"`
	Cam    string `json:"cam,omitempty"`    // cảm xúc quản trị viên thả
	CamMay string `json:"camMay,omitempty"` // cảm xúc người ngồi máy thả
	Thu    bool   `json:"thu,omitempty"`    // đã thu hồi
	Sua    bool   `json:"sua,omitempty"`    // đã sửa
	// Trả lời một tin cũ: chép sẵn tên + nội dung tin gốc, khỏi tra ngược (hộp thư chỉ giữ 100 tin)
	TraBy   string `json:"traBy,omitempty"`
	TraText string `json:"traText,omitempty"`
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

// addMsgFrom: như addMsg nhưng gắn id người gửi để cửa sổ hiện ảnh đại diện của họ, và id dòng chat
// bên server để sau còn thả cảm xúc hay thấy admin thu hồi.
func addMsgFrom(fromID, from, text, sid, traBy, traText string) {
	addNotice("", from, text, false)
	inbox.Lock()
	m := &inbox.msgs[len(inbox.msgs)-1]
	m.FromID, m.SID, m.TraBy, m.TraText = fromID, sid, traBy, traText
	inbox.Unlock()
}

// chatAct: áp một thay đổi do bên kia làm (admin thả cảm xúc, thu hồi, sửa) lên tin tương ứng.
func chatAct(sid, lam, cam, text string) {
	inbox.Lock()
	defer inbox.Unlock()
	for i := range inbox.msgs {
		if inbox.msgs[i].SID == "" || inbox.msgs[i].SID != sid {
			continue
		}
		m := &inbox.msgs[i]
		switch lam {
		case "cam":
			m.Cam = cam
		case "thu":
			m.Thu, m.Text, m.Sua = true, "", false
		case "sua":
			m.Text, m.Sua = text, true
		}
		return
	}
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
		var in struct{ Text, TraID string }
		if r.Method != "POST" || json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in) != nil {
			http.Error(w, "bad", 400)
			return
		}
		t := []rune(in.Text)
		if len(t) == 0 || len(t) > 300 {
			http.Error(w, "1-300", 400)
			return
		}
		// trả lời tin nào thì lấy luôn tên + nội dung tin đó trong hộp thư để hiện phần trích dẫn
		traBy, traText := "", ""
		if in.TraID != "" {
			inbox.Lock()
			for _, m := range inbox.msgs {
				if m.SID == in.TraID && !m.Thu {
					traBy, traText = m.From, m.Text
				}
			}
			inbox.Unlock()
		}
		resp, err := post("/api/agent/reply", map[string]string{"text": in.Text, "traId": in.TraID}, cfg.Token)
		if err != nil || resp.code != 200 {
			http.Error(w, "server", 502)
			return
		}
		var ra struct{ ID string }
		json.Unmarshal(resp.body, &ra) // server trả id dòng chat; không có thì tin này không thu hồi được
		addMsg("", in.Text, true)
		inbox.Lock()
		m := &inbox.msgs[len(inbox.msgs)-1]
		m.SID, m.TraBy, m.TraText = ra.ID, traBy, traText
		inbox.Unlock()
		w.Write([]byte("{}"))
	})
	// Người ngồi máy thả cảm xúc / thu hồi / sửa tin: báo server trước, server gật đầu mới đổi tại chỗ.
	mux.HandleFunc("/api/chat-act", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ SID, Lam, Cam, Text string }
		if r.Method != "POST" || json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in) != nil || in.SID == "" {
			http.Error(w, "bad", 400)
			return
		}
		if n := len([]rune(in.Text)); in.Lam == "sua" && (n < 1 || n > 300) {
			http.Error(w, "1-300", 400)
			return
		}
		resp, err := post("/api/agent/chat-act", map[string]string{"id": in.SID, "lam": in.Lam, "cam": in.Cam, "text": in.Text}, cfg.Token)
		if err != nil || resp.code != 200 {
			http.Error(w, "server", 502)
			return
		}
		inbox.Lock()
		for i := range inbox.msgs {
			if inbox.msgs[i].SID != in.SID {
				continue
			}
			m := &inbox.msgs[i]
			switch in.Lam {
			case "cam":
				m.CamMay = in.Cam
			case "thu":
				m.Thu, m.Text, m.Sua = true, "", false
			case "sua":
				m.Text, m.Sua = in.Text, true
			}
			break
		}
		inbox.Unlock()
		w.Write([]byte("{}"))
	})
	// File trong chat: cửa sổ MAM không có phiên đăng nhập Console nên phải đi vòng qua agent.
	// GET ?sid=<id dòng chat> tải file về xem; POST ?ten=&text= gửi file của người ngồi máy lên.
	mux.HandleFunc("/api/chat-tep", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			resp, err := tepStream("GET", "/api/agent/chat-tep?tin="+url.QueryEscape(r.URL.Query().Get("sid")), nil, 0)
			if err != nil {
				http.Error(w, "server", 502)
				return
			}
			defer resp.Body.Close()
			for _, h := range []string{"Content-Type", "Content-Disposition", "Content-Length"} {
				if v := resp.Header.Get(h); v != "" {
					w.Header().Set(h, v)
				}
			}
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.WriteHeader(resp.StatusCode)
			io.Copy(w, resp.Body)
			return
		}
		if r.Method != "POST" {
			http.Error(w, "bad", 400)
			return
		}
		q := "?ten=" + url.QueryEscape(r.URL.Query().Get("ten")) + "&text=" + url.QueryEscape(r.URL.Query().Get("text"))
		resp, err := tepStream("POST", "/api/agent/chat-tep"+q, r.Body, r.ContentLength)
		if err != nil {
			http.Error(w, "server", 502)
			return
		}
		defer resp.Body.Close()
		var ra struct {
			ID, Ten string
			Co      int64
			Anh     bool
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if resp.StatusCode != 200 || json.Unmarshal(b, &ra) != nil || ra.ID == "" {
			w.WriteHeader(resp.StatusCode)
			w.Write(b)
			return
		}
		addMsg("", r.URL.Query().Get("text"), true)
		inbox.Lock()
		m := &inbox.msgs[len(inbox.msgs)-1]
		m.SID, m.Tep, m.TepCo, m.TepAnh = ra.ID, ra.Ten, ra.Co, ra.Anh
		inbox.Unlock()
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
	// Người dùng đổi ngôn ngữ ngay trên cửa sổ MAM: lưu vào config, câu agent hiện ra (thông báo tắt máy...) cũng đổi theo.
	mux.HandleFunc("/api/lang", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Lang string }
		if r.Method != "POST" || json.NewDecoder(http.MaxBytesReader(w, r.Body, 256)).Decode(&in) != nil {
			http.Error(w, "bad", 400)
			return
		}
		if in.Lang != "vi" && in.Lang != "en" && in.Lang != "zh" {
			http.Error(w, "vi|en|zh", 400)
			return
		}
		setLang(in.Lang)
		cfg.Lang = uiLang
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

// tepClient: riêng cho chuyển file. httpc đặt Timeout 20 giây cho cả yêu cầu — file vài trăm MB
// không bao giờ xong trong 20 giây, dùng chung là lần nào cũng đứt giữa chừng.
var tepClient = &http.Client{Timeout: 45 * time.Minute}

// tepStream gọi server với token máy, thân là một LUỒNG: không ôm cả file vào RAM, máy người dùng
// có khi chỉ 4 GB. Người gọi phải đóng resp.Body.
func tepStream(method, path string, body io.Reader, clen int64) (*http.Response, error) {
	req, err := http.NewRequest(method, strings.TrimRight(cfg.Server, "/")+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	if clen > 0 {
		req.ContentLength = clen
	}
	return tepClient.Do(req)
}

// addTep: tin nhắn kèm file do admin gửi xuống.
func addTep(fromID, from, text, sid, ten string, co int64, anh bool) {
	addNotice("", from, text, false)
	inbox.Lock()
	m := &inbox.msgs[len(inbox.msgs)-1]
	m.FromID, m.SID, m.Tep, m.TepCo, m.TepAnh = fromID, sid, ten, co, anh
	inbox.Unlock()
}
