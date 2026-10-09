package stages

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bililive-go/bililive-go/src/media"
	"github.com/bililive-go/bililive-go/src/pipeline"
	"github.com/bililive-go/bililive-go/src/pkg/utils"
	"github.com/bililive-go/bililive-go/src/tools"
)

// BurnSubtitlesStage 弹幕烧录 / 硬件压缩阶段。
//
// 同一阶段内完成两种处理，避免"先烧录再压缩"的二次编码：
//   - 有同名 .ass 且启用了烧录：硬解 → 弹幕烧录 → 硬编码（H.265/H.264）
//   - 无 .ass 且启用压缩：硬解 → 硬编码，独立完成 H.265 压缩
//
// 输出规则：先写入同目录临时文件，校验通过后再原子发布；
// 目标文件已存在时自动改用带序号的新文件名，绝不覆盖现存文件。
// 失败或取消时只清理临时文件，源视频与弹幕字幕全部保留。
type BurnSubtitlesStage struct {
	config pipeline.StageConfig

	codec          string
	crf            string
	preset         string
	bitrate        string
	hardware       bool
	burnEnabled    bool
	compress       bool
	decodeFallback bool
	scaleWidth     int
	scaleHeight    int
	deleteAss      bool
	deleteSource   bool

	// capability 能力探测结果，一次任务内复用
	capability *media.Capability
	probed     bool

	commands []string
	logs     string
}

// NewBurnSubtitlesStage 创建弹幕烧录/压缩阶段工厂
func NewBurnSubtitlesStage(config pipeline.StageConfig) (pipeline.Stage, error) {
	return &BurnSubtitlesStage{
		config:         config,
		codec:          config.GetStringOption(pipeline.OptionCodec, media.DefaultSoftwareCodec),
		crf:            config.GetStringOption(pipeline.OptionCrf, media.DefaultCRF),
		preset:         config.GetStringOption(pipeline.OptionPreset, media.DefaultPreset),
		bitrate:        config.GetStringOption(pipeline.OptionBitrate, media.DefaultBitrate),
		hardware:       config.GetBoolOption(pipeline.OptionHardware, false),
		burnEnabled:    config.GetBoolOption(pipeline.OptionBurnEnabled, true),
		compress:       config.GetBoolOption(pipeline.OptionCompress, false),
		decodeFallback: config.GetBoolOption(pipeline.OptionDecodeFallback, true),
		scaleWidth:     config.GetIntOption(pipeline.OptionScaleWidth, 0),
		scaleHeight:    config.GetIntOption(pipeline.OptionScaleHeight, 0),
		deleteAss:      config.GetBoolOption(pipeline.OptionBurnDeleteAss, false),
		deleteSource:   config.GetBoolOption(pipeline.OptionBurnDeleteSource, false),
	}, nil
}

func (s *BurnSubtitlesStage) Name() string {
	return pipeline.StageNameBurnSubtitles
}

func (s *BurnSubtitlesStage) options() media.Options {
	return media.Options{
		Enabled:        s.hardware,
		Compress:       s.compress,
		Codec:          s.codec,
		Bitrate:        s.bitrate,
		CRF:            s.crf,
		Preset:         s.preset,
		DecodeFallback: s.decodeFallback,
		ScaleWidth:     s.scaleWidth,
		ScaleHeight:    s.scaleHeight,
	}
}

