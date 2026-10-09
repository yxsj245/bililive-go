package media

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	bilisentry "github.com/bililive-go/bililive-go/src/pkg/sentry"
)

// outputValidateSeconds 输出校验时实际解码的秒数。
// 只解码开头若干秒即可确认"帧可解码"，避免对长录像做全量解码而浪费 CPU。
const outputValidateSeconds = 5

// durationToleranceRatio 输出与输入时长的容差（流复制/重新编码都可能有极小偏差）
const durationToleranceRatio = 0.02

// durationToleranceSeconds 输出与输入时长的最小容差（秒）
const durationToleranceSeconds = 1.0

// stderrTailLimit 错误信息中保留的 stderr 尾部长度
const stderrTailLimit = 2000

// MediaInfo 描述一个媒体文件的关键信息
type MediaInfo struct {
	// Duration 时长（秒），无法解析时为 0
	Duration float64 `json:"duration"`
	// VideoCodec 首个视频流的编码名
	VideoCodec string `json:"video_codec"`
	// Width 视频宽度
	Width int `json:"width"`
	// Height 视频高度
	Height int `json:"height"`
	// HasAudio 是否存在音频流
	HasAudio bool `json:"has_audio"`
	// AudioCodec 首个音频流的编码名
	AudioCodec string `json:"audio_codec"`
}

// RunResult 描述一次 FFmpeg 执行结果
type RunResult struct {
	// Command 完整命令行（用于任务日志展示）
	Command string
	// Args 参数列表
	Args []string
	// Stderr FFmpeg 的错误输出
	Stderr string
	// Duration 实际执行耗时
	Duration time.Duration
	// Progress 最后一次上报的进度（0-100）
	Progress float64
}

// Run 执行 FFmpeg 命令。totalDuration > 0 时解析 -progress 输出并回调进度。
// 上下文取消时会终止子进程并返回 ctx 的错误（调用方据此保留源文件与弹幕）。
func Run(ctx context.Context, ffmpegPath string, args []string, totalDuration float64, onProgress func(float64)) (*RunResult, error) {
	cmd := exec.CommandContext(ctx, ffmpegPath, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("创建 ffmpeg 输出管道失败: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("创建 ffmpeg 错误管道失败: %w", err)
	}

	startedAt := time.Now()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动 ffmpeg 失败: %w", err)
	}

	result := &RunResult{
		Command: ffmpegPath + " " + strings.Join(args, " "),
		Args:    args,
	}

	// 必须持续读取 stdout/stderr，否则管道写满会让 ffmpeg 挂起。
	// stderr 通过有界缓冲收集：长录像转码失败时错误输出可能非常长，
	// 只保留尾部若干字节，避免无限占用内存。
	progressDone := make(chan struct{})
	stderrDone := make(chan struct{})
	lastProgress := 0.0
	stderrBuffer := newTailBuffer(stderrTailLimit)
	bilisentry.GoWithContext(ctx, func(goCtx context.Context) {
		defer close(progressDone)
		lastProgress = parseProgressOutput(goCtx, stdout, totalDuration, onProgress)
	})
	bilisentry.GoWithContext(ctx, func(goCtx context.Context) {
		defer close(stderrDone)
		_, _ = io.Copy(stderrBuffer, stderr)
	})
	waitErr := cmd.Wait()
	<-progressDone
	<-stderrDone

	result.Stderr = stderrBuffer.String()
	result.Duration = time.Since(startedAt)
	result.Progress = lastProgress

	if waitErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return result, ctxErr
		}
		return result, fmt.Errorf("ffmpeg 执行失败: %w；stderr: %s", waitErr, tailString(result.Stderr, stderrTailLimit))
	}
	return result, nil
}

