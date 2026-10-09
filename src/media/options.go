// Package media 封装音视频处理链的能力探测、参数生成与执行。
//
// 设计约束：
//   - 本包不依赖 pipeline / tools 包，避免 tools 与 pipeline 之间形成循环依赖；
//   - 能力探测只依据"当前配置指定的 FFmpeg 可执行文件"，并在需要时执行真实的编解码自检，
//     不做任何能力假设，避免向界面虚报硬件能力；
//   - 默认只使用硬件编码：硬件编码器不可用时直接失败并保留源文件，绝不静默回退到软件编码。
package media

import (
	"fmt"
	"strings"

	"github.com/bililive-go/bililive-go/src/configs"
)

const (
	// CodecHEVCRkmpp Rockchip MPP 的 H.265 硬件编码器
	CodecHEVCRkmpp = "hevc_rkmpp"
	// CodecH264Rkmpp Rockchip MPP 的 H.264 硬件编码器
	CodecH264Rkmpp = "h264_rkmpp"
	// CodecMjpegRkmpp Rockchip MPP 的 JPEG 硬件编码器（封面用）
	CodecMjpegRkmpp = "mjpeg_rkmpp"
	// DefaultHardwareCodec 默认硬件编码器（同画质下 H.265 体积更小）
	DefaultHardwareCodec = CodecHEVCRkmpp
	// DefaultSoftwareCodec 默认软件编码器（未启用硬件处理时保持历史行为）
	DefaultSoftwareCodec = "libx264"
	// DefaultBitrate 默认目标码率
	DefaultBitrate = "4M"
	// DefaultCRF 软件编码默认质量值
	DefaultCRF = "18"
	// DefaultPreset 软件编码默认预设
	DefaultPreset = "medium"
	// OutputExtension 硬件处理的输出容器扩展名
	// matroska 对 H.265 视频流与音频流复制最稳妥，且不会覆盖原始容器
	OutputExtension = ".mkv"
	// HardwareAccelName rkmpp 硬件解码所使用的 hwaccel 名称
	HardwareAccelName = "rkmpp"
	// HardwarePixelFormat rkmpp 硬解帧输出格式
	HardwarePixelFormat = "drm_prime"
)

// hardwareEncoderSuffixes 常见硬件编码器的后缀，用于判断编码器是否走硬件路径
var hardwareEncoderSuffixes = []string{
	"_rkmpp", "_nvenc", "_vaapi", "_v4l2m2m", "_qsv", "_amf", "_videotoolbox", "_mediacodec", "_vulkan",
}

// hardwareDecodableCodecs 可由 rkmpp 硬件解码的输入编码（与 ffmpeg-rockchip 提供的
// `*_rkmpp` 解码器一一对应），用于在能力探测结果中查找对应解码器
var hardwareDecodableCodecs = []string{
	"h264", "hevc", "vp8", "vp9", "av1", "mpeg1video", "mpeg2video", "mpeg4", "h263", "mjpeg",
}

// IsHardwareEncoder 判断编码器名是否为硬件编码器
func IsHardwareEncoder(codec string) bool {
	name := strings.ToLower(strings.TrimSpace(codec))
	if name == "" {
		return false
	}
	for _, suffix := range hardwareEncoderSuffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// IsRockchipEncoder 判断编码器名是否为 Rockchip MPP 硬件编码器
func IsRockchipEncoder(codec string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(codec)), "_rkmpp")
}

// Options 硬件处理参数。既可由配置映射而来，也可独立构造（便于测试与接口复用）
type Options struct {
	// Enabled 是否启用 RK3588 硬件处理
	Enabled bool
	// Compress 无弹幕时是否仍压缩为 H.265
	Compress bool
	// Codec 视频编码器
	Codec string
	// Bitrate 目标码率（硬件编码使用，形如 4M / 4000k）
	Bitrate string
	// CRF 软件编码质量值（仅软件编码器使用）
	CRF string
	// Preset 软件编码预设（仅软件编码器使用）
	Preset string
	// DecodeFallback 硬件解码不可用时是否允许软件解码（编码始终不回落软件编码）
	DecodeFallback bool
	// ScaleWidth 缩放目标宽度，0 表示不缩放
	ScaleWidth int
	// ScaleHeight 缩放目标高度，0 表示不缩放
	ScaleHeight int
}

// defaultOptions 返回软件编码的默认参数
func defaultOptions() Options {
	return Options{
		Codec:          DefaultSoftwareCodec,
		Bitrate:        DefaultBitrate,
		CRF:            DefaultCRF,
		Preset:         DefaultPreset,
		DecodeFallback: true,
	}
}

