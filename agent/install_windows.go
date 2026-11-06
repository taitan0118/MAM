//go:build windows

package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

func installDir() string { return filepath.Join(os.Getenv("ProgramFiles"), "MAM Agent") }

const taskXML = `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo><Description>MAM Agent - giám sát máy trong mạng LAN</Description></RegistrationInfo>
  <Triggers>
    <BootTrigger><Enabled>true</Enabled></BootTrigger>
    <TimeTrigger><Repetition><Interval>PT1M</Interval><StopAtDurationEnd>false</StopAtDurationEnd></Repetition><StartBoundary>2020-01-01T00:00:00</StartBoundary><Enabled>true</Enabled></TimeTrigger>
  </Triggers>
  <Principals><Principal id="Author"><UserId>S-1-5-18</UserId><RunLevel>HighestAvailable</RunLevel></Principal></Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowHardTerminate>false</AllowHardTerminate>
    <StartWhenAvailable>true</StartWhenAvailable>
    <AllowStartOnDemand>true</AllowStartOnDemand>
    <Enabled>true</Enabled>
    <Hidden>true</Hidden>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <RestartOnFailure><Interval>PT1M</Interval><Count>999</Count></RestartOnFailure>
  </Settings>
  <Actions Context="Author"><Exec><Command>"%s"</Command><Arguments>run</Arguments></Exec></Actions>
</Task>`

var autoMode bool

// một bộ đệm đọc dùng chung cho cả tài khoản lẫn mật khẩu (nếu mỗi chỗ tạo một bộ, chỗ đầu nuốt mất dòng sau khi đọc từ ống dẫn)
var stdin = bufio.NewReader(os.Stdin)

// die thoát với mã lỗi; ở chế độ bấm đúp thì chờ để người dùng đọc được lỗi trước khi cửa sổ đóng.
func die(code int) {
	if autoMode && code != 0 {
		fmt.Print("\n" + T("Bấm Enter để đóng..."))
		stdin.ReadString('\n')
	}
	os.Exit(code)
}

func autoInstall(b *Bundle, silent bool) {
	if hidden("net", "session").Run() != nil { // chưa có quyền Administrator: xin quyền (UAC) rồi chạy lại
		self, _ := os.Executable()
		verb, _ := syscall.UTF16PtrFromString("runas")
		f, _ := syscall.UTF16PtrFromString(self)
		var a *uint16
		if silent {
			a, _ = syscall.UTF16PtrFromString("/S")
		}
		syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteW").Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(f)), uintptr(unsafe.Pointer(a)), 0, 1)
		return
	}
	autoMode = !silent
	setLang(b.Lang)
	fmt.Println("MAM Agent -> " + b.Server)
	doInstall([]string{"-server", b.Server, "-key", b.Key, "-lang", b.Lang})
	if !silent {
		time.Sleep(6 * time.Second)
	}
}

func doInstall(args []string) {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	server := fs.String("server", "", T("địa chỉ server, ví dụ http://192.168.1.10:7070"))
	key := fs.String("key", "", T("mã đăng ký (nếu server có đặt)"))
	name := fs.String("name", "", T("tên máy (mặc định lấy tên Windows)"))
	lang := fs.String("lang", "", T("ngôn ngữ của agent: vi, en, zh"))
	fs.Parse(args)
	if *lang != "" {
		setLang(*lang)
	}
	if *server == "" {
		fmt.Println(T("Thiếu -server. Ví dụ: mam-agent.exe install -server http://192.168.1.10:7070"))
		die(2)
	}
	if !strings.HasPrefix(*server, "http") {
		*server = "http://" + *server
	}
	dir := installDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Println(T("Cần chạy bằng quyền Administrator:"), err)
		die(1)
	}
	// Gỡ bản cũ trước khi cài: dừng tác vụ và tắt mọi tiến trình agent cũ để giải phóng file đang khóa.
	hidden("schtasks", "/end", "/tn", "MAM Agent").Run()
	hidden("schtasks", "/end", "/tn", "MAM Client").Run()
	// giữ lại tên máy cũ (nếu cùng server)
	var old Config
	if b, err := os.ReadFile(filepath.Join(dir, "config.json")); err == nil {
		json.Unmarshal(b, &old)
	}
	self, _ := os.Executable()
	dst := filepath.Join(dir, "mam-agent.exe")
	if self != dst {
		// tắt agent cũ đang chạy (trừ chính trình cài này) rồi ghi đè, thử lại vài lần để qua lúc file còn khóa
		hidden("taskkill", "/f", "/im", "mam-agent.exe", "/fi", fmt.Sprintf("PID ne %d", os.Getpid())).Run()
		if err := copyOver(self, dst); err != nil {
			fmt.Println(T("Không ghi được"), dst, T("- agent cũ có thể đang chạy:"), err)
			die(1)
		}
	}
	nm := *name
	if nm == "" {
		nm = old.Name
	}
	newCfg := Config{Server: *server, Key: *key, Name: nm, Lang: uiLang}
	if old.Server == *server { // cài lại trên cùng server: chỉ giữ danh sách admin tin cậy.
		// Không giữ token cũ: nếu máy đã bị thu hồi, token cũ bị từ chối mãi; đăng ký lại thì server
		// tự nhận ra máy theo MAC (giữ nhóm và tên), còn máy đã thu hồi sẽ hiện lại như máy mới.
		newCfg.TrustedAdmins = old.TrustedAdmins
	}
	b, _ := json.MarshalIndent(newCfg, "", " ")
	os.WriteFile(filepath.Join(dir, "config.json"), b, 0o600)
	// chỉ SYSTEM và Administrators được sửa; người dùng thường không xóa hay sửa được
	hidden("icacls", dir, "/inheritance:r", "/grant:r", "*S-1-5-18:(OI)(CI)F", "*S-1-5-32-544:(OI)(CI)F").Run()
	u := utf16.Encode([]rune("\uFEFF" + fmt.Sprintf(taskXML, dst)))
	buf := make([]byte, 0, len(u)*2)
	for _, c := range u {
		buf = append(buf, byte(c), byte(c>>8))
	}
	xml := filepath.Join(os.TempDir(), "mam-task.xml")
	os.WriteFile(xml, buf, 0o600)
	defer os.Remove(xml)
	if out, err := hidden("schtasks", "/create", "/tn", "MAM Agent", "/xml", xml, "/f").CombinedOutput(); err != nil {
		fmt.Println(T("Không tạo được tác vụ khởi động:"), err, string(out))
		die(1)
	}
	hidden("schtasks", "/run", "/tn", "MAM Agent").Run()
	installClientUI(dir, dst)
	fmt.Println(T("Đã cài MAM Agent vào"), dir, T("và đăng ký chạy cùng Windows. Máy sẽ hiện trên Console sau vài giây."))
}

