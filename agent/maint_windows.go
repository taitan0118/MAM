//go:build windows

package main

// Tự sửa chữa: dọn file rác và giải phóng RAM (kiểu Mem Reduct). Chạy trong tiến trình chính (SYSTEM).
import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	psapi         = syscall.NewLazyDLL("psapi.dll")
	ntdll         = syscall.NewLazyDLL("ntdll.dll")
	adv           = syscall.NewLazyDLL("advapi32.dll")
	pEmptyWS      = psapi.NewProc("EmptyWorkingSet")
	pEnumProcs    = psapi.NewProc("EnumProcesses")
	pOpenProc     = k32.NewProc("OpenProcess")
	pCloseHandle  = k32.NewProc("CloseHandle")
	pCurProc      = k32.NewProc("GetCurrentProcess")
	pNtSetSysInfo = ntdll.NewProc("NtSetSystemInformation")
	pOpenTok      = adv.NewProc("OpenProcessToken")
	pLookupPriv   = adv.NewProc("LookupPrivilegeValueW")
	pAdjustPriv   = adv.NewProc("AdjustTokenPrivileges")
)

func freeMemMB() uint64 {
	var me memEx
	me.length = uint32(unsafe.Sizeof(me))
	pMemEx.Call(uintptr(unsafe.Pointer(&me)))
	return me.availPhys >> 20
}

func enablePrivilege(name string) {
	var tok syscall.Handle
	cur, _, _ := pCurProc.Call()
	if r, _, _ := pOpenTok.Call(cur, 0x28, uintptr(unsafe.Pointer(&tok))); r == 0 { // ADJUST_PRIVILEGES | QUERY
		return
	}
	defer syscall.CloseHandle(tok)
	var luid [2]uint32
	n, _ := syscall.UTF16PtrFromString(name)
	if r, _, _ := pLookupPriv.Call(0, uintptr(unsafe.Pointer(n)), uintptr(unsafe.Pointer(&luid))); r == 0 {
		return
	}
	tp := struct {
		Count uint32
		Luid  [2]uint32
		Attr  uint32
	}{1, luid, 2} // SE_PRIVILEGE_ENABLED
	pAdjustPriv.Call(uintptr(tok), 0, uintptr(unsafe.Pointer(&tp)), 0, 0, 0)
}

// freeRAM: thu nhỏ working set của mọi tiến trình rồi xả danh sách trang đã sửa và standby của hệ thống.
func freeRAM() (before, after uint64) {
	before = freeMemMB()
	enablePrivilege("SeProfileSingleProcessPrivilege")
	enablePrivilege("SeIncreaseQuotaPrivilege")
	pids := make([]uint32, 4096)
	var got uint32
	pEnumProcs.Call(uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)*4), uintptr(unsafe.Pointer(&got)))
	for _, pid := range pids[:got/4] {
		if pid == 0 || pid == 4 {
			continue
		}
		h, _, _ := pOpenProc.Call(0x0100|0x0400, 0, uintptr(pid)) // SET_QUOTA | QUERY_INFORMATION
		if h == 0 {
			continue
		}
		pEmptyWS.Call(h)
		pCloseHandle.Call(h)
	}
	for _, cmd := range []uint32{3, 4} { // 3 = xả trang đã sửa, 4 = xả standby
		c := cmd
		pNtSetSysInfo.Call(80, uintptr(unsafe.Pointer(&c)), 4) // SystemMemoryListInformation
	}
	time.Sleep(500 * time.Millisecond)
	return before, freeMemMB()
}

// cleanJunk dọn file rác theo danh sách của file xóa rác cũ (Temp, Prefetch, Recent, Internet tạm, thùng rác, *.tmp *._mp *.gid *.chk).
// Chỉ xóa file cũ hơn 1 ngày; file đang dùng bị bỏ qua. Cố ý KHÔNG xóa *.log, *.old, *.bak (hay là log và bản sao lưu của người dùng) và cookie (làm người dùng bị đăng xuất).
func cleanJunk() (files int, bytes int64) {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	drive := filepath.VolumeName(root) + `\`
	pd := os.Getenv("ProgramData")
	dirs := []string{filepath.Join(root, "Temp"), filepath.Join(root, "Prefetch"), filepath.Join(root, "Minidump"), filepath.Join(drive, `$Recycle.Bin`),
		filepath.Join(pd, `Microsoft\Windows\WER\ReportQueue`), filepath.Join(pd, `Microsoft\Windows\WER\ReportArchive`)}
	home := filepath.Join(drive, "Users")
	if us, err := os.ReadDir(home); err == nil {
		for _, u := range us {
			h := filepath.Join(home, u.Name())
			dirs = append(dirs, filepath.Join(h, `AppData\Local\Temp`), filepath.Join(h, `AppData\Local\CrashDumps`),
				filepath.Join(h, `AppData\Local\Microsoft\Windows\INetCache`), filepath.Join(h, `AppData\Roaming\Microsoft\Windows\Recent`))
		}
	}
	cut := time.Now().Add(-24 * time.Hour)
	del := func(p string, e fs.DirEntry) {
		if fi, err := e.Info(); err == nil && fi.ModTime().Before(cut) && os.Remove(p) == nil {
			files++
			bytes += fi.Size()
		}
	}
	for _, d := range dirs {
		if d == "" {
			continue
		}
		var subdirs []string
		filepath.WalkDir(d, func(p string, e fs.DirEntry, err error) error {
			if err != nil || p == d {
				return nil
			}
			if e.IsDir() {
				subdirs = append(subdirs, p)
				return nil
			}
			del(p, e)
			return nil
		})
		for i := len(subdirs) - 1; i >= 0; i-- { // chỉ xóa được thư mục đã rỗng
			os.Remove(subdirs[i])
		}
	}
	// quét cả ổ hệ thống tìm file tạm theo đuôi, tối đa 90 giây
	skip := map[string]bool{}
	for _, d := range []string{filepath.Join(root, "WinSxS"), filepath.Join(root, "Installer"), filepath.Join(root, "servicing"), filepath.Join(drive, "System Volume Information"), filepath.Join(drive, `$Recycle.Bin`)} {
		skip[strings.ToLower(d)] = true
	}
	deadline := time.Now().Add(90 * time.Second)
	filepath.WalkDir(drive, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if time.Now().After(deadline) {
			return filepath.SkipAll
		}
		if e.IsDir() {
			lp := strings.ToLower(p)
			if skip[lp] || strings.Contains(lp, `\mam`) { // không đụng thư mục cài và dữ liệu của MAM
				return filepath.SkipDir
			}
			return nil
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".tmp", "._mp", ".gid", ".chk":
			del(p, e)
		}
		return nil
	})
	return
}

func maintClean() Result {
	n, b := cleanJunk()
	return Result{OK: true, Out: fmt.Sprintf("Đã dọn %d tệp rác, giải phóng %d MB", n, b>>20)}
}

func maintRAM() Result {
	b, a := freeRAM()
	return Result{OK: true, Out: fmt.Sprintf("RAM trống: %d MB → %d MB", b, a)}
}
