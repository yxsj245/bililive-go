package configs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const rk3588BaseYAML = `
rpc:
  enable: false
interval: 15
out_put_path: ./
live_rooms:
- url: https://live.bilibili.com/1
  is_listening: true
`

func TestRK3588DefaultsOnLoad(t *testing.T) {
	cfg, err := NewConfigWithBytes([]byte(rk3588BaseYAML))
	if err != nil {
		t.Fatalf("解析配置失败: %v", err)
	}
	rk := cfg.OnRecordFinished.RK3588
	if rk == nil {
		t.Fatal("加载后 rk3588 不应为 nil（应补齐默认值）")
	}
	if rk.Enable || rk.Compress {
		t.Fatalf("默认不启用硬件处理: %+v", rk)
	}
	if rk.Codec != RK3588CodecHEVC {
		t.Errorf("默认编码器应为 %s，实际 %s", RK3588CodecHEVC, rk.Codec)
	}
	if rk.Bitrate != "4M" {
		t.Errorf("默认码率应为 4M，实际 %s", rk.Bitrate)
	}
	if !rk.DecodeFallback {
		t.Error("默认允许软件解码回退")
	}
	if rk.ScaleWidth != 0 || rk.ScaleHeight != 0 {
		t.Errorf("默认不缩放: %+v", rk)
	}
}

func TestRK3588PartialYAMLKeepsDefaults(t *testing.T) {
	yaml := rk3588BaseYAML + `
on_record_finished:
  rk3588:
    enable: true
    compress: true
`
	cfg, err := NewConfigWithBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("解析配置失败: %v", err)
	}
	rk := cfg.OnRecordFinished.RK3588
	if !rk.Enable || !rk.Compress {
		t.Fatalf("显式字段未生效: %+v", rk)
	}
	// 未在文件中出现的字段必须保留默认值，而不是被零值覆盖
	if rk.Codec != RK3588CodecHEVC || rk.Bitrate != "4M" || !rk.DecodeFallback {
		t.Fatalf("未出现的子字段应保留默认值: %+v", rk)
	}
}

func TestRK3588ExplicitDecodeFallbackFalse(t *testing.T) {
	yaml := rk3588BaseYAML + `
on_record_finished:
  rk3588:
    enable: true
    decode_fallback: false
`
	cfg, err := NewConfigWithBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("解析配置失败: %v", err)
	}
	if cfg.OnRecordFinished.RK3588.DecodeFallback {
		t.Fatal("显式 decode_fallback=false 必须生效")
	}
}

func TestRK3588DefaultsNotSharedBetweenConfigs(t *testing.T) {
	first, err := NewConfigWithBytes([]byte(rk3588BaseYAML))
	if err != nil {
		t.Fatal(err)
	}
	first.OnRecordFinished.RK3588.Enable = true

	second, err := NewConfigWithBytes([]byte(rk3588BaseYAML))
	if err != nil {
		t.Fatal(err)
	}
	if second.OnRecordFinished.RK3588.Enable {
		t.Fatal("配置实例之间不应共享同一个 rk3588 指针（会互相污染）")
	}
	if defaultRK3588Config.Enable {
		t.Fatal("包级默认值被就地改写")
	}
}

func TestValidateRK3588(t *testing.T) {
	cases := []struct {
		name    string
		config  *RK3588Config
		wantErr string
	}{
		{"nil 合法", nil, ""},
		{"默认值合法", DefaultRK3588Config(), ""},
		{"未启用时宽松", &RK3588Config{Codec: "whatever"}, ""},
		{"启用的编码器非法", &RK3588Config{Enable: true, Codec: "libx264", Bitrate: "4M"}, "硬件编码器仅支持"},
		{"启用的编码器合法", &RK3588Config{Enable: true, Codec: RK3588CodecH264, Bitrate: "4M"}, ""},
		{"码率非法", &RK3588Config{Enable: true, Codec: RK3588CodecHEVC, Bitrate: "4G"}, "码率"},
		{"缩放为奇数", &RK3588Config{ScaleWidth: 1281, ScaleHeight: 720}, "偶数"},
		{"缩放缺一项", &RK3588Config{ScaleWidth: 1280}, "同时配置"},
		{"缩放合法", &RK3588Config{ScaleWidth: 1280, ScaleHeight: 720}, ""},
	}
	for _, item := range cases {
		err := ValidateRK3588(item.config)
		if item.wantErr == "" {
			if err != nil {
				t.Errorf("%s：不应报错，实际 %v", item.name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), item.wantErr) {
			t.Errorf("%s：应包含 %q 的错误，实际 %v", item.name, item.wantErr, err)
		}
	}
}

func TestVerifyRejectsInvalidRK3588(t *testing.T) {
	yaml := rk3588BaseYAML + `
on_record_finished:
  rk3588:
    enable: true
    codec: libx264
`
	cfg, err := NewConfigWithBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("解析配置失败: %v", err)
	}
	if err := cfg.Verify(); err == nil {
		t.Fatal("配置校验应拒绝启用硬件处理但编码器为软件编码器的情况")
	}
}

