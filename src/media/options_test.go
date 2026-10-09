package media

import (
	"testing"

	"github.com/bililive-go/bililive-go/src/configs"
)

func TestOptionsFromConfigNil(t *testing.T) {
	options := OptionsFromConfig(nil)
	if options.Codec != DefaultSoftwareCodec || options.Bitrate != DefaultBitrate {
		t.Fatalf("默认参数不符: %+v", options)
	}
	if options.Enabled || options.Compress {
		t.Fatalf("默认不应启用硬件处理: %+v", options)
	}
	if !options.DecodeFallback {
		t.Fatal("默认允许软件解码回退")
	}
}

func TestOptionsFromConfigLegacyKeepsOldCodec(t *testing.T) {
	legacy := &configs.OnRecordFinished{BurnSubtitles: true, BurnSubtitlesCodec: "libx264"}
	options := OptionsFromConfig(legacy)
	if options.Enabled {
		t.Fatal("未启用 rk3588 时不应走硬件路径")
	}
	if options.Codec != "libx264" {
		t.Fatalf("应沿用 burn_subtitles_codec: %s", options.Codec)
	}

	legacy.BurnSubtitlesCodec = ""
	options = OptionsFromConfig(legacy)
	if options.Codec != DefaultSoftwareCodec {
		t.Fatalf("空的旧编码器配置应回落默认软件编码器: %s", options.Codec)
	}
}

func TestOptionsFromConfigHardware(t *testing.T) {
	legacy := &configs.OnRecordFinished{
		BurnSubtitles:      true,
		BurnSubtitlesCodec: "libx264", // 启用硬件后应被忽略
		RK3588: &configs.RK3588Config{
			Enable:         true,
			Compress:       true,
			Codec:          CodecH264Rkmpp,
			Bitrate:        "6M",
			DecodeFallback: false,
			ScaleWidth:     1280,
			ScaleHeight:    720,
		},
	}
	options := OptionsFromConfig(legacy)
	if !options.Enabled || !options.Compress {
		t.Fatalf("启用标记不符: %+v", options)
	}
	if options.Codec != CodecH264Rkmpp || options.Bitrate != "6M" {
		t.Fatalf("硬件参数不符: %+v", options)
	}
	if options.DecodeFallback {
		t.Fatal("decode_fallback=false 应生效")
	}
	if options.ScaleWidth != 1280 || options.ScaleHeight != 720 {
		t.Fatalf("缩放参数不符: %+v", options)
	}
}

func TestOptionsFromConfigCompressRequiresEnable(t *testing.T) {
	legacy := &configs.OnRecordFinished{
		RK3588: &configs.RK3588Config{Enable: false, Compress: true, Codec: CodecHEVCRkmpp},
	}
	options := OptionsFromConfig(legacy)
	if options.Enabled || options.Compress {
		t.Fatalf("未启用硬件处理时不应压缩: %+v", options)
	}
}

func TestOptionsValidate(t *testing.T) {
	cases := []struct {
		name    string
		options Options
		wantErr bool
	}{
		{"默认软件编码合法", Options{Codec: DefaultSoftwareCodec, Bitrate: "4M", DecodeFallback: true}, false},
		{"启用硬件但编码器为软件", Options{Enabled: true, Codec: "libx264", Bitrate: "4M"}, true},
		{"启用硬件且编码器正确", Options{Enabled: true, Codec: CodecHEVCRkmpp, Bitrate: "4M"}, false},
		{"码率非法", Options{Enabled: true, Codec: CodecHEVCRkmpp, Bitrate: "abc"}, true},
		{"缩放只给宽", Options{Codec: DefaultSoftwareCodec, Bitrate: "4M", ScaleWidth: 1280}, true},
		{"缩放为奇数", Options{Codec: DefaultSoftwareCodec, Bitrate: "4M", ScaleWidth: 1281, ScaleHeight: 721}, true},
		{"缩放合法", Options{Codec: DefaultSoftwareCodec, Bitrate: "4M", ScaleWidth: 1280, ScaleHeight: 720}, false},
	}
	for _, item := range cases {
		err := item.options.Validate()
		if item.wantErr && err == nil {
			t.Errorf("%s：应报错但通过", item.name)
		}
		if !item.wantErr && err != nil {
			t.Errorf("%s：不应报错但失败: %v", item.name, err)
		}
	}
}

func TestResolveSelfTestCodec(t *testing.T) {
	if got := ResolveSelfTestCodec(CodecH264Rkmpp); got != CodecH264Rkmpp {
		t.Errorf("硬件编码器应原样使用: %s", got)
	}
	if got := ResolveSelfTestCodec("libx264"); got != DefaultHardwareCodec {
		t.Errorf("软件编码器应回落到默认硬件编码器自检: %s", got)
	}
	if got := ResolveSelfTestCodec(""); got != DefaultHardwareCodec {
		t.Errorf("空编码器应回落到默认硬件编码器自检: %s", got)
	}
}

func TestIsHardwareEncoder(t *testing.T) {
	hardware := []string{"hevc_rkmpp", "h264_rkmpp", "h264_nvenc", "h264_vaapi", "h264_v4l2m2m"}
	for _, codec := range hardware {
		if !IsHardwareEncoder(codec) {
			t.Errorf("%s 应识别为硬件编码器", codec)
		}
	}
	for _, codec := range []string{"libx264", "libx265", "mpeg4", ""} {
		if IsHardwareEncoder(codec) {
			t.Errorf("%s 不应识别为硬件编码器", codec)
		}
	}
}
