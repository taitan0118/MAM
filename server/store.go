package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ---------- dữ liệu lưu xuống đĩa ----------

type Account struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	User        string    `json:"user"`
	Role        string    `json:"role"` // super | tech | view
	Scope       []string  `json:"scope"`
	Salt        string    `json:"salt"`
	Hash        string    `json:"hash"`
	Iter        int       `json:"iter"`
	MustChange  bool      `json:"mustChange"`
	Created     time.Time `json:"created"`
	LastLogin   time.Time `json:"lastLogin"`
	TOTP        string    `json:"totp,omitempty"`     // khóa xác thực hai bước (base32); rỗng = chưa bật
	TOTPLast    int64     `json:"totpLast,omitempty"` // bước 30 giây đã dùng gần nhất
	TOTPPending string    `json:"-"`                  // khóa đang chờ xác nhận
}

type Machine struct {
	ID          string                 `json:"id"`
	TokenHash   string                 `json:"tokenHash"`
	Name        string                 `json:"name"`
	OS          string                 `json:"os"`
	Group       string                 `json:"group"`
	IP          string                 `json:"ip"`
	MAC         string                 `json:"mac"`
	MACs        []string               `json:"macs,omitempty"` // MAC thật của mọi card mạng (cho Wake-on-LAN)
	Hw          map[string]interface{} `json:"hw"`
	Registered  time.Time              `json:"registered"`
	LastSeen    time.Time              `json:"lastSeen"`
	Revoked     bool                   `json:"revoked"`
	ThreatSeen  []string               `json:"threatSeen,omitempty"` // ID các phát hiện virus đã báo, để không báo lại
	RemoteNote  string                 `json:"remoteNote"`           // ID TeamViewer/AnyDesk hoặc địa chỉ RDP, admin tự ghi để tiện mở phần mềm remote desktop ngoài
	LicenseNote string                 `json:"licenseNote"`          // bản quyền Windows/Office/phần mềm máy này, admin tự ghi
}

type Settings struct {
	TempC           float64 `json:"temp"`
	CPU             float64 `json:"cpu"`
	DiskFreePc      float64 `json:"disk"`
	OfflineMin      float64 `json:"offline"`
	EnrollKey       string  `json:"enrollKey"`       // để trống = máy nào cài agent cũng tự đăng ký
	NoAutoUpdate    bool    `json:"noAutoUpdate"`    // true: không tự cập nhật agent
	NoMachineAvatar bool    `json:"noMachineAvatar"` // true: máy không được tự đổi ảnh đại diện
	// Thông báo ra ngoài khi có cảnh báo mới mở
	Telegram string `json:"telegram"` // token bot Telegram
	TgChat   string `json:"tgChat"`   // chat id nhận tin
	Webhook  string `json:"webhook"`  // URL nhận POST JSON {"text":...} — dùng cho Zalo OA (qua trung gian), Slack, n8n...
	SmtpHost string `json:"smtpHost"`
	SmtpPort string `json:"smtpPort"`
	SmtpUser string `json:"smtpUser"`
	SmtpPass string `json:"smtpPass"`
	SmtpTo   string `json:"smtpTo"`
	// Công tắc tắt từng kênh (mặc định false = bật, giữ nguyên hành vi cũ khi có cấu hình)
	TgOff   bool `json:"tgOff"`
	WhOff   bool `json:"whOff"`
	SmtpOff bool `json:"smtpOff"`
}

type Reply struct {
	By     string    `json:"by"`
	Text   string    `json:"text"`
	At     time.Time `json:"at"`
	Client bool      `json:"client"` // true: người ngồi máy viết
}

// Request: yêu cầu hỗ trợ do người ngồi máy gửi từ cửa sổ MAM.
type Request struct {
	ID         string    `json:"id"`
	Machine    string    `json:"machine"`
	Name       string    `json:"name"`     // tên máy lúc gửi
	User       string    `json:"user"`     // người dùng Windows
	Reporter   string    `json:"reporter"` // tên người báo (tự nhập)
	Phone      string    `json:"phone"`    // SĐT/Zalo người báo
	EmpID      string    `json:"empId"`    // mã nhân viên
	BadPC      string    `json:"badPc"`    // mã máy hư (tự nhập, có thể báo giùm máy khác)
	Text       string    `json:"text"`
	Urgent     bool      `json:"urgent"`
	DeviceType string    `json:"deviceType"` // loại thiết bị hỏng (máy tính, máy in...)
	Status     string    `json:"status"`     // new | working | done
	Solution   string    `json:"solution"`   // cách xử lý, bắt buộc khi chuyển sang done
	SolvedBy   string    `json:"solvedBy"`
	Created    time.Time `json:"created"`
	Updated    time.Time `json:"updated"`
	Replies    []Reply   `json:"replies"`
}

