package recorder

import (
	"strings"
	"testing"

	"cyannvr/server/config"
	"cyannvr/server/models"
)

// 回归：1.10.28 把 -hwaccel 等解码参数拼到 -i pipe:0 之后，
// ffmpeg 报 "Option hwaccel cannot be applied to output" 导致生产 crash loop。
// 本测试锁死顺序契约：解码参数必须全部位于 -i 之前。
func TestRtspInputArgsDecodeBeforeInput(t *testing.T) {
	w := &Worker{dev: &models.Device{Source: models.SourceRTSP}}
	args := w.rtspInputArgs("-hwaccel", "cuda")

	// 找到 -i 的位置
	iIdx := -1
	for i, a := range args {
		if a == "-i" {
			iIdx = i
			break
		}
	}
	if iIdx < 0 {
		t.Fatalf("args 缺少 -i: %v", args)
	}
	pipeTarget := args[iIdx+1]
	if pipeTarget != "pipe:0" {
		t.Errorf("-i 的目标是 %q, 期望 pipe:0", pipeTarget)
	}

	// 解码参数必须全部在 -i 之前
	for i, a := range args {
		if a == "-hwaccel" && i > iIdx {
			t.Errorf("-hwaccel 出现在 -i 之后（位置 %d > %d）: %v\n这是 1.10.28 的生产事故形态", i, iIdx, args)
		}
	}

	// whitelist 仍要在
	if strings.Join(args, " ") != strings.Join(args, " ") {
		t.Error("unreachable")
	}
	full := strings.Join(args, " ")
	if !strings.Contains(full, "-protocol_whitelist") || !strings.Contains(full, "concat") {
		t.Errorf("缺少 concat/whitelist 参数: %v", args)
	}
}

// 同契约约束 recordInputArgs（录像路径与预览共用 rtspInputArgs，但显式双保险）
func TestRecordInputArgsDecodeBeforeInput(t *testing.T) {
	w := &Worker{dev: &models.Device{Source: models.SourceRTSP, Port: 554}, mgr: &Manager{cfg: &config.Config{Ffmpeg: "/nonexistent-ffmpeg"}}}
	args := w.recordInputArgs()
	iIdx := -1
	for i, a := range args {
		if a == "-i" {
			iIdx = i
			break
		}
	}
	if iIdx < 0 {
		t.Fatalf("record args 缺少 -i: %v", args)
	}
	for i, a := range args {
		if a == "-hwaccel" && i > iIdx {
			t.Errorf("record 路径 -hwaccel 在 -i 之后: %v", args)
		}
	}
}
