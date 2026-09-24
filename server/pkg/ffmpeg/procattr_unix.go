//go:build !windows

package ffmpeg

import "syscall"

// sysProcAttr 让探测进程进入独立进程组，便于超时时整组强杀。
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}
