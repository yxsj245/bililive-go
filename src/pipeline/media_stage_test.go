package pipeline

import (
	"testing"

	"github.com/bililive-go/bililive-go/src/configs"
	"github.com/bililive-go/bililive-go/src/media"
)

func TestNeedsMediaStage(t *testing.T) {
	cases := []struct {
		name   string
		config *configs.OnRecordFinished
		want   bool
	}{
		{"nil 配置", nil, false},
		{"零值配置（rk3588 为 nil）", &configs.OnRecordFinished{}, false},
		{"rk3588 为 nil 且仅烧录", &configs.OnRecordFinished{BurnSubtitles: true}, true},
		{"仅烧录", &configs.OnRecordFinished{BurnSubtitles: true}, true},
		{"仅硬件启用不压缩", &configs.OnRecordFinished{RK3588: &configs.RK3588Config{Enable: true}}, false},
		{"硬件启用且压缩", &configs.OnRecordFinished{RK3588: &configs.RK3588Config{Enable: true, Compress: true}}, true},
		{"压缩但未启用硬件", &configs.OnRecordFinished{RK3588: &configs.RK3588Config{Compress: true}}, false},
	}
	for _, item := range cases {
		if got := NeedsMediaStage(item.config); got != item.want {
			t.Errorf("%s：NeedsMediaStage=%v，期望 %v", item.name, got, item.want)
		}
	}
}

func TestBuildMediaStageConfigLegacy(t *testing.T) {
	legacy := &configs.OnRecordFinished{
		BurnSubtitles:       true,
		BurnSubtitlesCodec:  "libx264",
		BurnSubtitlesCrf:    "20",
		BurnSubtitlesPreset: "slow",
		BurnDeleteAss:       false,
		BurnDeleteSource:    false,
	}
	stage := BuildMediaStageConfig(legacy, false)
	if stage.Name != StageNameBurnSubtitles {
		t.Fatalf("阶段名必须是 burn_subtitles: %s", stage.Name)
	}
	if got := stage.GetStringOption(OptionCodec, ""); got != "libx264" {
		t.Errorf("未启用硬件时应沿用旧编码器: %s", got)
	}
	if got := stage.GetStringOption(OptionCrf, ""); got != "20" {
		t.Errorf("CRF 未透传: %s", got)
	}
	if got := stage.GetStringOption(OptionPreset, ""); got != "slow" {
		t.Errorf("preset 未透传: %s", got)
	}
	if stage.GetBoolOption(OptionHardware, false) {
		t.Error("未启用硬件时 hardware 选项应为 false")
	}
	if !stage.GetBoolOption(OptionBurnEnabled, false) {
		t.Error("开启烧录时 burn_enabled 应为 true")
	}
	if stage.GetBoolOption(OptionCompress, false) {
		t.Error("未启用压缩时 compress 选项应为 false")
	}
}

func TestBuildMediaStageConfigHardware(t *testing.T) {
	legacy := &configs.OnRecordFinished{
		BurnSubtitles: true,
		RK3588: &configs.RK3588Config{
			Enable:         true,
			Compress:       true,
			Codec:          media.CodecH264Rkmpp,
			Bitrate:        "6M",
			DecodeFallback: false,
			ScaleWidth:     1280,
			ScaleHeight:    720,
		},
	}
	stage := BuildMediaStageConfig(legacy, false)
	if !stage.GetBoolOption(OptionHardware, false) {
		t.Error("启用硬件时 hardware 选项应为 true")
	}
	if !stage.GetBoolOption(OptionCompress, false) {
		t.Error("compress 选项应为 true")
	}
	if got := stage.GetStringOption(OptionCodec, ""); got != media.CodecH264Rkmpp {
		t.Errorf("编码器未取硬件配置: %s", got)
	}
	if got := stage.GetStringOption(OptionBitrate, ""); got != "6M" {
		t.Errorf("码率未取硬件配置: %s", got)
	}
	if stage.GetBoolOption(OptionDecodeFallback, true) {
		t.Error("decode_fallback=false 未透传")
	}
	if got := stage.GetIntOption(OptionScaleWidth, 0); got != 1280 {
		t.Errorf("缩放宽度未透传: %d", got)
	}
	if got := stage.GetIntOption(OptionScaleHeight, 0); got != 720 {
		t.Errorf("缩放高度未透传: %d", got)
	}
}

func TestBuildMediaStageConfigForceCompress(t *testing.T) {
	legacy := &configs.OnRecordFinished{
		BurnSubtitles: false,
		RK3588:        &configs.RK3588Config{Enable: true, Compress: false},
	}
	stage := BuildMediaStageConfig(legacy, true)
	if !stage.GetBoolOption(OptionCompress, false) {
		t.Error("批量压缩入口应强制开启压缩")
	}
	if stage.GetBoolOption(OptionBurnEnabled, true) {
		t.Error("未开启烧录时 burn_enabled 应为 false（只压缩）")
	}
}

func TestConvertLegacyConfigHardwareCompressOnly(t *testing.T) {
	legacy := &configs.OnRecordFinished{
		FixFlvAtFirst: false,
		RK3588:        &configs.RK3588Config{Enable: true, Compress: true},
	}
	config := ConvertLegacyConfig(legacy)
	if len(config.Stages) != 1 {
		t.Fatalf("应只注册烧录/压缩阶段: %+v", config.Stages)
	}
	stage := config.Stages[0]
	if stage.Name != StageNameBurnSubtitles {
		t.Fatalf("阶段名不符: %s", stage.Name)
	}
	if !stage.GetBoolOption(OptionCompress, false) {
		t.Error("自动管线应开启无弹幕压缩")
	}
	if stage.GetBoolOption(OptionBurnEnabled, true) {
		t.Error("未开启烧录时不应执行烧录")
	}
}

func TestConvertLegacyConfigUnchangedWithoutHardware(t *testing.T) {
	legacy := &configs.OnRecordFinished{
		ConvertToMp4:          true,
		DeleteFlvAfterConvert: true,
		FixFlvAtFirst:         true,
		BurnSubtitles:         true,
		BurnSubtitlesCodec:    "libx264",
		SaveCover:             true,
	}
	config := ConvertLegacyConfig(legacy)
	names := make([]string, 0, len(config.Stages))
	for _, stage := range config.Stages {
		names = append(names, stage.Name)
	}
	want := []string{StageNameFixFlv, StageNameConvertMp4, StageNameBurnSubtitles, StageNameExtractCover}
	if len(names) != len(want) {
		t.Fatalf("阶段数量不符: %v", names)
	}
	for index, name := range want {
		if names[index] != name {
			t.Fatalf("阶段顺序不符: %v", names)
		}
	}
	burn := config.Stages[2]
	if burn.GetStringOption(OptionCodec, "") != "libx264" {
		t.Errorf("旧配置编码器未透传: %v", burn.Options)
	}
	if burn.GetBoolOption(OptionHardware, false) {
		t.Error("未启用硬件处理时不应开启硬件选项")
	}
}
