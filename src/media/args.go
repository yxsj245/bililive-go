package media

import (
	"fmt"
	"strings"
)

// Request 描述一次"弹幕烧录 / 压缩"的处理需求
type Request struct {
	// FFmpegPath FFmpeg 可执行文件路径
	FFmpegPath string
	// Input 输入视频文件
	Input string
	// Output 输出文件（通常是同目录下的临时文件，校验通过后再原子发布）
	Output string
	// AssPath 弹幕字幕路径，为空表示本次只压缩不烧录
	AssPath string
	// Options 处理参数
	Options Options
	// Capability 能力探测结果，可为 nil（表示未知，此时按保守策略生成参数）
	Capability *Capability
	// InputCodec 输入视频编码（h264/hevc/...），用于判断能否硬件解码
	InputCodec string
	// ForceSoftwareDecode 强制软件解码（硬件解码失败后的重试路径）
	ForceSoftwareDecode bool
}

// Plan 描述最终采用的命令、处理链与降级情况
type Plan struct {
	// HardwareEncode 是否使用硬件编码
	HardwareEncode bool `json:"hardware_encode"`
	// HardwareDecode 是否使用硬件解码
	HardwareDecode bool `json:"hardware_decode"`
	// RGA 是否使用 RGA 做缩放/格式转换
	RGA bool `json:"rga"`
	// Burn 本次是否烧录弹幕
	Burn bool `json:"burn"`
	// Codec 视频编码器
	Codec string `json:"codec"`
	// Bitrate 目标码率
	Bitrate string `json:"bitrate"`
	// CRF 软件编码质量值
	CRF string `json:"crf,omitempty"`
	// Preset 软件编码预设
	Preset string `json:"preset,omitempty"`
	// FilterChain 滤镜链
	FilterChain string `json:"filter_chain,omitempty"`
	// Args FFmpeg 参数
	Args []string `json:"args"`
	// Warnings 降级说明（如软件解码回退）
	Warnings []string `json:"warnings,omitempty"`
}

// Summary 返回人读的处理链描述
func (p *Plan) Summary() string {
	if p == nil {
		return ""
	}
	steps := make([]string, 0, 6)
	if p.HardwareDecode {
		steps = append(steps, "硬件解码("+HardwareAccelName+"/"+HardwarePixelFormat+")")
	} else {
		steps = append(steps, "软件解码")
	}
	if p.RGA {
		steps = append(steps, "RGA缩放/格式转换")
	}
	if p.Burn {
		steps = append(steps, "弹幕烧录")
	}
	if p.HardwareEncode {
		steps = append(steps, "硬件编码("+p.Codec+", "+p.Bitrate+")")
	} else {
		steps = append(steps, "软件编码("+p.Codec+")")
	}
	return strings.Join(steps, " → ")
}

