package main

// Tự cập nhật: server giữ sẵn bản agent mới, agent tải về, kiểm tra SHA-256 rồi tự thay thế.
import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// errDatSan không phải lỗi thật: bản mới đã nằm đúng chỗ, chỉ chờ máy khởi động lại.
var errDatSan = errors.New("đã đặt sẵn bản mới, máy sẽ chạy bản mới sau lần khởi động tới")

var lastUpdateTry time.Time

// Chờ bao lâu mới thử lại. Hỏng thì thử lại sớm (đường truyền chập chờn, tải dở file), chứ đợi cả
// tiếng thì một máy đường mạng yếu có thể mãi không lên được bản mới.
var updateWait = time.Hour

func updFail(msg string) string {
	updateWait = 10 * time.Minute
	return msg
}

// Kết quả lượt cập nhật gần nhất, gửi kèm báo cáo sau đó để Console biết vì sao một máy không lên bản
// mới (trước đây chỉ ghi vào log trên máy đó, ngồi ở Console không thấy gì).
var updMu sync.Mutex
var updMsg string

func setUpdMsg(s string) { updMu.Lock(); updMsg = s; updMu.Unlock() }

// takeUpdMsg lấy và xóa ghi chú, để chỉ gửi lên server một lần.
func takeUpdMsg() string {
	updMu.Lock()
	defer updMu.Unlock()
	s := updMsg
	updMsg = ""
	return s
}

func checkUpdate(sha string, size int64) string {
	if sha == "" || time.Since(lastUpdateTry) < updateWait {
		return ""
	}
	lastUpdateTry = time.Now()
	updateWait = time.Hour
	req, _ := http.NewRequest("GET", strings.TrimRight(cfg.Server, "/")+"/api/agent/update?arch="+archName(), nil)
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	resp, err := httpc.Do(req)
	if err != nil {
		return updFail("Không tải được bản mới: " + err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return updFail(fmt.Sprintf("Server trả mã %d khi tải bản agent mới", resp.StatusCode))
	}
	dir := exeDir()
	tmp := filepath.Join(dir, "mam-agent.new")
	f, err := os.Create(tmp)
	if err != nil {
		return updFail("Không ghi được file tạm: " + err.Error())
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, size+1<<20))
	f.Close()
	if err != nil || hex.EncodeToString(h.Sum(nil)) != sha {
		os.Remove(tmp)
		return updFail("File cập nhật tải về không toàn vẹn, bỏ qua")
	}
	log.Printf(T("Đã tải bản agent mới (%d byte), tiến hành thay thế"), n)
	if err := swapSelf(tmp); err != nil {
		if errors.Is(err, errDatSan) { // không thay nóng được nhưng đã đặt sẵn bản mới cho lần bật sau
			return err.Error()
		}
		os.Remove(tmp)
		return updFail("Không thay được agent: " + err.Error())
	}
	return fmt.Sprintf("Đang cập nhật (%d byte), máy sẽ báo lại bằng bản mới sau ít giây", n)
}
