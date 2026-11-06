package main

// Lệnh theo yêu cầu từ Console: xem tiến trình (và tắt theo PID), đọc tình trạng S.M.A.R.T của ổ cứng.
import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type Result struct {
	OK   bool
	Out  string
	Data []byte
	Mime string
}

func jsonData(v interface{}, out string) Result {
	b, _ := json.Marshal(v)
	return Result{OK: true, Out: out, Data: b, Mime: "application/json"}
}

func fail(msg string) Result { return Result{Out: msg} }

// handleCommand chạy một lệnh và trả kết quả.
func handleCommand(typ string, a map[string]string) Result {
	switch typ {
	case "message":
		addMsgFrom(a["fromId"], a["from"], a["text"])
		return Result{OK: true, Out: "Đã chuyển tới máy"}
	case "procs":
		p, err := osProcs()
		if err != nil {
			return fail("Lỗi: " + err.Error())
		}
		return jsonData(p, fmt.Sprintf("%d tiến trình", len(p)))
	case "kill":
		if err := osKill(a["pid"]); err != nil {
			return fail("Lỗi: " + err.Error())
		}
		return Result{OK: true, Out: "Đã tắt tiến trình " + a["name"] + " (" + a["pid"] + ")"}
	case "wolsend": // máy này làm trạm phát gói đánh thức cho máy khác cùng mạng LAN
		for _, mac := range strings.Split(a["mac"], ",") {
			if err := sendWOL(mac, a["ip"]); err != nil {
				return fail("Lỗi: " + err.Error())
			}
		}
		return Result{OK: true, Out: "Đã phát gói đánh thức"}
	case "clean":
		return maintClean()
	case "freeram":
		return maintRAM()
	case "smart":
		d, err := osSmart()
		if err != nil {
			return fail("Lỗi: " + err.Error())
		}
		return jsonData(d, fmt.Sprintf("%d ổ", len(d)))
	}
	ok, msg := runCommand(typ, a)
	return Result{OK: ok, Out: msg}
}

// postResult gửi kết quả (kèm dữ liệu nếu có) về server.
func postResult(id string, r Result) {
	m := map[string]interface{}{"id": id, "ok": r.OK, "output": r.Out}
	if len(r.Data) > 0 {
		m["data"], m["mime"] = base64.StdEncoding.EncodeToString(r.Data), r.Mime
	}
	b, _ := json.Marshal(m)
	req, _ := http.NewRequest("POST", strings.TrimRight(cfg.Server, "/")+"/api/agent/result", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	if resp, err := httpc.Do(req); err == nil {
		resp.Body.Close()
	}
}
