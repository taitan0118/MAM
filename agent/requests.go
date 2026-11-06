package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type reqView struct {
	ID      string `json:"id"`
	Text    string `json:"text"`
	Status  string `json:"status"`
	Replies []struct {
		By     string `json:"by"`
		Text   string `json:"text"`
		Client bool   `json:"client"`
	} `json:"replies"`
}

func getReqs() ([]reqView, []byte, error) {
	req, _ := http.NewRequest("GET", strings.TrimRight(cfg.Server, "/")+"/api/agent/requests", nil)
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	resp, err := httpc.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	var out []reqView
	if resp.StatusCode != 200 || json.Unmarshal(b, &out) != nil {
		return nil, nil, io.ErrUnexpectedEOF
	}
	return out, b, nil
}

// requestWatcher: admin trả lời hoặc đóng yêu cầu thì đẩy thành tin nhắn để cửa sổ MAM tự bật lên.
func requestWatcher() {
	seen := map[string]int{}
	status := map[string]string{}
	first := true
	for {
		time.Sleep(10 * time.Second)
		if cfg.Token == "" {
			continue
		}
		list, _, err := getReqs()
		if err != nil {
			continue
		}
		for _, q := range list {
			admin := 0
			for _, r := range q.Replies {
				if !r.Client {
					admin++
				}
			}
			if !first {
				if admin > seen[q.ID] {
					last := ""
					by := ""
					for _, r := range q.Replies {
						if !r.Client {
							last, by = r.Text, r.By
						}
					}
					addNotice("req", T("Hỗ trợ · ")+by, last, false)
				} else if q.Status == "done" && status[q.ID] != "" && status[q.ID] != "done" {
					addNotice("req", T("Hỗ trợ"), T("Yêu cầu của bạn đã được xử lý xong: ")+q.Text, false)
				} else if q.Status == "working" && status[q.ID] == "new" {
					addNotice("req", T("Hỗ trợ"), T("Quản trị viên đã nhận yêu cầu của bạn và đang xử lý: ")+q.Text, false)
				}
			}
			seen[q.ID], status[q.ID] = admin, q.Status
		}
		first = false
	}
}

func registerReqRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/reqs", func(w http.ResponseWriter, r *http.Request) {
		_, b, err := getReqs()
		if err != nil {
			http.Error(w, "server", 502)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
	})
	mux.HandleFunc("/api/req", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ID       string `json:"id"`
			Text     string `json:"text"`
			Urgent   bool   `json:"urgent"`
			Reporter string `json:"reporter"`
			Phone    string `json:"phone"`
			EmpID    string `json:"empId"`
			BadPC    string `json:"badPc"`
			Type     string `json:"deviceType"`
		}
		if r.Method != "POST" || json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&in) != nil {
			http.Error(w, "bad", 400)
			return
		}
		resp, err := post("/api/agent/request", map[string]interface{}{"id": in.ID, "text": in.Text, "urgent": in.Urgent, "user": lastUser,
			"reporter": in.Reporter, "phone": in.Phone, "empId": in.EmpID, "badPc": in.BadPC, "deviceType": in.Type}, cfg.Token)
		if err != nil {
			http.Error(w, "server", 502)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.code)
		w.Write(resp.body)
	})
}

var (
	devTypesMu sync.Mutex
	devTypes   []string
)

var contactLabel atomic.Value // string; rỗng = dùng chữ mặc định theo ngôn ngữ

func setContactLabel(s string) { contactLabel.Store(s) }
func getContactLabel() string  { s, _ := contactLabel.Load().(string); return s }

func setDeviceTypes(t []string) { devTypesMu.Lock(); devTypes = t; devTypesMu.Unlock() }
func getDeviceTypes() []string  { devTypesMu.Lock(); defer devTypesMu.Unlock(); return devTypes }
