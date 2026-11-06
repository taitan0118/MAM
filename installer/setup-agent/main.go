//go:build windows

// MAM-Agent-Setup.exe: bộ cài một file cho MAM Agent (Windows 10/11 x64), có giao diện đồ họa (xem ../uikit).
// Mang theo mam-agent.exe bên trong và gọi lệnh install/uninstall của nó. Muốn đổi chữ, trường nhập hay các bước
// chỉ cần sửa khai báo ở main().
package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mam/uikit"
)

//go:embed logo.svg
var logoSVG string

//go:embed payload/mam-agent.exe
var agentExe []byte

var version = "dev" // build.sh gán bằng -ldflags "-X main.version=..."

func progDir() string { return filepath.Join(os.Getenv("ProgramFiles"), "MAM Agent") }
func installed() bool {
	_, err := os.Stat(filepath.Join(progDir(), "mam-agent.exe"))
	return err == nil
}

type config struct {
	Server, Key, Name, Token string
}

func readConfig() (c config) {
	b, err := os.ReadFile(filepath.Join(progDir(), "config.json"))
	if err != nil {
		return
	}
	var m map[string]interface{}
	json.Unmarshal(b, &m)
	s := func(k string) string { v, _ := m[k].(string); return v }
	return config{s("server"), s("key"), s("name"), s("token")}
}

func installedVersion() string {
	out, err := uikit.Exec(filepath.Join(progDir(), "mam-agent.exe"), "version")
	if err != nil {
		return ""
	}
	f := strings.Fields(out)
	if len(f) == 0 {
		return ""
	}
	return f[len(f)-1]
}

func lg(p map[string]string) int { return map[string]int{"vi": 0, "en": 1, "zh": 2}[p["lang"]] }

func doInstall(p map[string]string, r *uikit.Reporter) (uikit.Result, error) {
	lang := lg(p)
	srv := strings.TrimSpace(p["server"])
	if !strings.HasPrefix(srv, "http") {
		srv = "http://" + srv
	}
	// 1) máy này có tới được server không: bắt lỗi sai địa chỉ / thiếu cổng / tường lửa ngay từ đầu
	r.Step(0)
	cl := &http.Client{Timeout: 6 * time.Second}
	resp, err := cl.Get(strings.TrimRight(srv, "/") + "/api/status")
	if err != nil {
		r.Log(err.Error())
		return uikit.Result{}, errors.New([]string{
			"Máy này không nối được tới server tại " + srv + ".\nKiểm tra: địa chỉ đúng chưa, có đủ cổng không (ví dụ 192.168.1.10:8080), server đang chạy chưa, và tường lửa trên máy server.",
			"This computer cannot reach the server at " + srv + ".\nCheck: is the address right, does it include the port (e.g. 192.168.1.10:8080), is the server running, and the firewall on the server.",
			"此电脑无法连接到服务器 " + srv + "。\n请检查：地址是否正确、是否带端口（例如 192.168.1.10:8080）、服务器是否在运行，以及服务器上的防火墙。"}[lang])
	}
	resp.Body.Close()
	// 2) cài
	r.Step(1)
	dir, err := os.MkdirTemp("", "mam-setup")
	if err != nil {
		return uikit.Result{}, err
	}
	defer os.RemoveAll(dir)
	exe := filepath.Join(dir, "mam-agent.exe")
	if err := os.WriteFile(exe, agentExe, 0o755); err != nil {
		return uikit.Result{}, err
	}
	args := []string{"install", "-server", srv, "-lang", p["lang"]}
	if k := strings.TrimSpace(p["key"]); k != "" {
		args = append(args, "-key", k)
	}
	if n := strings.TrimSpace(p["name"]); n != "" {
		args = append(args, "-name", n)
	}
	out, err := uikit.Exec(exe, args...)
	r.Log(out)
	if err != nil {
		return uikit.Result{}, errors.New(strings.TrimSpace(out))
	}
	// 3) agent đã chạy chưa và đã đăng ký được với server chưa (config.json có token là xong)
	r.Step(2)
	ok := false
	for i := 0; i < 25 && !ok; i++ {
		time.Sleep(time.Second)
		ok = readConfig().Token != ""
	}
	res := uikit.Result{Title: uikit.L{"Đã cài xong MAM Agent", "MAM Agent is installed", "MAM Agent 安装完成"}}
	if ok {
		res.Lines = []uikit.L{{"Máy này đã nối với server và sẽ hiện trong Console sau vài giây.", "This computer is connected to the server and will appear in the Console within seconds.", "此电脑已连接服务器，几秒后会出现在控制台中。"}}
	} else {
		res.Lines = []uikit.L{{"Agent đã cài và đang chạy, nhưng chưa đăng ký được với server (có thể server đặt mã đăng ký, hoặc mạng chậm). Xem " + filepath.Join(progDir(), "agent.log"), "The agent is installed and running, but has not registered with the server yet (the server may require an enrollment key, or the network is slow). See " + filepath.Join(progDir(), "agent.log"), "代理已安装并运行，但尚未向服务器注册（可能服务器要求注册码，或网络较慢）。请查看 " + filepath.Join(progDir(), "agent.log")}}
	}
	res.Links = []uikit.Link{{Label: uikit.L{"Mở Console của server", "Open the server's Console", "打开服务器控制台"}, URL: strings.TrimRight(srv, "/")}}
	return res, nil
}

