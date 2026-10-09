package media

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// selfTestTTL 真实自检结果的缓存有效期。
// 自检会实际编码并解码若干帧，属于"完整运行"级别的探测，因此单独缓存且有效期更长：
// GET /api/hardware/status 默认复用缓存，只有显式 refresh=1 时才重新跑一遍。
const selfTestTTL = 10 * time.Minute

// selfTestStepTimeout 单个自检步骤的超时时间
const selfTestStepTimeout = 90 * time.Second

// selfTestMinRefreshInterval 两次真实自检之间的最小间隔。
// 真实自检会占用 MPP 硬件编解码单元，多个界面/调用方同时 refresh 会互相抢占，
// 因此该间隔内的 refresh 请求直接复用上一次结果。
const selfTestMinRefreshInterval = 15 * time.Second

// SelfTestStep 单个自检步骤的结果
type SelfTestStep struct {
	// Name 步骤名（encode/decode/hardware_decode）
	Name string `json:"name"`
	// OK 是否成功
	OK bool `json:"ok"`
	// Command 实际执行的命令行
	Command string `json:"command"`
	// Error 失败原因
	Error string `json:"error,omitempty"`
	// DurationMS 耗时（毫秒）
	DurationMS int64 `json:"duration_ms"`
}

// SelfTest 硬件编解码真实自检结果
type SelfTest struct {
	// OK 是否全部通过
	OK bool `json:"ok"`
	// Codec 被测试的编码器
	Codec string `json:"codec"`
	// Steps 各步骤结果
	Steps []SelfTestStep `json:"steps"`
	// Message 结论说明（中文）
	Message string `json:"message"`
	// TestedAt 自检时间
	TestedAt time.Time `json:"tested_at"`
	// Cached 是否为缓存结果
	Cached bool `json:"cached"`
}

type selfTestCacheEntry struct {
	result    *SelfTest
	expiresAt time.Time
}

// selfTestGate 串行化同一 FFmpeg/编码器的自检（singleflight），并记录上次真实执行时间。
// 锁会跨越整次自检（通常 1~3 秒，失败时最多数十秒），这是刻意的：
// 并发重复执行只会抢占硬件且结果相同，等待方随后直接命中缓存。
type selfTestGate struct {
	mu      sync.Mutex
	lastRun time.Time
}

var selfTestGates sync.Map // key → *selfTestGate

// selfTestGateFor 获取（或创建）指定 key 的自检闸门
func selfTestGateFor(key string) *selfTestGate {
	if value, ok := selfTestGates.Load(key); ok {
		return value.(*selfTestGate)
	}
	gate := &selfTestGate{}
	actual, _ := selfTestGates.LoadOrStore(key, gate)
	return actual.(*selfTestGate)
}

var (
	selfTestCacheMu sync.Mutex
	selfTestCache   = map[string]selfTestCacheEntry{}
)

// SelfTestCached 返回带缓存的硬件编解码自检结果。
//
// force 为 true 时忽略缓存重新执行，但仍受 selfTestMinRefreshInterval 与 singleflight 约束：
// 间隔内的重复 refresh、以及并发的重复调用，都直接复用已有结果，避免抢占 MPP 硬件。
// 返回对象只读，调用方不得修改。
func SelfTestCached(ctx context.Context, ffmpegPath, codec string, capability *Capability, force bool) *SelfTest {
	key := strings.TrimSpace(ffmpegPath) + "|" + strings.TrimSpace(codec)

	// 同一 key 串行执行：第二个并发调用会等待第一个完成后直接命中缓存
	gate := selfTestGateFor(key)
	gate.mu.Lock()
	defer gate.mu.Unlock()

	now := time.Now()
	selfTestCacheMu.Lock()
	entry, ok := selfTestCache[key]
	selfTestCacheMu.Unlock()

	if ok {
		withinMinInterval := !gate.lastRun.IsZero() && now.Sub(gate.lastRun) < selfTestMinRefreshInterval
		if !force || withinMinInterval || now.Before(entry.expiresAt) {
			return copySelfTestAsCached(entry.result)
		}
	}

	result := RunSelfTest(ctx, ffmpegPath, codec, capability)
	// 缓存中存的是完整结果；Cached 标记只在返回给调用方时设置，
	// 本次真实执行的结果必须标记为 Cached=false
	result.Cached = false

	// 上下文被取消时结果不可信（各步骤都会因取消而失败）：
	// 既不写入缓存、也不更新节流时间，保证后续任务能立即重新自检；
	// 若已有缓存则继续复用，避免把"取消失败"当成硬件结论。
	if ctx.Err() != nil {
		selfTestCacheMu.Lock()
		entry, ok := selfTestCache[key]
		selfTestCacheMu.Unlock()
		if ok && time.Now().Before(entry.expiresAt) {
			return copySelfTestAsCached(entry.result)
		}
		return result
	}

	gate.lastRun = time.Now()
	selfTestCacheMu.Lock()
	selfTestCache[key] = selfTestCacheEntry{result: result, expiresAt: now.Add(selfTestTTL)}
	selfTestCacheMu.Unlock()
	return result
}

// copySelfTestAsCached 返回结果的副本并标记为"缓存命中"（不修改缓存中的原对象）
func copySelfTestAsCached(result *SelfTest) *SelfTest {
	if result == nil {
		return nil
	}
	cached := *result
	cached.Cached = true
	return &cached
}

