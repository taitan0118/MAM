//go:build !windows

package uikit

import (
	"fmt"
	"os"
	"os/exec"
)

// Bản không phải Windows chỉ để thử giao diện: in địa chỉ ra, hoặc mở bằng xdg-open nếu có.
func openWindow(url string) {
	fmt.Fprintln(os.Stderr, "UI:", url)
	if os.Getenv("MAM_UI_NOOPEN") == "" {
		exec.Command("xdg-open", url).Start()
	}
}

func openDefault(url string) { exec.Command("xdg-open", url).Start() }
