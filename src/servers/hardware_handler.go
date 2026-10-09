package servers

import (
	"fmt"
	"net/http"
	"reflect"

	"github.com/bililive-go/bililive-go/src/configs"
	"github.com/bililive-go/bililive-go/src/media"
	"github.com/bililive-go/bililive-go/src/pkg/utils"
)

// getHardwareStatusHandler 返回 RK3588 硬件处理能力状态。
// 所有能力字段都来自"当前配置指定的 FFmpeg"的真实探测，available 以真实编解码自检为准。
//
// GET /api/hardware/status
// 查询参数 refresh=1：忽略缓存，重新探测并重新执行真实自检
func getHardwareStatusHandler(w http.ResponseWriter, r *http.Request) {
	options := media.Options{}
	if cfg := configs.GetCurrentConfig(); cfg != nil {
		options = media.OptionsFromConfig(&cfg.OnRecordFinished)
	}

	ffmpegPath := ""
	if path, err := utils.GetFFmpegPath(r.Context()); err == nil {
		ffmpegPath = path
	}

	forceRefresh := r.URL.Query().Get("refresh") == "1"
	writeJSON(w, media.BuildStatus(r.Context(), ffmpegPath, options, forceRefresh))
}

// applyRK3588Updates 按子字段合并全局配置里的 on_record_finished.rk3588。
// 未出现的子字段保持原值；显式传 null 时重置为默认值（等价于关闭硬件处理）。
func applyRK3588Updates(c *configs.Config, orf map[string]interface{}) error {
	raw, exists := orf["rk3588"]
	if !exists {
		return nil
	}
	if raw == nil {
		c.OnRecordFinished.RK3588 = configs.DefaultRK3588Config()
		return nil
	}
	updates, ok := raw.(map[string]interface{})
	if !ok {
		return fmt.Errorf("rk3588 配置格式无效，应为对象")
	}

	// 以当前值为基做副本，避免就地改写可能被其它 goroutine 读取的配置快照
	rk := c.OnRecordFinished.RK3588.CloneWithDefaults()
	applyRK3588Fields(rk, updates)
	if err := configs.ValidateRK3588(rk); err != nil {
		return fmt.Errorf("RK3588 硬件处理配置无效: %w", err)
	}
	c.OnRecordFinished.RK3588 = rk
	return nil
}

// applyRK3588Fields 把请求中的 rk3588 子字段写入目标配置（只覆盖出现的字段）
func applyRK3588Fields(rk *configs.RK3588Config, updates map[string]interface{}) {
	if rk == nil {
		return
	}
	if enable, ok := updates["enable"].(bool); ok {
		rk.Enable = enable
	}
	if compress, ok := updates["compress"].(bool); ok {
		rk.Compress = compress
	}
	if codec, ok := updates["codec"].(string); ok {
		rk.Codec = codec
	}
	if bitrate, ok := updates["bitrate"].(string); ok {
		rk.Bitrate = bitrate
	}
	if decodeFallback, ok := updates["decode_fallback"].(bool); ok {
		rk.DecodeFallback = decodeFallback
	}
	if scaleWidth, ok := updates["scale_width"].(float64); ok {
		rk.ScaleWidth = int(scaleWidth)
	}
	if scaleHeight, ok := updates["scale_height"].(float64); ok {
		rk.ScaleHeight = int(scaleHeight)
	}
}

// applyOnRecordFinishedOverride 把房间/平台级的 on_record_finished 覆盖按子字段合并进目标结构体。
// 只覆盖请求中出现的字段，避免"只改了转 mp4"的房间把烧录/上传等既有覆盖整段清掉。
// rk3588 覆盖非法时返回错误，由调用方中止配置写入。
func applyOnRecordFinishedOverride(target *configs.OnRecordFinished, updates map[string]interface{}) error {
	if target == nil {
		return nil
	}
	if convertToMp4, ok := updates["convert_to_mp4"].(bool); ok {
		target.ConvertToMp4 = convertToMp4
	}
	if deleteFlv, ok := updates["delete_flv_after_convert"].(bool); ok {
		target.DeleteFlvAfterConvert = deleteFlv
	}
	if customCmd, ok := updates["custom_commandline"].(string); ok {
		target.CustomCommandline = customCmd
	}
	if fixFlv, ok := updates["fix_flv_at_first"].(bool); ok {
		target.FixFlvAtFirst = fixFlv
	}
	if burnSubtitles, ok := updates["burn_subtitles"].(bool); ok {
		target.BurnSubtitles = burnSubtitles
	}
	if codec, ok := updates["burn_subtitles_codec"].(string); ok {
		target.BurnSubtitlesCodec = codec
	}
	if crf, ok := updates["burn_subtitles_crf"].(string); ok {
		target.BurnSubtitlesCrf = crf
	}
	if preset, ok := updates["burn_subtitles_preset"].(string); ok {
		target.BurnSubtitlesPreset = preset
	}
	if deleteAss, ok := updates["burn_delete_ass"].(bool); ok {
		target.BurnDeleteAss = deleteAss
	}
	if deleteSource, ok := updates["burn_delete_source"].(bool); ok {
		target.BurnDeleteSource = deleteSource
	}
	if uploadTiming, ok := updates["upload_timing"].(string); ok {
		target.UploadTiming = configs.UploadTiming(uploadTiming)
	}

	// rk3588 子字段合并；显式 null 表示该房间/平台不覆盖硬件处理（继承全局）
	raw, exists := updates["rk3588"]
	if !exists {
		return nil
	}
	if raw == nil {
		target.RK3588 = nil
		return nil
	}
	rkUpdates, ok := raw.(map[string]interface{})
	if !ok {
		return fmt.Errorf("rk3588 覆盖格式无效，应为对象")
	}

	// 基值优先取该房间已有的覆盖，否则取全局设置，使未出现的子字段自然继承
	base := target.RK3588
	if base == nil {
		base = globalRK3588OrDefault()
	}
	rk := base.CloneWithDefaults()
	applyRK3588Fields(rk, rkUpdates)
	if err := configs.ValidateRK3588(rk); err != nil {
		return fmt.Errorf("RK3588 硬件处理配置无效: %w", err)
	}
	target.RK3588 = rk
	return nil
}

