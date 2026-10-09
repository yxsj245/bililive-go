package media

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const mediaInfoFixture = `ffmpeg version 7.1 Copyright (c) 2000-2024 the FFmpeg developers
Input #0, matroska,webm, from '/data/in.mkv':
  Metadata:
    ENCODER         : Lavf61.7.100
  Duration: 00:00:12.02, start: 0.000000, bitrate: 1701 kb/s
  Stream #0:0: Video: h264 (High), yuv420p(tv, progressive), 640x360 [SAR 1:1 DAR 16:9], 30 fps, 30 tbr, 1k tbn
  Stream #0:1: Audio: aac (LC), 44100 Hz, stereo, fltp, 128 kb/s
At least one output file must be specified
`

const mediaInfoHexTagFixture = `Input #0, mov,mp4,m4a,3gp,3g2,mj2, from '/data/in.mp4':
  Duration: 00:01:00.00, start: 0.000000, bitrate: 900 kb/s
  Stream #0:0[0x1](und): Video: h264 (High) (avc1 / 0x31637661), yuv420p, 1920x1080, 30 fps, 30 tbr, 90k tbn
`

func TestParseMediaInfo(t *testing.T) {
	info := parseMediaInfo(mediaInfoFixture)
	if info.Duration < 12.0 || info.Duration > 12.1 {
		t.Errorf("时长解析不正确: %v", info.Duration)
	}
	if info.VideoCodec != "h264" {
		t.Errorf("视频编码解析不正确: %s", info.VideoCodec)
	}
	if info.Width != 640 || info.Height != 360 {
		t.Errorf("分辨率解析不正确: %dx%d", info.Width, info.Height)
	}
	if !info.HasAudio || info.AudioCodec != "aac" {
		t.Errorf("音轨解析不正确: %v %s", info.HasAudio, info.AudioCodec)
	}
}

func TestParseMediaInfoHexCodecTag(t *testing.T) {
	info := parseMediaInfo(mediaInfoHexTagFixture)
	if info.Width != 1920 || info.Height != 1080 {
		t.Fatalf("编码标签中的十六进制不应被当成分辨率: %dx%d", info.Width, info.Height)
	}
	if info.HasAudio {
		t.Fatal("该文件没有音频流")
	}
}

func TestCompareDuration(t *testing.T) {
	input := &MediaInfo{Duration: 12.0}
	if err := compareDuration(input, &MediaInfo{Duration: 11.9}); err != nil {
		t.Fatalf("容差内不应报错: %v", err)
	}
	if err := compareDuration(input, &MediaInfo{Duration: 6.0}); err == nil {
		t.Fatal("时长明显缺失应报错")
	}
	if err := compareDuration(input, &MediaInfo{}); err != nil {
		t.Fatalf("无法解析时长时应跳过校验: %v", err)
	}
}

func TestCreateTempOutputUniqueAndExtension(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "video.mkv")

	first, err := CreateTempOutput(target)
	if err != nil {
		t.Fatalf("创建临时输出失败: %v", err)
	}
	second, err := CreateTempOutput(target)
	if err != nil {
		t.Fatalf("创建临时输出失败: %v", err)
	}
	if first == second {
		t.Fatal("两次创建的临时文件必须不同，避免并发互相覆盖")
	}
	for _, path := range []string{first, second} {
		if filepath.Dir(path) != dir {
			t.Errorf("临时文件应与目标同目录: %s", path)
		}
		if !strings.HasSuffix(path, ".part.mkv") {
			t.Errorf("临时文件扩展名必须保留以便 ffmpeg 推断封装: %s", path)
		}
		if _, statErr := os.Stat(path); statErr != nil {
			t.Errorf("临时文件应已存在（独占占位）: %s", path)
		}
	}
}

func TestCreateTempCoverExtension(t *testing.T) {
	dir := t.TempDir()
	path, err := CreateTempCover(filepath.Join(dir, "video.jpg"))
	if err != nil {
		t.Fatalf("创建封面临时文件失败: %v", err)
	}
	if !strings.HasSuffix(path, ".cover.jpg") {
		t.Fatalf("封面临时文件扩展名不符: %s", path)
	}
}

func TestPublishTempFileDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "out.mkv")

	// 已存在的目标文件必须保持原样
	if err := os.WriteFile(target, []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}
	temp, err := CreateTempOutput(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(temp, []byte("NEW"), 0o644); err != nil {
		t.Fatal(err)
	}

	published, err := PublishTempFile(temp, target)
	if err != nil {
		t.Fatalf("发布失败: %v", err)
	}
	if published == target {
		t.Fatal("目标已存在时必须换用新文件名")
	}
	if !strings.HasSuffix(published, ".1.mkv") {
		t.Fatalf("新文件名应带序号: %s", published)
	}
	old, err := os.ReadFile(target)
	if err != nil || string(old) != "OLD" {
		t.Fatalf("现存文件被覆盖: %s %v", string(old), err)
	}
	fresh, err := os.ReadFile(published)
	if err != nil || string(fresh) != "NEW" {
		t.Fatalf("新文件内容不正确: %s %v", string(fresh), err)
	}
	if _, err := os.Stat(temp); !os.IsNotExist(err) {
		t.Fatal("发布成功后临时文件应被清理")
	}
}

func TestPublishTempFileNewTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "fresh.mkv")
	temp, err := CreateTempOutput(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(temp, []byte("DATA"), 0o644); err != nil {
		t.Fatal(err)
	}
	published, err := PublishTempFile(temp, target)
	if err != nil {
		t.Fatalf("发布失败: %v", err)
	}
	if published != target {
		t.Fatalf("目标不存在时应直接使用目标名: %s", published)
	}
	// 发布后的产物必须可被其它进程（如非 root 运行的容器）读取
	info, err := os.Stat(published)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Fatalf("发布后的文件权限应为 0644，实际 %o", perm)
	}
}

func TestPublishTempFileMissingTemp(t *testing.T) {
	dir := t.TempDir()
	if _, err := PublishTempFile(filepath.Join(dir, "nope.mkv"), filepath.Join(dir, "out.mkv")); err == nil {
		t.Fatal("临时文件不存在时应报错")
	}
}

func TestTailBufferBounded(t *testing.T) {
	buffer := newTailBuffer(16)
	payload := strings.Repeat("a", 100) + "TAIL"
	if _, err := buffer.Write([]byte(payload)); err != nil {
		t.Fatal(err)
	}
	if got := buffer.String(); got != "aaaaaaaaaaaaTAIL" {
		t.Fatalf("有界缓冲应只保留尾部: %q", got)
	}
}

func TestStageAssFile(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "字幕[1] it's: test.ass")
	content := "[Script Info]\nTitle: test\n"
	if err := os.WriteFile(source, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	staged, cleanup, err := StageAssFile(source)
	if err != nil {
		t.Fatalf("暂存失败: %v", err)
	}
	if filepath.Base(staged) != "subtitles.ass" {
		t.Fatalf("暂存文件名应为固定名称: %s", staged)
	}
	data, err := os.ReadFile(staged)
	if err != nil || string(data) != content {
		t.Fatalf("暂存内容不正确: %v", err)
	}
	cleanup()
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Fatal("清理后暂存文件应被删除")
	}
}

func TestStageAssFileEmptyPath(t *testing.T) {
	if _, _, err := StageAssFile(""); err == nil {
		t.Fatal("空路径应报错")
	}
}

func TestPublishCoverPermissions(t *testing.T) {
	dir := t.TempDir()
	cover := filepath.Join(dir, "video.jpg")
	temp, err := CreateTempCover(cover)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(temp, []byte("JPEGDATA"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := PublishCover(temp, cover); err != nil {
		t.Fatalf("发布封面失败: %v", err)
	}
	info, err := os.Stat(cover)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Fatalf("封面权限应为 0644，实际 %o", perm)
	}
	if _, err := os.Stat(temp); !os.IsNotExist(err) {
		t.Fatal("发布成功后临时封面应被清理")
	}
}

func TestPublishCoverReplacesExisting(t *testing.T) {
	dir := t.TempDir()
	cover := filepath.Join(dir, "video.jpg")
	if err := os.WriteFile(cover, []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}
	temp, err := CreateTempCover(cover)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(temp, []byte("NEW"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := PublishCover(temp, cover); err != nil {
		t.Fatalf("发布封面失败: %v", err)
	}
	data, err := os.ReadFile(cover)
	if err != nil || string(data) != "NEW" {
		t.Fatalf("同名旧封面应被替换: %q %v", string(data), err)
	}
}