// InvalidateSelfTestCache 清空自检缓存
func InvalidateSelfTestCache() {
	selfTestCacheMu.Lock()
	selfTestCache = map[string]selfTestCacheEntry{}
	selfTestCacheMu.Unlock()
}

// ResolveSelfTestCodec 返回应当用于自检的编码器：
// 配置的编码器本身是硬件编码器时直接使用，否则回落到默认硬件编码器，
// 以便界面在"未启用硬件处理"时仍能如实反映设备能力。
func ResolveSelfTestCodec(codec string) string {
	if IsHardwareEncoder(codec) {
		return strings.TrimSpace(codec)
	}
	return DefaultHardwareCodec
}

// RunSelfTest 真实执行"编码 → 解码"自检，确认硬件链路确实可用。
// 探测即完整运行：只列出编码器列表不足以证明硬件可用（设备权限、驱动、内存分配都可能失败）。
func RunSelfTest(ctx context.Context, ffmpegPath, codec string, capability *Capability) *SelfTest {
	codec = ResolveSelfTestCodec(codec)
	result := &SelfTest{
		Codec:    codec,
		Steps:    []SelfTestStep{},
		TestedAt: time.Now(),
	}

	if strings.TrimSpace(ffmpegPath) == "" {
		result.Message = "未找到可用的 FFmpeg，无法执行硬件自检"
		return result
	}
	if !IsHardwareEncoder(codec) {
		result.Message = fmt.Sprintf("编码器 %s 不是硬件编码器，跳过硬件自检", codec)
		return result
	}
	if capability != nil && !capability.HasEncoder(codec) {
		result.Message = fmt.Sprintf("当前 FFmpeg 不提供编码器 %s", codec)
		return result
	}

	tempDir, err := os.MkdirTemp("", "bililive-selftest-")
	if err != nil {
		result.Message = "创建自检临时目录失败: " + err.Error()
		return result
	}
	defer os.RemoveAll(tempDir)

	samplePath := filepath.Join(tempDir, "sample"+OutputExtension)

	encodeArgs := []string{
		"-hide_banner", "-v", "error",
		"-f", "lavfi", "-i", "testsrc2=size=320x180:rate=30",
		"-frames:v", "30",
		"-c:v", codec,
		"-b:v", DefaultBitrate,
		"-f", "matroska",
		samplePath,
	}
	encodeStep := runSelfTestStep(ctx, ffmpegPath, "encode", encodeArgs)
	result.Steps = append(result.Steps, encodeStep)

	if encodeStep.OK {
		if info, statErr := os.Stat(samplePath); statErr != nil || info.Size() == 0 {
			encodeStep.OK = false
			encodeStep.Error = "编码输出为空"
			result.Steps[len(result.Steps)-1] = encodeStep
		}
	}

	if encodeStep.OK {
		decodeArgs := []string{"-hide_banner", "-v", "error", "-i", samplePath, "-f", "null", "-"}
		result.Steps = append(result.Steps, runSelfTestStep(ctx, ffmpegPath, "decode", decodeArgs))

		// 只有能力信息确认存在对应硬解器时才测试硬解，避免把"未编译硬解"误报成失败
		decoder := hardwareDecoderName(strings.TrimSuffix(codec, "_rkmpp"))
		if capability != nil && capability.HasHwAccel(HardwareAccelName) && capability.HasDecoder(decoder) {
			hardwareDecodeArgs := []string{
				"-hide_banner", "-v", "error",
				"-hwaccel", HardwareAccelName,
				"-hwaccel_output_format", HardwarePixelFormat,
				"-i", samplePath,
				"-f", "null", "-",
			}
			result.Steps = append(result.Steps, runSelfTestStep(ctx, ffmpegPath, "hardware_decode", hardwareDecodeArgs))
		}
	}

	result.OK = len(result.Steps) > 0
	for _, step := range result.Steps {
		if !step.OK {
			result.OK = false
			if result.Message == "" {
				result.Message = fmt.Sprintf("自检步骤 %s 失败: %s", step.Name, step.Error)
			}
		}
	}
	if result.OK {
		result.Message = fmt.Sprintf("硬件编解码自检通过（编码器 %s）", codec)
	}
	return result
}

// runSelfTestStep 执行单个自检步骤
func runSelfTestStep(ctx context.Context, ffmpegPath, name string, args []string) SelfTestStep {
	step := SelfTestStep{
		Name:    name,
		Command: ffmpegPath + " " + strings.Join(args, " "),
	}
	stepCtx, cancel := context.WithTimeout(ctx, selfTestStepTimeout)
	defer cancel()

	startedAt := time.Now()
	cmd := exec.CommandContext(stepCtx, ffmpegPath, args...)
	output, err := cmd.CombinedOutput()
	step.DurationMS = time.Since(startedAt).Milliseconds()
	if err != nil {
		step.OK = false
		reason := err.Error()
		if stepCtx.Err() == context.DeadlineExceeded {
			reason = "执行超时"
		}
		if tail := tailString(string(output), 800); tail != "" {
			reason += "；stderr: " + tail
		}
		step.Error = reason
		return step
	}
	step.OK = true
	return step
}