// OptionsFromConfig 把录制完成后的动作配置映射为硬件处理参数。
// 未启用硬件处理时沿用 burn_subtitles_codec 等历史字段，保证既有部署行为不变。
func OptionsFromConfig(orf *configs.OnRecordFinished) Options {
	if orf == nil {
		return defaultOptions()
	}

	// rk3588 是指针字段：nil 表示未配置（等价于未启用硬件处理）
	rk := orf.RK3588
	codec := ""
	bitrate := ""
	scaleWidth := 0
	scaleHeight := 0
	if rk != nil {
		codec = rk.Codec
		bitrate = rk.Bitrate
		scaleWidth = rk.ScaleWidth
		scaleHeight = rk.ScaleHeight
	}
	opts := Options{
		Enabled: rk.IsEnabled(),
		// 仅"启用硬件处理且开启压缩"时才做无弹幕压缩
		Compress:       rk.SupportsCompress(),
		Codec:          codec,
		Bitrate:        bitrate,
		CRF:            orf.BurnSubtitlesCrf,
		Preset:         orf.BurnSubtitlesPreset,
		DecodeFallback: rk.DecodeFallbackEnabled(),
		ScaleWidth:     scaleWidth,
		ScaleHeight:    scaleHeight,
	}
	if !opts.Enabled {
		// 未启用硬件处理：编码器完全沿用旧配置（默认 libx264），不引入任何行为变化
		if codec := strings.TrimSpace(orf.BurnSubtitlesCodec); codec != "" {
			opts.Codec = codec
		} else {
			opts.Codec = DefaultSoftwareCodec
		}
	}
	return opts.WithDefaults()
}

// WithDefaults 补齐空字段。注意 DecodeFallback 不在此处强制为 true，
// 以便显式配置 false 的用户能够关闭软件解码回退（默认值由配置结构体给出）。
func (o Options) WithDefaults() Options {
	if strings.TrimSpace(o.Codec) == "" {
		if o.Enabled {
			o.Codec = DefaultHardwareCodec
		} else {
			o.Codec = DefaultSoftwareCodec
		}
	}
	if strings.TrimSpace(o.Bitrate) == "" {
		o.Bitrate = DefaultBitrate
	}
	if strings.TrimSpace(o.CRF) == "" {
		o.CRF = DefaultCRF
	}
	if strings.TrimSpace(o.Preset) == "" {
		o.Preset = DefaultPreset
	}
	return o
}

// UsesHardware 返回当前参数是否走硬件编码路径
func (o Options) UsesHardware() bool {
	return IsHardwareEncoder(o.Codec)
}

// ScaleEnabled 返回是否配置了缩放
func (o Options) ScaleEnabled() bool {
	return o.ScaleWidth > 0 && o.ScaleHeight > 0
}

// Validate 校验参数。未启用硬件处理时只做宽松校验，
// 避免历史配置（软件编码器 + CRF）在配置更新时被拒绝。
func (o Options) Validate() error {
	codec := strings.TrimSpace(o.Codec)
	if codec == "" {
		return fmt.Errorf("视频编码器不能为空")
	}
	if o.Enabled {
		if !IsRockchipEncoder(codec) {
			return fmt.Errorf("启用 RK3588 硬件处理时编码器必须为 %s 或 %s，当前为 %s",
				CodecHEVCRkmpp, CodecH264Rkmpp, codec)
		}
		if _, err := configs.ParseBitrate(o.Bitrate); err != nil {
			return err
		}
	}
	if err := configs.ValidateScaleSize(o.ScaleWidth, o.ScaleHeight); err != nil {
		return err
	}
	return nil
}

// SupportsHardwareDecodeByCodec 在不掌握 FFmpeg 能力信息时，判断某输入编码理论上
// 是否存在对应的 rkmpp 硬件解码器（用于能力探测失败时的保守判断）
func SupportsHardwareDecodeByCodec(codec string) bool {
	name := strings.ToLower(strings.TrimSpace(codec))
	if name == "" {
		return false
	}
	for _, candidate := range hardwareDecodableCodecs {
		if candidate == name {
			return true
		}
	}
	return false
}

// hardwareDecoderName 返回输入编码对应的 rkmpp 解码器名
func hardwareDecoderName(codec string) string {
	name := strings.ToLower(strings.TrimSpace(codec))
	if name == "" {
		return ""
	}
	return name + "_rkmpp"
}
