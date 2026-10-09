package stages

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/bililive-go/bililive-go/src/configs"
	"github.com/bililive-go/bililive-go/src/media"
	"github.com/bililive-go/bililive-go/src/pipeline"
	"github.com/bililive-go/bililive-go/src/pkg/livelogger"
)

// 本文件是"真实 FFmpeg 硬件链路"集成测试：只有在具备 MPP 硬编码（以及烧录所需的
// ass 滤镜）的 FFmpeg 上才会实际执行，否则自动跳过（宿主机形态的开发机通常会被跳过）。
//
// 运行方式（容器内或 RK3588 宿主机）：
//
//	RK3588_TEST_FFMPEG=/usr/local/bin/ffmpeg go test -tags dev -run Integration -v ./src/pipeline/stages/
//
// 未设置 RK3588_TEST_FFMPEG 时会尝试 PATH 中的 ffmpeg。

// resolveTestFFmpeg 返回集成测试使用的 FFmpeg
func resolveTestFFmpeg(t *testing.T) string {
	t.Helper()
	if path := strings.TrimSpace(os.Getenv("RK3588_TEST_FFMPEG")); path != "" {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("RK3588_TEST_FFMPEG 指向的 FFmpeg 不可用: %v", err)
		}
		return path
	}
	path, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("未找到 ffmpeg：设置 RK3588_TEST_FFMPEG 指向容器内 FFmpeg 后重跑")
	}
	return path
}

// requireCapability 确认当前 FFmpeg 具备测试所需能力，否则跳过
func requireCapability(t *testing.T, ffmpegPath, codec string, needAss bool) {
	t.Helper()
	capability := media.Probe(context.Background(), ffmpegPath)
	if capability.Err != "" {
		t.Skipf("FFmpeg 不可用，跳过集成测试: %s", capability.Err)
	}
	if !capability.HasEncoder(codec) {
		t.Skipf("当前 FFmpeg 缺少硬件编码器 %s（需 --enable-rkmpp），跳过集成测试", codec)
	}
	if needAss && !capability.HasFilter("ass") {
		t.Skip("当前 FFmpeg 缺少 ass 滤镜（需 libass），跳过弹幕烧录集成测试")
	}
}

// makeTestSource 生成带音轨的测试视频（mpeg4 编码，Rockchip FFmpeg 与官方 FFmpeg 都支持）
func makeTestSource(t *testing.T, ffmpegPath, path string) {
	t.Helper()
	args := []string{
		"-y", "-v", "error",
		"-f", "lavfi", "-i", "testsrc2=size=320x180:rate=30",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=2",
		"-t", "2",
		"-c:v", "mpeg4", "-q:v", "3",
		"-c:a", "aac",
		"-shortest",
		path,
	}
	cmd := exec.Command(ffmpegPath, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("生成测试视频失败: %v\n%s", err, output)
	}
}

