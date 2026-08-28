package stages

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsNvencCodec(t *testing.T) {
	tests := []struct {
		name  string
		codec string
		want  bool
	}{
		{"h264_nvenc", "h264_nvenc", true},
		{"hevc_nvenc", "hevc_nvenc", true},
		{"av1_nvenc", "av1_nvenc", true},
		{"大写编码器名", "H264_NVENC", true},
		{"软编码 libx264", "libx264", false},
		{"软编码 libx265", "libx265", false},
		{"空字符串", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isNvencCodec(tt.codec))
		})
	}
}

func TestBuildVideoEncodeArgs(t *testing.T) {
	t.Run("软编码 libx264 使用 -crf 且不包含 NVENC 参数", func(t *testing.T) {
		args := buildVideoEncodeArgs("libx264", "18", "medium")
		assert.Equal(t, []string{"-c:v", "libx264", "-crf", "18", "-preset", "medium"}, args)
		assert.NotContains(t, args, "-cq")
		assert.NotContains(t, args, "-rc:v")
		assert.NotContains(t, args, "-b:v")
	})

	t.Run("软编码 libx265 使用 -crf", func(t *testing.T) {
		args := buildVideoEncodeArgs("libx265", "20", "slow")
		assert.Equal(t, []string{"-c:v", "libx265", "-crf", "20", "-preset", "slow"}, args)
	})

	t.Run("NVENC h264_nvenc 使用 -cq 且不包含 -crf", func(t *testing.T) {
		args := buildVideoEncodeArgs("h264_nvenc", "18", "p5")
		assert.Equal(t, []string{
			"-c:v", "h264_nvenc",
			"-rc:v", "vbr",
			"-cq", "18",
			"-b:v", "0",
			"-preset", "p5",
		}, args)
		assert.NotContains(t, args, "-crf")
	})

	t.Run("NVENC hevc_nvenc 走相同的质量控制分支", func(t *testing.T) {
		args := buildVideoEncodeArgs("hevc_nvenc", "23", "p7")
		assert.Equal(t, []string{
			"-c:v", "hevc_nvenc",
			"-rc:v", "vbr",
			"-cq", "23",
			"-b:v", "0",
			"-preset", "p7",
		}, args)
		assert.NotContains(t, args, "-crf")
	})

	t.Run("NVENC CQ 为 0 时表示自动质量，省略 -cq 参数", func(t *testing.T) {
		args := buildVideoEncodeArgs("h264_nvenc", "0", "p5")
		assert.Equal(t, []string{
			"-c:v", "h264_nvenc",
			"-rc:v", "vbr",
			"-b:v", "0",
			"-preset", "p5",
		}, args)
		assert.NotContains(t, args, "-cq")
		assert.NotContains(t, args, "-crf")
	})

	t.Run("NVENC CQ 为数值 0 的其他写法（00/0.0）同样省略 -cq", func(t *testing.T) {
		for _, cq := range []string{"00", "0.0"} {
			args := buildVideoEncodeArgs("av1_nvenc", cq, "p4")
			assert.NotContains(t, args, "-cq")
			assert.Contains(t, args, "-preset")
			assert.Equal(t, "p4", args[len(args)-1])
		}
	})

	t.Run("NVENC CQ 为非法值时原样传递，交由 FFmpeg 报错", func(t *testing.T) {
		args := buildVideoEncodeArgs("h264_nvenc", "abc", "p5")
		assert.Equal(t, []string{
			"-c:v", "h264_nvenc",
			"-rc:v", "vbr",
			"-cq", "abc",
			"-b:v", "0",
			"-preset", "p5",
		}, args)
	})

	t.Run("软编码 CRF 为 0 时仍保留 -crf（libx264 的 0 表示无损）", func(t *testing.T) {
		args := buildVideoEncodeArgs("libx264", "0", "medium")
		assert.Equal(t, []string{"-c:v", "libx264", "-crf", "0", "-preset", "medium"}, args)
	})
}

func TestIsZeroQuality(t *testing.T) {
	tests := []struct {
		name    string
		quality string
		want    bool
	}{
		{"整数字符串 0", "0", true},
		{"带前导零", "00", true},
		{"小数 0.0", "0.0", true},
		{"带空格", " 0 ", true},
		{"非 0 数值", "18", false},
		{"非法字符串", "abc", false},
		{"空字符串", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isZeroQuality(tt.quality))
		})
	}
}