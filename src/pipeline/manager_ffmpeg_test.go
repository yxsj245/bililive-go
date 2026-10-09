package pipeline

import (
	"testing"

	"github.com/bililive-go/bililive-go/src/configs"
	"github.com/bililive-go/bililive-go/src/types"
)

const managerFFmpegYAML = `
rpc:
  enable: false
interval: 15
out_put_path: ./
ffmpeg_path: /usr/local/bin/ffmpeg
live_rooms:
- url: https://live.bilibili.com/1
  is_listening: true
  ffmpeg_path: /opt/special/ffmpeg
`

func TestEffectiveFFmpegPathForTaskUsesRoomOverride(t *testing.T) {
	cfg, err := configs.NewConfigWithBytes([]byte(managerFFmpegYAML))
	if err != nil {
		t.Fatalf("解析配置失败: %v", err)
	}
	cfg.LiveRooms[0].LiveId = types.LiveID("live-room-1")

	// 房间级 ffmpeg_path 覆盖必须对后处理生效
	got := effectiveFFmpegPathForTask(cfg, RecordInfo{LiveID: types.LiveID("live-room-1")})
	if got != "/opt/special/ffmpeg" {
		t.Fatalf("应使用房间级 FFmpeg 路径，实际 %q", got)
	}

	// 无对应房间（如手动批量任务）时回退全局配置
	got = effectiveFFmpegPathForTask(cfg, RecordInfo{LiveID: types.LiveID("batch-compress")})
	if got != "/usr/local/bin/ffmpeg" {
		t.Fatalf("无对应房间时应回退全局配置，实际 %q", got)
	}

	// 未设置 LiveId 时同样回退全局
	got = effectiveFFmpegPathForTask(cfg, RecordInfo{})
	if got != "/usr/local/bin/ffmpeg" {
		t.Fatalf("未设置 LiveId 时应回退全局配置，实际 %q", got)
	}

	if got := effectiveFFmpegPathForTask(nil, RecordInfo{}); got != "" {
		t.Fatalf("配置为空时应返回空串，实际 %q", got)
	}
}
