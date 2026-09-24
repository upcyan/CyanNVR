//go:build windows

package recorder

// diskFreeBytes Windows 下暂不支持剩余空间探测，返回「不低」以免误停录像。
func diskFreeBytes(string) (uint64, bool) { return 0, false }
