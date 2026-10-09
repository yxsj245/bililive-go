package servers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"github.com/bililive-go/bililive-go/src/configs"
	"github.com/bililive-go/bililive-go/src/pipeline"
	"github.com/bililive-go/bililive-go/src/types"
)

// RegisterPipelineHandlers 注册 Pipeline 任务管理相关的 HTTP 处理器
// 注意：r 已经是 /api 前缀的子路由器
func RegisterPipelineHandlers(r *mux.Router, pm *pipeline.Manager) {
	if pm == nil {
		return
	}

	// 获取 Pipeline 任务列表
	r.HandleFunc("/pipeline/tasks", makePipelineListTasksHandler(pm)).Methods("GET")

	// 获取队列统计
	r.HandleFunc("/pipeline/tasks/stats", makePipelineGetStatsHandler(pm)).Methods("GET")

	// 清除已完成的任务
	r.HandleFunc("/pipeline/tasks/clear-completed", makePipelineClearCompletedHandler(pm)).Methods("POST")

	// 获取单个任务
	r.HandleFunc("/pipeline/tasks/{id}", makePipelineGetTaskHandler(pm)).Methods("GET")

	// 取消任务
	r.HandleFunc("/pipeline/tasks/{id}/cancel", makePipelineCancelTaskHandler(pm)).Methods("POST")

	// 重试任务
	r.HandleFunc("/pipeline/tasks/{id}/retry", makePipelineRetryTaskHandler(pm)).Methods("POST")

	// 删除任务
	r.HandleFunc("/pipeline/tasks/{id}", makePipelineDeleteTaskHandler(pm)).Methods("DELETE")

	// 批量烧录弹幕字幕
	r.HandleFunc("/pipeline/batch-burn", makeBatchBurnHandler(pm)).Methods("POST")

	// 批量硬件压缩（无 ASS 字幕也处理，要求已启用硬件处理）
	r.HandleFunc("/pipeline/batch-compress", makeBatchCompressHandler(pm)).Methods("POST")
}

// makePipelineListTasksHandler 列出 Pipeline 任务
func makePipelineListTasksHandler(pm *pipeline.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filter := pipeline.TaskFilter{}

		// 解析查询参数
		if status := r.URL.Query().Get("status"); status != "" {
			s := pipeline.PipelineStatus(status)
			filter.Status = &s
		}
		if liveID := r.URL.Query().Get("live_id"); liveID != "" {
			filter.LiveID = &liveID
		}
		if limit := r.URL.Query().Get("limit"); limit != "" {
			if l, err := strconv.Atoi(limit); err == nil {
				filter.Limit = l
			}
		}
		if offset := r.URL.Query().Get("offset"); offset != "" {
			if o, err := strconv.Atoi(offset); err == nil {
				filter.Offset = o
			}
		}

		tasks, err := pm.ListTasks(filter)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// 确保返回空数组而不是 null
		if tasks == nil {
			tasks = []*pipeline.PipelineTask{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tasks)
	}
}

// makePipelineGetStatsHandler 获取队列统计
func makePipelineGetStatsHandler(pm *pipeline.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stats, err := pm.GetStats()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
	}
}

// makePipelineClearCompletedHandler 清除已完成的任务
func makePipelineClearCompletedHandler(pm *pipeline.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		count, err := pm.ClearCompletedTasks()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "success",
			"deleted": count,
		})
	}
}

// makePipelineGetTaskHandler 获取单个任务
func makePipelineGetTaskHandler(pm *pipeline.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		id, err := strconv.ParseInt(vars["id"], 10, 64)
		if err != nil {
			http.Error(w, "invalid task id", http.StatusBadRequest)
			return
		}

		task, err := pm.GetTask(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if task == nil {
			http.Error(w, "task not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(task)
	}
}

// makePipelineCancelTaskHandler 取消任务
func makePipelineCancelTaskHandler(pm *pipeline.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		id, err := strconv.ParseInt(vars["id"], 10, 64)
		if err != nil {
			http.Error(w, "invalid task id", http.StatusBadRequest)
			return
		}

		if err := pm.CancelTask(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "cancelled"})
	}
}

// makePipelineRetryTaskHandler 重试任务
// 支持可选的 JSON body: {"stage_name": "cloud_upload"}
// 无 body 或 stage_name 为空时全量重试
func makePipelineRetryTaskHandler(pm *pipeline.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		id, err := strconv.ParseInt(vars["id"], 10, 64)
		if err != nil {
			http.Error(w, "invalid task id", http.StatusBadRequest)
			return
		}

		// 解析可选的 JSON body
		var reqBody struct {
			StageName string `json:"stage_name"`
		}
		if r.Body != nil {
			if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil && err != io.EOF {
				http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
				return
			}
		}

		if reqBody.StageName != "" {
			// 从指定阶段重试
			if err := pm.RetryTaskFromStage(id, reqBody.StageName); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		} else {
			// 全量重试（向后兼容）
			if err := pm.RetryTask(id); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "retried"})
	}
}

