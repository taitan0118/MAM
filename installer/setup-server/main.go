//go:build windows

// MAM-Server-Setup.exe: bộ cài một file cho MAM Server, có giao diện đồ họa (xem ../uikit).
// Mang theo mam-server.exe và mam-agent.exe bên trong. Cài/cập nhật: chép file, mở cổng tường lửa,
// tác vụ SYSTEM chạy cùng Windows. Muốn đổi chữ, trường nhập hay các bước chỉ cần sửa khai báo ở main().
package main

import (
	"embed"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"mam/uikit"
)

//go:embed logo.svg
var logoSVG string

//go:embed payload/mam-server.exe
var serverExe []byte

// Các file agent cho mọi hệ điều hành (Windows 64/32-bit, Linux, macOS): server phát chúng khi người quản trị
// bấm "Tải agent" trong Console, và dùng bản Windows để tự cập nhật các máy.
//
//go:embed payload/agents/*
var agentFS embed.FS

var version = "dev" // build.sh gán bằng -ldflags "-X main.version=..."

func progDir() string { return filepath.Join(os.Getenv("ProgramFiles"), "MAM Server") }
func defData() string { return filepath.Join(os.Getenv("ProgramData"), "MAM") }
func installed() bool {
	_, err := os.Stat(filepath.Join(progDir(), "mam-server.exe"))
	return err == nil
}

func installedVersion() string {
	out, err := uikit.Exec(filepath.Join(progDir(), "mam-server.exe"), "version")
	if err != nil {
		return ""
	}
	f := strings.Fields(out)
	if len(f) == 0 {
		return ""
	}
	return f[len(f)-1]
}

var reAddr, reData = regexp.MustCompile(`-addr :(\d+)`), regexp.MustCompile(`-data "([^"]+)"`)

// settings đọc lại cổng và thư mục dữ liệu đang dùng từ tác vụ đã đăng ký.
func settings() (port, data string) {
	port, data = "7070", defData()
	if out, err := uikit.Exec("schtasks", "/query", "/tn", "MAM Server", "/xml"); err == nil {
		if m := reAddr.FindStringSubmatch(out); m != nil {
			port = m[1]
		}
		if m := reData.FindStringSubmatch(out); m != nil {
			data = m[1]
		}
	}
	return
}

func doInstall(p map[string]string, r *uikit.Reporter) (uikit.Result, error) {
	lang := map[string]int{"vi": 0, "en": 1, "zh": 2}[p["lang"]]
	was := installed()
	port := strings.TrimSpace(p["port"])
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return uikit.Result{}, errors.New([]string{"Cổng phải là số từ 1 đến 65535.", "The port must be a number from 1 to 65535.", "端口必须是 1 到 65535 之间的数字。"}[lang])
	}
	data := filepath.Clean(strings.TrimSpace(p["data"]))
	if !filepath.IsAbs(data) {
		return uikit.Result{}, errors.New([]string{"Thư mục dữ liệu phải là đường dẫn đầy đủ, ví dụ D:\\MAMdata.", "The data folder must be a full path, e.g. D:\\MAMdata.", "数据文件夹必须是完整路径，例如 D:\\MAMdata。"}[lang])
	}
	dir := progDir()
	r.Step(0)
	uikit.Exec("schtasks", "/end", "/tn", "MAM Server")
	uikit.Exec("taskkill", "/f", "/im", "mam-server.exe")
	time.Sleep(1500 * time.Millisecond)
	r.Step(1)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return uikit.Result{}, err
	}
	if err := os.MkdirAll(data, 0o700); err != nil {
		return uikit.Result{}, err
	}
	exe := filepath.Join(dir, "mam-server.exe")
	if err := os.WriteFile(exe, serverExe, 0o755); err != nil {
		return uikit.Result{}, err
	}
	ents, _ := agentFS.ReadDir("payload/agents")
	for _, e := range ents {
		b, err := agentFS.ReadFile("payload/agents/" + e.Name())
		if err != nil {
			return uikit.Result{}, err
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name()), b, 0o755); err != nil {
			return uikit.Result{}, err
		}
	}
	// dữ liệu có băm mật khẩu và token: chỉ SYSTEM và Administrators được đọc
	uikit.Exec("icacls", data, "/inheritance:r", "/grant:r", "*S-1-5-18:(OI)(CI)F", "*S-1-5-32-544:(OI)(CI)F")
	r.Step(2)
	uikit.Exec("netsh", "advfirewall", "firewall", "delete", "rule", "name=MAM Server")
	if out, err := uikit.Exec("netsh", "advfirewall", "firewall", "add", "rule", "name=MAM Server", "dir=in", "action=allow", "protocol=TCP", "localport="+port, "profile=any", "remoteip=LocalSubnet"); err != nil {
		r.Log(out)
		return uikit.Result{}, err
	}
	r.Step(3)
	u := utf16.Encode([]rune(string(rune(0xFEFF)) + fmt.Sprintf(taskXML, exe, port, data, p["lang"])))
	buf := make([]byte, 0, len(u)*2)
	for _, c := range u {
		buf = append(buf, byte(c), byte(c>>8))
	}
	tmp := filepath.Join(os.TempDir(), "mam-server-task.xml")
	os.WriteFile(tmp, buf, 0o600)
	out, err := uikit.Exec("schtasks", "/create", "/tn", "MAM Server", "/xml", tmp, "/f")
	os.Remove(tmp)
	if err != nil {
		r.Log(out)
		return uikit.Result{}, err
	}
	r.Step(4)
	uikit.Exec("schtasks", "/run", "/tn", "MAM Server")
	up := false
	for i := 0; i < 20 && !up; i++ {
		time.Sleep(time.Second)
		if resp, e := http.Get("http://127.0.0.1:" + port + "/api/status"); e == nil {
			resp.Body.Close()
			up = true
		}
	}
	if !up {
		return uikit.Result{}, errors.New([]string{"Server chưa trả lời. Xem nhật ký: ", "The server did not answer. See the log: ", "服务器没有响应。请查看日志："}[lang] + filepath.Join(data, "server.log"))
	}
	res := uikit.Result{Title: uikit.L{"Đã cài xong MAM Server", "MAM Server is installed", "MAM Server 安装完成"}}
	if was {
		res.Title = uikit.L{"Đã cập nhật MAM Server", "MAM Server is updated", "MAM Server 更新完成"}
	}
	res.Lines = []uikit.L{
		{"Mở một trong các địa chỉ dưới đây để vào Console (lần đầu sẽ tạo tài khoản Super admin).", "Open one of the addresses below to enter the Console (the first visit creates the Super admin account).", "打开下面任一地址进入控制台（首次访问会创建超级管理员账号）。"},
		{"Dữ liệu nằm ở: " + data, "Data is stored in: " + data, "数据保存在：" + data},
	}
	res.Links = append(res.Links, uikit.Link{Label: uikit.L{"Mở Console trên máy này", "Open the Console on this computer", "在本机打开控制台"}, URL: "http://localhost:" + port})
	if as, e := net.InterfaceAddrs(); e == nil {
		for _, a := range as {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && !n.IP.IsLoopback() {
				res.Links = append(res.Links, uikit.Link{Label: uikit.L{"Địa chỉ cho máy khác trong mạng LAN", "Address for other computers on the LAN", "局域网内其他电脑使用的地址"}, URL: "http://" + n.IP.String() + ":" + port})
			}
		}
	}
	return res, nil
}

