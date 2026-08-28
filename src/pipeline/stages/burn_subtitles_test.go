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
}