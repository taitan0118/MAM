// MAM Agent — chạy ngầm trên máy client, báo tình trạng về server và nhận lệnh.
// Chỉ dùng thư viện chuẩn của Go. Bản biên dịch này chạy Windows 10/11 và Server 2016 trở lên;
// Windows 7/8 dùng MAM-Agent.ps1.
package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const Version = "1.1.51"

//go:embed lib.ps1
var libPS string

type Config struct {
	Server        string   `json:"server"`
	Key           string   `json:"key"`
	ID            string   `json:"id"`
	Token         string   `json:"token"`
	Name          string   `json:"name"`
	Lang          string   `json:"lang"`
	TrustedAdmins []string `json:"trustedAdmins,omitempty"` // tài khoản admin được người ngồi máy chọn "luôn đồng ý", khỏi hỏi lại
}

var (
	cfgPath = flag.String("config", "", "đường dẫn config.json (mặc định: cạnh file chạy)")
	simName = flag.String("sim", "", "chạy giả lập số liệu với tên máy này (dùng để thử, không đọc máy thật)")
	cfg     Config
)

func exeDir() string {
	e, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(e)
}

func loadConfig() error {
	p := *cfgPath
	if p == "" {
		p = filepath.Join(exeDir(), "config.json")
		*cfgPath = p
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, &cfg)
}

func saveConfig() {
	b, _ := json.MarshalIndent(cfg, "", " ")
	os.WriteFile(*cfgPath, b, 0o600)
}

var lastUser string

// lastInfo: số liệu và phần cứng gần nhất, để cửa sổ MAM hiện "Thông tin máy".
var lastInfo struct {
	sync.Mutex
	rep map[string]interface{}
	hw  map[string]interface{}
}

var httpc = &http.Client{Timeout: 20 * time.Second}

// wakeNow: server báo có lệnh/tin nhắn mới, vòng báo cáo chạy ngay thay vì ngủ hết lượt.
var wakeNow = make(chan struct{}, 1)

// waitLoop giữ sẵn một yêu cầu /api/agent/wait; server trả lời ngay khi có việc cho máy này.
func waitLoop() {
	c := &http.Client{Timeout: 40 * time.Second}
	for {
		if cfg.Token == "" {
			time.Sleep(5 * time.Second)
			continue
		}
		req, _ := http.NewRequest("GET", strings.TrimRight(cfg.Server, "/")+"/api/agent/wait", nil)
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
		resp, err := c.Do(req)
		if err != nil {
			time.Sleep(5 * time.Second)
			continue
		}
		var out struct{ Wake bool }
		json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&out)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			time.Sleep(60 * time.Second) // server cũ chưa có /api/agent/wait: vẫn chạy bằng lượt 5 giây
			continue
		}
		if out.Wake {
			select {
			case wakeNow <- struct{}{}:
			default:
			}
		}
	}
}

type apiResp struct {
	code int
	body []byte
}