// parseProgressOutput 解析 `-progress pipe:1` 输出并回调进度
func parseProgressOutput(ctx context.Context, stdout io.Reader, totalDuration float64, onProgress func(float64)) float64 {
	scanner := bufio.NewScanner(stdout)
	timePattern := regexp.MustCompile(`out_time_(?:us|ms)=(\d+)`)
	last := 0.0

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return last
		default:
		}

		line := scanner.Text()
		matches := timePattern.FindStringSubmatch(line)
		if len(matches) < 2 || totalDuration <= 0 {
			continue
		}
		value, err := strconv.ParseFloat(matches[1], 64)
		if err != nil {
			continue
		}
		// FFmpeg 的 out_time_us 与 out_time_ms 均以微秒上报，统一按微秒换算
		seconds := value / 1000000
		progress := seconds / totalDuration * 100
		if progress > 100 {
			progress = 100
		}
		if progress < 0 {
			progress = 0
		}
		last = progress
		if onProgress != nil {
			onProgress(progress)
		}
	}
	return last
}

// ProbeMediaInfo 读取媒体文件的关键信息（时长、视频编码、分辨率、音轨）
func ProbeMediaInfo(ctx context.Context, ffmpegPath, input string) *MediaInfo {
	if strings.TrimSpace(ffmpegPath) == "" || strings.TrimSpace(input) == "" {
		return nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	cmd := exec.CommandContext(probeCtx, ffmpegPath, "-hide_banner", "-i", input)
	output, _ := cmd.CombinedOutput() // ffmpeg -i 无输出文件时必然返回非 0，信息在 stderr 中
	return parseMediaInfo(string(output))
}

// durationPattern 匹配 `Duration: 00:00:02.00`
var durationPattern = regexp.MustCompile(`Duration:\s*(\d+):(\d+):(\d+)\.(\d+)`)

// dimensionPattern 匹配 `640x360`
var dimensionPattern = regexp.MustCompile(`(\d{2,5})x(\d{2,5})`)

// parenPattern 匹配括号内容（编码标签如 `(avc1 / 0x31637661)`，避免被误判为分辨率）
var parenPattern = regexp.MustCompile(`\([^)]*\)`)

// parseMediaInfo 解析 `ffmpeg -i` 的 stderr 输出
func parseMediaInfo(output string) *MediaInfo {
	info := &MediaInfo{}
	for _, line := range strings.Split(output, "\n") {
		if info.Duration == 0 {
			if matches := durationPattern.FindStringSubmatch(line); len(matches) >= 5 {
				hours, _ := strconv.ParseFloat(matches[1], 64)
				minutes, _ := strconv.ParseFloat(matches[2], 64)
				seconds, _ := strconv.ParseFloat(matches[3], 64)
				centis, _ := strconv.ParseFloat(matches[4], 64)
				info.Duration = hours*3600 + minutes*60 + seconds + centis/100
			}
		}
		if !strings.Contains(line, "Stream #") {
			continue
		}
		clean := parenPattern.ReplaceAllString(line, "")
		if info.VideoCodec == "" && strings.Contains(clean, "Video: ") {
			info.VideoCodec = codecNameAfter(clean, "Video: ")
			if matches := dimensionPattern.FindStringSubmatch(clean); len(matches) >= 3 {
				info.Width, _ = strconv.Atoi(matches[1])
				info.Height, _ = strconv.Atoi(matches[2])
			}
		}
		if !info.HasAudio && strings.Contains(clean, "Audio: ") {
			info.HasAudio = true
			info.AudioCodec = codecNameAfter(clean, "Audio: ")
		}
	}
	return info
}

// codecNameAfter 取出标记后的编码名（形如 `h264 (High), yuv420p` → h264）
func codecNameAfter(line, marker string) string {
	index := strings.Index(line, marker)
	if index < 0 {
		return ""
	}
	rest := line[index+len(marker):]
	for _, sep := range []string{",", " ", "("} {
		if cut := strings.Index(rest, sep); cut >= 0 {
			rest = rest[:cut]
		}
	}
	return strings.ToLower(strings.TrimSpace(rest))
}

// ValidateOutput 校验输出文件确实可用：非空、含视频流、时长与音轨与源文件一致、帧可真实解码。
//
// 只解码输出文件的开头若干秒，既确认"帧可解码"（而不是只看元数据），
// 又不会对长录像做全量解码。使用 -xerror 与 -err_detect explode 让任何坏帧/解码错误
// 立刻以非 0 退出，避免"部分坏帧但退出码为 0"被误判为成功。
func ValidateOutput(ctx context.Context, ffmpegPath, input, output string) error {
	stat, err := os.Stat(output)
	if err != nil {
		return fmt.Errorf("输出文件不存在: %w", err)
	}
	if stat.Size() == 0 {
		return fmt.Errorf("输出文件为空: %s", output)
	}

	outputInfo := ProbeMediaInfo(ctx, ffmpegPath, output)
	if outputInfo == nil || outputInfo.VideoCodec == "" {
		return fmt.Errorf("输出文件缺少可识别的视频流: %s", output)
	}

	if input != "" {
		inputInfo := ProbeMediaInfo(ctx, ffmpegPath, input)
		if inputInfo != nil {
			if err := compareDuration(inputInfo, outputInfo); err != nil {
				return err
			}
			if inputInfo.HasAudio && !outputInfo.HasAudio {
				return fmt.Errorf("输出文件丢失音轨（源文件为 %s）: %s", inputInfo.AudioCodec, output)
			}
		}
	}

	// 真实解码开头若干秒，确认帧可解码（不做全量解码）
	validateCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	args := []string{
		"-hide_banner", "-v", "error",
		"-err_detect", "explode",
		"-xerror",
		"-i", output,
		"-t", strconv.Itoa(outputValidateSeconds),
		"-f", "null", "-",
	}
	cmd := exec.CommandContext(validateCtx, ffmpegPath, args...)
	if combined, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("输出文件解码校验失败: %w；stderr: %s", err, tailString(string(combined), stderrTailLimit))
	}
	return nil
}

