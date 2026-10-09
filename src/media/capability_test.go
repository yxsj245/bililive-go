package media

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// 真实的 `ffmpeg -encoders` 输出片段（Rockchip 构建）
const encoderFixture = `Encoders:
 V..... = Video
 A..... = Audio
 ------
 V....D libx264              libx264 H.264 / AVC / MPEG-4 AVC / MPEG-4 part 10 (codec h264)
 V..... h264_rkmpp           Rockchip MPP (Media Process Platform) H264 encoder (codec h264)
 V..... hevc_rkmpp           Rockchip MPP (Media Process Platform) HEVC encoder (codec hevc)
 V..... mjpeg_rkmpp          Rockchip MPP (Media Process Platform) MJPEG encoder (codec mjpeg)
 A....D aac                  AAC (Advanced Audio Coding)
`

const decoderFixture = `Decoders:
 V..... = Video
 ------
 V..... h264_rkmpp           Rockchip MPP (Media Process Platform) H264 decoder (codec h264)
 V..... hevc_rkmpp           Rockchip MPP (Media Process Platform) HEVC decoder (codec hevc)
 V..... vp9_rkmpp            Rockchip MPP (Media Process Platform) VP9 decoder (codec vp9)
 A..... aac                  AAC (Advanced Audio Coding)
`

const filterFixture = `Filters:
  T.. = Timeline support
  .S. = Slice threading
  ..C = Command support
  A = Audio input/output
  V = Video input/output
  N = Dynamic number and/or type of input/output
  | = Source or sink filter
 ... abench            A->A       Benchmark part of a filtergraph.
 ... ass               V->V       Render ASS subtitles libass.
 ..C scale             V->V       Scale the input video size and/or convert the image format.
 ... hwdownload        V->V       Download a hardware frame to a normal frame
 ... hwupload          V->V       Upload a normal frame to a hardware frame
 ... scale_rkrga       V->V       Rockchip RGA (2D Raster Graphic Acceleration) video resizer and format converter
 ... vpp_rkrga         V->V       Rockchip RGA (2D Raster Graphic Acceleration) video post-process (scale/crop/transpose)
 ... subtitles         V->V       Render text subtitles onto input video using the libass library.
`

const hwaccelFixture = `Hardware acceleration methods:
drm
rkmpp
`

func TestParseEncoderNames(t *testing.T) {
	names := parseEncoderNames(encoderFixture)
	for _, want := range []string{"libx264", "h264_rkmpp", "hevc_rkmpp", "mjpeg_rkmpp"} {
		if !containsFold(names, want) {
			t.Errorf("缺少编码器 %s，解析结果: %v", want, names)
		}
	}
	if containsFold(names, "aac") {
		t.Errorf("音频编码器不应出现在视频编码器列表中: %v", names)
	}
}

func TestParseDecoderNames(t *testing.T) {
	names := parseDecoderNames(decoderFixture)
	if !containsFold(names, "hevc_rkmpp") || !containsFold(names, "vp9_rkmpp") {
		t.Fatalf("解码器解析不完整: %v", names)
	}
	if containsFold(names, "aac") {
		t.Fatalf("音频解码器不应出现: %v", names)
	}
}

func TestParseFilterNames(t *testing.T) {
	names := parseFilterNames(filterFixture)
	for _, want := range []string{"ass", "scale", "hwdownload", "hwupload", "scale_rkrga", "vpp_rkrga", "subtitles"} {
		if !containsFold(names, want) {
			t.Errorf("缺少滤镜 %s，解析结果: %v", want, names)
		}
	}
	if containsFold(names, "Benchmark") {
		t.Errorf("不应把描述文字当成滤镜名: %v", names)
	}
}

func TestParseHwAccels(t *testing.T) {
	accels := parseHwAccels(hwaccelFixture)
	if len(accels) != 2 || accels[0] != "drm" || accels[1] != "rkmpp" {
		t.Fatalf("硬件加速解析不符: %v", accels)
	}
}

