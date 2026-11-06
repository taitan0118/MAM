package main

// Phát hiện virus/mã độc: nguồn là nhật ký Windows Defender (virus thật) và vài dấu hiệu nghi ngờ
// (tắt bảo vệ, loại trừ nguy hiểm, tiến trình lạ chạy từ thư mục tạm). Phần đọc dữ liệu nằm trong
// threat_windows.go; file này chỉ gồm kiểu dữ liệu, đoán nguyên nhân và vòng quét nên chạy được ở mọi HĐH.
import (
	"encoding/json"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Threat là một phát hiện gửi lên server. Kind: "virus" (Defender báo) hoặc "suspect" (nghi ngờ).
type Threat struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Detail string `json:"detail"`
	Cause  string `json:"cause"`
	Status string `json:"status"`
	At     int64  `json:"at"`
}

// rawThreat là dữ liệu thô PowerShell đọc được, dùng để đoán nguyên nhân.
type rawThreat struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Process string `json:"process"`
	Action  string `json:"action"`
	At      int64  `json:"at"`
	Drive   string `json:"drive"` // Removable | Network | Fixed
	ZoneURL string `json:"zoneUrl"`
	ZoneRef string `json:"zoneRef"`
	Note    string `json:"note"`
}

var (
	reHost = regexp.MustCompile(`(?i)^[a-z][a-z0-9+.-]*://([^/:?#]+)`)
	reBrow = regexp.MustCompile(`(?i)(chrome|msedge|firefox|brave|opera|iexplore)\.exe$`)
	reScr  = regexp.MustCompile(`(?i)(powershell|pwsh|cmd|wscript|cscript|mshta)\.exe$`)
	reZip  = regexp.MustCompile(`(?i)(winrar|7z|7zfm|winzip|peazip)`)
)

func hostOf(u string) string {
	if m := reHost.FindStringSubmatch(strings.TrimSpace(u)); m != nil {
		return m[1]
	}
	return ""
}

// inferCause đoán nguyên nhân từ nơi file nằm, tiến trình tạo ra nó và dấu vết tải về. Chỉ là suy đoán.
func inferCause(t rawThreat) string {
	p := strings.ToLower(t.Path)
	switch {
	case t.ZoneURL != "" || t.ZoneRef != "":
		h := hostOf(t.ZoneURL)
		if h == "" {
			h = hostOf(t.ZoneRef)
		}
		if h == "" {
			h = "không rõ trang"
		}
		return "Tải từ web, trang nguồn: " + h
	case strings.Contains(p, `content.outlook`) || strings.Contains(p, `\outlook\`) || strings.Contains(p, `\inetcache\`) && strings.Contains(p, `outlook`):
		return "Có thể là file đính kèm thư email (mở trực tiếp từ Outlook)"
	case t.Drive == "Removable":
		return "Từ USB hoặc ổ cắm ngoài (ổ " + drive(t.Path) + ")"
	case t.Drive == "Network" || strings.HasPrefix(t.Path, `\\`):
		return "Từ thư mục chia sẻ trong mạng" + shareHost(t.Path)
	case reBrow.MatchString(t.Process) && (strings.Contains(p, `\temp\`) || strings.Contains(p, `\inetcache\`)):
		return "Trình duyệt vừa tải hoặc mở file này (có thể là trang web độc hại)"
	case reScr.MatchString(t.Process):
		return "Do một đoạn lệnh/script chạy (" + base(t.Process) + ")"
	case reZip.MatchString(t.Process):
		return "Giải nén từ một file nén"
	case strings.Contains(p, `\downloads\`):
		return "Nằm trong thư mục Downloads: nhiều khả năng tải từ web hoặc nhận qua tin nhắn"
	}
	return "Chưa xác định được nguồn"
}

func drive(p string) string {
	if len(p) >= 2 && p[1] == ':' {
		return strings.ToUpper(p[:2])
	}
	return "?"
}
func base(p string) string {
	if i := strings.LastIndexAny(p, `\/`); i >= 0 {
		return p[i+1:]
	}
	return p
}
func shareHost(p string) string {
	if strings.HasPrefix(p, `\\`) {
		if parts := strings.Split(p[2:], `\`); len(parts) > 0 && parts[0] != "" {
			return " (máy " + parts[0] + ")"
		}
	}
	return ""
}

// toThreats chuyển JSON từ PowerShell thành danh sách gửi lên, kèm nguyên nhân.
func toThreats(b []byte) []Threat {
	var raws []rawThreat
	if json.Unmarshal(b, &raws) != nil {
		var one rawThreat
		if json.Unmarshal(b, &one) != nil || one.ID == "" {
			return nil
		}
		raws = []rawThreat{one}
	}
	out := []Threat{}
	for _, r := range raws {
		if r.ID == "" || r.Name == "" {
			continue
		}
		t := Threat{ID: r.ID, Kind: r.Kind, Name: r.Name, Status: r.Action, At: r.At}
		if t.Kind != "suspect" {
			t.Kind = "virus"
		}
		if t.Kind == "virus" {
			t.Detail = r.Path
			t.Cause = inferCause(r)
		} else {
			t.Detail = r.Note
			t.Cause = r.Process
		}
		out = append(out, t)
	}
	return out
}

var threats struct {
	sync.Mutex
	list []Threat
}

func currentThreats() []Threat {
	threats.Lock()
	defer threats.Unlock()
	cut := time.Now().Add(-24 * time.Hour).Unix()
	out := []Threat{}
	for _, t := range threats.list {
		if t.At >= cut || t.At == 0 {
			out = append(out, t)
		}
	}
	if len(out) > 30 {
		out = out[len(out)-30:]
	}
	return out
}

// threatLoop quét mỗi phút; lần đầu chờ 20 giây cho máy ổn định sau khi khởi động.
func threatLoop() {
	time.Sleep(20 * time.Second)
	for {
		if l := scanThreats(); l != nil {
			threats.Lock()
			threats.list = l
			threats.Unlock()
		}
		time.Sleep(time.Minute)
	}
}
