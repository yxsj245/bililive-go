package media

import (
	"context"
	"strings"
	"time"
)

// Status 是 GET /api/hardware/status 返回的硬件处理状态。
// 所有能力字段都来自"当前配置指定的 FFmpeg"的真实探测结果，不做任何假设。
type Status struct {
	// Enabled 是否在配置中启用了 RK3588 硬件处理
	Enabled bool `json:"enabled"`
	// FFmpegPath 当前实际使用的 FFmpeg 路径
	FFmpegPath string `json:"ffmpeg_path"`
	// Encoder 配置中指定的视频编码器
	Encoder string `json:"encoder"`
	// Encoders 当前 FFmpeg 提供的硬件编码器
	Encoders []string `json:"encoders"`
	// Decoders 当前 FFmpeg 提供的硬件解码器
	Decoders []string `json:"decoders"`
	// Filters 处理链需要且当前 FFmpeg 实际具备的滤镜
	Filters []string `json:"filters"`
	// Devices 相关设备节点是否存在
	Devices map[string]bool `json:"devices"`
	// Warnings 能力缺口与降级说明（中文）
	Warnings []string `json:"warnings"`
	// Available 硬件编码是否真正可用（以真实编解码自检结果为准）
	Available bool `json:"available"`
	// Ready 是否已启用且硬件确实可用
	Ready bool `json:"ready"`

	// 以下为便于界面展示的扩展字段
	// Compress 无弹幕时是否压缩
	Compress bool `json:"compress"`
	// Bitrate 目标码率
	Bitrate string `json:"bitrate"`
	// DecodeFallback 硬解不可用时是否允许软件解码
	DecodeFallback bool `json:"decode_fallback"`
	// ScaleWidth 缩放目标宽度
	ScaleWidth int `json:"scale_width"`
	// ScaleHeight 缩放目标高度
	ScaleHeight int `json:"scale_height"`
	// FFmpegVersion FFmpeg 版本
	FFmpegVersion string `json:"ffmpeg_version,omitempty"`
	// HwAccels 当前 FFmpeg 支持的硬件加速方式
	HwAccels []string `json:"hwaccels"`
	// Chain 预计采用的处理链描述
	Chain string `json:"chain,omitempty"`
	// SelfTest 真实编解码自检结果
	SelfTest *SelfTest `json:"self_test,omitempty"`
	// ProbedAt 本次状态生成时间
	ProbedAt time.Time `json:"probed_at"`
}

// BuildStatus 生成硬件处理状态。
// forceRefresh 为 true 时重新探测能力并重新执行真实自检，否则使用缓存结果。
func BuildStatus(ctx context.Context, ffmpegPath string, opts Options, forceRefresh bool) *Status {
	opts = opts.WithDefaults()
	status := &Status{
		Enabled:        opts.Enabled,
		FFmpegPath:     ffmpegPath,
		Encoder:        opts.Codec,
		Compress:       opts.Compress,
		Bitrate:        opts.Bitrate,
		DecodeFallback: opts.DecodeFallback,
		ScaleWidth:     opts.ScaleWidth,
		ScaleHeight:    opts.ScaleHeight,
		Encoders:       []string{},
		Decoders:       []string{},
		Filters:        []string{},
		HwAccels:       []string{},
		Devices:        map[string]bool{},
		Warnings:       []string{},
		ProbedAt:       time.Now(),
	}

	if err := opts.Validate(); err != nil {
		status.Warnings = append(status.Warnings, "硬件处理配置无效: "+err.Error())
	}
	if !opts.Enabled {
		status.Warnings = append(status.Warnings,
			"未启用 RK3588 硬件处理（on_record_finished.rk3588.enable=false），后处理仍使用原有软件编码路径")
	}

	if strings.TrimSpace(ffmpegPath) == "" {
		status.Warnings = append(status.Warnings, "未找到可用的 FFmpeg，无法探测硬件能力")
		return status
	}

	capability := ProbeCached(ctx, ffmpegPath, forceRefresh)
	status.FFmpegVersion = capability.Version
	status.HwAccels = nonNilStrings(capability.HwAccels)
	status.Encoders = nonNilStrings(capability.HardwareEncoders)
	status.Decoders = nonNilStrings(capability.HardwareDecoders)
	status.Filters = nonNilStrings(capability.KeyFilters)
	if capability.Devices != nil {
		status.Devices = capability.Devices
	}
	status.Warnings = append(status.Warnings, capability.Warnings...)
	if capability.Err != "" {
		status.Warnings = append(status.Warnings, capability.Err)
	}

	selfTestCodec := ResolveSelfTestCodec(opts.Codec)
	if !capability.HasEncoder(selfTestCodec) {
		status.Warnings = append(status.Warnings,
			"当前 FFmpeg 不提供硬件编码器 "+selfTestCodec+"（需编译 --enable-rkmpp）")
	}

	// 真实自检独立缓存：默认不重复执行，只有 refresh=1 时才重新跑完整编码/解码
	status.SelfTest = SelfTestCached(ctx, ffmpegPath, selfTestCodec, capability, forceRefresh)
	status.Available = status.SelfTest != nil && status.SelfTest.OK
	if status.SelfTest != nil && !status.SelfTest.OK && status.SelfTest.Message != "" {
		status.Warnings = append(status.Warnings, status.SelfTest.Message)
	}
	status.Ready = status.Enabled && status.Available
	status.Chain = DescribeChain(capability, opts, capability.HasFilter("ass"))
	status.Warnings = dedupeStrings(status.Warnings)
	return status
}

// DescribeChain 描述预计采用的处理链，便于界面直观展示当前配置的实际效果
func DescribeChain(capability *Capability, opts Options, burnPossible bool) string {
	opts = opts.WithDefaults()
	steps := make([]string, 0, 6)

	if capability != nil && capability.HasHwAccel(HardwareAccelName) && len(capability.HardwareDecoders) > 0 {
		steps = append(steps, "硬解(rkmpp/"+HardwarePixelFormat+")")
	} else {
		steps = append(steps, "软件解码")
	}
	if opts.ScaleEnabled() {
		if capability != nil && capability.HasFilter("scale_rkrga") {
			steps = append(steps, "RGA缩放")
		} else {
			steps = append(steps, "软件缩放")
		}
	}
	if opts.Compress {
		steps = append(steps, "无弹幕压缩")
	}
	if burnPossible {
		steps = append(steps, "弹幕烧录(ass，CPU 绘制)")
	}
	if opts.UsesHardware() {
		steps = append(steps, "硬件编码("+opts.Codec+" "+opts.Bitrate+")")
	} else {
		steps = append(steps, "软件编码("+opts.Codec+")")
	}
	return strings.Join(steps, " → ")
}

// nonNilStrings 保证 JSON 输出为空数组而不是 null
func nonNilStrings(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}