// compareDuration 校验输出时长与源文件一致（允许极小误差）
func compareDuration(inputInfo, outputInfo *MediaInfo) error {
	if inputInfo.Duration <= 0 || outputInfo.Duration <= 0 {
		return nil // 无法解析时长时不做该项校验
	}
	tolerance := inputInfo.Duration * durationToleranceRatio
	if tolerance < durationToleranceSeconds {
		tolerance = durationToleranceSeconds
	}
	diff := inputInfo.Duration - outputInfo.Duration
	if diff < 0 {
		diff = -diff
	}
	if diff > tolerance {
		return fmt.Errorf("输出时长异常：源文件 %.2fs，输出 %.2fs（容差 %.2fs）",
			inputInfo.Duration, outputInfo.Duration, tolerance)
	}
	return nil
}

// CreateExclusiveTemp 在 dir 下创建一个"已独占存在"的唯一临时文件，文件名以 suffix 结尾。
// 用 os.CreateTemp 而不是"进程号 + 文件名"拼名，避免并发任务或重复路径互相覆盖临时结果；
// ffmpeg 配合 -y 会直接覆盖这个空占位文件，因此不会出现半旧半新的内容。
func CreateExclusiveTemp(dir, prefix, suffix string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}
	// 文件名中的 * 会被 os.CreateTemp 替换为随机串，必须保留 * 才能保证唯一性；
	// suffix 必须放在 * 之后，否则扩展名会被随机串冲掉（ffmpeg 依赖扩展名推断封装格式）。
	pattern := prefix + "*" + suffix
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", fmt.Errorf("创建临时文件失败: %w", err)
	}
	name := file.Name()
	if err := file.Close(); err != nil {
		RemoveFileQuietly(name)
		return "", fmt.Errorf("关闭临时文件失败: %w", err)
	}
	return name, nil
}

// CreateTempOutput 为指定输出路径创建同目录、同扩展名的唯一临时文件。
// 同目录可保证后续发布使用同文件系统的硬链接，做到原子且不覆盖。
func CreateTempOutput(outputPath string) (string, error) {
	ext := filepath.Ext(outputPath)
	base := sanitizeTempName(strings.TrimSuffix(filepath.Base(outputPath), ext))
	suffix := ".part" + ext
	return CreateExclusiveTemp(filepath.Dir(outputPath), ".bililive-"+base+"-", suffix)
}

