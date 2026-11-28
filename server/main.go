// MAM Server — Missing App Monitor. Chỉ dùng thư viện chuẩn của Go, không cần cài thêm gì.
package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const Version = "1.1.122"

//go:embed web
var webFS embed.FS

func defaultDir() string {
	if d := os.Getenv("MAM_DATA"); d != "" {
		return d
	}
	if runtime.GOOS == "windows" {
		if pd := os.Getenv("ProgramData"); pd != "" {
			return filepath.Join(pd, "MAM")
		}
	}
	return "data"
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", s.hStatus)
	mux.HandleFunc("/api/setup", s.hSetup)
	mux.HandleFunc("/api/login", s.hLogin)
	mux.HandleFunc("/api/logout", s.authed("", s.hLogout))
	mux.HandleFunc("/api/me", s.authed("", s.hMe))
	mux.HandleFunc("/api/me/password", s.authed("", s.hMyPassword))
	mux.HandleFunc("/api/me/2fa/", s.authed("", s.hMy2FA))
	mux.HandleFunc("/api/overview", s.authed("", s.hOverview))
	mux.HandleFunc("/api/machines", s.authed("", s.hMachines))
	mux.HandleFunc("/api/machines/", s.authed("", s.hMachine))
	mux.HandleFunc("/api/groups", s.authed("", s.hGroups))
	mux.HandleFunc("/api/alerts", s.authed("", s.hAlerts))
	mux.HandleFunc("/api/avatar/", s.authed("", s.hAvatar))
	mux.HandleFunc("/api/avatar-policy", s.authed("", s.hAvatarPolicy))
	mux.HandleFunc("/api/agent/avatar", s.hAgentAvatar)
	mux.HandleFunc("/api/schedules", s.authed("", s.hSchedules))
	mux.HandleFunc("/api/alerts/", s.authed("", s.hAlerts))
	mux.HandleFunc("/api/settings", s.authed("", s.hSettings))
	mux.HandleFunc("/api/audit", s.authed("", s.hAudit))
	mux.HandleFunc("/api/agent-update", s.authed("", s.hAgentUpdateInfo))
	mux.HandleFunc("/api/requests", s.authed("", s.hRequests))
	mux.HandleFunc("/api/chat", s.authed("", s.hChat))
	mux.HandleFunc("/api/chat/", s.authed("", s.hChat))
	mux.HandleFunc("/api/remote-password", s.authed("", s.hRemotePassword))
	mux.HandleFunc("/api/uninstall-password", s.authed("", s.hUninstallPassword))
	mux.HandleFunc("/api/contact-label", s.authed("", s.hContactLabel))
	mux.HandleFunc("/api/purge", s.authed("super", s.hPurge))
	mux.HandleFunc("/api/device-types", s.authed("", s.hDeviceTypes))
	mux.HandleFunc("/api/requests/", s.authed("", s.hRequest))
	mux.HandleFunc("/api/report", s.authed("", s.hReport2))
	mux.HandleFunc("/api/export/", s.authed("", s.hExport))
	mux.HandleFunc("/api/uptime", s.authed("", s.hUptime))
	mux.HandleFunc("/api/policies", s.authed("", s.hPolicies))
	mux.HandleFunc("/api/discover", s.authed("write", s.hDiscover))
	mux.HandleFunc("/api/devices", s.authed("", s.hDevices))
	mux.HandleFunc("/api/devices/check", s.authed("", s.hDeviceCheck))
	mux.HandleFunc("/api/devices/tai-khoan", s.authed("super", s.hDeviceTK))
	mux.HandleFunc("/api/nvr", s.authed("super", s.hNvr))
	mux.HandleFunc("/api/devices/scan", s.authed("super", s.hDeviceScan))
	mux.HandleFunc("/api/devices/log", s.authed("", s.hDeviceLog))
	mux.HandleFunc("/api/devices/ack", s.authed("", s.hDeviceAck))
	mux.HandleFunc("/api/devices/snapshot", s.authed("", s.hDeviceSnap))
	mux.HandleFunc("/api/devices/video", s.authed("", s.hDeviceVideo))
	mux.HandleFunc("/api/devices/bulk", s.authed("super", s.hDeviceBulk))
	mux.HandleFunc("/api/devices/parse", s.authed("super", s.hDeviceParse))
	mux.HandleFunc("/api/devices/import", s.authed("super", s.hDeviceImport))
	mux.HandleFunc("/api/remote-install", s.authed("super", s.hRemoteInstall))
	mux.HandleFunc("/api/inventory", s.authed("", s.hInventory))
	mux.HandleFunc("/api/security", s.authed("", s.hSecurity))
	mux.HandleFunc("/api/backups", s.authed("", s.hBackups))
	mux.HandleFunc("/api/backups/", s.authed("super", s.hBackupFile))
	mux.HandleFunc("/api/backups/mirror", s.authed("", s.hMirror))
	mux.HandleFunc("/api/restore", s.hRestoreRoute)
	mux.HandleFunc("/api/accounts", s.authed("super", s.hAccounts))
	mux.HandleFunc("/api/accounts/", s.authed("super", s.hAccount))
	mux.HandleFunc("/api/data-dir", s.authed("super", s.hDataDir))
	mux.HandleFunc("/api/agent-download", s.authed("super", s.hAgentDownload))
	mux.HandleFunc("/api/agent-downloads", s.authed("super", s.hAgentDownloads))
	mux.HandleFunc("/dl/", s.hDL)
	mux.HandleFunc("/install/", s.hInstallScript)
	mux.HandleFunc("/api/agent/enroll", s.hEnroll)
	mux.HandleFunc("/api/agent/report", s.hReport)
	mux.HandleFunc("/api/agent/result", s.hResult)
	mux.HandleFunc("/api/agent/reply", s.hReply)
	mux.HandleFunc("/api/agent/chat-act", s.hAgentChatAct)
	mux.HandleFunc("/api/agent/chat-tep", s.hAgentChatTep)
	mux.HandleFunc("/api/agent/wait", s.hAgentWait)
	mux.HandleFunc("/api/chat-wait", s.authed("", s.hChatWait))
	mux.HandleFunc("/api/agent/rename", s.hAgentRename)
	mux.HandleFunc("/api/agent/screen/consent", s.hAgentScreenConsent)
	mux.HandleFunc("/api/agent/screen/frame", s.hAgentScreenFrame)
	mux.HandleFunc("/api/agent/screen/poll", s.hAgentScreenPoll)
	mux.HandleFunc("/api/agent/screen/ended", s.hAgentScreenEnded)
	mux.HandleFunc("/api/agent/request", s.hAgentRequest)
	mux.HandleFunc("/api/agent/requests", s.hAgentRequests)
	mux.HandleFunc("/api/agent/update", s.hAgentUpdate)
	mux.HandleFunc("/api/agent/uninstall", s.hUninstall)
	mux.HandleFunc("/local/reset-admin", s.hLocalReset)
	sub, _ := fs.Sub(webFS, "web")
	static := http.FileServer(http.FS(sub))
	// Trang chủ gắn số phiên bản vào app.js/style.css/i18n.js. Không có nó thì nâng server xong,
	// trình duyệt đang mở vẫn chạy app.js cũ — đại ca tưởng bản mới không có gì thay đổi.
	index, _ := fs.ReadFile(sub, "index.html")
	index = []byte(strings.NewReplacer(
		`"/app.js"`, `"/app.js?v=`+Version+`"`,
		`"/style.css"`, `"/style.css?v=`+Version+`"`,
		`"/i18n.js"`, `"/i18n.js?v=`+Version+`"`,
		`"/missing-intro.js"`, `"/missing-intro.js?v=`+Version+`"`,
	).Replace(string(index)))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/index.html" {
			static.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(index)
	})
	return secure(mux)
}