// writeTestAss 写入一份最小可用的 ASS 弹幕字幕
func writeTestAss(t *testing.T, path string) {
	t.Helper()
	content := `[Script Info]
ScriptType: v4.00+
PlayResX: 320
PlayResY: 180

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Default,sans-serif,24,&H00FFFFFF,&H000000FF,&H00000000,&H64000000,0,0,0,0,100,100,0,0,1,2,0,2,10,10,10,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:00.00,0:00:02.00,Default,,0,0,0,,集成测试弹幕
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写入 ASS 失败: %v", err)
	}
}

// hardwareTestConfig 返回启用硬件处理的旧格式配置
func hardwareTestConfig(burn bool, compress bool) *configs.OnRecordFinished {
	return &configs.OnRecordFinished{
		FixFlvAtFirst:      false,
		BurnSubtitles:      burn,
		BurnSubtitlesCodec: "libx264", // 启用硬件后应被忽略，用于验证硬件路径确实生效
		RK3588: &configs.RK3588Config{
			Enable:         true,
			Compress:       compress,
			Codec:          media.CodecHEVCRkmpp,
			Bitrate:        "1M",
			DecodeFallback: true,
		},
	}
}

// testConfigForEnvironment 返回当前环境可用的测试配置：
// 有 ass 滤镜时测试"烧录 + 压缩"路径，否则退化为"纯压缩"路径，
// 使失败/取消/不覆盖等语义在任意 Rockchip 环境都能跑到真实硬件链路。
func testConfigForEnvironment(t *testing.T, ffmpegPath string) (*configs.OnRecordFinished, bool) {
	t.Helper()
	capability := media.Probe(context.Background(), ffmpegPath)
	burn := capability.HasFilter("ass")
	if !burn {
		t.Log("当前 FFmpeg 缺少 ass 滤镜，改用纯压缩路径验证失败/取消语义")
	}
	return hardwareTestConfig(burn, true), burn
}

// runPipeline 在真实 FFmpeg 上执行一次管线，返回各阶段结果
func runPipeline(t *testing.T, ctx context.Context, ffmpegPath string, config *pipeline.PipelineConfig, inputs ...string) ([]pipeline.StageResult, error) {
	t.Helper()
	executor := pipeline.NewExecutor(logrus.New())
	RegisterBuiltinStages(executor)
	return executeWith(t, ctx, executor, ffmpegPath, config, inputs...)
}

// executeWith 使用给定执行器运行管线（便于注入模拟失败阶段）
func executeWith(t *testing.T, ctx context.Context, executor *pipeline.Executor, ffmpegPath string, config *pipeline.PipelineConfig, inputs ...string) ([]pipeline.StageResult, error) {
	t.Helper()
	files := make([]pipeline.FileInfo, 0, len(inputs))
	for _, input := range inputs {
		files = append(files, pipeline.NewVideoFileInfo(input))
	}
	pipelineCtx := &pipeline.PipelineContext{
		Ctx: ctx,
		RecordInfo: pipeline.RecordInfo{
			Platform:    "集成测试",
			HostName:    "tester",
			RoomName:    "room",
			DisplayName: "integration",
			StartTime:   time.Now(),
		},
		Logger:     livelogger.New(livelogger.DefaultBufferSize, logrus.Fields{"test": "integration"}),
		FFmpegPath: ffmpegPath,
	}
	return executor.Execute(pipelineCtx, config, files, 0, nil, nil)
}

// assertHardwareCommands 断言阶段命令确实使用硬件处理链（而不是静默回退软件编码）
func assertHardwareCommands(t *testing.T, results []pipeline.StageResult) {
	t.Helper()
	if len(results) == 0 {
		t.Fatal("没有阶段结果")
	}
	commands := results[len(results)-1].Commands
	if len(commands) == 0 {
		t.Fatal("阶段未记录任何 FFmpeg 命令")
	}
	joined := strings.Join(commands, " ")
	if !strings.Contains(joined, media.CodecHEVCRkmpp) {
		t.Fatalf("命令未使用硬件编码器: %s", joined)
	}
	if !strings.Contains(joined, "-b:v") {
		t.Fatalf("硬件编码应使用码率控制: %s", joined)
	}
	if strings.Contains(joined, "-crf") || strings.Contains(joined, "-preset") {
		t.Fatalf("硬件编码不应出现 -crf/-preset: %s", joined)
	}
}

// assertOutputMedia 校验输出文件的视频编码、音轨与时长
func assertOutputMedia(t *testing.T, ctx context.Context, ffmpegPath, output, wantCodec string) media.MediaInfo {
	t.Helper()
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("输出文件不存在: %s (%v)", output, err)
	}
	info := media.ProbeMediaInfo(ctx, ffmpegPath, output)
	if info == nil {
		t.Fatalf("无法探测输出文件: %s", output)
	}
	if info.VideoCodec != wantCodec {
		t.Errorf("输出视频编码应为 %s，实际 %s", wantCodec, info.VideoCodec)
	}
	if !info.HasAudio {
		t.Error("输出文件丢失音轨")
	}
	if info.Duration < 1.5 || info.Duration > 2.5 {
		t.Errorf("输出时长异常: %.2fs", info.Duration)
	}
	// 帧必须可真实解码（含坏帧检测）
	if err := media.ValidateOutput(ctx, ffmpegPath, "", output); err != nil {
		t.Errorf("输出校验失败: %v", err)
	}
	return *info
}

// assertNoTempLeftovers 确认目录内没有残留的临时文件
func assertNoTempLeftovers(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取目录失败: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".bililive-") {
			t.Errorf("存在残留临时文件: %s", entry.Name())
		}
	}
}

// TestBurnSubtitlesIntegrationBurnAndCompress 有 ASS 弹幕：烧录 + 压缩一次完成
func TestBurnSubtitlesIntegrationBurnAndCompress(t *testing.T) {
	ffmpegPath := resolveTestFFmpeg(t)
	requireCapability(t, ffmpegPath, media.CodecHEVCRkmpp, true)

	ctx := context.Background()
	dir := t.TempDir()
	source := filepath.Join(dir, "burn.mkv")
	makeTestSource(t, ffmpegPath, source)
	assPath := filepath.Join(dir, "burn.ass")
	writeTestAss(t, assPath)

	config := pipeline.ConvertLegacyConfig(hardwareTestConfig(true, true))
	results, err := runPipeline(t, ctx, ffmpegPath, config, source)
	if err != nil {
		t.Fatalf("管线执行失败: %v", err)
	}
	assertHardwareCommands(t, results)

	output := filepath.Join(dir, "burn.burned.mkv")
	assertOutputMedia(t, ctx, ffmpegPath, output, "hevc")

	// 源视频与弹幕必须保留（delete_ass / delete_source 默认 false）
	if _, err := os.Stat(source); err != nil {
		t.Errorf("源视频不应被删除: %v", err)
	}
	if _, err := os.Stat(assPath); err != nil {
		t.Errorf("ASS 弹幕不应被删除: %v", err)
	}
	assertNoTempLeftovers(t, dir)
}

// TestBurnSubtitlesIntegrationCompressWithoutAss 无 ASS：独立 H.265 压缩
func TestBurnSubtitlesIntegrationCompressWithoutAss(t *testing.T) {
	ffmpegPath := resolveTestFFmpeg(t)
	requireCapability(t, ffmpegPath, media.CodecHEVCRkmpp, false)

	ctx := context.Background()
	dir := t.TempDir()
	source := filepath.Join(dir, "compress.mkv")
	makeTestSource(t, ffmpegPath, source)

	// 未开启烧录，仅启用"无弹幕压缩"：ConvertLegacyConfig 应自动注册处理阶段
	config := pipeline.ConvertLegacyConfig(hardwareTestConfig(false, true))
	if len(config.Stages) == 0 {
		t.Fatal("启用无弹幕压缩时必须注册处理阶段")
	}
	results, err := runPipeline(t, ctx, ffmpegPath, config, source)
	if err != nil {
		t.Fatalf("管线执行失败: %v", err)
	}
	assertHardwareCommands(t, results)

	output := filepath.Join(dir, "compress.h265.mkv")
	assertOutputMedia(t, ctx, ffmpegPath, output, "hevc")

	if _, err := os.Stat(source); err != nil {
		t.Errorf("源视频不应被删除: %v", err)
	}
	assertNoTempLeftovers(t, dir)
}

// failingStage 模拟后续阶段失败
type failingStage struct{}

func (s *failingStage) Name() string { return "always_fail" }

func (s *failingStage) Execute(ctx *pipeline.PipelineContext, input []pipeline.FileInfo) ([]pipeline.FileInfo, error) {
	return nil, fmt.Errorf("模拟后续阶段失败")
}

// TestBurnSubtitlesIntegrationLaterStageFailureKeepsSource 后续阶段失败时保留源视频与弹幕
func TestBurnSubtitlesIntegrationLaterStageFailureKeepsSource(t *testing.T) {
	ffmpegPath := resolveTestFFmpeg(t)
	requireCapability(t, ffmpegPath, media.CodecHEVCRkmpp, false)
	legacy, _ := testConfigForEnvironment(t, ffmpegPath)
	// 显式开启删除：删除必须延迟到整条管线成功之后，后续阶段失败时源文件与弹幕必须仍在
	legacy.BurnDeleteSource = true
	legacy.BurnDeleteAss = true

	ctx := context.Background()
	dir := t.TempDir()
	source := filepath.Join(dir, "fail.mkv")
	makeTestSource(t, ffmpegPath, source)
	assPath := filepath.Join(dir, "fail.ass")
	writeTestAss(t, assPath)

	executor := pipeline.NewExecutor(logrus.New())
	RegisterBuiltinStages(executor)
	executor.RegisterStage("always_fail", func(config pipeline.StageConfig) (pipeline.Stage, error) {
		return &failingStage{}, nil
	})

	config := pipeline.ConvertLegacyConfig(legacy)
	config.Stages = append(config.Stages, pipeline.StageConfig{Name: "always_fail"})

	if _, err := executeWith(t, ctx, executor, ffmpegPath, config, source); err == nil {
		t.Fatal("后续阶段失败时管线应返回错误")
	}

	if _, err := os.Stat(source); err != nil {
		t.Errorf("失败时源视频必须保留: %v", err)
	}
	if _, err := os.Stat(assPath); err != nil {
		t.Errorf("失败时 ASS 弹幕必须保留: %v", err)
	}
	assertNoTempLeftovers(t, dir)
}

// TestBurnSubtitlesIntegrationCancelKeepsSource 取消任务时保留源视频、弹幕且不留临时文件
func TestBurnSubtitlesIntegrationCancelKeepsSource(t *testing.T) {
	ffmpegPath := resolveTestFFmpeg(t)
	requireCapability(t, ffmpegPath, media.CodecHEVCRkmpp, false)
	legacy, burn := testConfigForEnvironment(t, ffmpegPath)

	dir := t.TempDir()
	source := filepath.Join(dir, "cancel.mkv")
	makeTestSource(t, ffmpegPath, source)
	assPath := filepath.Join(dir, "cancel.ass")
	writeTestAss(t, assPath)

	// 已取消的上下文：管线应立即返回取消错误，不产生任何输出。
	// 说明：这里验证的是"取消时不产生输出、不删源"；运行中取消（转码中途中断）
	// 需通过 API/任务管理器触发，属于本用例未覆盖的边界。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	config := pipeline.ConvertLegacyConfig(legacy)
	if _, err := runPipeline(t, ctx, ffmpegPath, config, source); err == nil {
		t.Fatal("取消后管线应返回错误")
	}
	_ = burn

	if _, err := os.Stat(source); err != nil {
		t.Errorf("取消时源视频必须保留: %v", err)
	}
	if _, err := os.Stat(assPath); err != nil {
		t.Errorf("取消时 ASS 弹幕必须保留: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "cancel.burned.mkv")); err == nil {
		t.Error("取消时不应产出结果文件")
	}
	assertNoTempLeftovers(t, dir)
}

// TestBurnSubtitlesIntegrationDoesNotOverwriteExistingOutput 已存在的输出文件不被覆盖
func TestBurnSubtitlesIntegrationDoesNotOverwriteExistingOutput(t *testing.T) {
	ffmpegPath := resolveTestFFmpeg(t)
	requireCapability(t, ffmpegPath, media.CodecHEVCRkmpp, false)
	legacy, burn := testConfigForEnvironment(t, ffmpegPath)

	ctx := context.Background()
	dir := t.TempDir()
	source := filepath.Join(dir, "keep.mkv")
	makeTestSource(t, ffmpegPath, source)
	assPath := filepath.Join(dir, "keep.ass")
	writeTestAss(t, assPath)

	// 预置一个"旧的输出文件"：烧录路径输出 keep.burned.mkv，纯压缩路径输出 keep.h265.mkv
	existingName := "keep.h265.mkv"
	if burn {
		existingName = "keep.burned.mkv"
	}
	existing := filepath.Join(dir, existingName)
	if err := os.WriteFile(existing, []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}

	config := pipeline.ConvertLegacyConfig(legacy)
	results, err := runPipeline(t, ctx, ffmpegPath, config, source)
	if err != nil {
		t.Fatalf("管线执行失败: %v", err)
	}
	assertHardwareCommands(t, results)

	old, err := os.ReadFile(existing)
	if err != nil || string(old) != "OLD" {
		t.Fatalf("已存在的输出文件被覆盖: %q %v", string(old), err)
	}
	assertOutputMedia(t, ctx, ffmpegPath, filepath.Join(dir, strings.Replace(existingName, ".mkv", ".1.mkv", 1)), "hevc")
	assertNoTempLeftovers(t, dir)
}

// TestBurnSubtitlesIntegrationCancelDoesNotPoisonCapabilityCache 回归测试：
// 取消中的任务不得把"探测失败/被取消的能力结果"写进共享缓存，否则后续任务会误报
// "当前 FFmpeg 不提供硬件编码器 hevc_rkmpp"（容器实测曾出现该问题）。
func TestBurnSubtitlesIntegrationCancelDoesNotPoisonCapabilityCache(t *testing.T) {
	ffmpegPath := resolveTestFFmpeg(t)
	requireCapability(t, ffmpegPath, media.CodecHEVCRkmpp, false)

	// 从空缓存开始，确保下面第一次任务的探测结果若有污染就会留在缓存里
	media.InvalidateCapabilityCache()

	dir := t.TempDir()
	source := filepath.Join(dir, "poison.mkv")
	makeTestSource(t, ffmpegPath, source)
	legacy, _ := testConfigForEnvironment(t, ffmpegPath)
	config := pipeline.ConvertLegacyConfig(legacy)

	// 用很短的超时模拟"运行中取消"（可能落在探测阶段，也可能落在转码阶段）
	cancelledCtx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := runPipeline(t, cancelledCtx, ffmpegPath, config, source); err == nil {
		t.Log("第一次任务在取消前已完成，仍继续验证缓存未被污染")
	}

	// 紧接着的正常任务必须成功使用硬件链路
	next := filepath.Join(dir, "next.mkv")
	makeTestSource(t, ffmpegPath, next)
	results, err := runPipeline(t, context.Background(), ffmpegPath, config, next)
	if err != nil {
		t.Fatalf("取消后的正常任务失败（能力缓存可能被取消的探测结果污染）: %v", err)
	}
	assertHardwareCommands(t, results)
	assertNoTempLeftovers(t, dir)
}
