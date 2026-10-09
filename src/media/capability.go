package media

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// capabilityTTL 能力探测缓存有效期。探测本身只读取 FFmpeg 的编码器/解码器/滤镜列表，
// 开销很小，但仍做缓存以避免界面频繁刷新时反复启动子进程。
const capabilityTTL = 3 * time.Minute

// probeTimeout 单项探测命令的超时时间
const probeTimeout = 30 * time.Second

// devicePaths 与硬件处理链相关的设备节点
var devicePaths = []string{
	"/dev/mpp_service",
	"/dev/mpp-service",
	"/dev/rga",
	"/dev/dri",
	"/dev/dri/renderD128",
	"/dev/dri/card0",
	"/dev/dma_heap",
	"/dev/iep",
	"/dev/rkvdec",
	"/dev/rkvenc",
	"/dev/vpu_service",
	"/dev/hevc_service",
}

// keyFilterNames 弹幕烧录与硬件压缩链关心的滤镜
var keyFilterNames = []string{
	"ass", "subtitles", "scale", "format", "hwdownload", "hwupload", "scale_rkrga", "vpp_rkrga", "overlay_rkrga",
}

// Capability 描述"当前 FFmpeg 可执行文件"的能力。所有字段均来自真实探测结果
type Capability struct {
	// FFmpegPath 被探测的 FFmpeg 路径
	FFmpegPath string `json:"ffmpeg_path"`
	// Version FFmpeg 版本（首行）
	Version string `json:"version"`
	// HwAccels 支持的硬件加速方式
	HwAccels []string `json:"hwaccels"`
	// Encoders 全部视频编码器
	Encoders []string `json:"encoders"`
	// Decoders 全部视频解码器
	Decoders []string `json:"decoders"`
	// Filters 全部滤镜
	Filters []string `json:"filters"`
	// HardwareEncoders 其中的硬件编码器
	HardwareEncoders []string `json:"hardware_encoders"`
	// HardwareDecoders 其中的硬件解码器
	HardwareDecoders []string `json:"hardware_decoders"`
	// KeyFilters 上述关心的滤镜中实际存在的部分
	KeyFilters []string `json:"key_filters"`
	// Devices 相关设备节点是否存在
	Devices map[string]bool `json:"devices"`
	// Warnings 探测过程中发现的问题（缺编码器、缺滤镜、缺设备等）
	Warnings []string `json:"warnings"`
	// Incomplete 探测是否不完整（有步骤失败或结果为空）：不完整的结果不会被缓存
	Incomplete bool `json:"incomplete,omitempty"`
	// Err 探测失败原因（例如 FFmpeg 无法执行、列表读取失败）
	Err string `json:"err,omitempty"`
	// ProbedAt 探测时间
	ProbedAt time.Time `json:"probed_at"`
}

// HasEncoder 判断是否存在指定编码器
func (c *Capability) HasEncoder(name string) bool {
	return c != nil && containsFold(c.Encoders, name)
}

// HasDecoder 判断是否存在指定解码器
func (c *Capability) HasDecoder(name string) bool {
	return c != nil && containsFold(c.Decoders, name)
}

// HasFilter 判断是否存在指定滤镜
func (c *Capability) HasFilter(name string) bool {
	return c != nil && containsFold(c.Filters, name)
}

// HasHwAccel 判断是否支持指定硬件加速方式
func (c *Capability) HasHwAccel(name string) bool {
	return c != nil && containsFold(c.HwAccels, name)
}

// SupportsHardwareDecode 判断指定输入编码能否用当前 FFmpeg 硬件解码。
// 必须同时具备 rkmpp hwaccel 与对应解码器，二者缺一不可。
func (c *Capability) SupportsHardwareDecode(codec string) bool {
	if c == nil || !c.HasHwAccel(HardwareAccelName) {
		return false
	}
	decoder := hardwareDecoderName(codec)
	if decoder == "" {
		return false
	}
	return c.HasDecoder(decoder)
}