// CreateTempCover 为封面提取创建唯一临时文件（扩展名必须是 .jpg 以便推断输出格式）
func CreateTempCover(coverPath string) (string, error) {
	ext := filepath.Ext(coverPath)
	if ext == "" {
		ext = ".jpg"
	}
	base := sanitizeTempName(strings.TrimSuffix(filepath.Base(coverPath), filepath.Ext(coverPath)))
	return CreateExclusiveTemp(filepath.Dir(coverPath), ".bililive-"+base+"-", ".cover"+ext)
}

// sanitizeTempName 去掉文件名中会影响 os.CreateTemp 模板的字符
func sanitizeTempName(name string) string {
	replaced := strings.NewReplacer("*", "_", string(filepath.Separator), "_", "\x00", "_").Replace(name)
	if strings.TrimSpace(replaced) == "" {
		return "output"
	}
	return replaced
}

// uniqueNameMaxAttempts 发布时的候选文件名尝试次数上限
const uniqueNameMaxAttempts = 1000

// PublishTempFile 以"绝不覆盖现存文件"的方式原子发布临时文件。
//
// 实现要点：用 os.Link 在同一目录内创建正式文件——硬链接是原子的且目标必须不存在，
// 因此不存在"先检查再 rename"的竞态窗口（os.Rename 会静默覆盖竞态产生的目标文件）。
// 目标已被占用时换用带序号的文件名重试；硬链接不可用（跨设备或文件系统不支持）时
// 明确返回错误并保留临时文件，而不是退化成会覆盖的 rename。
func PublishTempFile(tempPath, preferred string) (string, error) {
	if !fileExists(tempPath) {
		return "", fmt.Errorf("临时输出文件不存在: %s", tempPath)
	}
	// os.CreateTemp 默认权限为 0600，发布前放开为 0644，
	// 保证非 root 运行（如容器内 1000:1000）的其它进程也能读取产物
	if err := os.Chmod(tempPath, 0o644); err != nil {
		return "", fmt.Errorf("调整输出文件权限失败: %w", err)
	}
	if dir := filepath.Dir(preferred); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("创建输出目录失败: %w", err)
		}
	}

	ext := filepath.Ext(preferred)
	base := strings.TrimSuffix(preferred, ext)

	for attempt := 0; attempt < uniqueNameMaxAttempts; attempt++ {
		candidate := preferred
		if attempt > 0 {
			candidate = fmt.Sprintf("%s.%d%s", base, attempt, ext)
		}
		err := os.Link(tempPath, candidate)
		if err == nil {
			// 发布成功后删除临时文件（硬链接已让正式文件指向同一数据块）
			RemoveFileQuietly(tempPath)
			return candidate, nil
		}
		if errors.Is(err, os.ErrExist) {
			continue // 目标已被占用，换下一个候选名字
		}
		return "", fmt.Errorf("发布输出文件失败（未覆盖任何现存文件，临时文件保留在 %s）: %w", tempPath, err)
	}
	return "", fmt.Errorf("发布输出文件失败：%d 个候选文件名均被占用，临时文件保留在 %s", uniqueNameMaxAttempts, tempPath)
}

