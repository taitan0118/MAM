//go:build windows

package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

func installDir() string { return filepath.Join(os.Getenv("ProgramFiles"), "MAM Agent") }

// taskDefVer: phiên bản của taskXML. Sửa taskXML thì TĂNG số này, máy cài từ bản cũ mới đăng ký lại (fixTaskTriggers).
const taskDefVer = 3

const taskXML = `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo><Description>MAM Agent - giám sát máy trong mạng LAN</Description></RegistrationInfo>
  <Triggers>
    <BootTrigger><Enabled>true</Enabled></BootTrigger>
    <LogonTrigger><Enabled>true</Enabled></LogonTrigger>
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
	newCfg := Config{Server: *server, Key: *key, Name: nm, Lang: uiLang, TaskVer: taskDefVer}
	if old.Server == *server { // cài lại trên cùng server: chỉ giữ danh sách admin tin cậy.
		// Không giữ token cũ: nếu máy đã bị thu hồi, token cũ bị từ chối mãi; đăng ký lại thì server
		// tự nhận ra máy theo MAC (giữ nhóm và tên), còn máy đã thu hồi sẽ hiện lại như máy mới.
		newCfg.TrustedAdmins = old.TrustedAdmins
	}
	b, _ := json.MarshalIndent(newCfg, "", " ")
	os.WriteFile(filepath.Join(dir, "config.json"), b, 0o600)
	// chỉ SYSTEM và Administrators được sửa; người dùng thường không xóa hay sửa được
	hidden("icacls", dir, "/inheritance:r", "/grant:r", "*S-1-5-18:(OI)(CI)F", "*S-1-5-32-544:(OI)(CI)F").Run()
	xml, err := writeTaskXML(dst)
	if err != nil {
		fmt.Println(T("Không ghi được tệp tác vụ:"), err)
		die(1)
	}
	defer os.Remove(xml)
	if out, err := hidden("schtasks", "/create", "/tn", "MAM Agent", "/xml", xml, "/f").CombinedOutput(); err != nil {
		fmt.Println(T("Không tạo được tác vụ khởi động:"), err, string(out))
		die(1)
	}
	hidden("schtasks", "/run", "/tn", "MAM Agent").Run()
	installClientUI(dir, dst)
	fmt.Println(T("Đã cài MAM Agent vào"), dir, T("và đăng ký chạy cùng Windows. Máy sẽ hiện trên Console sau vài giây."))
}

// writeTaskXML ghi định nghĩa tác vụ ra tệp UTF-16 (schtasks chỉ đọc được UTF-16).
func writeTaskXML(exe string) (string, error) {
	u := utf16.Encode([]rune("\uFEFF" + fmt.Sprintf(taskXML, exe)))
	buf := make([]byte, 0, len(u)*2)
	for _, c := range u {
		buf = append(buf, byte(c), byte(c>>8))
	}
	p := filepath.Join(os.TempDir(), "mam-task.xml")
	return p, os.WriteFile(p, buf, 0o600)
}

// taskNeedsFix: máy này có cần đăng ký lại tác vụ khởi động không. Chỉ đăng ký lại cho bản đã
// cài thật (nằm trong installDir): bản chạy tay hay bản thử không được kéo tác vụ sang chỗ khác.
func taskNeedsFix(exe string, ver int) bool {
	return ver < taskDefVer && strings.EqualFold(filepath.Dir(exe), installDir())
}

// fixTaskTriggers đăng ký lại tác vụ khởi động khi định nghĩa cũ đã lỗi thời.
// Vì sao cần: tự cập nhật chỉ THAY FILE EXE, không bao giờ sửa định nghĩa tác vụ — máy cài từ
// bản cũ vì thế mãi không có lượt chạy lúc đăng nhập, nên máy đã vào tới màn hình mà Windows
// chưa kịp chạy tác vụ khởi động thì server phải đợi vài phút mới thấy máy.
// Làm bằng script rời: schtasks /create có thể kết thúc luôn tiến trình đang chạy (chính mình),
// script chạy tiếp và bật agent lên lại.
func fixTaskTriggers() {
	exe, err := os.Executable()
	if err != nil || !taskNeedsFix(exe, cfg.TaskVer) {
		return
	}
	xml, err := writeTaskXML(exe)
	if err != nil {
		return
	}
	// Phải ghi ra TỆP .cmd, đừng truyền chuỗi lệnh cho `cmd /c`: Go bọc cả chuỗi trong ngoặc kép và
	// đổi dấu " bên trong thành \", cmd.exe không hiểu dấu đó nên `/tn "MAM Agent"` vỡ làm hai —
	// đúng cái làm bản 1.1.116 đăng ký lại không được mà vẫn ghi vào log là xong.
	// Giữ output của schtasks lại trong task-fix.log để biết vì sao nếu máy nào vẫn không nhận.
	script := filepath.Join(exeDir(), "task-fix.cmd")
	logf := filepath.Join(exeDir(), "task-fix.log")
	body := fmt.Sprintf("@echo off\r\nping 127.0.0.1 -n 2 >nul\r\nschtasks /create /tn \"MAM Agent\" /xml \"%s\" /f >\"%s\" 2>&1\r\nschtasks /run /tn \"MAM Agent\" >>\"%s\" 2>&1\r\ndel \"%s\"\r\ndel \"%%~f0\"\r\n", xml, logf, logf, xml)
	if err := os.WriteFile(script, []byte(body), 0o600); err != nil {
		log.Printf(T("Không ghi được script đăng ký lại tác vụ: %v"), err)
		return
	}
	// Đánh dấu TRƯỚC khi chạy script: script có thể kết thúc tiến trình này, mà lần chạy sau lại
	// thấy tác vụ cũ thì sẽ đăng ký lại nữa — thành vòng tự giết mình mỗi vài giây.
	cfg.TaskVer = taskDefVer
	saveConfig()
	if err := hidden("cmd", "/c", script).Start(); err != nil {
		log.Printf(T("Không đăng ký lại được tác vụ khởi động: %v"), err)
		return
	}
	log.Print(T("Đã đăng ký lại tác vụ khởi động (thêm lượt chạy lúc đăng nhập)"))
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
