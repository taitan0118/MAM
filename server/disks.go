package main

import "fmt"

// Chấm sức khỏe ổ cứng từ chỉ số S.M.A.R.T agent gửi lên.
// Windows chỉ báo Unhealthy khi ổ gần chết: SSD mòn 63% vẫn ra "Healthy", nên không dùng được một mình.
// Ổ không đọc được S.M.A.R.T (máy ảo, USB, RAID) thì giữ nguyên trạng thái Windows chứ không đoán.

const diskLifeWarn = 40 // SSD còn dưới ngần này phần trăm tuổi thọ là cần chú ý

func num(m map[string]interface{}, k string) (float64, bool) {
	switch v := m[k].(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	}
	return 0, false
}

// rateDisk đặt lại health/note/wear của một ổ; trả về false nếu ổ không có S.M.A.R.T.
func rateDisk(d map[string]interface{}) bool {
	if ok, _ := d["smart"].(bool); !ok {
		return false
	}
	var bad, warn []string
	if f, _ := d["predictFail"].(bool); f {
		bad = append(bad, "Windows báo ổ sắp hỏng")
	}
	for _, c := range []struct{ key, msg string }{
		{"pending", "%.0f khối chờ thay"},
		{"offline", "%.0f khối hỏng ngoại tuyến"},
		{"uncorrect", "%.0f lỗi không sửa được"},
	} {
		if v, ok := num(d, c.key); ok && v > 0 {
			bad = append(bad, fmt.Sprintf(c.msg, v))
		}
	}
	if v, ok := num(d, "realloc"); ok && v > 0 {
		warn = append(warn, fmt.Sprintf("%.0f khối hỏng đã thay", v))
	}
	if v, ok := num(d, "lifeLeft"); ok {
		d["wear"] = 100 - v // Windows để mòn 0%, lấy theo tuổi thọ còn lại của chính ổ
		if v <= diskLifeWarn {
			warn = append(warn, fmt.Sprintf("tuổi thọ còn %.0f%%", v))
		}
	}
	switch {
	case len(bad) > 0:
		d["health"], d["note"] = "Sắp hỏng", join(append(bad, warn...))
	case len(warn) > 0:
		d["health"], d["note"] = "Cần chú ý", join(warn)
	default:
		d["health"], d["note"] = "Tốt", ""
	}
	return true
}

func join(xs []string) string {
	out := ""
	for i, x := range xs {
		if i > 0 {
			out += ", "
		}
		out += x
	}
	return out
}

// rateDisks chấm mọi ổ trong thông tin phần cứng agent gửi lên.
func rateDisks(hw map[string]interface{}) {
	ds, _ := hw["disks"].([]interface{})
	for _, x := range ds {
		if d, ok := x.(map[string]interface{}); ok {
			rateDisk(d)
		}
	}
}
