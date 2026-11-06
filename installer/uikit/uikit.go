// Package uikit: khung giao diện cài đặt / gỡ cho MAM. Chạy một máy chủ web nhỏ chỉ nghe 127.0.0.1,
// mở nó trong cửa sổ trình duyệt riêng (kiểu ứng dụng) rồi tự thoát khi cửa sổ đóng.
// Mỗi bộ cài (server, agent) chỉ khai báo trường nhập, các bước và hàm thực hiện.
package uikit

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

//go:embed page.html logo.svg
var assets embed.FS

// L là một câu ở 3 ngôn ngữ: vi, en, zh.
type L [3]string

type Field struct {
	ID          string `json:"id"`
	Label       L      `json:"label"`
	Type        string `json:"type"` // text | password | number | checkbox
	Default     string `json:"default"`
	Placeholder L      `json:"placeholder"`
	Help        L      `json:"help"`
	Required    bool   `json:"required"`
}

type Action struct {
	Title    L       `json:"title"`
	Intro    L       `json:"intro"`
	Fields   []Field `json:"fields"`
	Button   L       `json:"button"`
	Steps    []L     `json:"steps"`
	Danger   bool    `json:"danger"`
	Removes  []L     `json:"removes"`  // danh sách "Sẽ gỡ khỏi máy" (tùy chọn)
	Footnote L       `json:"footnote"` // dòng nhỏ cạnh nút bấm (tùy chọn)
}

type Link struct {
	Label L      `json:"label"`
	URL   string `json:"url"`
}

type Result struct {
	Title L      `json:"title"`
	Lines []L    `json:"lines"`
	Links []Link `json:"links"`
}

type App struct {
	Name             string        // ví dụ "MAM Server"
	Logo             string        // SVG riêng của sản phẩm; rỗng thì dùng logo Missing
	Version          string        // bản trong bộ cài này
	InstalledVersion func() string // bản đang cài trên máy (rỗng nếu chưa cài)
	UpdateButton     L             // nhãn nút khi đã cài: làm lại các bước cài, giữ dữ liệu và cài đặt
	Tagline          L
	Installed        func() bool
	Defaults         func() map[string]string // điền sẵn các trường (khóa là Field.ID)
	Install          Action
	Uninstall        Action
	DoInstall        func(p map[string]string, r *Reporter) (Result, error)
	DoUninstall      func(p map[string]string, r *Reporter) (Result, error)
}

// Reporter báo tiến độ: Step(i) = bắt đầu bước i (các bước trước đó xong).
type Reporter struct{ s *runner }

func (r *Reporter) Step(i int) { r.s.step(i) }
func (r *Reporter) Log(s string) {
	r.s.mu.Lock()
	r.s.log = append(r.s.log, s)
	r.s.mu.Unlock()
}

type runner struct {
	mu     sync.Mutex
	phase  string // idle | running | done | error
	action string
	steps  []string
	log    []string
	result *Result
	err    string
	n      int
}

func (r *runner) step(i int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for j := range r.steps {
		switch {
		case j < i:
			r.steps[j] = "ok"
		case j == i:
			r.steps[j] = "run"
		}
	}
}

func (r *runner) snapshot() map[string]interface{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	return map[string]interface{}{"phase": r.phase, "action": r.action, "steps": r.steps, "log": r.log, "result": r.result, "error": r.err}
}

type server struct {
	app   App
	token string
	port  string
	run   *runner
	mu    sync.Mutex
	ping  time.Time
	seen  bool
	quit  chan struct{}
	links map[string]bool
}

// Run mở cửa sổ cài đặt và chờ đến khi người dùng đóng.
func Run(app App) error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	b := make([]byte, 16)
	rand.Read(b)
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	s := &server{app: app, token: hex.EncodeToString(b), port: port, run: &runner{phase: "idle"}, quit: make(chan struct{}), links: map[string]bool{}, ping: time.Now()}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.page)
	mux.HandleFunc("/api/spec", s.spec)
	mux.HandleFunc("/api/run", s.start)
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) { s.json(w, s.run.snapshot()) })
	mux.HandleFunc("/api/reset", s.reset)
	mux.HandleFunc("/api/open", s.open)
	mux.HandleFunc("/api/ping", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.ping, s.seen = time.Now(), true
		s.mu.Unlock()
		s.json(w, "ok")
	})
	mux.HandleFunc("/api/quit", func(w http.ResponseWriter, r *http.Request) {
		s.json(w, "ok")
		go func() { time.Sleep(200 * time.Millisecond); close(s.quit) }()
	})
	srv := &http.Server{Handler: s.guard(mux), ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln)
	go s.watch()
	openWindow("http://127.0.0.1:" + port + "/?k=" + s.token)
	<-s.quit
	srv.Close()
	return nil
}

