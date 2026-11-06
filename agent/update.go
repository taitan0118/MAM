package main

// Tự cập nhật: server giữ sẵn bản agent mới, agent tải về, kiểm tra SHA-256 rồi tự thay thế.
import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var lastUpdateTry time.Time

func checkUpdate(sha string, size int64) string {
	if sha == "" || time.Since(lastUpdateTry) < time.Hour {
		return ""
	}
	lastUpdateTry = time.Now()
	req, _ := http.NewRequest("GET", strings.TrimRight(cfg.Server, "/")+"/api/agent/update?arch="+archName(), nil)
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	resp, err := httpc.Do(req)
	if err != nil {
		return "Không tải được bản mới: " + err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return ""
	}
	dir := exeDir()
	tmp := filepath.Join(dir, "mam-agent.new")
	f, err := os.Create(tmp)
	if err != nil {
		return "Không ghi được file tạm: " + err.Error()
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, size+1<<20))
	f.Close()
	if err != nil || hex.EncodeToString(h.Sum(nil)) != sha {
		os.Remove(tmp)
		return "File cập nhật tải về không toàn vẹn, bỏ qua"
	}
	log.Printf(T("Đã tải bản agent mới (%d byte), tiến hành thay thế"), n)
	if err := swapSelf(tmp); err != nil {
		os.Remove(tmp)
		return "Không thay được agent: " + err.Error()
	}
	return fmt.Sprintf("Đang cập nhật (%d byte), máy sẽ báo lại bằng bản mới sau ít giây", n)
}
