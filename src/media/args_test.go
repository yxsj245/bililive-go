package media

import (
	"strings"
	"testing"
)

// testCapability 构造一份典型 RK3588 容器内的 FFmpeg 能力（仅用于参数生成测试）
func testCapability() *Capability {
	return &Capability{
		FFmpegPath:       "/usr/local/bin/ffmpeg",
		Version:          "ffmpeg version 7.1",
		HwAccels:         []string{"drm", "rkmpp"},
		Encoders:         []string{"h264_rkmpp", "hevc_rkmpp", "mjpeg_rkmpp", "libx264"},
		Decoders:         []string{"h264_rkmpp", "hevc_rkmpp", "mpeg4_rkmpp"},
		Filters:          []string{"ass", "format", "hwdownload", "hwupload", "scale", "scale_rkrga", "subtitles"},
		HardwareEncoders: []string{"h264_rkmpp", "hevc_rkmpp", "mjpeg_rkmpp"},
		HardwareDecoders: []string{"h264_rkmpp", "hevc_rkmpp", "mpeg4_rkmpp"},
		KeyFilters:       []string{"ass", "format", "hwdownload", "hwupload", "scale", "scale_rkrga", "subtitles"},
		Devices:          map[string]bool{"/dev/mpp_service": true, "/dev/rga": true, "/dev/dri": true, "/dev/dma_heap": true},
	}
}

func hardwareOptions() Options {
	return Options{
		Enabled:        true,
		Compress:       true,
		Codec:          CodecHEVCRkmpp,
		Bitrate:        "4M",
		DecodeFallback: true,
	}
}

func TestBuildPlanHardwareDecodeBurnChain(t *testing.T) {
	plan, err := BuildPlan(Request{
		FFmpegPath: "/usr/local/bin/ffmpeg",
		Input:      "/data/in.flv",
		Output:     "/data/.tmp.mkv",
		AssPath:    "/data/in.ass",
		Options:    hardwareOptions(),
		Capability: testCapability(),
		InputCodec: "h264",
	})
	if err != nil {
		t.Fatalf("BuildPlan 失败: %v", err)
	}
	if !plan.HardwareEncode || !plan.HardwareDecode || !plan.RGA || !plan.Burn {
		t.Fatalf("处理链标记不正确: %+v", plan)
	}
	// 烧录必须回到 CPU 帧：RGA 转格式 → hwdownload → ass
	wantChain := "scale_rkrga=format=nv12,hwdownload,format=nv12,ass='" + "/data/in.ass" + "'"
	if plan.FilterChain != wantChain {
		t.Fatalf("滤镜链不符:\n got=%s\nwant=%s", plan.FilterChain, wantChain)
	}
	joined := strings.Join(plan.Args, " ")
	for _, want := range []string{"-hwaccel rkmpp", "-hwaccel_output_format drm_prime", "-c:v hevc_rkmpp", "-b:v 4M", "-c:a copy"} {
		if !strings.Contains(joined, want) {
			t.Errorf("命令缺少 %q: %s", want, joined)
		}
	}
	// MPP 编码器不支持 -crf / -preset
	if strings.Contains(joined, "-crf") || strings.Contains(joined, "-preset") {
		t.Errorf("硬件编码不应出现 -crf/-preset: %s", joined)
	}
}

func TestBuildPlanScaleUsesRGA(t *testing.T) {
	options := hardwareOptions()
	options.ScaleWidth = 1280
	options.ScaleHeight = 720
	plan, err := BuildPlan(Request{
		Input:      "/data/in.mkv",
		Output:     "/data/out.mkv",
		AssPath:    "/data/in.ass",
		Options:    options,
		Capability: testCapability(),
		InputCodec: "hevc",
	})
	if err != nil {
		t.Fatalf("BuildPlan 失败: %v", err)
	}
	if !strings.Contains(plan.FilterChain, "scale_rkrga=w=1280:h=720:format=nv12") {
		t.Fatalf("缩放应优先使用 RGA: %s", plan.FilterChain)
	}
}

func TestBuildPlanCompressOnlyKeepsZeroCopy(t *testing.T) {
	options := hardwareOptions()
	options.Compress = true
	plan, err := BuildPlan(Request{
		Input:      "/data/in.mkv",
		Output:     "/data/out.mkv",
		Options:    options,
		Capability: testCapability(),
		InputCodec: "hevc",
	})
	if err != nil {
		t.Fatalf("BuildPlan 失败: %v", err)
	}
	if plan.FilterChain != "" {
		t.Fatalf("纯压缩无需 CPU 帧，应保持零拷贝: %s", plan.FilterChain)
	}
	if !plan.HardwareDecode {
		t.Fatal("应使用硬件解码")
	}
}

func TestBuildPlanHardwareDecoderMissingFails(t *testing.T) {
	capability := testCapability()
	capability.Decoders = []string{"h264_rkmpp"}
	capability.HardwareDecoders = []string{"h264_rkmpp"}
	options := hardwareOptions()
	options.DecodeFallback = false

	_, err := BuildPlan(Request{
		Input:      "/data/in.mkv",
		Output:     "/data/out.mkv",
		Options:    options,
		Capability: capability,
		InputCodec: "hevc",
	})
	if err == nil {
		t.Fatal("禁止软件解码回退时应报错")
	}
	if !strings.Contains(err.Error(), "无法硬件解码") {
		t.Fatalf("错误信息应说明原因: %v", err)
	}
}

