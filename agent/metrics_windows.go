//go:build windows

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	k32          = syscall.NewLazyDLL("kernel32.dll")
	wts          = syscall.NewLazyDLL("wtsapi32.dll")
	pSysTimes    = k32.NewProc("GetSystemTimes")
	pMemEx       = k32.NewProc("GlobalMemoryStatusEx")
	pRegGetValue = adv.NewProc("RegGetValueW")
	pDiskFree    = k32.NewProc("GetDiskFreeSpaceExW")
	pLogical     = k32.NewProc("GetLogicalDrives")
	pDriveType   = k32.NewProc("GetDriveTypeW")
	pTick64      = k32.NewProc("GetTickCount64")
	pConsoleSess = k32.NewProc("WTSGetActiveConsoleSessionId")
	pWtsQuery    = wts.NewProc("WTSQuerySessionInformationW")
	pWtsFree     = wts.NewProc("WTSFreeMemory")
)

func setupLog() {
	if f, err := os.OpenFile(filepath.Join(exeDir(), "agent.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		if st, _ := f.Stat(); st != nil && st.Size() > 2<<20 { // quá 2MB thì làm mới
			f.Close()
			os.Remove(filepath.Join(exeDir(), "agent.log"))
			f, _ = os.OpenFile(filepath.Join(exeDir(), "agent.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		}
		log.SetOutput(f)
	}
}

// regStr đọc một chuỗi trong registry máy (HKLM). Trả về "" nếu không có.
// Dùng RegGetValueW nên chuỗi là UTF-16, không lệ thuộc bảng mã như chạy lệnh ngoài.
func regStr(key, val string) string {
	k, err1 := syscall.UTF16PtrFromString(key)
	v, err2 := syscall.UTF16PtrFromString(val)
	if err1 != nil || err2 != nil {
		return ""
	}
	buf := make([]uint16, 256)
	n := uint32(len(buf) * 2)
	// 0x80000002 = HKEY_LOCAL_MACHINE; 2 = chỉ nhận REG_SZ; 0x10000 = đọc nhánh 64-bit (agent 32-bit vẫn đúng)
	r, _, _ := pRegGetValue.Call(0x80000002, uintptr(unsafe.Pointer(k)), uintptr(unsafe.Pointer(v)),
		2|0x10000, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
	if r != 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

// osName: "Windows 11 Pro 23H2 (build 22631)". Windows 11 vẫn ghi ProductName là "Windows 10 ..."
// nên phân biệt bằng số bản dựng: từ 22000 trở lên là Windows 11.
func osName() string {
	const k = `SOFTWARE\Microsoft\Windows NT\CurrentVersion`
	name := strings.TrimSpace(regStr(k, "ProductName"))
	if name == "" {
		out, _ := hidden("cmd", "/c", "ver").Output() // máy quá cũ không có khóa này
		return "Windows · " + strings.TrimSpace(string(out))
	}
	build, _ := strconv.Atoi(regStr(k, "CurrentBuildNumber"))
	if build >= 22000 {
		name = strings.Replace(name, "Windows 10", "Windows 11", 1)
	}
	if v := regStr(k, "DisplayVersion"); v != "" {
		name += " " + v
	}
	if build > 0 {
		name += " (build " + strconv.Itoa(build) + ")"
	}
	return name
}

func hidden(name string, args ...string) *exec.Cmd {
	c := exec.Command(name, args...)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return c
}

type ft struct{ lo, hi uint32 }

func (f ft) u64() uint64 { return uint64(f.hi)<<32 | uint64(f.lo) }

var prevIdle, prevKern, prevUser uint64

func cpuPercent() float64 {
	var i, k, u ft
	r, _, _ := pSysTimes.Call(uintptr(unsafe.Pointer(&i)), uintptr(unsafe.Pointer(&k)), uintptr(unsafe.Pointer(&u)))
	if r == 0 {
		return 0
	}
	idle, kern, user := i.u64(), k.u64(), u.u64()
	defer func() { prevIdle, prevKern, prevUser = idle, kern, user }()
	if prevKern == 0 {
		return 0
	}
	dI, dK, dU := idle-prevIdle, kern-prevKern, user-prevUser
	tot := dK + dU // kernel đã gồm thời gian rảnh
	if tot == 0 {
		return 0
	}
	return 100 * (1 - float64(dI)/float64(tot))
}

type memEx struct {
	length, load                               uint32
	totalPhys, availPhys, totalPage, availPage uint64
	totalVirt, availVirt, availExt             uint64
}

func collectMetrics() Metrics {
	m := Metrics{CPU: cpuPercent()}
	var me memEx
	me.length = uint32(unsafe.Sizeof(me))
	if r, _, _ := pMemEx.Call(uintptr(unsafe.Pointer(&me))); r != 0 {
		m.RAM = float64(me.load)
	}
	t, _, _ := pTick64.Call()
	m.Uptime = int64(t) / 1000
	mask, _, _ := pLogical.Call()
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		root := string(rune('A'+i)) + `:\`
		p, _ := syscall.UTF16PtrFromString(root)
		if dt, _, _ := pDriveType.Call(uintptr(unsafe.Pointer(p))); dt != 3 { // 3 = ổ cố định
			continue
		}
		var free, total, tfree uint64
		if r, _, _ := pDiskFree.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&free)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&tfree))); r != 0 {
			m.Disks = append(m.Disks, Disk{string(rune('A'+i)) + ":", round1(float64(total) / 1e9), round1(float64(free) / 1e9)})
		}
	}
	m.User = activeUser()
	return m
}

func round1(f float64) float64 { return float64(int(f*10+0.5)) / 10 }

func activeUser() string {
	sid, _, _ := pConsoleSess.Call()
	if uint32(sid) == 0xFFFFFFFF {
		return ""
	}
	var buf uintptr
	var n uint32
	if r, _, _ := pWtsQuery.Call(0, sid, 5, uintptr(unsafe.Pointer(&buf)), uintptr(unsafe.Pointer(&n))); r == 0 || buf == 0 {
		return ""
	}
	defer pWtsFree.Call(buf)
	u16 := (*[256]uint16)(unsafe.Pointer(buf))[:n/2]
	return strings.TrimRight(syscall.UTF16ToString(u16), "\x00")
}

func runPS(script string, timeout time.Duration) ([]byte, error) {
	// mỗi lần chạy một file riêng: các vòng quét (phần cứng, virus, nhật ký lỗi) chạy song song
	tf, err := os.CreateTemp(exeDir(), "tmp-*.ps1")
	if err != nil {
		return nil, err
	}
	f := tf.Name()
	defer os.Remove(f)
	_, err = tf.Write(append([]byte{0xEF, 0xBB, 0xBF}, []byte(libPS+"\n"+script)...))
	if tf.Close(); err != nil {
		return nil, err
	}
	c := hidden("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", f)
	var out, errb bytes.Buffer
	c.Stdout, c.Stderr = &out, &errb
	if err := c.Start(); err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	select {
	case err := <-done:
		if msg := strings.Join(strings.Fields(errb.String()), " "); err != nil && msg != "" {
			// kèm lời PowerShell báo, không thì log chỉ có "exit status 1"
			if r := []rune(msg); len(r) > 300 {
				msg = string(r[:300])
			}
			err = fmt.Errorf("%w: %s", err, msg)
		}
		return out.Bytes(), err
	case <-time.After(timeout):
		c.Process.Kill()
		return nil, os.ErrDeadlineExceeded
	}
}

func collectHw() map[string]interface{} {
	b, err := runPS(`Write-Output (ConvertTo-MamJson (Get-MamHw))`, 90*time.Second)
	if err != nil {
		log.Printf(T("Không lấy được thông tin phần cứng: %v"), err)
		return nil
	}
	var hw map[string]interface{}
	if json.Unmarshal(bytes.TrimSpace(bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})), &hw) != nil {
		return nil
	}
	return hw
}

func collectTemp() *float64 {
	b, err := runPS(`Write-Output (ConvertTo-MamJson (Get-MamTemp))`, 30*time.Second)
	if err != nil {
		return nil
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
	if err != nil {
		return nil
	}
	return &v
}

// runCommand thực hiện lệnh từ server. Agent chạy bằng quyền SYSTEM trong phiên 0 nên khóa màn hình
// và gửi tin nhắn dùng các lệnh của Windows nhắm vào phiên người dùng đang ngồi ở máy.
func runCommand(typ string, args map[string]string) (bool, string) {
	var c *exec.Cmd
	switch typ {
	case "restart":
		c = hidden("shutdown", "/r", "/t", "10", "/c", T("MAM: quản trị viên yêu cầu khởi động lại máy"))
	case "shutdown":
		c = hidden("shutdown", "/s", "/t", "10", "/c", T("MAM: quản trị viên yêu cầu tắt máy"))
	case "lock":
		// ngắt kết nối phiên đang ngồi ở máy: quay về màn hình đăng nhập (cần Windows Pro trở lên có tsdiscon)
		sid, _, _ := pConsoleSess.Call()
		c = hidden("tsdiscon", strconv.Itoa(int(uint32(sid))))
	case "message":
		c = hidden("msg", "*", "/TIME:120", args["text"]) // msg.exe có trên Windows Pro trở lên
	default:
		return false, "Lệnh không hỗ trợ: " + typ
	}
	out, err := c.CombinedOutput()
	if err != nil {
		return false, "Lỗi: " + err.Error() + " " + strings.TrimSpace(string(out))
	}
	return true, "Đã thực hiện"
}

const timeoutSec = time.Second
