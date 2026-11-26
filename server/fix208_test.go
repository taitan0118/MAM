package main

import (
	"testing"
)

// tinCuoi: id và nội dung tin cuối cùng trong cuộc trò chuyện với máy m.
func tinCuoi(t *testing.T, f *fixture, m string) map[string]interface{} {
	t.Helper()
	code, r, _ := f.super.do("GET", "/api/chat/"+m, nil, nil)
	expect(t, code, 200, "đọc chat")
	ls, _ := r["lines"].([]interface{})
	if len(ls) == 0 {
		t.Fatal("chưa có tin nào")
	}
	return ls[len(ls)-1].(map[string]interface{})
}

// Console thả cảm xúc, sửa, thu hồi tin của chính mình.
func TestConsoleThaCamXucSuaThuHoi(t *testing.T) {
	f := setup(t)
	report(t, f.url, f.tok1, 10, 50)
	code, _, _ := f.super.do("POST", "/api/chat/"+f.m1, map[string]string{"text": "Xin chào"}, nil)
	expect(t, code, 200, "gửi chat")
	l := tinCuoi(t, f, f.m1)
	id, _ := l["id"].(string)
	if id == "" {
		t.Fatal("tin nhắn phải có id thì mới thả cảm xúc hay thu hồi được")
	}

	// máy phải nhận được id đó để sau này biết thu hồi tin nào
	f.s.mu.RLock()
	thay := false
	for _, c := range f.s.st.Commands {
		if c.Type == "message" && c.Args["mid"] == id {
			thay = true
		}
	}
	f.s.mu.RUnlock()
	if !thay {
		t.Fatal("lệnh gửi tin xuống máy phải kèm id dòng chat (mid)")
	}

	// thả cảm xúc
	code, _, _ = f.super.do("POST", "/api/chat/"+f.m1+"/tin", map[string]string{"id": id, "lam": "cam", "cam": "👍"}, nil)
	expect(t, code, 200, "thả cảm xúc")
	if l = tinCuoi(t, f, f.m1); l["cam"] != "👍" {
		t.Fatalf("phải nhớ cảm xúc, đang là %v", l["cam"])
	}
	// bỏ cảm xúc
	code, _, _ = f.super.do("POST", "/api/chat/"+f.m1+"/tin", map[string]string{"id": id, "lam": "cam", "cam": ""}, nil)
	expect(t, code, 200, "bỏ cảm xúc")
	if l = tinCuoi(t, f, f.m1); l["cam"] != nil {
		t.Fatalf("bỏ cảm xúc thì phải sạch, đang là %v", l["cam"])
	}
	// cảm xúc lạ thì chặn: nó đi thẳng xuống màn hình người khác
	code, _, _ = f.super.do("POST", "/api/chat/"+f.m1+"/tin", map[string]string{"id": id, "lam": "cam", "cam": "<script>"}, nil)
	expect(t, code, 400, "cảm xúc lạ")

	// sửa
	code, _, _ = f.super.do("POST", "/api/chat/"+f.m1+"/tin", map[string]string{"id": id, "lam": "sua", "text": "Xin chào anh"}, nil)
	expect(t, code, 200, "sửa tin")
	l = tinCuoi(t, f, f.m1)
	if l["text"] != "Xin chào anh" || l["sua"] != true {
		t.Fatalf("phải đổi nội dung và đánh dấu đã sửa, đang là %v / %v", l["text"], l["sua"])
	}

	// thu hồi: nội dung phải mất hẳn, không chỉ ẩn đi
	code, _, _ = f.super.do("POST", "/api/chat/"+f.m1+"/tin", map[string]string{"id": id, "lam": "thu"}, nil)
	expect(t, code, 200, "thu hồi")
	l = tinCuoi(t, f, f.m1)
	if l["thu"] != true || l["text"] != "" {
		t.Fatalf("thu hồi phải xóa hẳn nội dung, đang là %v / %q", l["thu"], l["text"])
	}
	// thu hồi lần nữa thì thôi
	code, _, _ = f.super.do("POST", "/api/chat/"+f.m1+"/tin", map[string]string{"id": id, "lam": "thu"}, nil)
	expect(t, code, 400, "thu hồi hai lần")

	// lệnh báo cho máy biết phải nằm chờ sẵn
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	n := 0
	for _, c := range f.s.st.Commands {
		if c.Type == "chatact" && c.Args["mid"] == id {
			n++
		}
	}
	if n < 4 {
		t.Fatalf("mỗi lần đổi phải có một lệnh xuống máy, mới có %d", n)
	}
}