// validateRK3588Override 在写入配置之前校验请求中的 rk3588 覆盖，
// 使房间/平台级接口可以直接返回 400，而不是写入一半再报错。
// existing 必须是"该房间/平台当前已有的覆盖"，与真正合并时的基值保持一致。
func validateRK3588Override(updates map[string]interface{}, existing *configs.RK3588Config) error {
	orfRaw, exists := updates["on_record_finished"]
	if !exists {
		return nil
	}
	orfUpdates, ok := orfRaw.(map[string]interface{})
	if !ok {
		return nil
	}
	raw, exists := orfUpdates["rk3588"]
	if !exists || raw == nil {
		// null 表示恢复继承，合法
		return nil
	}
	rkUpdates, ok := raw.(map[string]interface{})
	if !ok {
		return fmt.Errorf("rk3588 覆盖格式无效，应为对象")
	}
	base := existing
	if base == nil {
		base = globalRK3588OrDefault()
	}
	rk := base.CloneWithDefaults()
	applyRK3588Fields(rk, rkUpdates)
	if err := configs.ValidateRK3588(rk); err != nil {
		return fmt.Errorf("RK3588 硬件处理配置无效: %w", err)
	}
	return nil
}

// existingRK3588ForPlatform 返回平台级已有的 rk3588 覆盖
func existingRK3588ForPlatform(platformKey string) *configs.RK3588Config {
	cfg := configs.GetCurrentConfig()
	if cfg == nil {
		return nil
	}
	platformConfig, ok := cfg.PlatformConfigs[platformKey]
	if !ok || platformConfig.OnRecordFinished == nil {
		return nil
	}
	return platformConfig.OnRecordFinished.RK3588
}

// existingRK3588ForRoomUrl 返回房间级已有的 rk3588 覆盖
func existingRK3588ForRoomUrl(roomUrl string) *configs.RK3588Config {
	cfg := configs.GetCurrentConfig()
	if cfg == nil {
		return nil
	}
	room, err := cfg.GetLiveRoomByUrl(roomUrl)
	if err != nil || room == nil || room.OnRecordFinished == nil {
		return nil
	}
	return room.OnRecordFinished.RK3588
}

// existingRK3588ForLiveID 按 LiveId 返回房间级已有的 rk3588 覆盖
func existingRK3588ForLiveID(liveID string) *configs.RK3588Config {
	cfg := configs.GetCurrentConfig()
	if cfg == nil || liveID == "" {
		return nil
	}
	for i := range cfg.LiveRooms {
		room := &cfg.LiveRooms[i]
		if string(room.LiveId) != liveID || room.OnRecordFinished == nil {
			continue
		}
		return room.OnRecordFinished.RK3588
	}
	return nil
}

// overridableBaseOnRecordFinished 返回指定平台层级"当前生效"的录制后处理配置。
// 房间级覆盖的基值优先取平台级覆盖，其次取全局配置：首次创建覆盖时以它为基，
// 保证只提交 rk3588 的请求不会把 burn_subtitles/cloud_upload 等未提交字段零值覆盖。
func overridableBaseOnRecordFinished(cfg *configs.Config, platformKey string) configs.OnRecordFinished {
	if cfg == nil {
		return configs.OnRecordFinished{}
	}
	if platformKey != "" {
		if platformConfig, ok := cfg.PlatformConfigs[platformKey]; ok && platformConfig.OnRecordFinished != nil {
			return *platformConfig.OnRecordFinished
		}
	}
	return cfg.OnRecordFinished
}

// onRecordFinishedOverrideIsRedundant 判断该覆盖是否已经等价于基值（忽略 rk3588 字段）：
// 例如客户端先把 rk3588 改掉、再把它置回继承（null），此时不应残留一个多余覆盖对象，
// 应当直接删除覆盖以真正恢复继承上层设置。
func onRecordFinishedOverrideIsRedundant(override, base configs.OnRecordFinished) bool {
	if override.RK3588 != nil {
		return false
	}
	overrideCopy := override
	overrideCopy.RK3588 = nil
	baseCopy := base
	baseCopy.RK3588 = nil
	return reflect.DeepEqual(overrideCopy, baseCopy)
}

// globalRK3588OrDefault 返回全局配置的 rk3588 副本，用于房间/平台级子字段合并的基值
func globalRK3588OrDefault() *configs.RK3588Config {
	if cfg := configs.GetCurrentConfig(); cfg != nil && cfg.OnRecordFinished.RK3588 != nil {
		return cfg.OnRecordFinished.RK3588.CloneWithDefaults()
	}
	return configs.DefaultRK3588Config()
}