// copyOver chép file cài mới đè lên đích; thử lại vài lần vì Windows có thể còn khóa file một lúc
// sau khi tắt tiến trình agent cũ.
func copyOver(self, dst string) error {
	in, err := os.Open(self)
	if err != nil {
		return err
	}
	defer in.Close()
	var out *os.File
	for i := 0; i < 15; i++ {
		if out, err = os.Create(dst); err == nil {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	out.Close()
	return err
}

func readPassword(prompt string) string {
	fmt.Print(prompt)
	h := syscall.Handle(os.Stdin.Fd())
	var mode uint32
	gm, sm := k32.NewProc("GetConsoleMode"), k32.NewProc("SetConsoleMode")
	gm.Call(uintptr(h), uintptr(unsafe.Pointer(&mode)))
	sm.Call(uintptr(h), uintptr(mode&^0x0004)) // tắt ECHO
	line, _ := stdin.ReadString('\n')
	sm.Call(uintptr(h), uintptr(mode))
	fmt.Println()
	return strings.TrimSpace(line)
}

// Gỡ agent bằng mật khẩu gỡ do quản trị viên đặt trong Console: server kiểm tra rồi mới cho xóa.
func doUninstall() {
	*cfgPath = filepath.Join(installDir(), "config.json")
	if err := loadConfig(); err != nil {
		fmt.Println(T("Không tìm thấy agent đã cài:"), err)
		die(1)
	}
	setLang(cfg.Lang)
	code := readPassword(T("Mật khẩu gỡ: "))
	r, err := post("/api/agent/uninstall", map[string]string{"code": code}, cfg.Token)
	if err != nil {
		fmt.Println(T("Không liên lạc được server, không gỡ được:"), err)
		die(1)
	}
	if r.code != 200 {
		var e struct{ Error string }
		if json.Unmarshal(r.body, &e) == nil && e.Error != "" {
			fmt.Println(uninstErr(e.Error))
		} else {
			fmt.Println(T("Bị từ chối:"), string(r.body))
		}
		die(1)
	}
	dnsRestore()      // trả DNS cũ cho card mạng nếu bộ lọc DNS đang bật
	setDohLock(false) // trả lại DNS bảo mật của trình duyệt + bỏ rule tường lửa MAM đã đặt
	writeHosts(nil)   // bỏ danh sách chặn web MAM đã thêm vào hosts
	hidden("schtasks", "/end", "/tn", "MAM Agent").Run()
	hidden("schtasks", "/delete", "/tn", "MAM Agent", "/f").Run()
	removeClientUI()
	hidden("taskkill", "/f", "/im", "mam-agent.exe", "/fi", fmt.Sprintf("PID ne %d", os.Getpid())).Run()
	// tự xóa thư mục sau khi tiến trình này thoát
	hidden("cmd", "/c", fmt.Sprintf(`ping 127.0.0.1 -n 3 >nul & rmdir /s /q "%s"`, installDir())).Start()
	fmt.Println(T("Đã gỡ MAM Agent."))
}

// uninstErr dịch lời từ chối của server (server trả tiếng Việt) sang ngôn ngữ của agent.
func uninstErr(m string) string {
	if n := strings.TrimSuffix(strings.TrimPrefix(m, "Mật khẩu gỡ không đúng. Còn "), " lần thử"); n != m && len(n) < 3 {
		return T("Mật khẩu gỡ không đúng. Còn %s lần thử", n)
	}
	return T(m)
}
