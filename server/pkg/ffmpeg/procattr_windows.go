//go:build windows

package ffmpeg

import "syscall"

// sysProcAttr Windows 下不设进程组（由 CommandContext 的 Kill 兜底）。
func sysProcAttr() *syscall.SysProcAttr { return nil }
