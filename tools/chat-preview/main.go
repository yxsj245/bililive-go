// Command chat-preview 把弹幕原始消息 JSONL 离线渲染为抖音风格聊天列表 ASS。
//
// 用途：在不启动真实直播录制的前提下，用录到的原始消息快速出样片，
// 便于用 ffmpeg 渲染单帧图片核对样式（字体、气泡、位置、颜色）。
//
// 输入：JSONL，每行一条 JSON 消息，字段如下（未知字段忽略）：
//
//	{"time_ms":1234,"username":"小明","text":"你好","gift_name":"","num":0,"type":"danmaku"}
//
//	type 可选: danmaku(默认) / gift / guard / super_chat / sc
//	time_ms 为消息相对录制开始（或文件首条消息）的毫秒数
//
// 输出：chat 布局 ASS（气泡 + 文字），完全离线，不访问网络。
//
// 用法示例：
//
//	go run ./tools/chat-preview -in messages.jsonl -out preview.ass -width 1920 -height 1080
//	go run ./tools/chat-preview -in messages.jsonl -out preview.ass -width 1080 -height 1920 -font-size 40
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/bililive-go/bililive-go/src/configs"
	"github.com/bililive-go/bililive-go/src/recorders/danmaku"
	"gopkg.in/yaml.v3"
)

// chatMessage JSONL 中的一条原始消息
type chatMessage struct {
	TimeMs   json.Number `json:"time_ms"`
	Username string      `json:"username"`
	Text     string      `json:"text"`
	GiftName string      `json:"gift_name"`
	Num      int         `json:"num"`
	Price    int         `json:"price"`
	CoinType string      `json:"coin_type"`
	Type     string      `json:"type"`
}

func main() {
	var (
		in           = flag.String("in", "", "输入 JSONL 文件（- 表示标准输入）")
		out          = flag.String("out", "chat-preview.ass", "输出 ASS 文件路径")
		configPath   = flag.String("config", "", "可选：读取项目 config.yml 作为弹幕配置基线")
		fontName     = flag.String("font-name", "Noto Sans CJK SC", "字体名称")
		fontSize     = flag.Int("font-size", 36, "字号")
		width        = flag.Int("width", 1920, "画布宽度（像素），可传竖屏如 1080")
		height       = flag.Int("height", 1080, "画布高度（像素），可传竖屏如 1920")
		rows         = flag.Int("rows", 5, "左下角显示行数")
		widthPercent = flag.Int("width-percent", 72, "气泡宽度占画面宽度百分比")
		bottomPct    = flag.Int("bottom-percent", 8, "底部留白占画面高度百分比")
		staySeconds  = flag.Int("stay-seconds", 15, "每条消息停留秒数")
		maxLines     = flag.Int("max-lines", 2, "单条消息最多显示行数")
		limit        = flag.Int("limit", 0, "最多处理多少条消息（0 表示不限制）")
		verbose      = flag.Bool("v", false, "输出更多统计信息")
	)
	flag.Parse()

	if *in == "" {
		fmt.Fprintln(os.Stderr, "缺少 -in 参数：请指定 JSONL 文件（或 - 表示标准输入）")
		flag.Usage()
		os.Exit(2)
	}

	cfg, err := buildConfig(*configPath, *fontName, *fontSize, *rows, *widthPercent, *bottomPct, *staySeconds, *maxLines, *width, *height)
	if err != nil {
		fmt.Fprintf(os.Stderr, "配置构建失败：%v\n", err)
		os.Exit(1)
	}

	messages, err := readMessages(*in, *limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取消息失败：%v\n", err)
		os.Exit(1)
	}
	if len(messages) == 0 {
		fmt.Fprintln(os.Stderr, "没有可用消息：JSONL 为空或字段缺失")
		os.Exit(1)
	}

	// 独占创建输出文件，避免误覆盖已有文件（包括误把输入与输出写成同一个文件）
	create := func(name string) (*os.File, error) {
		return os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	}
	if samePath(*in, *out) {
		fmt.Fprintf(os.Stderr, "输入与输出是同一个文件，拒绝覆盖：%s\n", *out)
		os.Exit(1)
	}

	base := time.Unix(0, 0)
	writer, err := danmaku.NewDanmakuWriterWithCreate(*out, base, cfg, "Chat Preview", create)
	if err != nil {
		fmt.Fprintf(os.Stderr, "创建写入器失败（若文件已存在请先删除或改名）：%v\n", err)
		os.Exit(1)
	}
	chatWriter, ok := writer.(*danmaku.ChatWriter)
	if !ok {
		fmt.Fprintln(os.Stderr, "当前配置不是 chat 布局，请勿设置 layout=scroll")
		os.Exit(1)
	}

	lastAt := base
	for _, m := range messages {
		at := base.Add(time.Duration(m.millis()) * time.Millisecond)
		if at.After(lastAt) {
			lastAt = at
		}
		switch normType(m.Type) {
		case "gift":
			writer.AddGift(at, m.Username, m.GiftName, m.Num, m.Price, m.CoinType)
		case "guard":
			writer.AddGuard(at, m.Username, m.GiftName, m.Price)
		case "super_chat":
			writer.AddSuperChat(at, m.Username, m.Text, m.Price)
		default:
			writer.AddDanmaku(at, m.Username, m.Text, 0)
		}
	}
	if err := writer.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "关闭写入器失败：%v\n", err)
		os.Exit(1)
	}

	fmt.Printf("已生成：%s\n", *out)
	fmt.Printf("消息条数：%d，时间跨度：%.1f 秒\n", len(messages), lastAt.Sub(base).Seconds())
	if *verbose {
		if info, err := os.Stat(*out); err == nil {
			fmt.Printf("文件大小：%d 字节\n", info.Size())
		}
		w, h := chatWriter.Bounds()
		fmt.Printf("画布：%dx%d，布局：chat（行数 %d，宽度 %d%%，底部留白 %d%%，停留 %d 秒）\n",
			w, h, cfg.Chat.Rows, cfg.Chat.WidthPercent, cfg.Chat.BottomPercentValue(), cfg.Chat.StaySeconds)
	}
}