// Admin KHÔNG được sửa hay thu hồi lời của người ngồi máy.
func TestConsoleKhongSuaLoiNguoiNgoiMay(t *testing.T) {
	f := setup(t)
	report(t, f.url, f.tok1, 10, 50)
	h := map[string]string{"Authorization": "Bearer " + f.tok1}
	code, _, _ := client(t, f.url).do("POST", "/api/agent/reply", map[string]string{"text": "Dạ em nghe"}, h)
	expect(t, code, 200, "máy trả lời")
	id, _ := tinCuoi(t, f, f.m1)["id"].(string)

	for _, lam := range []string{"sua", "thu"} {
		code, _, _ = f.super.do("POST", "/api/chat/"+f.m1+"/tin", map[string]string{"id": id, "lam": lam, "text": "sửa bậy"}, nil)
		expect(t, code, 403, "admin "+lam+" lời người ngồi máy")
	}
	// nhưng thả cảm xúc lên lời của họ thì được
	code, _, _ = f.super.do("POST", "/api/chat/"+f.m1+"/tin", map[string]string{"id": id, "lam": "cam", "cam": "❤️"}, nil)
	expect(t, code, 200, "thả cảm xúc lên lời người ngồi máy")
	if tinCuoi(t, f, f.m1)["text"] != "Dạ em nghe" {
		t.Fatal("nội dung lời người ngồi máy phải nguyên vẹn")
	}
}

// Người ngồi máy thu hồi được tin của mình, không đụng được tin của admin.
func TestMayThuHoiTinCuaMinh(t *testing.T) {
	f := setup(t)
	report(t, f.url, f.tok1, 10, 50)
	h := map[string]string{"Authorization": "Bearer " + f.tok1}

	// tin của admin: máy không được thu hồi
	code, _, _ := f.super.do("POST", "/api/chat/"+f.m1, map[string]string{"text": "Xin chào"}, nil)
	expect(t, code, 200, "admin gửi")
	idAdmin, _ := tinCuoi(t, f, f.m1)["id"].(string)
	code, _, _ = client(t, f.url).do("POST", "/api/agent/chat-act", map[string]string{"id": idAdmin, "lam": "thu"}, h)
	expect(t, code, 403, "máy thu hồi tin admin")

	// nhưng thả cảm xúc lên tin admin thì được
	code, _, _ = client(t, f.url).do("POST", "/api/agent/chat-act", map[string]string{"id": idAdmin, "lam": "cam", "cam": "😂"}, h)
	expect(t, code, 200, "máy thả cảm xúc")
	l := tinCuoi(t, f, f.m1)
	if l["camMay"] != "😂" || l["cam"] != nil {
		t.Fatalf("cảm xúc của máy phải để riêng, đang là camMay=%v cam=%v", l["camMay"], l["cam"])
	}

	// tin của chính máy: sửa và thu hồi được
	code, r, _ := client(t, f.url).do("POST", "/api/agent/reply", map[string]string{"text": "Dạ em nghe"}, h)
	expect(t, code, 200, "máy trả lời")
	idMay, _ := r["id"].(string)
	if idMay == "" {
		t.Fatal("server phải trả id dòng chat về cho máy, không có thì máy không thu hồi được tin của mình")
	}
	code, _, _ = client(t, f.url).do("POST", "/api/agent/chat-act", map[string]string{"id": idMay, "lam": "sua", "text": "Dạ em nghe ạ"}, h)
	expect(t, code, 200, "máy sửa tin của mình")
	code, _, _ = client(t, f.url).do("POST", "/api/agent/chat-act", map[string]string{"id": idMay, "lam": "thu"}, h)
	expect(t, code, 200, "máy thu hồi tin của mình")
	l = tinCuoi(t, f, f.m1)
	if l["thu"] != true || l["text"] != "" {
		t.Fatalf("thu hồi phải xóa hẳn nội dung, đang là %v / %q", l["thu"], l["text"])
	}
}

// Tin nhắn lưu từ bản cũ chưa có id thì phải được vá, không thì không thao tác gì được.
func TestVaIDChoTinNhanCu(t *testing.T) {
	f := setup(t)
	f.s.mu.Lock()
	f.s.st.Chat = append(f.s.st.Chat, &ChatLine{Machine: f.m1, By: "cũ", Text: "tin bản cũ"})
	co := &ChatLine{ID: "giunguyen", Machine: f.m1, By: "x", Text: "đã có id"}
	f.s.st.Chat = append(f.s.st.Chat, co)
	n := f.s.vaIDChat()
	f.s.mu.Unlock()
	if n != 1 {
		t.Fatalf("phải vá đúng 1 dòng, vá %d", n)
	}
	if co.ID != "giunguyen" {
		t.Fatalf("dòng đã có id thì để yên, đang là %q", co.ID)
	}
	if id, _ := tinCuoi(t, f, f.m1)["id"].(string); id == "" {
		t.Fatal("vá xong mà vẫn còn dòng không có id")
	}
}