// capabilityCacheEntry 探测结果缓存项
type capabilityCacheEntry struct {
	capability *Capability
	expiresAt  time.Time
}

var (
	capabilityCacheMu sync.Mutex
	capabilityCache   = map[string]capabilityCacheEntry{}
)

// ProbeCached 返回带缓存的探测结果。forceRefresh 为 true 时忽略缓存重新探测。
//
// 缓存规则（防止把"失败/被取消的探测"当成能力结论）：
//   - 只有探测完整成功（cap.Err == "" 且 !cap.Incomplete）且调用方上下文未取消时才写入缓存；
//   - 失败的刷新绝不覆盖已有的有效缓存，此时返回旧缓存；
//   - 缓存中只可能存放可用的探测结果，命中的结果可放心复用。
//
// 返回值只读，调用方不得修改（缓存对象会被共享）。
func ProbeCached(ctx context.Context, ffmpegPath string, forceRefresh bool) *Capability {
	key := strings.TrimSpace(ffmpegPath)
	now := time.Now()

	if !forceRefresh {
		if capability, ok := cachedCapability(key, now); ok {
			return capability
		}
	}

	capability := Probe(ctx, key)

	// 取消或探测失败：不缓存；若已有有效缓存则继续复用旧结果
	if ctx.Err() != nil || capability.Err != "" || capability.Incomplete {
		if cached, ok := cachedCapability(key, now); ok {
			return cached
		}
		return capability
	}

	capabilityCacheMu.Lock()
	capabilityCache[key] = capabilityCacheEntry{capability: capability, expiresAt: now.Add(capabilityTTL)}
	capabilityCacheMu.Unlock()
	return capability
}

// cachedCapability 读取仍然有效的缓存项。不完整/失败的探测结果从不写入缓存，
// 这里再做一次防御性校验，避免历史或外部写入的坏数据被当成能力结论。
func cachedCapability(key string, now time.Time) (*Capability, bool) {
	capabilityCacheMu.Lock()
	entry, ok := capabilityCache[key]
	capabilityCacheMu.Unlock()
	if !ok || now.After(entry.expiresAt) || entry.capability == nil {
		return nil, false
	}
	if entry.capability.Err != "" || entry.capability.Incomplete {
		return nil, false
	}
	return entry.capability, true
}

// InvalidateCapabilityCache 清空探测缓存（配置切换 FFmpeg 路径或用户手动刷新时使用）
func InvalidateCapabilityCache() {
	capabilityCacheMu.Lock()
	capabilityCache = map[string]capabilityCacheEntry{}
	capabilityCacheMu.Unlock()
}