func doUninstall(p map[string]string, r *uikit.Reporter) (uikit.Result, error) {
	if !installed() {
		lang := map[string]int{"vi": 0, "en": 1, "zh": 2}[p["lang"]]
		return uikit.Result{}, errors.New([]string{"MAM Server chưa được cài trên máy này.", "MAM Server is not installed on this computer.", "此电脑尚未安装 MAM Server。"}[lang])
	}
	_, data := settings()
	r.Step(0)
	uikit.Exec("schtasks", "/end", "/tn", "MAM Server")
	uikit.Exec("schtasks", "/delete", "/tn", "MAM Server", "/f")
	uikit.Exec("taskkill", "/f", "/im", "mam-server.exe")
	r.Step(1)
	uikit.Exec("netsh", "advfirewall", "firewall", "delete", "rule", "name=MAM Server")
	time.Sleep(time.Second)
	r.Step(2)
	if err := os.RemoveAll(progDir()); err != nil {
		return uikit.Result{}, err
	}
	r.Step(3)
	res := uikit.Result{Title: uikit.L{"Đã gỡ MAM Server", "MAM Server is removed", "MAM Server 已卸载"}}
	if p["deldata"] == "1" {
		if b, err := os.ReadFile(filepath.Join(data, "redirect.txt")); err == nil { // dữ liệu đã được chuyển sang thư mục khác trong Console
			if moved := strings.TrimSpace(string(b)); filepath.IsAbs(moved) {
				os.RemoveAll(moved)
			}
		}
		if err := os.RemoveAll(data); err != nil {
			return uikit.Result{}, err
		}
		res.Lines = []uikit.L{{"Đã xóa cả dữ liệu.", "The data was deleted too.", "数据也已删除。"}}
	} else {
		res.Lines = []uikit.L{{"Dữ liệu vẫn được giữ ở: " + data, "The data is kept in: " + data, "数据仍保留在：" + data}}
	}
	return res, nil
}