// watch thoát khi cửa sổ đóng (hết nhịp ping) và không có việc đang chạy.
func (s *server) watch() {
	for range time.Tick(2 * time.Second) {
		s.mu.Lock()
		idle := time.Since(s.ping)
		seen := s.seen
		s.mu.Unlock()
		s.run.mu.Lock()
		busy := s.run.phase == "running"
		s.run.mu.Unlock()
		if !busy && ((seen && idle > 15*time.Second) || (!seen && idle > 90*time.Second)) {
			close(s.quit)
			return
		}
	}
}

// guard: chỉ nhận yêu cầu từ chính cửa sổ này (mã trong cookie) và đúng Host, chống trang web khác gọi vào localhost.
func (s *server) guard(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "127.0.0.1:"+s.port {
			http.Error(w, "forbidden", 403)
			return
		}
		if r.URL.Path == "/" && r.URL.Query().Get("k") == s.token {
			http.SetCookie(w, &http.Cookie{Name: "k", Value: s.token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
			http.Redirect(w, r, "/", 302)
			return
		}
		if c, err := r.Cookie("k"); err != nil || c.Value != s.token {
			http.Error(w, "forbidden", 403)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; img-src data:")
		h.ServeHTTP(w, r)
	})
}

func (s *server) json(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}

func (s *server) page(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	p, _ := assets.ReadFile("page.html")
	logo, _ := assets.ReadFile("logo.svg")
	if s.app.Logo != "" {
		logo = []byte(s.app.Logo)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(strings.Replace(string(p), "{{LOGO}}", string(logo), 1)))
}

func (s *server) spec(w http.ResponseWriter, r *http.Request) {
	def := map[string]string{}
	if s.app.Defaults != nil {
		def = s.app.Defaults()
	}
	fill := func(a Action) Action {
		fs := make([]Field, len(a.Fields))
		copy(fs, a.Fields)
		for i := range fs {
			if v, ok := def[fs[i].ID]; ok {
				fs[i].Default = v
			}
		}
		a.Fields = fs
		return a
	}
	s.json(w, map[string]interface{}{"name": s.app.Name, "tagline": s.app.Tagline, "installed": s.app.Installed != nil && s.app.Installed(), "version": s.app.Version, "installedVersion": s.installedVer(), "updateButton": s.app.UpdateButton, "install": fill(s.app.Install), "uninstall": fill(s.app.Uninstall)})
}

func (s *server) start(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Action string            `json:"action"`
		Params map[string]string `json:"params"`
	}
	if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&in) != nil {
		http.Error(w, "bad request", 400)
		return
	}
	act, do := s.app.Install, s.app.DoInstall
	if in.Action == "uninstall" {
		act, do = s.app.Uninstall, s.app.DoUninstall
	}
	for _, f := range act.Fields {
		if f.Required && strings.TrimSpace(in.Params[f.ID]) == "" {
			http.Error(w, "missing "+f.ID, 400)
			return
		}
	}
	s.run.mu.Lock()
	if s.run.phase == "running" {
		s.run.mu.Unlock()
		http.Error(w, "busy", 409)
		return
	}
	st := make([]string, len(act.Steps))
	for i := range st {
		st[i] = "wait"
	}
	s.run.phase, s.run.action, s.run.steps, s.run.log, s.run.result, s.run.err = "running", in.Action, st, nil, nil, ""
	s.run.mu.Unlock()
	go func() {
		res, err := do(in.Params, &Reporter{s.run})
		s.run.mu.Lock()
		defer s.run.mu.Unlock()
		if err != nil {
			s.run.phase, s.run.err = "error", err.Error()
			for i, v := range s.run.steps {
				if v == "run" {
					s.run.steps[i] = "err"
				}
			}
			return
		}
		for i := range s.run.steps {
			s.run.steps[i] = "ok"
		}
		s.run.phase, s.run.result = "done", &res
		s.mu.Lock()
		for _, l := range res.Links {
			s.links[l.URL] = true
		}
		s.mu.Unlock()
	}()
	s.json(w, "started")
}

func (s *server) reset(w http.ResponseWriter, r *http.Request) {
	s.run.mu.Lock()
	if s.run.phase != "running" {
		s.run.phase = "idle"
	}
	s.run.mu.Unlock()
	s.json(w, "ok")
}

// open mở một địa chỉ (chỉ những địa chỉ do bộ cài tự đưa ra) bằng trình duyệt mặc định.
func (s *server) open(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL string `json:"url"`
	}
	s.mu.Lock()
	ok := json.NewDecoder(r.Body).Decode(&in) == nil && s.links[in.URL]
	s.mu.Unlock()
	if !ok {
		http.Error(w, "forbidden", 403)
		return
	}
	openDefault(in.URL)
	s.json(w, "ok")
}

func (s *server) installedVer() string {
	if s.app.InstalledVersion == nil {
		return ""
	}
	return s.app.InstalledVersion()
}