func TestFilterNamesPicksHardwareImplementations(t *testing.T) {
	encoders := filterNames(parseEncoderNames(encoderFixture), true)
	if containsFold(encoders, "libx264") || !containsFold(encoders, "hevc_rkmpp") {
		t.Fatalf("硬件编码器筛选不符: %v", encoders)
	}
	decoders := filterNames(parseDecoderNames(decoderFixture), false)
	if len(decoders) != 3 {
		t.Fatalf("硬件解码器筛选不符: %v", decoders)
	}
}

func TestIntersectFoldKeepsWantedOrder(t *testing.T) {
	source := []string{"subtitles", "ass", "scale", "format"}
	wanted := []string{"ass", "scale", "scale_rkrga", "hwdownload"}
	got := intersectFold(source, wanted)
	if strings.Join(got, ",") != "ass,scale" {
		t.Fatalf("交集结果不符: %v", got)
	}
}

// unusableFFmpegPath 返回一个"存在且可执行、但不是 FFmpeg"的路径，用于构造探测失败场景
func unusableFFmpegPath(t *testing.T) string {
	t.Helper()
	for _, candidate := range []string{"/bin/true", "/usr/bin/true"} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	t.Skip("找不到可用于构造探测失败的可执行文件")
	return ""
}

// TestProbeMarksIncompleteOnUnusableBinary 目标可执行但拿不到任何编码器时，必须标记为不完整
func TestProbeMarksIncompleteOnUnusableBinary(t *testing.T) {
	capability := Probe(context.Background(), unusableFFmpegPath(t))
	if capability.Err == "" {
		t.Fatal("无法解析出编码器时必须记录错误原因")
	}
	if !capability.Incomplete {
		t.Fatal("探测失败必须标记 Incomplete")
	}
}

// TestProbeCachedSkipsCacheOnCancelledContext 取消上下文的探测结果绝不能进入缓存
func TestProbeCachedSkipsCacheOnCancelledContext(t *testing.T) {
	InvalidateCapabilityCache()
	path := unusableFFmpegPath(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	capability := ProbeCached(ctx, path, true)
	if capability.Err == "" {
		t.Fatal("取消的探测应报告失败")
	}

	capabilityCacheMu.Lock()
	_, cached := capabilityCache[path]
	capabilityCacheMu.Unlock()
	if cached {
		t.Fatal("失败/取消的探测结果不得写入缓存（否则会污染后续任务）")
	}
}

// TestProbeCachedFailedRefreshKeepsExistingEntry 失败的 refresh 不得覆盖已有有效缓存
func TestProbeCachedFailedRefreshKeepsExistingEntry(t *testing.T) {
	InvalidateCapabilityCache()

	key := "/nonexistent/dir/ffmpeg"
	good := &Capability{
		FFmpegPath:       key,
		HwAccels:         []string{"rkmpp"},
		Encoders:         []string{"hevc_rkmpp", "h264_rkmpp"},
		Decoders:         []string{"hevc_rkmpp"},
		Filters:          []string{"ass", "scale_rkrga"},
		HardwareEncoders: []string{"hevc_rkmpp", "h264_rkmpp"},
		HardwareDecoders: []string{"hevc_rkmpp"},
		KeyFilters:       []string{"ass", "scale_rkrga"},
		Devices:          map[string]bool{},
	}
	capabilityCacheMu.Lock()
	capabilityCache[key] = capabilityCacheEntry{capability: good, expiresAt: time.Now().Add(time.Minute)}
	capabilityCacheMu.Unlock()

	// 强制刷新会失败（路径不存在），此时必须继续复用已有的有效缓存
	got := ProbeCached(context.Background(), key, true)
	if got.Err != "" {
		t.Fatalf("失败刷新应返回已有有效缓存，实际返回失败结果: %s", got.Err)
	}
	if !got.HasEncoder("hevc_rkmpp") {
		t.Fatal("已有有效缓存的内容被污染")
	}

	capabilityCacheMu.Lock()
	entry := capabilityCache[key]
	capabilityCacheMu.Unlock()
	if entry.capability == nil || !entry.capability.HasEncoder("hevc_rkmpp") {
		t.Fatal("缓存中的有效结果被失败刷新覆盖")
	}
}