func main() {
	if !uikit.IsAdmin() {
		uikit.Elevate()
		return
	}
	port, data := settings()
	uikit.Run(uikit.App{
		Logo:             logoSVG,
		Name:             "MAM Server",
		Version:          version,
		Tagline:          uikit.L{"Bộ cài máy chủ quản lý máy tính trong mạng LAN", "Setup for the LAN computer-management server", "局域网电脑管理服务器安装程序"},
		Installed:        installed,
		InstalledVersion: installedVersion,
		UpdateButton:     uikit.L{"Cập nhật", "Update", "更新"},
		Defaults:         func() map[string]string { return map[string]string{"port": port, "data": data} },
		Install: uikit.Action{
			Title:  uikit.L{"Cài đặt MAM Server", "Install MAM Server", "安装 MAM Server"},
			Intro:  uikit.L{"Cài một lần là server tự chạy cùng Windows. Đang cài sẵn thì bấm Cập nhật: dữ liệu và cài đặt được giữ nguyên.", "Install once and the server starts with Windows. If it is already installed just press Update: your data and settings are kept.", "安装一次，服务器就会随 Windows 自动运行。已安装时点击“更新”，数据和设置会保留。"},
			Button: uikit.L{"Cài đặt", "Install", "安装"},
			Fields: []uikit.Field{
				{ID: "port", Label: uikit.L{"Cổng (port)", "Port", "端口"}, Type: "number", Default: "7070", Required: true,
					Help: uikit.L{"Địa chỉ vào Console sẽ là http://địa-chỉ-máy:cổng. Giữ 7070 nếu không bị trùng.", "The Console address will be http://machine-address:port. Keep 7070 unless it clashes.", "控制台地址为 http://电脑地址:端口。端口不冲突就保持 7070。"}},
				{ID: "data", Label: uikit.L{"Thư mục lưu dữ liệu", "Data folder", "数据文件夹"}, Type: "text", Default: defData(), Required: true,
					Help: uikit.L{"Tài khoản, danh sách máy, nhật ký. Có thể đặt ổ khác, ví dụ D:\\MAMdata.", "Accounts, machine list, audit log. You can use another drive, e.g. D:\\MAMdata.", "账号、设备列表、日志。可以放到其他磁盘，例如 D:\\MAMdata。"}},
			},
			Steps: []uikit.L{
				{"Dừng bản cũ (nếu có)", "Stop the old version (if any)", "停止旧版本（如有）"},
				{"Chép chương trình và dữ liệu", "Copy the program and prepare the data folder", "复制程序并准备数据文件夹"},
				{"Mở cổng tường lửa (chỉ trong mạng LAN)", "Open the firewall port (LAN only)", "开放防火墙端口（仅限局域网）"},
				{"Đăng ký chạy cùng Windows", "Register to start with Windows", "注册为随 Windows 启动"},
				{"Khởi động và kiểm tra server", "Start and check the server", "启动并检查服务器"},
			},
		},
		Uninstall: uikit.Action{
			Title:  uikit.L{"Gỡ MAM Server", "Uninstall MAM Server", "卸载 MAM Server"},
			Intro:  uikit.L{"Gỡ chương trình, tác vụ tự chạy và luật tường lửa. Dữ liệu được giữ lại trừ khi bạn tick xóa.", "Removes the program, the auto-start task and the firewall rule. Data is kept unless you tick delete.", "移除程序、自动启动任务和防火墙规则。除非勾选删除，否则数据会保留。"},
			Button: uikit.L{"Gỡ cài đặt", "Uninstall", "卸载"},
			Danger: true,
			Fields: []uikit.Field{
				{ID: "deldata", Label: uikit.L{"Xóa luôn dữ liệu (tài khoản, máy, nhật ký)", "Also delete ALL data (accounts, machines, audit log)", "同时删除全部数据（账号、设备、日志）"}, Type: "checkbox",
					Help: uikit.L{"Không khôi phục được. Để trống nếu định cài lại sau.", "This cannot be undone. Leave it unticked if you plan to reinstall.", "无法恢复。如果打算以后重装，请不要勾选。"}},
			},
			Steps: []uikit.L{
				{"Dừng server và xóa tác vụ tự chạy", "Stop the server and remove the auto-start task", "停止服务器并删除自动启动任务"},
				{"Xóa luật tường lửa", "Remove the firewall rule", "删除防火墙规则"},
				{"Xóa chương trình", "Remove the program files", "删除程序文件"},
				{"Xử lý dữ liệu", "Handle the data", "处理数据"},
			},
		},
		DoInstall:   doInstall,
		DoUninstall: doUninstall,
	})
}

const taskXML = `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo><Description>MAM Server - Missing App Monitor</Description></RegistrationInfo>
  <Triggers><BootTrigger><Enabled>true</Enabled></BootTrigger></Triggers>
  <Principals><Principal id="Author"><UserId>S-1-5-18</UserId><RunLevel>HighestAvailable</RunLevel></Principal></Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries><StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit><Hidden>true</Hidden><StartWhenAvailable>true</StartWhenAvailable>
    <RestartOnFailure><Interval>PT1M</Interval><Count>999</Count></RestartOnFailure>
  </Settings>
  <Actions Context="Author"><Exec><Command>"%s"</Command><Arguments>-addr :%s -data "%s" -lang %s</Arguments></Exec></Actions>
</Task>`