// makePipelineDeleteTaskHandler 删除任务
func makePipelineDeleteTaskHandler(pm *pipeline.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		id, err := strconv.ParseInt(vars["id"], 10, 64)
		if err != nil {
			http.Error(w, "invalid task id", http.StatusBadRequest)
			return
		}

		if err := pm.DeleteTask(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
	}
}

// isVideoFile 判断文件扩展名是否为常见视频格式
func isVideoFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".mp4", ".flv", ".ts", ".mkv", ".avi", ".mov", ".wmv", ".webm":
		return true
	}
	return false
}

// batchMediaMode 批量处理模式
type batchMediaMode int

const (
	// batchMediaModeBurn 批量烧录：要求存在同名 ASS（启用了无弹幕压缩时允许无 ASS）
	batchMediaModeBurn batchMediaMode = iota
	// batchMediaModeCompress 批量压缩：无 ASS 也处理，但要求已启用硬件处理
	batchMediaModeCompress
)

// batchMediaRequest 批量处理请求体
type batchMediaRequest struct {
	Paths []string `json:"paths"`
}

// batchMediaResponse 批量处理响应体
type batchMediaResponse struct {
	Enqueued int      `json:"enqueued"`
	Skipped  []string `json:"skipped"`
	TaskIDs  []int64  `json:"task_ids"`
}

// batchWritingCheckInterval 判断文件是否仍在写入的采样间隔。
// 采样期间体积或修改时间发生变化即视为"正在写入"（可能正在录制），该文件直接拒绝处理。
const batchWritingCheckInterval = 600 * time.Millisecond

// makeBatchBurnHandler 批量烧录弹幕字幕
// POST /api/pipeline/batch-burn
func makeBatchBurnHandler(pm *pipeline.Manager) http.HandlerFunc {
	return makeBatchMediaHandler(pm, batchMediaModeBurn)
}

// makeBatchCompressHandler 批量硬件压缩，无 ASS 字幕也处理
// POST /api/pipeline/batch-compress
func makeBatchCompressHandler(pm *pipeline.Manager) http.HandlerFunc {
	return makeBatchMediaHandler(pm, batchMediaModeCompress)
}

