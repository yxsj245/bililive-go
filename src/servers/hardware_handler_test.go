package servers

import (
	"testing"

	"github.com/bililive-go/bililive-go/src/configs"
)

func TestApplyRK3588UpdatesPartialMerge(t *testing.T) {
	c := configs.NewConfig()
	c.OnRecordFinished.RK3588 = &configs.RK3588Config{
		Enable:         true,
		Compress:       true,
		Codec:          configs.RK3588CodecHEVC,
		Bitrate:        "6M",
		DecodeFallback: true,
		ScaleWidth:     1280,
		ScaleHeight:    720,
	}

	// 只传部分子字段：其余字段必须保持原值
	updates := map[string]interface{}{
		"rk3588": map[string]interface{}{
			"bitrate": "8M",
		},
	}
	if err := applyRK3588Updates(c, updates); err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	rk := c.OnRecordFinished.RK3588
	if rk.Bitrate != "8M" {
		t.Errorf("码率未更新: %s", rk.Bitrate)
	}
	if !rk.Enable || !rk.Compress || rk.Codec != configs.RK3588CodecHEVC {
		t.Errorf("未出现的子字段被清空: %+v", rk)
	}
	if rk.ScaleWidth != 1280 || rk.ScaleHeight != 720 {
		t.Errorf("缩放设置被清空: %+v", rk)
	}
}

func TestApplyRK3588UpdatesNullResetsToDefault(t *testing.T) {
	c := configs.NewConfig()
	c.OnRecordFinished.RK3588.Enable = true

	updates := map[string]interface{}{"rk3588": nil}
	if err := applyRK3588Updates(c, updates); err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	rk := c.OnRecordFinished.RK3588
	if rk == nil {
		t.Fatal("全局配置的 rk3588 重置后仍应是默认对象")
	}
	if rk.Enable || rk.Compress {
		t.Fatalf("显式 null 应重置为默认（未启用）: %+v", rk)
	}
	if rk.Codec != configs.RK3588CodecHEVC || rk.Bitrate != "4M" || !rk.DecodeFallback {
		t.Fatalf("重置后应保留默认值: %+v", rk)
	}
}

func TestApplyRK3588UpdatesRejectsInvalid(t *testing.T) {
	c := configs.NewConfig()
	c.OnRecordFinished.RK3588 = &configs.RK3588Config{Enable: true, Codec: configs.RK3588CodecHEVC, Bitrate: "4M"}

	updates := map[string]interface{}{
		"rk3588": map[string]interface{}{"codec": "libx264"},
	}
	if err := applyRK3588Updates(c, updates); err == nil {
		t.Fatal("非法编码器应被拒绝")
	}
	if c.OnRecordFinished.RK3588.Codec != configs.RK3588CodecHEVC {
		t.Fatal("校验失败时不应写入非法值")
	}

	badScale := map[string]interface{}{
		"rk3588": map[string]interface{}{"scale_width": float64(1281), "scale_height": float64(720)},
	}
	if err := applyRK3588Updates(c, badScale); err == nil {
		t.Fatal("奇数缩放尺寸应被拒绝")
	}
}

func TestApplyRK3588UpdatesAbsentKeyKeepsValue(t *testing.T) {
	c := configs.NewConfig()
	c.OnRecordFinished.RK3588.Enable = true

	if err := applyRK3588Updates(c, map[string]interface{}{"burn_subtitles": true}); err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if !c.OnRecordFinished.RK3588.Enable {
		t.Fatal("请求未包含 rk3588 时不应改动其取值")
	}
}

func TestApplyOnRecordFinishedOverrideMergesFields(t *testing.T) {
	target := &configs.OnRecordFinished{BurnSubtitles: true, BurnDeleteAss: true}

	updates := map[string]interface{}{
		"convert_to_mp4": true,
		"rk3588": map[string]interface{}{
			"enable":  true,
			"bitrate": "6M",
		},
	}
	applyOnRecordFinishedOverride(target, updates)

	if !target.ConvertToMp4 {
		t.Error("转 mp4 覆盖未生效")
	}
	// 关键：只覆盖了转 mp4 与 rk3588，已有的烧录覆盖不能被清掉
	if !target.BurnSubtitles || !target.BurnDeleteAss {
		t.Errorf("既有覆盖被清空: %+v", target)
	}
	if target.RK3588 == nil || !target.RK3588.Enable || target.RK3588.Bitrate != "6M" {
		t.Fatalf("rk3588 覆盖未生效: %+v", target.RK3588)
	}
	// 未在请求中出现的子字段应继承全局默认值
	if target.RK3588.Codec != configs.RK3588CodecHEVC || !target.RK3588.DecodeFallback {
		t.Fatalf("未出现的子字段应继承默认值: %+v", target.RK3588)
	}
}

