// MAM Console (ứng dụng desktop): mở Console của server thành một cửa sổ riêng, không thanh địa chỉ,
// bằng Edge/Chrome/Chromium có sẵn. Chạy được trên Windows, macOS và Linux; chỉ dùng thư viện chuẩn.
package main

import (
	_ "embed"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Version được build.sh gán bằng -ldflags -X.
var Version = "dev"

//go:embed connect.html
var connectHTML string

// browsers: trình duyệt nền Chromium hỗ trợ cửa sổ --app.
func browsers() []string {
	switch runtime.GOOS {
	case "windows":
		var out []string
		for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles", "LocalAppData"} {
			if b := os.Getenv(env); b != "" {
				out = append(out, filepath.Join(b, "Microsoft", "Edge", "Application", "msedge.exe"),
					filepath.Join(b, "Google", "Chrome", "Application", "chrome.exe"),
					filepath.Join(b, "BraveSoftware", "Brave-Browser", "Application", "brave.exe"))
			}
		}
		return out
	case "darwin":
		var out []string
		for _, app := range []string{"Google Chrome", "Microsoft Edge", "Chromium", "Brave Browser"} {
			out = append(out, "/Applications/"+app+".app/Contents/MacOS/"+app)
			if h, err := os.UserHomeDir(); err == nil {
				out = append(out, filepath.Join(h, "Applications", app+".app", "Contents", "MacOS", app))
			}
		}
		return out
	}
	var out []string
	for _, n := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "microsoft-edge", "microsoft-edge-stable", "brave-browser"} {
		if p, err := exec.LookPath(n); err == nil {
			out = append(out, p)
		}
	}
	return out
}

func findBrowser() string {
	for _, p := range browsers() {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// fileURL đổi đường dẫn file thành địa chỉ file:// (cả kiểu C:\... của Windows).
func fileURL(p string) string {
	p = filepath.ToSlash(p)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

// openDefault: không có trình duyệt Chromium thì mở bằng trình duyệt mặc định (có thanh địa chỉ).
func openDefault(u string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
	case "darwin":
		return exec.Command("open", u).Start()
	}
	return exec.Command("xdg-open", u).Start()
}

func main() {
	choose := flag.Bool("connect", false, "luôn hiện màn hình chọn server, không tự kết nối")
	server := flag.String("server", "", "mở thẳng một server, ví dụ 192.168.1.10:7070")
	ver := flag.Bool("version", false, "in phiên bản")
	flag.Parse()
	if *ver {
		fmt.Println("MAM Console", Version)
		return
	}
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "MAM-Console")
	prof := filepath.Join(dir, "profile")
	if err := os.MkdirAll(prof, 0o755); err != nil {
		fail(err)
	}
	page := filepath.Join(dir, "connect.html")
	if err := os.WriteFile(page, []byte(strings.ReplaceAll(connectHTML, "{{VER}}", Version)), 0o644); err != nil {
		fail(err)
	}
	q := url.Values{}
	if *choose {
		q.Set("choose", "1")
	}
	if *server != "" {
		q.Set("server", *server)
	}
	target := fileURL(page)
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	b := findBrowser()
	if b == "" {
		if err := openDefault(target); err != nil {
			fail(err)
		}
		return
	}
	args := []string{"--app=" + target, "--window-size=1360,860", "--user-data-dir=" + prof, "--no-first-run", "--no-default-browser-check", "--disable-extensions", "--disable-features=Translate"}
	if err := exec.Command(b, args...).Start(); err != nil {
		fail(err)
	}
	keepHook()
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "MAM Console:", err)
	os.Exit(1)
}