func (s *BurnSubtitlesStage) Execute(ctx *pipeline.PipelineContext, input []pipeline.FileInfo) ([]pipeline.FileInfo, error) {
	if len(input) == 0 {
		s.logs = "没有输入文件"
		return input, nil
	}

	ffmpegPath, err := s.resolveFFmpegPath(ctx)
	if err != nil {
		s.logs += ffmpegPath + "\n"
		return nil, err
	}

	output := make([]pipeline.FileInfo, 0, len(input)+2)

	for _, file := range input {
		// 任务已取消：直接返回取消错误，交由 Executor 标记为 cancelled，
		// 避免取消过程中产生的探测/编码失败被误报成硬件错误
		if err := ctx.Ctx.Err(); err != nil {
			return nil, err
		}

		// 只处理视频文件
		if file.Type != pipeline.FileTypeVideo {
			output = append(output, file)
			continue
		}

		// 跳过已标记待删除的文件（如 ConvertMp4 阶段标记的原始 FLV），
		// 但仍将其透传到 output，保证 Executor 的延迟删除机制正常工作。
		if file.Deletable {
			output = append(output, file)
			continue
		}

		if _, err := os.Stat(file.Path); os.IsNotExist(err) {
			s.logs += fmt.Sprintf("文件不存在: %s\n", file.Path)
			continue
		}

		// 查找同名 .ass 文件（未启用烧录时不查找）
		assPath := ""
		if s.burnEnabled {
			assPath = s.findAssFile(file.Path)
		}
		if assPath == "" && !s.compress {
			s.logs += fmt.Sprintf("未找到 ASS 字幕文件且未启用无弹幕压缩，跳过: %s\n", filepath.Base(file.Path))
			output = append(output, file)
			continue
		}

		// 记录处理前的源文件状态：处理结束后若体积/修改时间发生变化，说明该文件仍在被写入
		// （例如仍在录制的录像），此时不标记删除，保证源文件不被误删
		sourceStatBefore, _ := os.Stat(file.Path)

		outputPath := s.buildOutputPath(file.Path, assPath != "")
		// 使用 os.CreateTemp 生成唯一临时文件，避免并发任务或重复路径互相覆盖
		tempFile, err := media.CreateTempOutput(outputPath)
		if err != nil {
			s.logs += err.Error() + "\n"
			return nil, err
		}

		finalPath, err := s.processFile(ctx, ffmpegPath, file.Path, assPath, outputPath, tempFile)
		if err != nil {
			// 失败或取消都要清理临时文件；源视频与 ASS 弹幕一律保留，供用户重试
			media.RemoveFileQuietly(tempFile)
			s.logs += fmt.Sprintf("处理失败（源文件与弹幕已保留）: %s - %s\n", filepath.Base(file.Path), err.Error())
			return nil, fmt.Errorf("处理 %s 失败: %w", filepath.Base(file.Path), err)
		}

		// 添加输出文件
		output = append(output, pipeline.FileInfo{
			Path:       finalPath,
			Type:       pipeline.FileTypeVideo,
			SourcePath: file.Path,
		})

		// 可选：标记 ASS 文件为可删除（由 Executor 在管道全部成功后统一删除）
		if s.deleteAss && assPath != "" {
			output = append(output, pipeline.FileInfo{
				Path:      assPath,
				Type:      pipeline.FileTypeOther,
				Deletable: true,
			})
			s.logs += fmt.Sprintf("已标记 ASS 文件待删除: %s\n", assPath)
			ctx.Logger.Infof("已标记 ASS 文件待删除: %s", assPath)
		}

		// 可选：标记源视频文件为可删除（由 Executor 在管道全部成功后统一删除）
		if s.deleteSource && file.Path != finalPath {
			if sourceChangedSince(file.Path, sourceStatBefore) {
				// 失败保护：源文件在处理期间仍在变化，说明它正在被写入，绝不能删除
				s.logs += fmt.Sprintf("源文件在处理期间仍在变化，已保留不删除: %s\n", file.Path)
				ctx.Logger.Warnf("源文件在处理期间仍在变化，已保留源文件: %s", file.Path)
			} else {
				file.Deletable = true
				s.logs += fmt.Sprintf("已标记源视频文件待删除: %s\n", file.Path)
				ctx.Logger.Infof("已标记源视频文件待删除: %s", file.Path)
			}
		}
		output = append(output, file)

		s.logs += fmt.Sprintf("处理完成: %s -> %s\n", filepath.Base(file.Path), filepath.Base(finalPath))
		ctx.Logger.Infof("弹幕烧录/压缩完成: %s", finalPath)
	}

	return output, nil
}