// Policy: chính sách áp cho một nhóm máy (Group "" = máy chưa phân nhóm).
type Policy struct {
	Group   string   `json:"group"`
	USB     bool     `json:"usb"`
	SitesOn bool     `json:"sitesOn"`
	Sites   []string `json:"sites"`
	AppsOn  bool     `json:"appsOn"`
	Apps    []string `json:"apps"`
	HoursOn bool     `json:"hoursOn"`
	From    string   `json:"from"` // HH:MM
	To      string   `json:"to"`
}

type Alert struct {
	ID      string    `json:"id"`
	Machine string    `json:"machine"` // id máy
	Name    string    `json:"name"`
	Kind    string    `json:"kind"` // temp | cpu | disk | offline
	Sev     string    `json:"sev"`  // crit | warn
	Msg     string    `json:"msg"`
	Opened  time.Time `json:"opened"`
	Closed  time.Time `json:"closed"`
	Status  string    `json:"status"` // open | closed
	By      string    `json:"by"`     // người đóng, hoặc "tự động"
}

type Command struct {
	ID      string            `json:"id"`
	Machine string            `json:"machine"`
	Type    string            `json:"type"`
	Args    map[string]string `json:"args"`
	By      string            `json:"by"`
	Created time.Time         `json:"created"`
	Status  string            `json:"status"` // pending | sent | done | failed
	Result  string            `json:"result"`
	Done    time.Time         `json:"done"`
}

type State struct {
	Accounts  []*Account  `json:"accounts"`
	Groups    []string    `json:"groups"`
	Machines  []*Machine  `json:"machines"`
	Settings  Settings    `json:"settings"`
	Alerts    []*Alert    `json:"alerts"`
	Commands  []*Command  `json:"commands"`
	Schedules []*Schedule `json:"schedules"`
	Requests  []*Request  `json:"requests"`
	Policies  []*Policy   `json:"policies"`
	// Loại thiết bị cho yêu cầu hỗ trợ (Super admin tạo/xóa). Rỗng = dùng danh sách mặc định.
	DeviceTypes []string `json:"deviceTypes"`
	// Tên ô liên hệ trong form yêu cầu hỗ trợ (vd "WeChat ID"). Rỗng = "SĐT/Zalo".
	ContactLabel string `json:"contactLabel"`
	// Mật khẩu điều khiển từ xa (băm). Có đặt thì admin nhập đúng mật khẩu là vào thẳng, không cần máy đồng ý.
	RemoteSalt string `json:"remoteSalt"`
	RemoteHash string `json:"remoteHash"`
	// Mật khẩu gỡ agent (băm): người ngồi máy nhập để gỡ, không cần tài khoản quản trị. Rỗng = không ai gỡ được trên máy.
	UninstSalt string      `json:"uninstSalt"`
	UninstHash string      `json:"uninstHash"`
	UninstAt   time.Time   `json:"uninstAt"`
	UninstBy   string      `json:"uninstBy"`
	Chat       []*ChatLine `json:"chat"`
	// Các lần bật máy [bắt đầu, kết thúc] (giây unix) theo ID máy, xem uptime.go.
	Uptime map[string][][2]int64 `json:"uptime,omitempty"`
}

var defaultDeviceTypes = []string{"Máy tính", "Máy in", "Máy photo", "Máy scan", "Server", "Khác"}

// deviceTypes: gọi khi đang giữ s.mu.
func (s *Server) deviceTypes() []string {
	if len(s.st.DeviceTypes) == 0 {
		return defaultDeviceTypes
	}
	return s.st.DeviceTypes
}

// contactLabel: gọi khi đang giữ s.mu.
func (s *Server) contactLabel() string {
	if s.st.ContactLabel == "" {
		return "SĐT/Zalo"
	}
	return s.st.ContactLabel
}

type Sample struct {
	T    int64   `json:"t"`
	CPU  float32 `json:"cpu"`
	RAM  float32 `json:"ram"`
	Temp float32 `json:"temp"`
}