func TestApplyOnRecordFinishedOverrideNullRK3588RestoresInheritance(t *testing.T) {
	target := &configs.OnRecordFinished{
		BurnSubtitles: true,
		RK3588:        &configs.RK3588Config{Enable: true, Codec: configs.RK3588CodecH264, Bitrate: "4M"},
	}
	applyOnRecordFinishedOverride(target, map[string]interface{}{"rk3588": nil})

	if target.RK3588 != nil {
		t.Fatalf("显式 rk3588:null 应恢复继承（置 nil）: %+v", target.RK3588)
	}
	if !target.BurnSubtitles {
		t.Error("其他覆盖字段不应受影响")
	}
}

func TestApplyOnRecordFinishedOverrideRejectsInvalidRK3588(t *testing.T) {
	target := &configs.OnRecordFinished{
		RK3588: &configs.RK3588Config{Enable: true, Codec: configs.RK3588CodecHEVC, Bitrate: "4M"},
	}
	err := applyOnRecordFinishedOverride(target, map[string]interface{}{
		"rk3588": map[string]interface{}{"codec": "libx264"},
	})
	if err == nil {
		t.Fatal("非法 rk3588 覆盖必须返回错误，由接口返回 400")
	}
	if target.RK3588.Codec != configs.RK3588CodecHEVC {
		t.Fatalf("非法覆盖不应写入: %+v", target.RK3588)
	}
}

func TestValidateRK3588OverrideUsesExistingBase(t *testing.T) {
	// 请求只改了缩放，已有的覆盖里编码器是非法旧值：
	// 只有用"已有覆盖"作为基值才会报错，用全局默认值会漏判
	existing := &configs.RK3588Config{Enable: true, Codec: "libx264", Bitrate: "4M"}
	updates := map[string]interface{}{
		"on_record_finished": map[string]interface{}{
			"rk3588": map[string]interface{}{"scale_width": float64(1280), "scale_height": float64(720)},
		},
	}
	if err := validateRK3588Override(updates, existing); err == nil {
		t.Fatal("应以已有覆盖为基值校验并发现编码器非法")
	}

	// 已有覆盖合法时同一请求应通过
	valid := &configs.RK3588Config{Enable: true, Codec: configs.RK3588CodecH264, Bitrate: "4M"}
	if err := validateRK3588Override(updates, valid); err != nil {
		t.Fatalf("合法覆盖不应报错: %v", err)
	}

	// 没有 on_record_finished / rk3588 时不校验
	if err := validateRK3588Override(map[string]interface{}{}, nil); err != nil {
		t.Fatalf("无 rk3588 字段时不应报错: %v", err)
	}
	// 显式 null 表示恢复继承，合法
	nullUpdates := map[string]interface{}{
		"on_record_finished": map[string]interface{}{"rk3588": nil},
	}
	if err := validateRK3588Override(nullUpdates, valid); err != nil {
		t.Fatalf("rk3588:null 应合法: %v", err)
	}
}

func TestApplyOverridableConfigUpdatesDoesNotMutateOnFailure(t *testing.T) {
	oc := &configs.OverridableConfig{
		OnRecordFinished: &configs.OnRecordFinished{
			BurnSubtitles: true,
			BurnDeleteAss: true,
			RK3588:        &configs.RK3588Config{Enable: true, Codec: configs.RK3588CodecHEVC, Bitrate: "4M"},
		},
	}
	updates := map[string]interface{}{
		"on_record_finished": map[string]interface{}{
			"burn_subtitles": false,
			"rk3588":         map[string]interface{}{"codec": "libx264"},
		},
	}
	if err := applyOverridableConfigUpdates(oc, updates, *oc.OnRecordFinished); err == nil {
		t.Fatal("非法 rk3588 覆盖必须返回错误")
	}
	// 校验失败时不得留下半写状态（CloneConfigShallow 会共享该指针，就地修改会污染在线内存）
	if !oc.OnRecordFinished.BurnSubtitles || !oc.OnRecordFinished.BurnDeleteAss {
		t.Fatalf("校验失败时不应修改已有覆盖: %+v", oc.OnRecordFinished)
	}
	if oc.OnRecordFinished.RK3588 == nil || oc.OnRecordFinished.RK3588.Codec != configs.RK3588CodecHEVC {
		t.Fatalf("校验失败时不应写入非法 rk3588: %+v", oc.OnRecordFinished.RK3588)
	}
}

