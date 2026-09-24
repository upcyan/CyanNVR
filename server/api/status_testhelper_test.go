//go:build !windows

package api

import (
	"cyannvr/server/config"
)

// newStatusTestConfig 构造状态接口测试用的最小配置。
// 放在 _test 文件里，避免污染生产代码。
func newStatusTestConfig(dataDir string) *config.Config {
	return &config.Config{DataDir: dataDir, MinFreeMB: 512}
}