type Disk struct {
	Name    string  `json:"name"`
	TotalGB float64 `json:"totalGB"`
	FreeGB  float64 `json:"freeGB"`
}

type Live struct {
	CPU, RAM float64
	Temp     *float64
	Uptime   int64
	Disks    []Disk
	User     string
	Ver      string
	At       time.Time
	CPUHigh  int // số lần báo liên tiếp CPU cao
	Hist     []Sample
	lastHist time.Time
}

type Session struct {
	AccID string
	Exp   time.Time
}

type Server struct {
	mu      sync.RWMutex
	dir     string
	boot    string // thư mục -data lúc khởi động; redirect.txt nằm ở đây
	st      State
	live    map[string]*Live
	sess    map[string]*Session
	fails   map[string]*failRec
	tok     map[string]*Machine // băm token → máy
	dirty   bool
	auditMu sync.Mutex
	blobs   map[string]*Blob          // dữ liệu lớn của lệnh (tiến trình, S.M.A.R.T) giữ trong RAM ít phút
	screens map[string]*ScreenSession // phiên xem/điều khiển màn hình đang diễn ra (không lưu xuống đĩa)
}

type failRec struct {
	n     int
	until time.Time
}

func defaultSettings() Settings {
	return Settings{TempC: 85, CPU: 90, DiskFreePc: 10, OfflineMin: 5}
}

func NewServer(dir string) (*Server, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Server{dir: dir, boot: dir, live: map[string]*Live{}, sess: map[string]*Session{}, fails: map[string]*failRec{}, tok: map[string]*Machine{}, blobs: map[string]*Blob{}, screens: map[string]*ScreenSession{}}
	s.st.Settings = defaultSettings()
	avatarDir = filepath.Join(dir, "avatars")
	b, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err == nil {
		if err := json.Unmarshal(b, &s.st); err != nil {
			return nil, err
		}
		if s.st.Settings.TempC == 0 {
			s.st.Settings = defaultSettings()
		}
	}
	for _, m := range s.st.Machines {
		s.tok[m.TokenHash] = m
	}
	return s, nil
}

// save ghi nguyên tử: file tạm rồi đổi tên. Gọi khi đang giữ s.mu.
func (s *Server) save() {
	b, err := json.MarshalIndent(&s.st, "", " ")
	if err != nil {
		return
	}
	p := filepath.Join(s.dir, "state.json")
	tmp := p + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		os.Rename(tmp, p)
	}
	s.dirty = false
}

func (s *Server) backup() {
	s.mu.RLock()
	b, _ := json.Marshal(&s.st)
	d := filepath.Join(s.dir, "backup")
	s.mu.RUnlock()
	os.MkdirAll(d, 0o700)
	os.WriteFile(filepath.Join(d, "state-"+time.Now().Format("20060102-1504")+".json"), b, 0o600)
	files, _ := filepath.Glob(filepath.Join(d, "state-*.json"))
	for len(files) > 30 {
		os.Remove(files[0])
		files = files[1:]
	}
}

func rid(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func sha(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// ---------- nhật ký thao tác: chỉ ghi thêm ----------

type AuditEntry struct {
	T      time.Time `json:"t"`
	User   string    `json:"user"`
	Name   string    `json:"name"`
	Role   string    `json:"role"`
	IP     string    `json:"ip"`
	Kind   string    `json:"kind"`
	Msg    string    `json:"msg"`
	Target string    `json:"target"`
	Sens   bool      `json:"sens"`
}

func (s *Server) audit(e AuditEntry) {
	e.T = time.Now()
	s.auditMu.Lock()
	defer s.auditMu.Unlock()
	f, err := os.OpenFile(filepath.Join(s.dir, "audit.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	b, _ := json.Marshal(e)
	f.Write(append(b, '\n'))
}

func (s *Server) readAudit(max int) []AuditEntry {
	s.auditMu.Lock()
	defer s.auditMu.Unlock()
	b, err := os.ReadFile(filepath.Join(s.dir, "audit.jsonl"))
	if err != nil {
		return nil
	}
	var out []AuditEntry
	start := 0
	for i := 0; i < len(b); i++ {
		if b[i] == '\n' {
			var e AuditEntry
			if json.Unmarshal(b[start:i], &e) == nil {
				out = append(out, e)
			}
			start = i + 1
		}
	}
	if max > 0 && len(out) > max {
		out = out[len(out)-max:]
	}
	return out
}