// samePath 判断输入与输出是否指向同一个文件（含硬链接与相对路径）
func samePath(a, b string) bool {
	if a == "" || a == "-" {
		return false
	}
	ai, errA := os.Stat(a)
	bi, errB := os.Stat(b)
	if errA != nil || errB != nil {
		return false
	}
	return os.SameFile(ai, bi)
}

// millis 兼容整数与浮点的 time_ms
func (m chatMessage) millis() int64 {
	if m.TimeMs == "" {
		return 0
	}
	if v, err := m.TimeMs.Int64(); err == nil {
		return v
	}
	if f, err := m.TimeMs.Float64(); err == nil {
		return int64(f)
	}
	return 0
}

// normType 归一化消息类型
func normType(t string) string {
	switch t {
	case "gift", "guard", "super_chat":
		return t
	case "sc", "superchat":
		return "super_chat"
	case "", "danmaku", "chat", "text":
		return "danmaku"
	default:
		return "danmaku"
	}
}

// readMessages 读取 JSONL 消息（支持 - 表示标准输入）
func readMessages(path string, limit int) ([]chatMessage, error) {
	var f *os.File
	var err error
	if path == "-" {
		f = os.Stdin
	} else {
		f, err = os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
	}

	var out []chatMessage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var m chatMessage
		if err := json.Unmarshal(line, &m); err != nil {
			return nil, fmt.Errorf("第 %d 行 JSON 解析失败: %w", lineNo, err)
		}
		if m.Text == "" && m.GiftName == "" {
			continue
		}
		out = append(out, m)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// buildConfig 构建弹幕配置：优先使用 -config 指定的 YAML，其次使用默认值，命令行参数始终覆盖
func buildConfig(configPath, fontName string, fontSize, rows, widthPercent, bottomPct, staySeconds, maxLines, width, height int) (configs.DanmakuConfig, error) {
	cfg := configs.GetDefaultDanmakuConfig()
	if configPath != "" {
		raw, err := os.ReadFile(configPath)
		if err != nil {
			return cfg, err
		}
		var root struct {
			Danmaku configs.DanmakuConfig `yaml:"danmaku"`
		}
		if err := yaml.Unmarshal(raw, &root); err != nil {
			return cfg, fmt.Errorf("解析 %s 失败: %w", configPath, err)
		}
		loaded := root.Danmaku
		loaded.SetDefaults()
		cfg = loaded
	}
	cfg.Layout = configs.DanmakuLayoutChat
	if fontName != "" {
		cfg.FontName = fontName
	}
	if fontSize > 0 {
		cfg.FontSize = fontSize
	}
	chat := configs.DefaultChatLayoutConfig()
	if cfg.Chat != nil {
		chat = cfg.Chat.Clone()
	}
	chat.Rows = rows
	chat.WidthPercent = widthPercent
	chat.BottomPercent = configs.IntPtr(bottomPct)
	chat.StaySeconds = staySeconds
	chat.MaxLinesPerItem = maxLines
	cfg.Chat = &chat
	// 画布分辨率直接写入配置：写入器创建时即写入 ASS 头部，构造后不允许再改
	cfg.Resolution = fmt.Sprintf("%dx%d", width, height)
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}
