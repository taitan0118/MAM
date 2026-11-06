package main

import (
	"fmt"
	"os"
	"strings"
)

// Ngôn ngữ của agent: vi (mặc định), en, zh. Chọn lúc cài bằng -lang, lưu trong config.json.
// Chỉ có các câu in ra cửa sổ lệnh, nhật ký agent và lời nhắn "tắt/khởi động lại máy" người dùng thấy.
var uiLang = "vi"

func setLang(l string) {
	l = strings.ToLower(strings.TrimSpace(l))
	switch {
	case strings.HasPrefix(l, "zh"):
		uiLang = "zh"
	case strings.HasPrefix(l, "en"):
		uiLang = "en"
	default:
		uiLang = "vi"
	}
}

func init() {
	if l := os.Getenv("MAM_LANG"); l != "" {
		setLang(l)
	}
}

var cat = map[string][2]string{ // khóa tiếng Việt -> {en, zh}
	"Hỗ trợ":    {"Support", "支持"},
	"Hỗ trợ · ": {"Support · ", "支持 · "},
	"Yêu cầu của bạn đã được xử lý xong: ":                                            {"Your request has been resolved: ", "您的请求已处理完成："},
	"Quản trị viên đã nhận yêu cầu của bạn và đang xử lý: ":                           {"An administrator has taken your request and is working on it: ", "管理员已接收您的请求并正在处理："},
	"⚠ RAM đang dùng %.0f%%. Hãy đóng bớt chương trình đang mở.":                      {"⚠ RAM usage is %.0f%%. Please close some open programs.", "⚠ 内存使用率为 %.0f%%，请关闭部分正在运行的程序。"},
	"⚠ Ổ %s sắp đầy: còn trống %.1f GB. Hãy dọn bớt dữ liệu.":                         {"⚠ Drive %s is almost full: %.1f GB free. Please clean up some data.", "⚠ 磁盘 %s 即将占满：仅剩 %.1f GB。请清理部分数据。"},
	"%s đang xin xem màn hình máy này. Hãy bấm Đồng ý hoặc Từ chối trong cửa sổ MAM.": {"%s is asking to view this computer's screen. Please click Agree or Decline in the MAM window.", "%s 请求查看此电脑的屏幕。请在 MAM 窗口中点击“同意”或“拒绝”。"},
	"Bấm Enter để đóng...":                                                         {"Press Enter to close...", "按回车键关闭..."},
	"địa chỉ server, ví dụ http://192.168.1.10:7070":                               {"server address, e.g. http://192.168.1.10:7070", "服务器地址，例如 http://192.168.1.10:7070"},
	"mã đăng ký (nếu server có đặt)":                                               {"enrollment key (if the server sets one)", "注册码（如服务器已设置）"},
	"tên máy (mặc định lấy tên Windows)":                                           {"machine name (defaults to the Windows name)", "设备名称（默认使用 Windows 计算机名）"},
	"ngôn ngữ của agent: vi, en, zh":                                               {"agent language: vi, en, zh", "代理语言：vi、en、zh"},
	"Thiếu -server. Ví dụ: mam-agent.exe install -server http://192.168.1.10:7070": {"Missing -server. Example: mam-agent.exe install -server http://192.168.1.10:7070", "缺少 -server。示例：mam-agent.exe install -server http://192.168.1.10:7070"},
	"Cần chạy bằng quyền Administrator:":                                           {"Run as Administrator:", "需要以管理员身份运行："},
	"Lỗi:":                         {"Error:", "错误："},
	"Không ghi được":               {"Cannot write", "无法写入"},
	"- agent cũ có thể đang chạy:": {"- the old agent may be running:", "- 旧代理可能正在运行："},
	"Không tạo được tác vụ khởi động:":                                     {"Cannot create the startup task:", "无法创建启动任务："},
	"Đã cài MAM Agent vào":                                                 {"MAM Agent installed in", "MAM 代理已安装到"},
	"và đăng ký chạy cùng Windows. Máy sẽ hiện trên Console sau vài giây.": {"and registered to start with Windows. The machine will appear in the Console within seconds.", "并已注册为随 Windows 启动。几秒钟后设备会出现在控制台中。"},
	"Không tìm thấy agent đã cài:":                                         {"No installed agent found:", "未找到已安装的代理："},
	"Mật khẩu gỡ: ":                                                        {"Uninstall password: ", "卸载密码："},
	"Mật khẩu gỡ không đúng. Còn %s lần thử":                               {"Wrong uninstall password. %s tries left", "卸载密码错误，还可尝试 %s 次"},
	"Mật khẩu gỡ không đúng. Đã khóa 1 phút":                               {"Wrong uninstall password. Locked for 1 minute", "卸载密码错误，已锁定 1 分钟"},
	"Quản trị viên chưa đặt mật khẩu gỡ":                                   {"The administrator has not set an uninstall password", "管理员尚未设置卸载密码"},
	"Sai quá nhiều lần, thử lại sau 1 phút":                                {"Too many wrong tries, try again in 1 minute", "错误次数过多，请 1 分钟后再试"},
	"Tài khoản Super admin: ":                                              {"Super admin username: ", "超级管理员账号："},
	"Mật khẩu: ":                                                           {"Password: ", "密码："},
	"Không liên lạc được server, không gỡ được:":                           {"Cannot reach the server, cannot uninstall:", "无法连接服务器，无法卸载："},
	"Bị từ chối:":                                                          {"Refused:", "被拒绝："},
	"Đã gỡ MAM Agent.":                                                     {"MAM Agent uninstalled.", "MAM 代理已卸载。"},
	"Lệnh: install -server http://IP:7070 | uninstall | run | version":     {"Commands: install -server http://IP:7070 [-lang en] | uninstall | run | version", "命令：install -server http://IP:7070 [-lang zh] | uninstall | run | version"},
	"Không đọc được config.json: %v":                                       {"Cannot read config.json: %v", "无法读取 config.json：%v"},
	"MAM Agent %s khởi động, server %s":                                    {"MAM Agent %s started, server %s", "MAM 代理 %s 已启动，服务器 %s"},
	"Chưa đăng ký được: %v":                                                {"Cannot register yet: %v", "尚未注册成功：%v"},
	"Không liên lạc được server: %v":                                       {"Cannot reach the server: %v", "无法连接服务器：%v"},
	"Token không còn hợp lệ, đăng ký lại":                                  {"Token no longer valid, registering again", "令牌已失效，重新注册"},
	"Máy đã bị thu hồi khỏi hệ thống":                                      {"This machine has been revoked from the system", "此设备已被系统吊销"},
	"Đã đăng ký với server, mã máy %s":                                     {"Registered with the server, machine id %s", "已向服务器注册，设备编号 %s"},
	"Không lấy được thông tin phần cứng: %v":                               {"Cannot read hardware information: %v", "无法获取硬件信息：%v"},
	"MAM: quản trị viên yêu cầu khởi động lại máy":                         {"MAM: an administrator requested a restart", "MAM：管理员请求重启计算机"},
	"MAM: quản trị viên yêu cầu tắt máy":                                   {"MAM: an administrator requested a shutdown", "MAM：管理员请求关闭计算机"},
	"Nhận lệnh %s %v (bản thử nghiệm: chỉ ghi nhận, không thực hiện)":      {"Received command %s %v (test build: recorded only, not executed)", "收到命令 %s %v（测试版：仅记录，不执行）"},
	"Lệnh install chỉ chạy trên Windows":                                   {"The install command only runs on Windows", "install 命令只能在 Windows 上运行"},
	"Lệnh uninstall chỉ chạy trên Windows":                                 {"The uninstall command only runs on Windows", "uninstall 命令只能在 Windows 上运行"},
}

func T(key string, a ...any) string {
	s := key
	if v, ok := cat[key]; ok && uiLang != "vi" {
		s = v[0]
		if uiLang == "zh" {
			s = v[1]
		}
	}
	if len(a) > 0 {
		return fmt.Sprintf(s, a...)
	}
	return s
}
