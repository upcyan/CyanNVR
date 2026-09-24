//go:build !windows

package recorder

import "syscall"

// diskFreeBytes 返回 path 所在文件系统的剩余可用字节数。
// 路径不存在时回退到其父目录继续找，直到根。
func diskFreeBytes(path string) (uint64, bool) {
	for p := path; ; {
		var st syscall.Statfs_t
		if err := syscall.Statfs(p, &st); err == nil {
			return uint64(st.Bavail) * uint64(st.Bsize), true
		}
		// 找不到上一级即放弃（根目录也失败说明平台不支持）
		next := parentDir(p)
		if next == p {
			return 0, false
		}
		p = next
	}
}

func parentDir(p string) string {
	for i := len(p) - 1; i > 0; i-- {
		if p[i] == '/' {
			if i == 0 {
				return "/"
			}
			return p[:i]
		}
	}
	return p
}