func TestBuildPlanMissingHardwareEncoderFails(t *testing.T) {
	capability := testCapability()
	capability.Encoders = []string{"libx264"}
	capability.HardwareEncoders = nil

	_, err := BuildPlan(Request{
		Input:      "/data/in.mkv",
		Output:     "/data/out.mkv",
		Options:    hardwareOptions(),
		Capability: capability,
		InputCodec: "h264",
	})
	if err == nil {
		t.Fatal("硬件编码器缺失时必须报错，不能回退软件编码")
	}
	if !strings.Contains(err.Error(), "不提供硬件编码器") {
		t.Fatalf("错误信息应说明原因: %v", err)
	}
}

func TestBuildPlanMissingAssFilterFails(t *testing.T) {
	capability := testCapability()
	capability.Filters = []string{"format", "scale"}
	capability.KeyFilters = []string{"format", "scale"}

	_, err := BuildPlan(Request{
		Input:      "/data/in.mkv",
		Output:     "/data/out.mkv",
		AssPath:    "/data/in.ass",
		Options:    hardwareOptions(),
		Capability: capability,
		InputCodec: "h264",
	})
	if err == nil || !strings.Contains(err.Error(), "ass") {
		t.Fatalf("缺少 ass 滤镜必须报错: %v", err)
	}
}

func TestBuildPlanSoftwareEncoderLegacyBehavior(t *testing.T) {
	options := Options{Codec: DefaultSoftwareCodec, CRF: "18", Preset: "medium", DecodeFallback: true}
	plan, err := BuildPlan(Request{
		Input:      "/data/in.flv",
		Output:     "/data/out.mkv",
		AssPath:    "/data/in.ass",
		Options:    options,
		Capability: testCapability(),
		InputCodec: "h264",
	})
	if err != nil {
		t.Fatalf("BuildPlan 失败: %v", err)
	}
	if plan.HardwareEncode || plan.HardwareDecode {
		t.Fatal("软件编码路径不应启用硬件处理")
	}
	joined := strings.Join(plan.Args, " ")
	if !strings.Contains(joined, "-crf 18") || !strings.Contains(joined, "-preset medium") {
		t.Fatalf("软件编码需保留 crf/preset: %s", joined)
	}
	if strings.Contains(joined, "-hwaccel") {
		t.Fatalf("软件编码路径不应带硬解参数: %s", joined)
	}
	if plan.FilterChain != "ass='/data/in.ass'" {
		t.Fatalf("软件路径滤镜链不符: %s", plan.FilterChain)
	}
}

func TestBuildPlanUnknownCapabilityAssumesHardwareDecode(t *testing.T) {
	plan, err := BuildPlan(Request{
		Input:      "/data/in.mkv",
		Output:     "/data/out.mkv",
		Options:    hardwareOptions(),
		Capability: nil,
		InputCodec: "h264",
	})
	if err != nil {
		t.Fatalf("BuildPlan 失败: %v", err)
	}
	if !plan.HardwareDecode {
		t.Fatal("能力未知时应先尝试硬解")
	}
	if len(plan.Warnings) == 0 {
		t.Fatal("能力未知时应给出告警")
	}
}

func TestBuildCoverPlanHardwareJpegWithoutHardwareDecode(t *testing.T) {
	capability := testCapability()
	capability.Decoders = []string{}
	capability.HardwareDecoders = []string{}
	capability.HwAccels = []string{"drm"}

	plan := BuildCoverPlan(CoverRequest{
		Input:      "/data/in.mkv",
		Output:     "/data/out.jpg",
		InputCodec: "hevc",
		Capability: capability,
	})
	if plan.HardwareDecode {
		t.Fatal("无硬解器时不应使用硬解")
	}
	if !plan.HardwareEncode {
		t.Fatal("mjpeg_rkmpp 存在时应使用硬件 JPEG 编码（MPP 接受 CPU 帧）")
	}
	joined := strings.Join(plan.Args, " ")
	if !strings.Contains(joined, "-c:v mjpeg_rkmpp") || !strings.Contains(joined, "-ss 0") {
		t.Fatalf("封面参数不符: %s", joined)
	}
}

func TestEscapeAssPathHandlesSpecialCharacters(t *testing.T) {
	cases := []string{
		"/data/a b.ass",
		"/data/a,b.ass",
		"/data/a:b.ass",
		"/data/a[1].ass",
		"/data/it's.ass",
		`/data/back\slash.ass`,
	}
	for _, input := range cases {
		escaped := EscapeAssPath(input)
		if !strings.HasPrefix(escaped, "'") || !strings.HasSuffix(escaped, "'") {
			t.Errorf("转义结果应为单引号包裹: %s -> %s", input, escaped)
		}
		if strings.Trim(escaped, "'") == input && strings.ContainsAny(input, ",:[]") {
			t.Errorf("特殊字符未被转义: %s -> %s", input, escaped)
		}
	}
	if EscapeAssPath("") != "''" {
		t.Error("空路径应返回空引号")
	}
}

func TestSupportsHardwareDecode(t *testing.T) {
	capability := testCapability()
	if !capability.SupportsHardwareDecode("h264") {
		t.Error("h264 应支持硬解")
	}
	if capability.SupportsHardwareDecode("mpeg1video") {
		t.Error("mpeg1video 无对应硬解器时不应声称支持")
	}
	if capability.SupportsHardwareDecode("") {
		t.Error("空编码不应声称支持硬解")
	}
}