// BuildPlan 依据能力探测结果生成 FFmpeg 参数。
//
// 关键规则：
//   - 硬件编码器不可用时直接报错，绝不回退到软件编码（软编会打满 CPU）；
//   - 解码优先硬解，硬解不可用时按 decode_fallback 决定是否允许软件解码；
//   - 硬解链路上需要缩放或格式转换时优先使用 RGA（含 10bit → nv12 的格式转换），
//     RGA 不可用或走软解时回退到软件缩放；
//   - 弹幕烧录需要 CPU 帧，因此在烧录前用 hwdownload 把硬解帧下载回内存。
func BuildPlan(req Request) (*Plan, error) {
	opts := req.Options.WithDefaults()
	plan := &Plan{
		Codec:   opts.Codec,
		Bitrate: opts.Bitrate,
		CRF:     opts.CRF,
		Preset:  opts.Preset,
		Burn:    strings.TrimSpace(req.AssPath) != "",
	}
	cap := req.Capability

	hardwareEncode := IsHardwareEncoder(opts.Codec)
	plan.HardwareEncode = hardwareEncode

	var filters []string
	hardwareDecode := false

	if hardwareEncode {
		if cap != nil && !cap.HasEncoder(opts.Codec) {
			return nil, fmt.Errorf("当前 FFmpeg（%s）不提供硬件编码器 %s；为避免回退到软件编码已中止处理，"+
				"请检查容器内 FFmpeg 是否编译了 --enable-rkmpp", cap.FFmpegPath, opts.Codec)
		}

		switch {
		case req.ForceSoftwareDecode:
			plan.Warnings = append(plan.Warnings, "按重试策略改用软件解码")
		case cap == nil:
			// 未探测到能力时按保守策略：先按编码类型推断，失败后由调用方重试软件解码
			hardwareDecode = SupportsHardwareDecodeByCodec(req.InputCodec)
			if !hardwareDecode && !opts.DecodeFallback {
				return nil, fmt.Errorf("无法确认输入编码 %q 的硬件解码能力，且配置禁止软件解码回退", req.InputCodec)
			}
			plan.Warnings = append(plan.Warnings, "未获取到 FFmpeg 能力信息，硬件解码结果以实际执行情况为准")
		default:
			hardwareDecode = cap.SupportsHardwareDecode(req.InputCodec)
			if !hardwareDecode {
				if !opts.DecodeFallback {
					return nil, fmt.Errorf("输入编码 %q 无法硬件解码，且配置禁止软件解码回退（decode_fallback=false）", req.InputCodec)
				}
				plan.Warnings = append(plan.Warnings, fmt.Sprintf("输入编码 %q 无可用硬件解码器，改用软件解码", req.InputCodec))
			}
		}
	} else {
		plan.Warnings = append(plan.Warnings,
			fmt.Sprintf("编码器 %s 为软件编码器：仅在显式配置时使用，本次不启用硬件处理链", opts.Codec))
		if req.ForceSoftwareDecode {
			plan.Warnings = append(plan.Warnings, "按重试策略改用软件解码")
		}
	}
	plan.HardwareDecode = hardwareDecode

	// 滤镜链：RGA 缩放/格式转换 → 硬解帧下载 → 弹幕烧录
	needCPUTransfer := plan.Burn // 烧录弹幕必须在 CPU 帧上绘制
	rgaAvailable := cap == nil || cap.HasFilter("scale_rkrga")

	if hardwareDecode {
		if opts.ScaleEnabled() {
			if rgaAvailable {
				filters = append(filters, fmt.Sprintf("scale_rkrga=w=%d:h=%d:format=nv12", opts.ScaleWidth, opts.ScaleHeight))
				plan.RGA = true
			} else {
				plan.Warnings = append(plan.Warnings, "当前 FFmpeg 缺少 scale_rkrga（RGA）滤镜，缩放回退到软件实现")
			}
		} else if needCPUTransfer && rgaAvailable {
			// 只做格式转换（含 10bit → nv12）也优先交给 RGA，避免 libswscale 处理 DRM 帧失败
			filters = append(filters, "scale_rkrga=format=nv12")
			plan.RGA = true
		}

		if needCPUTransfer || (opts.ScaleEnabled() && !plan.RGA) {
			filters = append(filters, "hwdownload", "format=nv12")
		}
		if opts.ScaleEnabled() && !plan.RGA {
			filters = append(filters, fmt.Sprintf("scale=%d:%d", opts.ScaleWidth, opts.ScaleHeight), "format=nv12")
		}
	} else if opts.ScaleEnabled() {
		filters = append(filters, fmt.Sprintf("scale=%d:%d", opts.ScaleWidth, opts.ScaleHeight), "format=nv12")
	}

	if plan.Burn {
		if cap != nil && !cap.HasFilter("ass") {
			return nil, fmt.Errorf("当前 FFmpeg（%s）缺少 ass 滤镜，无法烧录弹幕；"+
				"需要编译 libass 并安装中文字体", cap.FFmpegPath)
		}
		filters = append(filters, "ass="+EscapeAssPath(req.AssPath))
	}
	plan.FilterChain = strings.Join(filters, ",")

	args := make([]string, 0, 24)
	if hardwareDecode {
		args = append(args,
			"-hwaccel", HardwareAccelName,
			"-hwaccel_output_format", HardwarePixelFormat,
		)
	}
	args = append(args, "-i", req.Input)
	if plan.FilterChain != "" {
		args = append(args, "-vf", plan.FilterChain)
	}
	args = append(args, "-c:v", opts.Codec)
	if hardwareEncode {
		// MPP 编码器不支持 -crf / -preset，必须使用码率控制
		args = append(args, "-b:v", opts.Bitrate)
	} else {
		args = append(args, "-crf", opts.CRF, "-preset", opts.Preset)
	}
	args = append(args,
		"-c:a", "copy", // 音频直接流复制，不做二次编码
		"-map", "0:v:0", // 只取第一条视频流，避免同容器内的封面/数据流混入
		"-map", "0:a?", // 保留全部音轨（没有音轨时不报错）
		"-y",
		"-nostdin",
		"-progress", "pipe:1",
		req.Output,
	)
	plan.Args = args
	return plan, nil
}