// makeBatchMediaHandler 批量处理的公共实现。
// 两个入口共用 pipeline.BuildMediaStageConfig 生成的阶段配置，
// 因此自动后处理与手动批量处理的硬件参数完全一致。
func makeBatchMediaHandler(pm *pipeline.Manager, mode batchMediaMode) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req batchMediaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if len(req.Paths) == 0 {
			http.Error(w, "paths is required", http.StatusBadRequest)
			return
		}

		config := configs.GetCurrentConfig()
		if config == nil {
			http.Error(w, "config not available", http.StatusInternalServerError)
			return
		}
		legacy := &config.OnRecordFinished

		// 批量压缩必须启用硬件处理：否则会退化为软件编码，与设备能力不符
		if mode == batchMediaModeCompress && !legacy.RK3588.IsEnabled() {
			writeJsonWithStatusCode(w, http.StatusBadRequest, commonResp{
				ErrNo:  http.StatusBadRequest,
				ErrMsg: "未启用 RK3588 硬件处理（on_record_finished.rk3588.enable），已拒绝批量压缩以避免使用软件编码",
			})
			return
		}

		stageConfig := pipeline.BuildMediaStageConfig(legacy, mode == batchMediaModeCompress)
		mediaPipeline := &pipeline.PipelineConfig{Stages: []pipeline.StageConfig{stageConfig}}

		outputPath := config.OutPutPath
		// 无 ASS 时是否仍然处理：批量压缩入口始终处理；批量烧录取决于是否启用了无弹幕压缩
		compressEnabled := legacy.RK3588.SupportsCompress() || mode == batchMediaModeCompress

		skipped := make([]string, 0)
		seen := make(map[string]bool, len(req.Paths))
		type batchCandidate struct {
			absPath string
			relPath string
		}
		candidates := make([]batchCandidate, 0, len(req.Paths))

		// 第一轮：路径安全校验 + 类型校验 + 规范化去重
		for _, p := range req.Paths {
			absPath, err := resolveBatchMediaPath(outputPath, p)
			if err != nil {
				skipped = append(skipped, filepath.Base(p)+" - "+err.Error())
				continue
			}
			if !isVideoFile(absPath) {
				skipped = append(skipped, filepath.Base(p)+" - 非视频文件")
				continue
			}
			// 解析软链接后的规范路径用于去重：同一文件在一次请求里只入队一次
			if seen[absPath] {
				skipped = append(skipped, filepath.Base(p)+" - 重复文件（本次请求已包含）")
				continue
			}
			if !compressEnabled {
				assPath := strings.TrimSuffix(absPath, filepath.Ext(absPath)) + ".ass"
				if _, err := os.Stat(assPath); err != nil {
					skipped = append(skipped, filepath.Base(p)+" - 无 ASS 字幕文件")
					continue
				}
			}
			seen[absPath] = true

			relPath := p
			if rel, err := filepath.Rel(outputPath, absPath); err == nil {
				relPath = rel
			}
			candidates = append(candidates, batchCandidate{absPath: absPath, relPath: relPath})
		}

		// 第二轮：识别仍在写入的文件（可能正在录制），一次采样覆盖全部候选
		writingPaths := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			writingPaths = append(writingPaths, candidate.absPath)
		}
		writing := detectWritingFiles(writingPaths)

		result := batchMediaResponse{Skipped: skipped, TaskIDs: []int64{}}
		for _, candidate := range candidates {
			if writing[candidate.absPath] {
				result.Skipped = append(result.Skipped,
					filepath.Base(candidate.absPath)+" - 文件仍在写入（可能正在录制），已拒绝处理")
				continue
			}

			files := []pipeline.FileInfo{pipeline.NewVideoFileInfo(candidate.absPath)}
			recordInfo := pipeline.RecordInfo{
				LiveID:      batchMediaLiveID(mode),
				Platform:    batchMediaPlatform(mode),
				HostName:    candidate.relPath,
				DisplayName: candidate.relPath,
				StartTime:   time.Now(),
			}
			task := pipeline.NewPipelineTask(recordInfo, mediaPipeline, files)

			if err := pm.EnqueueTask(task, nil); err != nil {
				result.Skipped = append(result.Skipped,
					filepath.Base(candidate.absPath)+" - 入队失败: "+err.Error())
				continue
			}
			result.Enqueued++
			result.TaskIDs = append(result.TaskIDs, task.ID)
		}

		writeJSON(w, result)
	}
}

// batchMediaLiveID 返回批量任务的 LiveID 标识
func batchMediaLiveID(mode batchMediaMode) types.LiveID {
	if mode == batchMediaModeCompress {
		return types.LiveID("batch-compress")
	}
	return types.LiveID("batch-burn")
}

// batchMediaPlatform 返回批量任务的展示用来源
func batchMediaPlatform(mode batchMediaMode) string {
	if mode == batchMediaModeCompress {
		return "手动批量压缩"
	}
	return "手动批量烧录"
}

// resolveBatchMediaPath 校验并解析批量处理的文件路径，返回解析软链接后的规范绝对路径。
//
// getSafePath 只做词法校验（阻止 ../ 形式的越权），这里补上：
//  1. EvalSymlinks 解析软链接后再次确认仍在输出目录内（阻止软链接指向目录外）；
//  2. 目标必须是常规文件（排除目录、设备文件、FIFO 等）。
func resolveBatchMediaPath(outputPath, input string) (string, error) {
	absPath, err := getSafePath(outputPath, input)
	if err != nil {
		return "", fmt.Errorf("路径越权")
	}
	resolved, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", fmt.Errorf("文件不存在或无法访问")
	}

	root := outputPath
	if rootResolved, err := filepath.EvalSymlinks(outputPath); err == nil && rootResolved != "" {
		root = rootResolved
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("输出目录不可用")
	}
	rel, err := filepath.Rel(rootAbs, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("路径越权（解析软链接后超出输出目录）")
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("文件不存在或无法访问")
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("不是常规文件")
	}
	return resolved, nil
}

// detectWritingFiles 采样两次文件状态，识别仍在写入（体积或修改时间发生变化）的文件。
// 正在录制的文件在处理期间还会继续增长，读出的是半截录像，因此批量入口直接拒绝处理。
// 采样对所有候选文件一次完成，总耗时固定为一个采样间隔。
func detectWritingFiles(paths []string) map[string]bool {
	result := make(map[string]bool, len(paths))
	if len(paths) == 0 {
		return result
	}

	before := make(map[string]os.FileInfo, len(paths))
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil {
			before[path] = info
		}
	}
	if len(before) == 0 {
		return result
	}

	time.Sleep(batchWritingCheckInterval)

	for path, info := range before {
		after, err := os.Stat(path)
		if err != nil {
			result[path] = true // 采样期间文件消失，视为不可处理
			continue
		}
		if after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
			result[path] = true
		}
	}
	return result
}