// resolveFFmpegPath 解析本次处理要使用的 FFmpeg 路径
func (s *BurnSubtitlesStage) resolveFFmpegPath(ctx *pipeline.PipelineContext) (string, error) {
	if ctx.FFmpegPath != "" {
		return ctx.FFmpegPath, nil
	}
	// FFmpeg 可能仍在后台异步下载（首次启动场景），等待其就绪后再查找，
	// 避免短直播录制结束时后处理任务因 FFmpeg 未下载完成而失败。
	// 等待被中断（如 pipeline ctx 取消）时立即退出，不再启动处理子进程
	if waitErr := tools.WaitFFmpegAsyncInitDone(ctx.Ctx, nil); waitErr != nil {
		return fmt.Sprintf("等待 FFmpeg 就绪被中断: %s", waitErr.Error()), waitErr
	}
	ffmpegPath, err := utils.GetFFmpegPath(ctx.Ctx)
	if err != nil {
		return fmt.Sprintf("ffmpeg 不可用: %s", err.Error()), fmt.Errorf("ffmpeg not available: %w", err)
	}
	return ffmpegPath, nil
}

// getCapability 返回当前 FFmpeg 的能力（一次任务内只探测一次）
func (s *BurnSubtitlesStage) getCapability(ctx *pipeline.PipelineContext, ffmpegPath string) *media.Capability {
	if !s.probed {
		s.probed = true
		s.capability = media.ProbeCached(ctx.Ctx, ffmpegPath, false)
	}
	return s.capability
}

// processFile 处理单个视频文件：生成处理链 → 执行 → 校验 → 原子发布
func (s *BurnSubtitlesStage) processFile(
	ctx *pipeline.PipelineContext,
	ffmpegPath, inputPath, assPath, outputPath, tempFile string,
) (string, error) {
	// 把弹幕字幕暂存到临时目录下的固定文件名，彻底规避 FFmpeg 滤镜层的路径转义问题
	stagedAssPath := assPath
	if assPath != "" {
		if staged, cleanup, stageErr := media.StageAssFile(assPath); stageErr == nil {
			stagedAssPath = staged
			defer cleanup()
		} else {
			ctx.Logger.Warnf("暂存弹幕字幕失败，将直接使用原路径（需要转义）: %v", stageErr)
			s.logs += fmt.Sprintf("暂存弹幕字幕失败: %s\n", stageErr.Error())
		}
	}

	capability := s.getCapability(ctx, ffmpegPath)
	info := media.ProbeMediaInfo(ctx.Ctx, ffmpegPath, inputPath)

	inputCodec := ""
	duration := 0.0
	if info != nil {
		inputCodec = info.VideoCodec
		duration = info.Duration
	}

	buildRequest := func(forceSoftwareDecode bool) media.Request {
		return media.Request{
			FFmpegPath:          ffmpegPath,
			Input:               inputPath,
			Output:              tempFile,
			AssPath:             stagedAssPath,
			Options:             s.options(),
			Capability:          capability,
			InputCodec:          inputCodec,
			ForceSoftwareDecode: forceSoftwareDecode,
		}
	}

	plan, err := media.BuildPlan(buildRequest(false))
	if err != nil {
		// 取消导致的探测/规划失败按取消处理，不要报成硬件能力问题
		if ctxErr := ctx.Ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", err
	}

	runErr := s.runPlan(ctx, ffmpegPath, plan, duration)
	if runErr != nil && plan.HardwareDecode && s.decodeFallback && ctx.Ctx.Err() == nil {
		// 硬解失败（例如该输入格式的 rkmpp 解码器异常）：按配置回退软件解码重试一次，
		// 编码仍保持硬件编码，不引入软件编码。
		ctx.Logger.Warnf("硬件解码失败，改用软件解码重试: %v", runErr)
		s.logs += "硬件解码失败，改用软件解码重试\n"
		media.RemoveFileQuietly(tempFile)

		retryPlan, retryErr := media.BuildPlan(buildRequest(true))
		if retryErr != nil {
			if ctxErr := ctx.Ctx.Err(); ctxErr != nil {
				return "", ctxErr
			}
			return "", retryErr
		}
		plan = retryPlan
		runErr = s.runPlan(ctx, ffmpegPath, plan, duration)
	}
	if runErr != nil {
		return "", runErr
	}

	// 校验输出确实可用（含真实解码、时长与音轨比对），通过后才发布
	if err := media.ValidateOutput(ctx.Ctx, ffmpegPath, inputPath, tempFile); err != nil {
		return "", fmt.Errorf("输出校验未通过: %w", err)
	}

	finalPath, err := media.PublishTempFile(tempFile, outputPath)
	if err != nil {
		return "", err
	}
	return finalPath, nil
}

