//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

func diskFree(p string) (free, total uint64, err error) {
	var st syscall.Statfs_t
	if err = syscall.Statfs(p, &st); err != nil {
		return
	}
	return uint64(st.Bavail) * uint64(st.Bsize), uint64(st.Blocks) * uint64(st.Bsize), nil
}

func hideWindow(c *exec.Cmd) {}