func doUninstall(p map[string]string, r *uikit.Reporter) (uikit.Result, error) {
	lang := lg(p)
	if !installed() {
		return uikit.Result{}, errors.New([]string{"MAM Agent chưa được cài trên máy này.", "MAM Agent is not installed on this computer.", "此电脑尚未安装 MAM Agent。"}[lang])
	}
	r.Step(0)
	// agent đọc mật khẩu gỡ từ đầu vào chuẩn; server kiểm tra rồi mới cho gỡ
	c := uikit.ExecIn(p["code"]+"\n", filepath.Join(progDir(), "mam-agent.exe"), "uninstall")
	r.Step(1)
	r.Log(c.Out)
	if c.Err != nil {
		msg := strings.TrimSpace(c.Out)
		if msg == "" {
			msg = c.Err.Error()
		}
		return uikit.Result{}, errors.New(msg)
	}
	return uikit.Result{Title: uikit.L{"Đã gỡ MAM Agent", "MAM Agent is removed", "MAM Agent 已卸载"}, Lines: []uikit.L{{"Máy này không còn được quản lý. Có thể đóng cửa sổ này.", "This computer is no longer managed. You can close this window.", "此电脑已不再受管理，可以关闭此窗口。"}}}, nil
}

func main() {
	if !uikit.IsAdmin() {
		uikit.Elevate()
		return
	}
	uikit.Run(uikit.App{
		Logo:             logoSVG,
		Name:             "MAM Agent",
		Version:          version,
		Tagline:          uikit.L{"Bộ cài chương trình giám sát trên máy client", "Setup for the monitoring agent on a client computer", "客户端监控代理安装程序"},
		Installed:        installed,
		InstalledVersion: installedVersion,
		UpdateButton:     uikit.L{"Cập nhật", "Update", "更新"},
		Defaults: func() map[string]string {
			c := readConfig()
			return map[string]string{"server": strings.TrimPrefix(c.Server, "http://"), "key": c.Key, "name": c.Name}
		},
		Install: uikit.Action{
			Title:  uikit.L{"Cài đặt MAM Agent", "Install MAM Agent", "安装 MAM Agent"},
			Intro:  uikit.L{"Nhập địa chỉ server, bộ cài sẽ kiểm tra kết nối trước khi cài. Cài xong máy tự hiện trong Console.", "Enter the server address; the setup checks the connection before installing. The machine then appears in the Console by itself.", "输入服务器地址，安装前会先检查连接。安装后设备会自动出现在控制台中。"},
			Button: uikit.L{"Cài đặt", "Install", "安装"},
			Fields: []uikit.Field{
				{ID: "server", Label: uikit.L{"Địa chỉ server", "Server address", "服务器地址"}, Type: "text", Required: true,
					Placeholder: uikit.L{"192.168.1.10:7070", "192.168.1.10:7070", "192.168.1.10:7070"},
					Help:        uikit.L{"Nhớ kèm cổng nếu server không dùng 7070, ví dụ 192.168.1.10:8080.", "Include the port if the server does not use 7070, e.g. 192.168.1.10:8080.", "如果服务器不是 7070 端口，请带上端口，例如 192.168.1.10:8080。"}},
				{ID: "key", Label: uikit.L{"Mã đăng ký (nếu server có đặt)", "Enrollment key (if the server has one)", "注册码（如服务器已设置）"}, Type: "text"},
				{ID: "name", Label: uikit.L{"Tên máy (để trống = tên Windows)", "Machine name (empty = Windows name)", "设备名称（留空 = Windows 计算机名）"}, Type: "text"},
			},
			Steps: []uikit.L{
				{"Kiểm tra kết nối tới server", "Check the connection to the server", "检查与服务器的连接"},
				{"Cài agent và đăng ký chạy cùng Windows", "Install the agent and register it to start with Windows", "安装代理并注册为随 Windows 启动"},
				{"Đăng ký máy với server", "Register this computer with the server", "向服务器注册此电脑"},
			},
		},
		Uninstall: uikit.Action{
			Title:  uikit.L{"Gỡ MAM Agent khỏi máy này", "Remove MAM Agent from this computer", "从此电脑卸载 MAM Agent"},
			Intro:  uikit.L{"Nhập mật khẩu gỡ do quản trị viên cấp. Không cần tài khoản quản trị.", "Enter the uninstall password from your administrator. No admin account is needed.", "请输入管理员提供的卸载密码，无需管理员账号。"},
			Button: uikit.L{"Gỡ cài đặt", "Uninstall", "卸载"},
			Danger: true,
			Fields: []uikit.Field{
				{ID: "code", Label: uikit.L{"Mật khẩu gỡ", "Uninstall password", "卸载密码"}, Type: "password", Required: true,
					Help: uikit.L{"Không có mật khẩu này thì hỏi bộ phận IT. Nhập sai 5 lần sẽ bị khóa 1 phút.", "Ask your IT team if you do not have it. 5 wrong tries lock it for 1 minute.", "没有此密码请联系 IT 部门。输错 5 次将锁定 1 分钟。"}},
			},
			Removes: []uikit.L{
				{"Chương trình MAM và tác vụ chạy cùng Windows", "The MAM program and its startup task", "MAM 程序及其开机任务"},
				{"Cửa sổ MAM và biểu tượng ở khay hệ thống", "The MAM window and the tray icon", "MAM 窗口和托盘图标"},
				{"Danh sách chặn web và luật tường lửa MAM đã đặt", "Website blocks and firewall rules set by MAM", "MAM 设置的网站屏蔽和防火墙规则"},
				{"Máy được báo về server là đã gỡ (tài liệu và phần mềm khác giữ nguyên)", "The server is told this computer was removed (your files and other software stay)", "通知服务器此电脑已卸载（您的文件和其他软件保持不变）"},
			},
			Footnote: uikit.L{"Mỗi lần gỡ, đúng hay sai mật khẩu, đều ghi vào nhật ký của quản trị viên.", "Every attempt, right or wrong, is logged for the administrator.", "每次卸载尝试（无论对错）都会记录在管理员日志中。"},
			Steps: []uikit.L{
				{"Kiểm tra mật khẩu gỡ với server", "Check the uninstall password with the server", "向服务器验证卸载密码"},
				{"Gỡ chương trình, bỏ chặn web và dọn thư mục", "Remove the program, lift web blocks and clean up", "卸载程序、解除网站屏蔽并清理"},
			},
		},
		DoInstall:   doInstall,
		DoUninstall: doUninstall,
	})
}