// TestApplyOverridableConfigUpdatesInheritsBaseForNewOverride 回归测试（真实房间 GUI 覆盖曾出现）：
// 房间首次提交且只改 rk3588 时，必须以"当前生效配置"为基值，
// 不能把 burn_subtitles 等未提交字段零值覆盖（否则该房间会静默停止烧录）。
func TestApplyOverridableConfigUpdatesInheritsBaseForNewOverride(t *testing.T) {
	base := configs.OnRecordFinished{
		ConvertToMp4:       true,
		FixFlvAtFirst:      true,
		BurnSubtitles:      true,
		BurnSubtitlesCodec: "libx264",
		BurnDeleteAss:      false,
		BurnDeleteSource:   false,
		SaveCover:          true,
		UploadTiming:       configs.UploadTimingAfterProcess,
		RK3588:             &configs.RK3588Config{Enable: true, Codec: configs.RK3588CodecHEVC, Bitrate: "4M"},
	}
	oc := &configs.OverridableConfig{}

	updates := map[string]interface{}{
		"on_record_finished": map[string]interface{}{
			"rk3588": map[string]interface{}{"bitrate": "8M"},
		},
	}
	if err := applyOverridableConfigUpdates(oc, updates, base); err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if oc.OnRecordFinished == nil {
		t.Fatal("应创建房间级覆盖")
	}
	if !oc.OnRecordFinished.BurnSubtitles || !oc.OnRecordFinished.ConvertToMp4 ||
		!oc.OnRecordFinished.FixFlvAtFirst || !oc.OnRecordFinished.SaveCover {
		t.Fatalf("只改 rk3588 时不得把未提交字段零值覆盖: %+v", oc.OnRecordFinished)
	}
	if oc.OnRecordFinished.BurnSubtitlesCodec != "libx264" {
		t.Fatalf("未提交的编码器配置被清零: %q", oc.OnRecordFinished.BurnSubtitlesCodec)
	}
	if oc.OnRecordFinished.RK3588 == nil || oc.OnRecordFinished.RK3588.Bitrate != "8M" {
		t.Fatalf("rk3588 修改未生效: %+v", oc.OnRecordFinished.RK3588)
	}
	if !oc.OnRecordFinished.RK3588.Enable {
		t.Fatal("rk3588 未提交字段应继承基值")
	}
}

// TestApplyOverridableConfigUpdatesNilRK3588RestoresInheritance 只改过 rk3588 的房间把 rk3588
// 置回继承（null）后，不应残留一个多余的覆盖结构。
func TestApplyOverridableConfigUpdatesNilRK3588RestoresInheritance(t *testing.T) {
	base := configs.OnRecordFinished{
		BurnSubtitles: true,
		RK3588:        &configs.RK3588Config{Enable: true, Codec: configs.RK3588CodecHEVC, Bitrate: "4M"},
	}
	oc := &configs.OverridableConfig{}

	// 第一步：只改 rk3588，建立覆盖
	first := map[string]interface{}{
		"on_record_finished": map[string]interface{}{
			"rk3588": map[string]interface{}{"bitrate": "8M"},
		},
	}
	if err := applyOverridableConfigUpdates(oc, first, base); err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if oc.OnRecordFinished == nil {
		t.Fatal("第一步应创建覆盖")
	}
	if !oc.OnRecordFinished.BurnSubtitles {
		t.Fatal("第一步不应影响 burn_subtitles")
	}

	// 第二步：把 rk3588 置回继承
	second := map[string]interface{}{
		"on_record_finished": map[string]interface{}{"rk3588": nil},
	}
	if err := applyOverridableConfigUpdates(oc, second, base); err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if oc.OnRecordFinished != nil {
		t.Fatalf("rk3588 置回继承且其余字段与基值一致时应删除覆盖，实际残留: %+v", oc.OnRecordFinished)
	}
}

// TestApplyOverridableConfigUpdatesKeepsExplicitOverride 已存在的房间级覆盖仍是合并基值：
// 房间曾显式关闭烧录时，后续只改 rk3588 不得把它改回 true。
func TestApplyOverridableConfigUpdatesKeepsExplicitOverride(t *testing.T) {
	base := configs.OnRecordFinished{BurnSubtitles: true, RK3588: &configs.RK3588Config{Enable: true, Codec: configs.RK3588CodecHEVC, Bitrate: "4M"}}
	oc := &configs.OverridableConfig{
		OnRecordFinished: &configs.OnRecordFinished{BurnSubtitles: false},
	}
	updates := map[string]interface{}{
		"on_record_finished": map[string]interface{}{
			"rk3588": map[string]interface{}{"bitrate": "8M"},
		},
	}
	if err := applyOverridableConfigUpdates(oc, updates, base); err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if oc.OnRecordFinished.BurnSubtitles {
		t.Fatal("房间已显式关闭烧录时不应被改回 true")
	}
	if oc.OnRecordFinished.RK3588 == nil || oc.OnRecordFinished.RK3588.Bitrate != "8M" {
		t.Fatalf("rk3588 修改未生效: %+v", oc.OnRecordFinished.RK3588)
	}
}