func post(path string, in interface{}, token string) (apiResp, error) {
	b, _ := json.Marshal(in)
	req, _ := http.NewRequest("POST", strings.TrimRight(cfg.Server, "/")+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := httpc.Do(req)
	if err != nil {
		return apiResp{}, err
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	return apiResp{resp.StatusCode, buf.Bytes()}, nil
}

// rawReq gọi server với token máy và nội dung thô (ảnh); trả mã, loại nội dung, thân phản hồi. Lỗi mạng trả 502.
func rawReq(method, path string, body []byte) (int, string, []byte) {
	req, err := http.NewRequest(method, strings.TrimRight(cfg.Server, "/")+path, bytes.NewReader(body))
	if err != nil {
		return 502, "", nil
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	resp, err := httpc.Do(req)
	if err != nil {
		return 502, "", nil
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return resp.StatusCode, resp.Header.Get("Content-Type"), b
}

// localAddr: IP và MAC của card mạng đang dùng để nói chuyện với server.
func localAddr() (ip, mac string) {
	u, err := url.Parse(cfg.Server)
	if err != nil {
		return
	}
	host := u.Host
	if !strings.Contains(host, ":") {
		host += ":80"
	}
	c, err := net.DialTimeout("udp", host, 2*time.Second)
	if err != nil {
		return
	}
	ip = c.LocalAddr().(*net.UDPAddr).IP.String()
	c.Close()
	ifs, _ := net.Interfaces()
	for _, ifc := range ifs {
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.String() == ip {
				mac = strings.ToUpper(ifc.HardwareAddr.String())
			}
		}
	}
	return
}

// wolMACs: MAC thật của các card mạng (bỏ loopback và MAC "tự đặt" của card ảo/VPN/MAC ngẫu nhiên),
// để Wake-on-LAN gửi đúng card dây. Card đang nói chuyện với server có thể là card ảo nên không đủ.
func wolMACs() []string {
	var out []string
	ifs, _ := net.Interfaces()
	for _, ifc := range ifs {
		hw := ifc.HardwareAddr
		if ifc.Flags&net.FlagLoopback != 0 || len(hw) != 6 || hw[0]&0x02 != 0 || len(out) >= 8 {
			continue
		}
		out = append(out, strings.ToUpper(hw.String()))
	}
	return out
}

// hostName: tên máy trong Windows/macOS/Linux (chế độ giả lập dùng tên giả).
func hostName() string {
	if *simName != "" {
		return *simName
	}
	h, _ := os.Hostname()
	return h
}

func enroll(hw map[string]interface{}) error {
	name := cfg.Name
	if name == "" || *simName != "" {
		name = hostName()
	}
	ip, mac := localAddr()
	if *simName != "" { // giả lập: mỗi tên một MAC riêng để server coi là máy khác nhau
		var h uint32 = 2166136261
		for _, c := range *simName {
			h = (h ^ uint32(c)) * 16777619
		}
		mac = fmt.Sprintf("02:00:%02X:%02X:%02X:%02X", byte(h>>24), byte(h>>16), byte(h>>8), byte(h))
	}
	r, err := post("/api/agent/enroll", map[string]interface{}{"name": name, "os": osName(), "mac": mac, "ip": ip, "key": cfg.Key, "hw": hw}, "")
	if err != nil {
		return err
	}
	if r.code != 200 {
		return fmt.Errorf("server từ chối đăng ký (%d): %s", r.code, string(r.body))
	}
	var out struct{ ID, Token string }
	json.Unmarshal(r.body, &out)
	cfg.ID, cfg.Token = out.ID, out.Token
	saveConfig()
	log.Print(T("Đã đăng ký với server, mã máy %s", cfg.ID))
	return nil
}

func main() {
	flag.Parse()
	args := flag.Args()
	// file tải từ Console đã gắn sẵn địa chỉ server: bấm đúp (hoặc chạy với "/S") là tự cài
	if len(args) == 0 || args[0] == "auto" || args[0] == "/S" {
		if b := readBundle(); b != nil {
			autoInstall(b, len(args) > 0 && args[0] == "/S")
			return
		}
	}
	if len(args) > 0 {
		switch args[0] {
		case "install":
			doInstall(args[1:])
			return
		case "ui":
			runUI(args[1:])
			return
		case "dnsrestore": // script gỡ trên macOS gọi trước khi xóa agent
			dnsRestore()
			return
		case "uninstall":
			doUninstall()
			return
		case "version":
			fmt.Println("MAM Agent", Version, verMarker) // verMarker phải được dùng ở đây, không thì trình liên kết bỏ mất chuỗi và server không đọc được phiên bản
			return
		case "run":
		default:
			fmt.Println(T("Lệnh: install -server http://IP:7070 | uninstall | run | version"))
			return
		}
	}
	setupLog()
	if err := loadConfig(); err != nil {
		log.Fatalf(T("Không đọc được config.json: %v"), err)
	}
	setLang(cfg.Lang)
	log.Printf(T("MAM Agent %s khởi động, server %s"), Version, cfg.Server)
	time.Sleep(time.Duration(rand.Intn(1500)) * time.Millisecond)
	runLoop()
}

func runLoop() {
	// Phần cứng và nhiệt độ lấy bằng PowerShell nên chậm (vài giây đến vài chục giây): chạy nền,
	// vòng báo cáo RAM/ổ đĩa/CPU không bao giờ phải chờ.
	var mu sync.Mutex
	var hw map[string]interface{}
	hwNew := false
	var temp *float64
	go func() {
		for {
			wait := time.Hour
			if h := collectHw(); h != nil {
				mu.Lock()
				hw, hwNew = h, true
				mu.Unlock()
			} else {
				wait = time.Minute
			}
			time.Sleep(wait)
		}
	}()
	go func() {
		for {
			t := collectTemp()
			mu.Lock()
			temp = t
			mu.Unlock()
			time.Sleep(30 * time.Second)
		}
	}()
	go threatLoop()
	go eventLoop()
	go waitLoop()
	go enableWOL()
	go uiWatchdog()
	startLocal()
	go requestWatcher()
	collectMetrics() // lần đầu chỉ để lấy mốc tính % CPU
	time.Sleep(700 * time.Millisecond)
	interval := 5 * time.Second
	failStreak := 0
	for {
		if cfg.Token == "" {
			if err := enroll(nil); err != nil {
				log.Printf(T("Chưa đăng ký được: %v"), err)
				time.Sleep(15 * time.Second)
				continue
			}
		}
		mu.Lock()
		tNow, sendHw, hwNow := temp, hwNew, hw
		mu.Unlock()
		m := collectMetrics()
		ip, _ := localAddr()
		lastUser = m.User
		rep := map[string]interface{}{"cpu": m.CPU, "ram": m.RAM, "temp": tNow, "uptime": m.Uptime, "disks": m.Disks, "user": m.User, "ip": ip, "ver": Version, "arch": runtime.GOARCH, "macs": wolMACs(), "host": hostName()}
		if sendHw {
			rep["hw"] = hwNow
		}
		if th := currentThreats(); len(th) > 0 {
			rep["threats"] = th
		}
		evs := takeEvents()
		if len(evs) > 0 {
			rep["events"] = evs
		}
		lastInfo.Lock()
		lastInfo.rep = rep
		if hwNow != nil {
			lastInfo.hw = hwNow
		}
		lastInfo.Unlock()
		r, err := post("/api/agent/report", rep, cfg.Token)
		switch {
		case err != nil:
			setOnline(false)
			failStreak++
			if failStreak%12 == 1 {
				log.Printf(T("Không liên lạc được server: %v"), err)
			}
		case r.code == 401: // server không biết token này (cài lại server): đăng ký lại
			log.Print(T("Token không còn hợp lệ, đăng ký lại"))
			cfg.Token = ""
			continue
		case r.code == 403: // máy đã bị admin thu hồi: im lặng, thử lại mỗi 5 phút
			log.Print(T("Máy đã bị thu hồi khỏi hệ thống"))
			time.Sleep(5 * time.Minute)
			continue
		case r.code == 200:
			setOnline(true)
			failStreak = 0
			ackEvents(len(evs))
			if sendHw {
				mu.Lock()
				hwNew = false
				mu.Unlock()
			}
			var out struct {
				Commands []struct {
					ID   string
					Type string
					Args map[string]string
				}
				Interval     int
				Name         string
				DeviceTypes  []string
				ContactLabel string
				Policy       *Policy
				Screen       *struct {
					ID, By, ByUser string
					Auto           bool
				}
				Update *struct {
					Sha  string
					Size int64
				}
			}
			json.Unmarshal(r.body, &out)
			if out.Interval >= 2 && out.Interval <= 60 {
				interval = time.Duration(out.Interval) * time.Second
			}
			if len(out.DeviceTypes) > 0 {
				setDeviceTypes(out.DeviceTypes)
			}
			setContactLabel(out.ContactLabel)
			if out.Name != "" && out.Name != cfg.Name { // quản trị đổi tên trên Console: cửa sổ MAM trên máy hiện tên mới
				cfg.Name = out.Name
				saveConfig()
			}
			if out.Policy != nil {
				osApplyPolicy(out.Policy)
			}
			if out.Screen != nil {
				announceScreen(out.Screen.ID, out.Screen.By, out.Screen.ByUser, out.Screen.Auto)
			} else {
				expirePendingScreen()
			}
			if out.Update != nil {
				go func(sha string, size int64) {
					if msg := checkUpdate(sha, size); msg != "" {
						log.Print(msg)
					}
				}(out.Update.Sha, out.Update.Size)
			}
			for _, c := range out.Commands {
				c := c
				go func() { postResult(c.ID, handleCommand(c.Type, c.Args)) }()
			}
		}
		select {
		case <-time.After(interval):
		case <-wakeNow:
		}
	}
}

type Disk struct {
	Name    string  `json:"name"`
	TotalGB float64 `json:"totalGB"`
	FreeGB  float64 `json:"freeGB"`
}

type Metrics struct {
	CPU, RAM float64
	Uptime   int64
	Disks    []Disk
	User     string
}

// verMarker: server tìm chuỗi này trong file .exe để biết phiên bản agent mà không phải chạy nó.
var verMarker = "MAMVER=" + Version