func TestParseBitrate(t *testing.T) {
	valid := map[string]int64{"4M": 4000000, "4m": 4000000, "4000k": 4000000, "4000000": 4000000, "4.5M": 4500000}
	for input, want := range valid {
		got, err := ParseBitrate(input)
		if err != nil {
			t.Errorf("%s 应可解析: %v", input, err)
			continue
		}
		if got != want {
			t.Errorf("%s 解析应为 %d，实际 %d", input, want, got)
		}
	}
	for _, input := range []string{"", "abc", "0", "-1M", "1G", "999M"} {
		if _, err := ParseBitrate(input); err == nil {
			t.Errorf("%s 应被拒绝", input)
		}
	}
}

func TestValidateScaleSize(t *testing.T) {
	if err := ValidateScaleSize(0, 0); err != nil {
		t.Errorf("0/0 表示不缩放，应合法: %v", err)
	}
	if err := ValidateScaleSize(1920, 1080); err != nil {
		t.Errorf("合法尺寸不应报错: %v", err)
	}
	for _, pair := range [][2]int{{1920, 0}, {0, 1080}, {1921, 1080}, {8, 8}, {9000, 1080}} {
		if err := ValidateScaleSize(pair[0], pair[1]); err == nil {
			t.Errorf("%v 应被拒绝", pair)
		}
	}
}

func TestCloneWithDefaults(t *testing.T) {
	var empty *RK3588Config
	clone := empty.CloneWithDefaults()
	if clone == nil || clone.Codec != RK3588CodecHEVC || clone.Bitrate != "4M" || !clone.DecodeFallback {
		t.Fatalf("nil 接收者应返回默认配置副本: %+v", clone)
	}
	// 返回的必须是副本，修改它不应影响包级默认值
	clone.Enable = true
	if defaultRK3588Config.Enable {
		t.Fatal("CloneWithDefaults 返回的必须是独立副本")
	}
}

func TestRK3588RoomOverrideInheritsGlobal(t *testing.T) {
	yaml := `
rpc:
  enable: false
interval: 15
out_put_path: ./
on_record_finished:
  rk3588:
    enable: true
    compress: true
    bitrate: 6M
live_rooms:
- url: https://live.bilibili.com/1
  is_listening: true
  on_record_finished:
    convert_to_mp4: true
`
	cfg, err := NewConfigWithBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("解析配置失败: %v", err)
	}
	resolved := cfg.GetEffectiveConfigForRoom("https://live.bilibili.com/1")
	if resolved.OnRecordFinished.RK3588 == nil {
		t.Fatal("房间未覆盖 rk3588 时应继承全局设置")
	}
	if !resolved.OnRecordFinished.RK3588.Enable || resolved.OnRecordFinished.RK3588.Bitrate != "6M" {
		t.Fatalf("房间应继承全局硬件设置: %+v", resolved.OnRecordFinished.RK3588)
	}
	if !resolved.OnRecordFinished.ConvertToMp4 {
		t.Fatal("房间级转 mp4 覆盖应生效")
	}
}

func TestRK3588HelperMethods(t *testing.T) {
	var nilConfig *RK3588Config
	if nilConfig.IsEnabled() || nilConfig.SupportsCompress() {
		t.Error("nil 配置不应视为启用")
	}
	if !nilConfig.DecodeFallbackEnabled() {
		t.Error("nil 配置默认允许软件解码回退")
	}
	enabled := &RK3588Config{Enable: true, Compress: false}
	if !enabled.IsEnabled() || enabled.SupportsCompress() {
		t.Error("只启用硬件但不压缩时 SupportsCompress 应为 false")
	}
	enabled.Compress = true
	if !enabled.SupportsCompress() {
		t.Error("启用且压缩时 SupportsCompress 应为 true")
	}
}

func TestMarshalWritesRK3588Comments(t *testing.T) {
	cfg, err := NewConfigWithBytes([]byte(rk3588BaseYAML))
	if err != nil {
		t.Fatalf("解析配置失败: %v", err)
	}
	cfg.File = filepath.Join(t.TempDir(), "config.yml")
	if err := cfg.Marshal(); err != nil {
		t.Fatalf("写回配置失败: %v", err)
	}
	data, err := os.ReadFile(cfg.File)
	if err != nil {
		t.Fatalf("读取配置失败: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		"rk3588:",
		"enable: false",
		"compress: false",
		"codec: hevc_rkmpp",
		"bitrate: 4M",
		"decode_fallback: true",
		"# 是否启用 RK3588（MPP/RGA）硬件处理",
		"# 硬件编码器：hevc_rkmpp（H.265，默认）或 h264_rkmpp",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("生成的配置文件缺少 %q", want)
		}
	}
}