// PublishCover 原子发布封面文件：用硬链接以"目标不存在"方式创建，
// 已存在同名封面（同一视频的旧封面）时先删除再链接，保证不会留下半张图。
func PublishCover(tempPath, coverPath string) error {
	if !fileExists(tempPath) {
		return fmt.Errorf("临时封面文件不存在: %s", tempPath)
	}
	// 与视频输出保持一致：0600 → 0644，便于其它进程/容器读取
	if err := os.Chmod(tempPath, 0o644); err != nil {
		return fmt.Errorf("调整封面文件权限失败: %w", err)
	}
	if dir := filepath.Dir(coverPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("创建封面目录失败: %w", err)
		}
	}
	// 封面与视频一一对应，旧封面允许被替换（不是"现存文件不得覆盖"的范畴）
	if fileExists(coverPath) {
		RemoveFileQuietly(coverPath)
	}
	if err := os.Link(tempPath, coverPath); err != nil {
		// 硬链接不可用时退化为同目录 rename（此处目标是刚删除的封面路径，无覆盖风险）
		if renameErr := os.Rename(tempPath, coverPath); renameErr != nil {
			return fmt.Errorf("发布封面文件失败: %w", err)
		}
	}
	RemoveFileQuietly(tempPath)
	return nil
}

// StageAssFile 把弹幕字幕复制到临时工作目录下的固定文件名（subtitles.ass）。
//
// 为什么必须这样做：FFmpeg 的滤镜链有两层转义规则，路径中的逗号、冒号、方括号、
// 单引号、空格等字符在滤镜参数里极易被误解析。使用固定且不含特殊字符的临时路径，
// 可以从根本上规避转义问题，而不是依赖脆弱的转义实现。
// 返回暂存路径与清理函数；失败时返回错误，调用方应回退到 EscapeAssPath。
func StageAssFile(assPath string) (string, func(), error) {
	noop := func() {}
	if strings.TrimSpace(assPath) == "" {
		return "", noop, fmt.Errorf("ASS 字幕路径为空")
	}
	dir, err := os.MkdirTemp("", "bililive-ass-")
	if err != nil {
		return "", noop, fmt.Errorf("创建弹幕暂存目录失败: %w", err)
	}
	staged := filepath.Join(dir, "subtitles.ass")
	if err := copyFile(assPath, staged); err != nil {
		os.RemoveAll(dir)
		return "", noop, err
	}
	return staged, func() { os.RemoveAll(dir) }, nil
}

// copyFile 复制文件（弹幕字幕通常只有几百 KB）
func copyFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("打开弹幕字幕失败: %w", err)
	}
	defer source.Close()

	target, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("创建弹幕字幕副本失败: %w", err)
	}
	if _, err := io.Copy(target, source); err != nil {
		target.Close()
		RemoveFileQuietly(dst)
		return fmt.Errorf("复制弹幕字幕失败: %w", err)
	}
	if err := target.Close(); err != nil {
		RemoveFileQuietly(dst)
		return fmt.Errorf("关闭弹幕字幕副本失败: %w", err)
	}
	return nil
}

// RemoveFileQuietly 删除文件并忽略错误（清理临时文件用）
func RemoveFileQuietly(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	_ = os.Remove(path)
}

// fileExists 判断文件是否存在
func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// tailBuffer 只保留最后 limit 字节的写入缓冲。
// 长录像转码失败时 ffmpeg 的 stderr 可能极长，直接 io.ReadAll 会把内存吃光。
type tailBuffer struct {
	limit int
	data  []byte
}

// newTailBuffer 创建有界缓冲
func newTailBuffer(limit int) *tailBuffer {
	if limit <= 0 {
		limit = stderrTailLimit
	}
	return &tailBuffer{limit: limit, data: make([]byte, 0, limit)}
}

// Write 实现 io.Writer：始终返回完整写入长度，但只保留尾部内容
func (b *tailBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	if len(b.data) > b.limit {
		b.data = b.data[len(b.data)-b.limit:]
		// 复制到新的底层数组，避免切片长期持有巨大的底层数组
		trimmed := make([]byte, len(b.data))
		copy(trimmed, b.data)
		b.data = trimmed
	}
	return len(p), nil
}

// String 返回缓冲内容
func (b *tailBuffer) String() string {
	return string(b.data)
}

// tailString 截取字符串尾部，用于错误信息中保留最有价值的 stderr 结尾
func tailString(text string, limit int) string {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) <= limit {
		return trimmed
	}
	return "..." + trimmed[len(trimmed)-limit:]
}
