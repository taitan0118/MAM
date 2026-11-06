package main

import (
	"encoding/binary"
	"encoding/json"
	"os"
)

// Bundle là cấu hình server dán vào đuôi file mam-agent.exe khi tải từ Console:
// JSON + độ dài (uint32 LE) + "MAMCFG01". Windows bỏ qua phần dư sau file .exe.
type Bundle struct {
	Server string `json:"server"`
	Key    string `json:"key"`
	Lang   string `json:"lang"`
}

const bundleMagic = "MAMCFG01"

func readBundle() *Bundle {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	return readBundleFrom(exe)
}

func readBundleFrom(path string) *Bundle {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() < 12 {
		return nil
	}
	tail := make([]byte, 12)
	if _, err := f.ReadAt(tail, st.Size()-12); err != nil || string(tail[4:]) != bundleMagic {
		return nil
	}
	n := int64(binary.LittleEndian.Uint32(tail[:4]))
	if n <= 0 || n > 4096 || n > st.Size()-12 {
		return nil
	}
	buf := make([]byte, n)
	if _, err := f.ReadAt(buf, st.Size()-12-n); err != nil {
		return nil
	}
	var b Bundle
	if json.Unmarshal(buf, &b) != nil || b.Server == "" {
		return nil
	}
	return &b
}
