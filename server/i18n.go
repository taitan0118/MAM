package main

import (
	"fmt"
	"os"
	"strings"
)

// Ngôn ngữ cho dòng lệnh và nhật ký của server: vi (mặc định), en, zh. Chọn bằng -lang hoặc biến MAM_LANG.
// Giao diện web tự dịch ở i18n.js; ở đây chỉ có các câu in ra cửa sổ lệnh.
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

var cat = map[string][2]string{ // khóa tiếng Việt -> {en, zh}
	"Cách dùng: mam-server reset-admin <tài khoản>": {"Usage: mam-server reset-admin <username>", "用法：mam-server reset-admin <账号>"},
	"Lỗi:": {"Error:", "错误："},
	"Đã đặt lại. Mật khẩu tạm của %s: %s\nĐăng nhập rồi hệ thống sẽ bắt đổi mật khẩu ngay.\n": {"Done. Temporary password for %s: %s\nAfter signing in, the system will force a password change immediately.\n", "已重置。%s 的临时密码：%s\n登录后系统会立即要求修改密码。\n"},
	"Lệnh: version | reset-admin <tài khoản>":    {"Commands: version | reset-admin <username>", "命令：version | reset-admin <账号>"},
	"Không đọc được dữ liệu ở %s: %v":            {"Cannot read data in %s: %v", "无法读取 %s 中的数据：%v"},
	"MAM Server %s · dữ liệu: %s · lắng nghe %s": {"MAM Server %s · data: %s · listening on %s", "MAM Server %s · 数据：%s · 监听 %s"},
	"Không có tài khoản ":                        {"No such account: ", "没有账号："},
	"MAC không hợp lệ":                           {"Invalid MAC address", "MAC 地址无效"},
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

// TErr dịch câu lỗi, kể cả câu có phần đuôi động ("Không có tài khoản xyz").
func TErr(msg string) string {
	if uiLang == "vi" {
		return msg
	}
	if p := "Không có tài khoản "; strings.HasPrefix(msg, p) {
		return T(p) + strings.TrimPrefix(msg, p)
	}
	return T(msg)
}

func init() {
	if l := os.Getenv("MAM_LANG"); l != "" {
		setLang(l)
	}
}
