package main

import (
	"encoding/json"
	"net/http"
	"time"
)

var launchAt int64 // lần cuối cho phép mở cửa sổ; bảo vệ bằng inbox.Mutex

// mayLaunch hỏi agent xem có được mở thêm cửa sổ MAM không (tối đa một cửa sổ, tránh mở nhiều tab). Không hỏi được thì cho mở.
func mayLaunch() bool {
	r, err := (&http.Client{Timeout: 2 * time.Second}).Get("http://" + uiAddr + "/api/launch")
	if err != nil {
		return true
	}
	defer r.Body.Close()
	var o struct{ Go bool }
	return json.NewDecoder(r.Body).Decode(&o) != nil || o.Go
}
