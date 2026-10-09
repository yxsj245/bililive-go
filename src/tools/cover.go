package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bililive-go/bililive-go/src/media"
	"github.com/bililive-go/bililive-go/src/pkg/utils"
	"github.com/sirupsen/logrus"
)

// CoverOptions 封面提取的可选参数
type CoverOptions struct {
	// FFmpegPath 显式指定要使用的 FFmpeg 路径（为空时按配置/环境查找）。
	// 由 pipeline 阶段传入 ctx.FFmpegPath，保证与烧录/压缩使用同一个 FFmpeg
	// （房间级 ffmpeg_path 覆盖因此同样生效）。
	FFmpegPath string
	// OnCommand 可选回调：报告实际执行的命令，以及该命令是否属于软件回退路径
	OnCommand func(command string, fallback bool)
}

// ExtractCover 从视频文件提取第一帧作为封面图
// 输出文件名与视频文件相同，扩展名改为 .jpg
// 返回封面文件路径，如果提取失败返回空字符串和错误
func ExtractCover(ctx context.Context, videoPath string) (string, error) {
	return ExtractCoverTo(ctx, videoPath, "")
}

// ExtractCoverTo 从视频文件提取第一帧到指定路径（coverPath 为空时按视频文件名推导）
func ExtractCoverTo(ctx context.Context, videoPath, coverPath string) (string, error) {
	return ExtractCoverWithOptions(ctx, videoPath, coverPath, CoverOptions{})
}

// ExtractCoverWithOptions 提取封面，支持显式 FFmpeg 路径与命令回调。
//
// 处理链与弹幕烧录/压缩共用同一套 media 能力探测：
//   - 优先"硬件解码 + mjpeg_rkmpp 硬件 JPEG 编码"；
//   - MPP 编码器可直接消费 CPU 帧，因此硬解不可用时仍使用硬件 JPEG 编码；
//   - mjpeg_rkmpp 执行失败时明确记录日志并回退软件 JPEG；
//   - 输出先写临时文件，校验可解码后再原子发布，避免留下半张图。
func ExtractCoverWithOptions(ctx context.Context, videoPath, coverPath string, opts CoverOptions) (string, error) {
	// 检查输入文件是否存在
	if _, err := os.Stat(videoPath); os.IsNotExist(err) {
		return "", fmt.Errorf("video file does not exist: %s", videoPath)
	}

	if strings.TrimSpace(coverPath) == "" {
		ext := filepath.Ext(videoPath)
		coverPath = strings.TrimSuffix(videoPath, ext) + ".jpg"
	}

	// 获取 ffmpeg 路径：优先使用调用方显式指定的路径
	ffmpegPath := strings.TrimSpace(opts.FFmpegPath)
	if ffmpegPath == "" {
		resolved, err := utils.GetFFmpegPath(ctx)
		if err != nil {
			return "", fmt.Errorf("failed to get ffmpeg path: %w", err)
		}
		ffmpegPath = resolved
	}

	tempCover, err := media.CreateTempCover(coverPath)
	if err != nil {
		return "", err
	}

	// 探测输入编码与当前 FFmpeg 能力，用于选择硬解/硬件 JPEG
	info := media.ProbeMediaInfo(ctx, ffmpegPath, videoPath)
	inputCodec := ""
	if info != nil {
		inputCodec = info.VideoCodec
	}
	request := media.CoverRequest{
		FFmpegPath: ffmpegPath,
		Input:      videoPath,
		Output:     tempCover,
		InputCodec: inputCodec,
		Capability: media.ProbeCached(ctx, ffmpegPath, false),
	}

	report := func(command string, fallback bool) {
		if opts.OnCommand != nil {
			opts.OnCommand(command, fallback)
		}
	}

	plan := media.BuildCoverPlan(request)
	for _, warning := range plan.Warnings {
		logrus.WithField("video", videoPath).Warnf("封面提取提示: %s", warning)
	}
	logrus.WithFields(logrus.Fields{
		"video":           videoPath,
		"input_codec":     inputCodec,
		"hardware_decode": plan.HardwareDecode,
		"hardware_jpeg":   plan.HardwareEncode,
	}).Debug("extracting cover from video")

	command := ffmpegPath + " " + strings.Join(plan.Args, " ")
	report(command, false)

	result, err := media.Run(ctx, ffmpegPath, plan.Args, 0, nil)
	if err != nil {
		// 硬件封面提取失败：明确记录原因后回退软件 JPEG（不再使用硬件解码，避免重复失败）
		logrus.WithFields(logrus.Fields{
			"video":           videoPath,
			"hardware_jpeg":   plan.HardwareEncode,
			"hardware_decode": plan.HardwareDecode,
			"error":           err.Error(),
		}).Warn("硬件封面提取失败，回退软件 JPEG")
		media.RemoveFileQuietly(tempCover)

		fallback := media.BuildSoftwareCoverPlan(request)
		fallbackCommand := ffmpegPath + " " + strings.Join(fallback.Args, " ")
		report(fallbackCommand, true)
		result, err = media.Run(ctx, ffmpegPath, fallback.Args, 0, nil)
	}
	if err != nil {
		media.RemoveFileQuietly(tempCover)
		logrus.WithFields(logrus.Fields{
			"video":  videoPath,
			"error":  err,
			"stderr": stderrOf(result),
		}).Warn("failed to extract cover")
		return "", fmt.Errorf("ffmpeg failed: %w", err)
	}

	// 校验临时封面确实可解码后再发布
	if err := media.ValidateOutput(ctx, ffmpegPath, "", tempCover); err != nil {
		media.RemoveFileQuietly(tempCover)
		logrus.WithFields(logrus.Fields{
			"video": videoPath,
			"error": err.Error(),
		}).Warn("封面文件校验失败")
		return "", fmt.Errorf("cover validation failed: %w", err)
	}
	if err := media.PublishCover(tempCover, coverPath); err != nil {
		media.RemoveFileQuietly(tempCover)
		return "", err
	}

	logrus.WithFields(logrus.Fields{
		"video": videoPath,
		"cover": coverPath,
	}).Info("cover extracted successfully")

	return coverPath, nil
}

// stderrOf 提取 FFmpeg 执行结果中的 stderr，便于排查失败原因
func stderrOf(result *media.RunResult) string {
	if result == nil {
		return ""
	}
	return strings.TrimSpace(result.Stderr)
}

// RenameCover 重命名封面文件以匹配视频文件名
// oldVideoPath: 原视频文件路径（封面文件应该与此同名）
// newVideoPath: 新视频文件路径（封面文件将改名以匹配此文件）
func RenameCover(oldVideoPath, newVideoPath string) error {
	// 构建旧封面路径
	oldExt := filepath.Ext(oldVideoPath)
	oldCoverPath := strings.TrimSuffix(oldVideoPath, oldExt) + ".jpg"

	// 检查旧封面是否存在
	if _, err := os.Stat(oldCoverPath); os.IsNotExist(err) {
		// 旧封面不存在，无需操作
		return nil
	}

	// 构建新封面路径
	newExt := filepath.Ext(newVideoPath)
	newCoverPath := strings.TrimSuffix(newVideoPath, newExt) + ".jpg"

	// 如果路径相同，无需操作
	if oldCoverPath == newCoverPath {
		return nil
	}

	// 重命名
	if err := os.Rename(oldCoverPath, newCoverPath); err != nil {
		return fmt.Errorf("failed to rename cover: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"old": oldCoverPath,
		"new": newCoverPath,
	}).Debug("cover renamed")

	return nil
}