// Probe 真实探测指定 FFmpeg 的能力。ffmpegPath 为空时直接返回"未找到 FFmpeg"的结果
func Probe(ctx context.Context, ffmpegPath string) *Capability {
	capability := &Capability{
		FFmpegPath: ffmpegPath,
		Encoders:   []string{},
		Decoders:   []string{},
		Filters:    []string{},
		HwAccels:   []string{},
		Devices:    map[string]bool{},
		Warnings:   []string{},
		ProbedAt:   time.Now(),
	}

	if strings.TrimSpace(ffmpegPath) == "" {
		capability.Err = "未找到可用的 FFmpeg"
		capability.Warnings = append(capability.Warnings, "未找到可用的 FFmpeg，硬件能力无法探测")
		capability.probeDevices()
		return capability
	}

	if fi, err := os.Stat(ffmpegPath); err != nil || fi.IsDir() {
		capability.Err = fmt.Sprintf("FFmpeg 路径不可用: %s", ffmpegPath)
		capability.Warnings = append(capability.Warnings, capability.Err)
		capability.probeDevices()
		return capability
	}

	versionOut, versionErr := runProbeCommand(ctx, ffmpegPath, "-hide_banner", "-version")
	if versionErr != nil {
		capability.Err = fmt.Sprintf("执行 FFmpeg 失败: %v", versionErr)
		capability.Incomplete = true
		capability.Warnings = append(capability.Warnings, capability.Err)
		capability.probeDevices()
		return capability
	}
	capability.Version = firstLine(versionOut)

	// 任一列表探测失败都视为"探测不完整"并记录原因：
	// 不完整的结果绝不能当作能力结论（否则会误判成"没有硬件编码器"）
	var probeFailures []string
	if out, err := runProbeCommand(ctx, ffmpegPath, "-hide_banner", "-hwaccels"); err == nil {
		capability.HwAccels = parseHwAccels(out)
	} else {
		probeFailures = append(probeFailures, fmt.Sprintf("读取硬件加速方式失败: %v", err))
	}

	if out, err := runProbeCommand(ctx, ffmpegPath, "-hide_banner", "-encoders"); err == nil {
		capability.Encoders = parseEncoderNames(out)
	} else {
		probeFailures = append(probeFailures, fmt.Sprintf("读取编码器列表失败: %v", err))
	}

	if out, err := runProbeCommand(ctx, ffmpegPath, "-hide_banner", "-decoders"); err == nil {
		capability.Decoders = parseDecoderNames(out)
	} else {
		probeFailures = append(probeFailures, fmt.Sprintf("读取解码器列表失败: %v", err))
	}

	if out, err := runProbeCommand(ctx, ffmpegPath, "-hide_banner", "-filters"); err == nil {
		capability.Filters = parseFilterNames(out)
	} else {
		probeFailures = append(probeFailures, fmt.Sprintf("读取滤镜列表失败: %v", err))
	}

	// 命令成功但解析不到任何编码器，同样说明结果不可信（例如目标不是 FFmpeg）
	if len(capability.Encoders) == 0 {
		probeFailures = append(probeFailures, "未能从 FFmpeg 解析出任何编码器")
	}
	if len(probeFailures) > 0 {
		capability.Incomplete = true
		capability.Err = "能力探测不完整: " + strings.Join(probeFailures, "; ")
	}
	capability.Warnings = append(capability.Warnings, probeFailures...)

	capability.HardwareEncoders = filterNames(capability.Encoders, true)
	capability.HardwareDecoders = filterNames(capability.Decoders, false)
	capability.KeyFilters = intersectFold(capability.Filters, keyFilterNames)
	capability.probeDevices()
	capability.collectWarnings()
	return capability
}

// probeDevices 检查硬件处理链所需的设备节点
func (c *Capability) probeDevices() {
	for _, path := range devicePaths {
		if _, err := os.Stat(path); err == nil {
			c.Devices[path] = true
		} else {
			c.Devices[path] = false
		}
	}
}

// collectWarnings 汇总能力缺口（这些是"想用硬件但用不了"的明确原因）
func (c *Capability) collectWarnings() {
	if !c.HasHwAccel(HardwareAccelName) {
		c.Warnings = append(c.Warnings, "当前 FFmpeg 未提供 rkmpp 硬件加速，硬件解码不可用")
	}
	if len(c.HardwareEncoders) == 0 {
		c.Warnings = append(c.Warnings, "当前 FFmpeg 未提供任何硬件编码器（需编译 --enable-rkmpp）")
	}
	if !c.HasFilter("ass") {
		c.Warnings = append(c.Warnings, "当前 FFmpeg 缺少 ass 滤镜（需编译 libass 并安装中文字体），无法烧录弹幕")
	}
	if !c.HasFilter("scale_rkrga") {
		c.Warnings = append(c.Warnings, "当前 FFmpeg 缺少 scale_rkrga（RGA）滤镜，缩放将回退到软件实现")
	}
	if !c.Devices["/dev/mpp_service"] && !c.Devices["/dev/mpp-service"] {
		c.Warnings = append(c.Warnings, "未检测到 /dev/mpp_service，MPP 编解码不可用（需映射设备节点）")
	}
	if !c.Devices["/dev/rga"] {
		c.Warnings = append(c.Warnings, "未检测到 /dev/rga，RGA 缩放不可用")
	}
	if !c.Devices["/dev/dri"] {
		c.Warnings = append(c.Warnings, "未检测到 /dev/dri，DRM 帧分配不可用（rkmpp 硬解需要）")
	}
	if !c.Devices["/dev/dma_heap"] {
		c.Warnings = append(c.Warnings, "未检测到 /dev/dma_heap，DMA 缓冲分配可能失败")
	}
}

