package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestHardenDataDir 覆盖本次安全修复的核心保证：
// 启动后，凭据类文件必须收紧到 0600，且不存在的文件不能导致报错。
func TestHardenDataDir(t *testing.T) {
	dir := t.TempDir()

	// 模拟历史遗留状态：安装脚本执行过 chmod -R 755，凭据文件全局可读。
	files := []string{"jwt_secret", "nvr.db", "nvr.db-wal", "nvr.db-shm", "settings.json"}
	for _, f := range files {
		p := filepath.Join(dir, f)
		if err := os.WriteFile(p, []byte("secret-content"), 0o644); err != nil {
			t.Fatalf("setup %s: %v", f, err)
		}
		if err := os.Chmod(p, 0o755); err != nil { // 复现脚本刷出来的 755
			t.Fatalf("chmod %s: %v", f, err)
		}
	}

	// TLS 私钥场景
	tlsDir := filepath.Join(dir, "tls")
	if err := os.MkdirAll(tlsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(tlsDir, "key.pem")
	if err := os.WriteFile(keyPath, []byte("PRIVATE KEY"), 0o755); err != nil {
		t.Fatal(err)
	}

	hardenDataDir(dir)

	for _, f := range files {
		p := filepath.Join(dir, f)
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %s: %v", f, err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("%s 权限 = %04o, 期望 0600（否则本机任意用户可读走凭据）", f, got)
		}
	}

	if info, err := os.Stat(keyPath); err != nil {
		t.Fatal(err)
	} else if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("tls/key.pem 权限 = %04o, 期望 0600", got)
	}
	if info, err := os.Stat(tlsDir); err != nil {
		t.Fatal(err)
	} else if got := info.Mode().Perm(); got != 0o700 {
		t.Errorf("tls/ 目录权限 = %04o, 期望 0700", got)
	}

	// 文件内容必须原样保留（只改权限，不动数据）
	if b, err := os.ReadFile(filepath.Join(dir, "jwt_secret")); err != nil {
		t.Fatal(err)
	} else if string(b) != "secret-content" {
		t.Errorf("jwt_secret 内容被改动: %q", string(b))
	}
}

// TestHardenDataDirMissingFiles：目录几乎是空的（全新安装）时不能 panic 或报错。
func TestHardenDataDirMissingFiles(t *testing.T) {
	dir := t.TempDir()
	hardenDataDir(dir) // 不应 panic

	if err := os.WriteFile(filepath.Join(dir, "only-this"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	hardenDataDir(dir) // 无关文件也不该被误伤

	info, err := os.Stat(filepath.Join(dir, "only-this"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Errorf("无关文件权限被误改 = %04o, 期望保持 0644", got)
	}
}

// TestHardenDataDirNonexistentDir：数据目录不存在时静默跳过（服务尚未建目录）。
func TestHardenDataDirNonexistentDir(t *testing.T) {
	hardenDataDir(filepath.Join(t.TempDir(), "does-not-exist"))
}
