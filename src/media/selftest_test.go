package media

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestSelfTestRefreshThrottle 验证真实自检的缓存与刷新节流：
// 首次执行必须标记 Cached=false；随后的 force refresh 在最小间隔内必须复用缓存，
// 避免多个调用方并发抢占 MPP 硬件。
func TestSelfTestRefreshThrottle(t *testing.T) {
	ffmpegPath := os.Getenv("RK3588_TEST_FFMPEG")
	if ffmpegPath == "" {
		path, err := exec.LookPath("ffmpeg")
		if err != nil {
			t.Skip("未找到 ffmpeg，跳过自检节流测试")
		}
		ffmpegPath = path
	}
	capability := Probe(context.Background(), ffmpegPath)
	if capability.Err != "" {
		t.Skipf("FFmpeg 不可用: %s", capability.Err)
	}
	if !capability.HasEncoder(DefaultHardwareCodec) {
		t.Skipf("当前 FFmpeg 缺少 %s，跳过自检节流测试", DefaultHardwareCodec)
	}

	// 清空缓存，保证下面的断言不受历史结果影响
	InvalidateSelfTestCache()

	ctx := context.Background()
	first := SelfTestCached(ctx, ffmpegPath, DefaultHardwareCodec, capability, true)
	if first == nil {
		t.Fatal("自检结果为空")
	}
	if first.Cached {
		t.Fatal("首次真实执行的结果必须标记 Cached=false")
	}

	// 最小刷新间隔内再次强制刷新：必须命中缓存而不是再次占用硬件
	start := time.Now()
	second := SelfTestCached(ctx, ffmpegPath, DefaultHardwareCodec, capability, true)
	if second == nil {
		t.Fatal("第二次自检结果为空")
	}
	if !second.Cached {
		t.Fatal("最小刷新间隔内的 refresh 必须复用缓存结果")
	}
	if elapsed := time.Since(start); elapsed > selfTestMinRefreshInterval {
		t.Fatalf("刷新被节流时应立即返回，实际耗时 %s", elapsed)
	}

	// 非 force 调用同样命中缓存
	third := SelfTestCached(ctx, ffmpegPath, DefaultHardwareCodec, capability, false)
	if third == nil || !third.Cached {
		t.Fatal("非强制调用应命中缓存")
	}
}

// TestSelfTestCancelledResultNotCached 取消导致的自检结果不得污染缓存
func TestSelfTestCancelledResultNotCached(t *testing.T) {
	ffmpegPath := os.Getenv("RK3588_TEST_FFMPEG")
	if ffmpegPath == "" {
		path, err := exec.LookPath("ffmpeg")
		if err != nil {
			t.Skip("未找到 ffmpeg，跳过取消自检测试")
		}
		ffmpegPath = path
	}
	capability := Probe(context.Background(), ffmpegPath)
	if capability.Err != "" || !capability.HasEncoder(DefaultHardwareCodec) {
		t.Skipf("当前 FFmpeg 不满足自检条件: %s", capability.Err)
	}

	InvalidateSelfTestCache()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelled := SelfTestCached(ctx, ffmpegPath, DefaultHardwareCodec, capability, true)
	if cancelled == nil {
		t.Fatal("自检结果为空")
	}
	if cancelled.Cached {
		t.Fatal("取消结果不应标记为缓存命中")
	}

	// 取消结果不得写入缓存：随后的正常同步自检必须真正执行并成功
	normal := SelfTestCached(context.Background(), ffmpegPath, DefaultHardwareCodec, capability, true)
	if normal.Cached {
		t.Fatal("取消结果污染了缓存：正常自检返回了缓存命中")
	}
	if !normal.OK {
		t.Fatalf("正常自检应成功: %s", normal.Message)
	}
}