// runProbeCommand 执行一条探测命令并返回输出
func runProbeCommand(ctx context.Context, ffmpegPath string, args ...string) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	cmd := exec.CommandContext(probeCtx, ffmpegPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%s %s: %w", ffmpegPath, strings.Join(args, " "), err)
	}
	return string(output), nil
}

// encoderLinePattern 匹配 `-encoders` / `-decoders` 的条目行
var encoderLinePattern = regexp.MustCompile(`^\s*([A-Z.]{6})\s+(\S+)\s`)

// parseEncoderNames 解析 `ffmpeg -encoders` 输出中的视频编码器名
func parseEncoderNames(output string) []string {
	return parseCodecNames(output, "V")
}

// parseDecoderNames 解析 `ffmpeg -decoders` 输出中的视频解码器名
func parseDecoderNames(output string) []string {
	return parseCodecNames(output, "V")
}

// parseCodecNames 解析编解码器列表，flagPrefix 用于过滤类型（V=视频）
func parseCodecNames(output, flagPrefix string) []string {
	names := []string{}
	for _, line := range strings.Split(output, "\n") {
		matches := encoderLinePattern.FindStringSubmatch(line)
		if matches == nil {
			continue
		}
		if flagPrefix != "" && !strings.HasPrefix(matches[1], flagPrefix) {
			continue
		}
		names = append(names, matches[2])
	}
	sort.Strings(names)
	return dedupeStrings(names)
}

// parseFilterNames 解析 `ffmpeg -filters` 输出中的滤镜名
func parseFilterNames(output string) []string {
	names := []string{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		if !strings.Contains(fields[2], "->") {
			continue
		}
		names = append(names, fields[1])
	}
	sort.Strings(names)
	return dedupeStrings(names)
}

// parseHwAccels 解析 `ffmpeg -hwaccels` 输出
func parseHwAccels(output string) []string {
	names := []string{}
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "Hardware acceleration") {
			continue
		}
		names = append(names, trimmed)
	}
	sort.Strings(names)
	return dedupeStrings(names)
}

// filterNames 从编码器/解码器列表中筛出硬件实现
func filterNames(names []string, encoder bool) []string {
	result := []string{}
	for _, name := range names {
		if encoder {
			if IsHardwareEncoder(name) {
				result = append(result, name)
			}
			continue
		}
		if IsHardwareEncoder(name) || strings.HasSuffix(name, "_rkmpp") {
			result = append(result, name)
		}
	}
	sort.Strings(result)
	return result
}

// intersectFold 返回 source 中命中 wanted 的项（忽略大小写，按 wanted 顺序输出）
func intersectFold(source, wanted []string) []string {
	result := []string{}
	for _, want := range wanted {
		if containsFold(source, want) {
			result = append(result, want)
		}
	}
	return result
}

// containsFold 忽略大小写判断列表是否包含某项
func containsFold(list []string, name string) bool {
	if name == "" {
		return false
	}
	for _, item := range list {
		if strings.EqualFold(item, name) {
			return true
		}
	}
	return false
}

// dedupeStrings 去重并保持顺序
func dedupeStrings(list []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(list))
	for _, item := range list {
		if seen[item] {
			continue
		}
		seen[item] = true
		result = append(result, item)
	}
	return result
}

// firstLine 返回首行（去掉首尾空白）
func firstLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}