// runPlan 执行一次处理链
func (s *BurnSubtitlesStage) runPlan(ctx *pipeline.PipelineContext, ffmpegPath string, plan *media.Plan, duration float64) error {
	command := ffmpegPath + " " + strings.Join(plan.Args, " ")
	s.commands = append(s.commands, command)

	ctx.Logger.Infof("处理链: %s", plan.Summary())
	ctx.Logger.Infof("FFmpeg 命令: %s", command)
	for _, warning := range plan.Warnings {
		s.logs += warning + "\n"
		ctx.Logger.Warnf("处理链提示: %s", warning)
	}

	result, err := media.Run(ctx.Ctx, ffmpegPath, plan.Args, duration, nil)
	if err != nil {
		if result != nil && strings.TrimSpace(result.Stderr) != "" {
			ctx.Logger.Errorf("ffmpeg stderr: %s", tailForLog(result.Stderr))
		}
		return err
	}
	return nil
}

// buildOutputPath 计算输出文件名。
// 烧录输出统一为 .mkv（避免覆盖源文件）；纯压缩输出带 .h265/.h264 后缀以便区分。
// 目标已存在时的去重由 media.PublishTempFile 完成。
func (s *BurnSubtitlesStage) buildOutputPath(inputPath string, burn bool) string {
	ext := strings.ToLower(filepath.Ext(inputPath))
	base := strings.TrimSuffix(inputPath, ext)

	if burn {
		if ext == media.OutputExtension {
			// 源文件已经是 mkv，不能与源文件同名
			return base + ".burned" + media.OutputExtension
		}
		return base + media.OutputExtension
	}

	suffix := ".h265"
	if strings.HasPrefix(strings.ToLower(s.codec), "h264") {
		suffix = ".h264"
	}
	return base + suffix + media.OutputExtension
}

// tailForLog 截取 FFmpeg 错误输出的尾部，避免超长 stderr 刷屏
func tailForLog(text string) string {
	trimmed := strings.TrimSpace(text)
	const limit = 2000
	if len(trimmed) <= limit {
		return trimmed
	}
	return "..." + trimmed[len(trimmed)-limit:]
}

// sourceChangedSince 判断源文件自 before 之后是否被修改（体积或修改时间变化）。
// 用于避免把仍在写入的录像当作"已处理完成的源文件"删除。
func sourceChangedSince(path string, before os.FileInfo) bool {
	if before == nil {
		return true
	}
	after, err := os.Stat(path)
	if err != nil {
		return true
	}
	return after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime())
}

// findAssFile 查找与视频文件同名的 .ass 字幕文件
func (s *BurnSubtitlesStage) findAssFile(videoPath string) string {
	ext := strings.ToLower(filepath.Ext(videoPath))
	base := strings.TrimSuffix(videoPath, ext)
	assPath := base + ".ass"
	if info, err := os.Stat(assPath); err == nil && !info.IsDir() {
		return assPath
	}
	return ""
}

func (s *BurnSubtitlesStage) GetCommands() []string {
	return s.commands
}

func (s *BurnSubtitlesStage) GetLogs() string {
	return s.logs
}