func secure(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; media-src 'self' blob:; connect-src 'self' http://127.0.0.1:47640; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-cache")
		}
		h.ServeHTTP(w, r)
	})
}

func main() {
	dir := flag.String("data", defaultDir(), "thư mục dữ liệu")
	addr := flag.String("addr", envOr("MAM_ADDR", ":7070"), "địa chỉ lắng nghe")
	cert := flag.String("cert", "", "file chứng chỉ TLS (tùy chọn)")
	key := flag.String("key", "", "file khóa TLS (tùy chọn)")
	lang := flag.String("lang", "", "ngôn ngữ dòng lệnh: vi, en, zh (mặc định vi hoặc biến MAM_LANG)")
	flag.Parse()
	if *lang != "" {
		setLang(*lang)
	}
	args := flag.Args()
	if len(args) > 0 {
		cli(args, *dir, *addr)
		return
	}
	boot := *dir
	var rerr error
	if *dir, rerr = resolveDir(boot); rerr != nil {
		log.Fatal(TErr(rerr.Error()))
	}
	os.MkdirAll(*dir, 0o700)
	if lf, err := os.OpenFile(filepath.Join(*dir, "server.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		log.SetOutput(io.MultiWriter(os.Stdout, lf))
	}
	s, err := NewServer(*dir)
	if err != nil {
		log.Fatalf(T("Không đọc được dữ liệu ở %s: %v"), *dir, err)
	}
	s.boot = boot
	// dọn cảnh báo trùng đã lỡ sinh ra ở bản cũ (lỗi chỉ dò 400 dòng gần nhất)
	s.mu.Lock()
	if n := s.donCanhBaoTrung(); n > 0 {
		s.save()
		log.Printf(T("Đã đóng %d cảnh báo trùng"), n)
	}
	// dọn mốc "treo không ra hình" giả mà bản trước 1.1.99 đẻ ra sau mỗi lượt đo luồng hình
	if n := s.donMocTreoGia(); n > 0 {
		s.save()
		log.Printf(T("Đã dọn %d mốc treo giả trong lịch sử thiết bị"), n)
	}
	// đính chính cảnh báo "không ra hình" do lỗi kết luận của bản cũ — một lần duy nhất
	nHinh := s.donCanhBaoHinhGia()
	// tin nhắn cũ chưa có ID thì không thả cảm xúc hay thu hồi được — vá luôn lúc khởi động
	nTin := s.vaIDChat()
	// đầu ghi khai hồi bản chỉ cho một cái: chuyển vào danh sách nhiều đầu ghi
	nGhi := s.chuyenDauGhiCu()
	// file chat không còn tin nhắn nào trỏ tới (tin đã bị cắt bớt, hoặc gửi dở lúc server tắt)
	nRac := s.donTepMoCoi()
	if nHinh > 0 || nTin > 0 || nGhi {
		s.save()
	}
	s.mu.Unlock()
	if nRac > 0 {
		log.Printf(T("Đã dọn %d file chat không còn tin nhắn nào dùng"), nRac)
	}
	if nTin > 0 {
		log.Printf(T("Đã đánh số %d tin nhắn cũ để thả cảm xúc và thu hồi được"), nTin)
	}
	if nGhi {
		log.Print(T("Đã chuyển đầu ghi camera khai từ bản cũ sang danh sách nhiều đầu ghi"))
	}
	if nHinh > 0 {
		log.Printf(T("Đã đính chính %d cảnh báo \"không ra hình\" của bản cũ"), nHinh)
		s.audit(AuditEntry{User: "system", Name: T("Hệ thống"), Role: "system", Kind: T("Cảnh báo"),
			Msg: T("Đóng %d cảnh báo \"không ra hình\" do bản ≤ 1.1.102 kết luận sai khi chưa đo được luồng hình", nHinh)})
	}
	go s.watchdog()
	go s.schedLoop()
	go s.devLoop()
	srv := &http.Server{Addr: *addr, Handler: s.routes(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second}
	log.Printf(T("MAM Server %s · dữ liệu: %s · lắng nghe %s"), Version, *dir, *addr)
	if *cert != "" && *key != "" {
		log.Fatal(srv.ListenAndServeTLS(*cert, *key))
	}
	log.Fatal(srv.ListenAndServe())
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func cli(args []string, dir, addr string) {
	switch args[0] {
	case "version":
		fmt.Println("MAM Server", Version)
	case "reset-admin":
		if len(args) < 2 {
			fmt.Println(T("Cách dùng: mam-server reset-admin <tài khoản>"))
			os.Exit(2)
		}
		host := addr
		if strings.HasPrefix(host, ":") {
			host = "127.0.0.1" + host
		}
		body, _ := json.Marshal(map[string]string{"user": args[1]})
		resp, err := (&http.Client{Timeout: 5 * time.Second}).Post("http://"+host+"/local/reset-admin", "application/json", bytes.NewReader(body))
		var out struct {
			TempPassword string `json:"tempPassword"`
			Error        string `json:"error"`
		}
		if err == nil {
			json.NewDecoder(resp.Body).Decode(&out)
			resp.Body.Close()
		} else { // server không chạy: sửa thẳng dữ liệu
			dir, _ = resolveDir(dir)
			s, e := NewServer(dir)
			if e != nil {
				fmt.Println(T("Lỗi:"), TErr(e.Error()))
				os.Exit(1)
			}
			tp, e := s.resetLocal(args[1])
			if e != nil {
				out.Error = e.Error()
			}
			out.TempPassword = tp
		}
		if out.Error != "" {
			fmt.Println(T("Lỗi:"), TErr(out.Error))
			os.Exit(1)
		}
		fmt.Print(T("Đã đặt lại. Mật khẩu tạm của %s: %s\nĐăng nhập rồi hệ thống sẽ bắt đổi mật khẩu ngay.\n", args[1], out.TempPassword))
	default:
		fmt.Println(T("Lệnh: version | reset-admin <tài khoản>"))
		os.Exit(2)
	}
}