// CoverRequest 描述一次封面提取需求
type CoverRequest struct {
	// FFmpegPath FFmpeg 可执行文件路径
	FFmpegPath string
	// Input 输入视频
	Input string
	// Output 输出 JPEG（通常是临时文件）
	Output string
	// InputCodec 输入视频编码
	InputCodec string
	// Capability 能力探测结果，可为 nil
	Capability *Capability
}

// CoverPlan 描述封面提取采用的命令
type CoverPlan struct {
	// HardwareDecode 是否使用硬件解码
	HardwareDecode bool `json:"hardware_decode"`
	// HardwareEncode 是否使用 MPP 硬件 JPEG 编码（mjpeg_rkmpp）
	HardwareEncode bool `json:"hardware_encode"`
	// Args FFmpeg 参数
	Args []string `json:"args"`
	// Warnings 降级说明
	Warnings []string `json:"warnings,omitempty"`
}

// BuildCoverPlan 生成封面提取参数：优先"硬解 + mjpeg_rkmpp 硬件 JPEG 编码"。
// MPP 编码器同样接受 CPU 帧（实测 mjpeg_rkmpp 可直接消费软件帧），
// 因此硬解不可用时仍使用硬件 JPEG 编码，只有 mjpeg_rkmpp 缺失时才退化为软件 JPEG。
// 命令执行失败后的软件回退由调用方触发（见 tools.ExtractCoverWithOptions）。
func BuildCoverPlan(req CoverRequest) *CoverPlan {
	plan := &CoverPlan{}

	hardwareDecode := false
	if req.Capability != nil {
		hardwareDecode = req.Capability.SupportsHardwareDecode(req.InputCodec)
	} else {
		hardwareDecode = SupportsHardwareDecodeByCodec(req.InputCodec)
	}
	plan.HardwareDecode = hardwareDecode

	plan.HardwareEncode = req.Capability != nil && req.Capability.HasEncoder(CodecMjpegRkmpp)

	args := make([]string, 0, 16)
	if hardwareDecode {
		args = append(args, "-hwaccel", HardwareAccelName)
	} else {
		// 无硬解能力时保持传统行为：跳到第 0 秒再解码
		args = append(args, "-ss", "0")
	}
	args = append(args, "-i", req.Input, "-frames:v", "1")
	if plan.HardwareEncode {
		args = append(args, "-c:v", CodecMjpegRkmpp)
	}
	args = append(args,
		"-q:v", "2", // JPEG 质量（2 为高质量），mjpeg_rkmpp 同样接受该参数
		"-y",
		"-nostdin",
		req.Output,
	)
	plan.Args = args
	return plan
}

// BuildSoftwareCoverPlan 生成纯软件封面提取参数（回退路径）
func BuildSoftwareCoverPlan(req CoverRequest) *CoverPlan {
	return &CoverPlan{
		Args: []string{
			"-ss", "0",
			"-i", req.Input,
			"-frames:v", "1",
			"-q:v", "2",
			"-y",
			"-nostdin",
			req.Output,
		},
	}
}

// EscapeAssPath 转义 ASS 文件路径（尽力而为的兜底实现）。
//
// 首选方案是 StageAssFile：把弹幕复制到临时目录下的固定文件名（subtitles.ass），
// 从根本上避免滤镜层的转义问题。只有在暂存失败时才会用到本函数，
// 因此这里按 FFmpeg 的"两层转义"规则做保守处理：
//   - 第一层（滤镜参数值）：转义 \ ' :
//   - 第二层（整个滤镜描述）：再转义 \ ' [ ] , ;
func EscapeAssPath(path string) string {
	if path == "" {
		return "''"
	}
	// 统一使用正斜杠，避免 Windows 反斜杠被当作转义符
	value := strings.ReplaceAll(path, "\\", "/")
	// 第一层：滤镜参数值内的特殊字符
	value = strings.NewReplacer(
		`\`, `\\`,
		`'`, `\'`,
		`:`, `\:`,
	).Replace(value)
	// 第二层：整个滤镜描述内的特殊字符
	value = strings.NewReplacer(
		`\`, `\\`,
		`'`, `\'`,
		`[`, `\[`,
		`]`, `\]`,
		`,`, `\,`,
		`;`, `\;`,
	).Replace(value)
	return "'" + value + "'"
}
