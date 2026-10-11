package danmaku

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bililive-go/bililive-go/src/configs"
)

// maxInt / minInt 测试用整数工具
func maxInt(v []int) int {
	m := v[0]
	for _, x := range v {
		if x > m {
			m = x
		}
	}
	return m
}

func minInt(v []int) int {
	m := v[0]
	for _, x := range v {
		if x < m {
			m = x
		}
	}
	return m
}

func chatTestConfig() configs.DanmakuConfig {
	cfg := configs.GetDefaultDanmakuConfig()
	cfg.Layout = configs.DanmakuLayoutChat
	cfg.Resolution = "1920x1080"
	cfg.FontName = "Noto Sans CJK SC"
	chat := configs.DefaultChatLayoutConfig()
	cfg.Chat = &chat
	return cfg
}

func newTestChatWriter(t *testing.T, cfg configs.DanmakuConfig) (*ChatWriter, string, time.Time) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "chat.ass")
	start := time.Unix(1700000000, 0)
	w, err := NewChatWriter(path, start, cfg, "Douyin Danmaku")
	if err != nil {
		t.Fatalf("创建 ChatWriter 失败: %v", err)
	}
	return w, path, start
}

// assEvent 解析后的 ASS 事件行
type assEvent struct {
	layer int
	start int64 // 厘秒
	end   int64
	style string
	text  string
}

// parseEvents 解析 ASS 文件中的 Dialogue 行
func parseEvents(t *testing.T, path string) []assEvent {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 ASS 失败: %v", err)
	}
	var events []assEvent
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(line, "Dialogue: ") {
			continue
		}
		parts := strings.SplitN(strings.TrimPrefix(line, "Dialogue: "), ",", 10)
		if len(parts) < 10 {
			t.Fatalf("事件行字段不足: %q", line)
		}
		var layer int
		fmt.Sscanf(parts[0], "%d", &layer)
		events = append(events, assEvent{
			layer: layer,
			start: parseCSTime(t, parts[1]),
			end:   parseCSTime(t, parts[2]),
			style: parts[3],
			text:  parts[9],
		})
	}
	return events
}

// parseCSTime 解析 ASS 时间 "H:MM:SS.cc" 为厘秒数
func parseCSTime(t *testing.T, s string) int64 {
	t.Helper()
	var h, m, sec, cs int
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d:%d:%d.%d", &h, &m, &sec, &cs); err != nil {
		t.Fatalf("时间格式非法: %q (%v)", s, err)
	}
	return int64(h)*360000 + int64(m)*6000 + int64(sec)*100 + int64(cs)
}

// eventY 从事件文本中取出 \pos(x,y)
func eventY(t *testing.T, e assEvent) int {
	t.Helper()
	idx := strings.Index(e.text, "\\pos(")
	if idx < 0 {
		t.Fatalf("事件缺少 \\pos: %q", e.text)
	}
	rest := e.text[idx+len("\\pos("):]
	end := strings.Index(rest, ")")
	if end < 0 {
		t.Fatalf("\\pos 缺少右括号: %q", e.text)
	}
	var x, y int
	if _, err := fmt.Sscanf(rest[:end], "%d,%d", &x, &y); err != nil {
		t.Fatalf("解析 \\pos 失败: %q", rest[:end])
	}
	return y
}

// textEvents 只保留文字层事件
func textEvents(events []assEvent) []assEvent {
	out := make([]assEvent, 0, len(events))
	for _, e := range events {
		if e.style == chatTextStyle {
			out = append(out, e)
		}
	}
	return out
}

// TestChatWriterLayoutBasics 输出结构：两层、正确样式与画布
func TestChatWriterLayoutBasics(t *testing.T) {
	w, path, start := newTestChatWriter(t, chatTestConfig())
	w.AddDanmaku(start, "小明", "你好，世界", 16777215)
	if err := w.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 ASS 失败: %v", err)
	}
	content := string(raw)
	for _, want := range []string{
		"PlayResX: 1920", "PlayResY: 1080", "Style: ChatText", "Style: ChatBubble",
		"\\p1\\bord0\\shad0", "\\an7\\pos(", chatNickColor, chatTextColor, chatBubbleColor,
		"小明：", "你好，世界",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("ASS 输出缺少 %q", want)
		}
	}
	if strings.Contains(content, "Banner;") {
		t.Error("聊天布局不应生成横向滚动 Banner 事件")
	}
	// 第一版不做淡入淡出，避免高频弹幕闪烁
	if strings.Contains(content, "\\fad(") {
		t.Error("聊天布局不应使用 fad 动效")
	}
	// 气泡透明度必须由 \1a 单独设置（\1c 的 alpha 会被 libass 忽略）
	if !strings.Contains(content, "\\1a&H") {
		t.Error("气泡缺少 \\1a 透明度设置")
	}
	events := parseEvents(t, path)
	if len(events) < 2 {
		t.Fatalf("一条消息至少应产生 2 个事件（气泡+文字），实际 %d", len(events))
	}
	if len(events)%2 != 0 {
		t.Errorf("气泡与文字事件必须成对出现，实际 %d 个", len(events))
	}
	if events[0].layer == events[1].layer {
		t.Error("气泡与文字应位于不同层")
	}
	// 每个事件段的气泡与文字时间段必须一致
	for i := 0; i+1 < len(events); i += 2 {
		if events[i].start != events[i+1].start || events[i].end != events[i+1].end {
			t.Errorf("气泡与文字时间段不一致: %+v vs %+v", events[i], events[i+1])
		}
	}
	// \N 只能作为换行符，不得成为字面量
	if strings.Contains(events[1].text, "\\\\N") {
		t.Error("正文分隔符不应被转义成字面 \\N")
	}
}

// TestChatWriterKeepsFullHistory 历史必须保留（不重写），且可见时长受“行数 + 停留时间”双约束
func TestChatWriterKeepsFullHistory(t *testing.T) {
	cfg := chatTestConfig()
	stay := int64(cfg.Chat.StaySeconds) * 100
	w, path, start := newTestChatWriter(t, cfg)
	total := 40
	for i := 0; i < total; i++ {
		w.AddDanmaku(start.Add(time.Duration(i)*time.Second), fmt.Sprintf("用户%02d", i), fmt.Sprintf("消息 %d", i), 0)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}
	events := textEvents(parseEvents(t, path))

	for i := 0; i < total; i++ {
		name := fmt.Sprintf("用户%02d：", i)
		begin := int64(i) * 100
		var covered, firstStart int64 = 0, -1
		for _, e := range events {
			if !strings.Contains(e.text, name) {
				continue
			}
			if firstStart < 0 || e.start < firstStart {
				firstStart = e.start
			}
			s, en := e.start, e.end
			if s < begin {
				s = begin
			}
			if en > begin+stay {
				en = begin + stay
			}
			if en > s {
				covered += en - s
			}
		}
		if firstStart < 0 {
			t.Fatalf("消息 %d（%s）完全丢失，历史被截断", i, name)
		}
		if firstStart != begin {
			t.Errorf("消息 %d 应从锚点 %d 开始显示，实际 %d", i, begin, firstStart)
		}
		// 消息密集时会被更晚的消息挤出可见区域（行数上限），因此只要求合理的最短可见时长
		if covered < 400 {
			t.Errorf("消息 %d 可见时长过短：%d 厘秒", i, covered)
		}
		// 任何时候都不得超过停留时间上限
		if covered > stay {
			t.Errorf("消息 %d 可见时长超过停留时间: %d > %d", i, covered, stay)
		}
	}

	// 最新消息必须完整显示停留时间
	last := fmt.Sprintf("用户%02d：", total-1)
	var lastCovered int64
	for _, e := range events {
		if strings.Contains(e.text, last) {
			lastCovered += e.end - e.start
		}
	}
	if lastCovered < stay {
		t.Errorf("最新消息显示时长不足: %d < %d", lastCovered, stay)
	}

	// 任意时刻同时渲染的消息不超过 rows 条
	for _, e := range events {
		visible := 0
		for _, other := range events {
			if other.start <= e.start && other.end > e.start {
				visible++
			}
		}
		if visible > cfg.Chat.Rows {
			t.Fatalf("t=%d 同时可见 %d 条，超过行数上限 %d", e.start, visible, cfg.Chat.Rows)
		}
	}
}

// TestChatWriterMoreThanRowsMessages 超过行数的旧消息仍必须保留已写入的历史
func TestChatWriterMoreThanRowsMessages(t *testing.T) {
	cfg := chatTestConfig()
	chat := *cfg.Chat
	chat.Rows = 5
	cfg.Chat = &chat

	w, path, start := newTestChatWriter(t, cfg)
	base := start.Add(1500 * time.Millisecond)
	for i := 0; i < 9; i++ {
		w.AddDanmaku(base.Add(time.Duration(i)*100*time.Millisecond), fmt.Sprintf("观众%d", i), fmt.Sprintf("内容%d", i), 0)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}

	events := textEvents(parseEvents(t, path))
	// 前 4 条虽然被挤出屏幕，但其已显示的 0.5 秒历史仍必须存在
	for i := 0; i < 4; i++ {
		name := fmt.Sprintf("观众%d：", i)
		var duration int64
		for _, e := range events {
			if strings.Contains(e.text, name) {
				duration += e.end - e.start
			}
		}
		if duration < 40 {
			t.Errorf("被挤出屏幕的消息 %d 历史被丢弃：累计显示 %d 厘秒", i, duration)
		}
	}
	// 最后一条必须完整显示停留时间
	last := "观众8："
	var lastDuration int64
	for _, e := range events {
		if strings.Contains(e.text, last) {
			lastDuration += e.end - e.start
		}
	}
	if want := int64(cfg.Chat.StaySeconds) * 100; lastDuration < want*9/10 {
		t.Errorf("最新消息显示时长不足：%d，期望约 %d", lastDuration, want)
	}
}

// TestChatWriterStayLimitNotExtended 每条消息显示时间不得超过停留时间
func TestChatWriterStayLimitNotExtended(t *testing.T) {
	cfg := chatTestConfig()
	stay := int64(cfg.Chat.StaySeconds) * 100
	// 消息很稀疏：每条都应恰好显示 stayCS 后消失，不能被后续消息拖长
	w, path, start := newTestChatWriter(t, cfg)
	w.AddDanmaku(start.Add(time.Second), "甲", "第一条", 0)
	w.AddDanmaku(start.Add(60*time.Second), "乙", "第二条", 0)
	if err := w.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}
	events := textEvents(parseEvents(t, path))

	var firstDuration int64
	for _, e := range events {
		if strings.Contains(e.text, "甲：") {
			firstDuration += e.end - e.start
		}
	}
	if firstDuration < stay-100 || firstDuration > stay {
		t.Errorf("第一条显示时长应约等于停留时间 %d 厘秒，实际 %d", stay, firstDuration)
	}
	// 第一条在第二条出现时（60 秒）早已结束，不应继续出现
	for _, e := range events {
		if strings.Contains(e.text, "甲：") && e.end > 100+stay {
			t.Errorf("第一条被错误延长到 %d 厘秒", e.end)
		}
	}
}

// TestChatWriterOutOfOrderTime 时间倒流/乱序消息必须安全处理，不产生负长度事件
func TestChatWriterOutOfOrderTime(t *testing.T) {
	cfg := chatTestConfig()
	w, path, start := newTestChatWriter(t, cfg)
	base := start.Add(5 * time.Second)
	w.AddDanmaku(base, "正序", "第一条", 0)
	// 时间回拨：不应产生负长度或乱序事件
	w.AddDanmaku(base.Add(-3*time.Second), "回拨", "第二条", 0)
	w.AddDanmaku(base.Add(time.Second), "正常", "第三条", 0)
	if err := w.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}
	events := parseEvents(t, path)
	for i, e := range events {
		if e.end < e.start {
			t.Errorf("第 %d 个事件长度为负: %d -> %d", i, e.start, e.end)
		}
	}
	// 回拨消息应被夹取到已写入位置，仍然有内容
	found := false
	for _, e := range events {
		if strings.Contains(e.text, "回拨：") {
			found = true
		}
	}
	if !found {
		t.Error("时间回拨的消息被丢弃")
	}
}

// TestChatWriterSameInstantBurst 同一时刻突发消息不产生无穷输出
func TestChatWriterSameInstantBurst(t *testing.T) {
	cfg := chatTestConfig()
	w, path, start := newTestChatWriter(t, cfg)
	at := start.Add(3 * time.Second)
	total := 300
	for i := 0; i < total; i++ {
		w.AddDanmaku(at, "弹幕机", fmt.Sprintf("突发消息 %d", i), 0)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}
	events := parseEvents(t, path)
	// 每个事件块最多 rows 条 × 2 层
	if maxEvents := total*cfg.Chat.Rows*2 + cfg.Chat.Rows*2; len(events) > maxEvents {
		t.Errorf("突发场景事件数量超出上界: %d > %d", len(events), maxEvents)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("统计文件失败: %v", err)
	}
	if info.Size() > 3*1024*1024 {
		t.Errorf("突发场景文件异常膨胀: %d 字节", info.Size())
	}
}

// TestChatWriterBottomNewestOrder 最新消息在最底部，位置按固定行高排列
func TestChatWriterBottomNewestOrder(t *testing.T) {
	cfg := chatTestConfig()
	w, path, start := newTestChatWriter(t, cfg)
	base := start.Add(time.Second)
	// 同时可见的消息：较短间隔，保证 co-visible
	for i := 0; i < 5; i++ {
		w.AddDanmaku(base.Add(time.Duration(i)*time.Second), fmt.Sprintf("排%d", i), fmt.Sprintf("内容%d", i), 0)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}
	events := textEvents(parseEvents(t, path))

	// 找出所有消息都可见的时刻（最后一条出现后、第一条到期前）
	probe := int64(5*100 + 1) // 第 5 条出现后 10ms
	yByMsg := map[int]int{}
	for _, e := range events {
		if probe < e.start || probe >= e.end {
			continue
		}
		for i := 0; i < 5; i++ {
			if strings.Contains(e.text, fmt.Sprintf("排%d：", i)) {
				yByMsg[i] = eventY(t, e)
			}
		}
	}
	if len(yByMsg) < 5 {
		t.Fatalf("探测时刻应同时可见 5 条，实际 %d 条", len(yByMsg))
	}
	for i := 0; i < 4; i++ {
		if yByMsg[i+1] <= yByMsg[i] {
			t.Errorf("较新的消息应更靠下（y 更大）: 排%d y=%d, 排%d y=%d", i, yByMsg[i], i+1, yByMsg[i+1])
		}
	}
	// 位置必须按“各自真实气泡高度 + 行间距”排列，避免不同行数消息互相重叠
	oneLineBubble := w.lineHeight + w.padY*2
	step := oneLineBubble + w.gapY
	for i := 0; i < 4; i++ {
		if diff := yByMsg[i+1] - yByMsg[i]; diff != step {
			t.Errorf("行距不固定: 排%d->排%d 间距 %d，期望 %d", i, i+1, diff, step)
		}
	}
	// 气泡高度必须容纳昵称行
	if w.lineHeight <= 0 || oneLineBubble < w.lineHeight {
		t.Errorf("气泡高度不足以容纳文字: %d < %d", oneLineBubble, w.lineHeight)
	}
}

// TestChatWriterSingleLineMode max_lines=1 时必须只渲染一行（昵称过长则截断昵称）
func TestChatWriterSingleLineMode(t *testing.T) {
	cfg := chatTestConfig()
	chat := *cfg.Chat
	chat.MaxLinesPerItem = 1
	cfg.Chat = &chat

	w, path, start := newTestChatWriter(t, cfg)
	longName := strings.Repeat("超长昵称", 10)
	w.AddDanmaku(start, longName, strings.Repeat("很长的正文内容", 10), 0)
	if err := w.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}
	events := textEvents(parseEvents(t, path))
	if len(events) == 0 {
		t.Fatal("未生成文字事件")
	}
	for _, e := range events {
		if strings.Contains(e.text, "\\N") {
			t.Errorf("max_lines=1 不应出现换行: %q", e.text)
		}
	}
	if !strings.Contains(events[0].text, chatEllipsis) {
		t.Error("超长内容应带省略号")
	}
	// 气泡高度必须与单行一致，否则会与相邻行重叠
	if got := events[0].end - events[0].start; got <= 0 {
		t.Errorf("事件时长非法: %d", got)
	}
	for _, e := range parseEvents(t, path) {
		if e.style != chatBubbleStyle {
			continue
		}
		ys := []int{}
		for _, m := range regexp.MustCompile(`[mlb]\s+-?\d+\s+(-?\d+)`).FindAllStringSubmatch(e.text, -1) {
			v, _ := strconv.Atoi(m[1])
			ys = append(ys, v)
		}
		if len(ys) > 0 {
			height := maxInt(ys) - minInt(ys)
			want := w.lineHeight + w.padY*2
			if height != want {
				t.Errorf("单行气泡高度应为 %d，实际 %d", want, height)
			}
		}
	}
}

// TestChatWriterCloseReportsWriteError 写入失败必须通过 Close 返回，不能静默
func TestChatWriterCloseReportsWriteError(t *testing.T) {
	w, _, start := newTestChatWriter(t, chatTestConfig())
	w.AddDanmaku(start, "用户", "内容", 0)
	// 模拟写入过程中出现过错误
	w.writeErr = true
	w.firstErr = fmt.Errorf("模拟写入失败")
	if err := w.Close(); err == nil {
		t.Error("发生过写入错误时 Close 必须返回错误")
	}
}

// TestChatWriterTextInjection 用户文本不得注入 ASS 控制码
func TestChatWriterTextInjection(t *testing.T) {
	w, path, start := newTestChatWriter(t, chatTestConfig())
	nick := "坏{\\an8}人"
	text := "换行\\N覆盖{\\p1}m 0 0 l 100 0 100 100 0 100{\\c&H0000FF&}"
	w.AddDanmaku(start, nick, text, 0)
	if err := w.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}
	events := textEvents(parseEvents(t, path))
	if len(events) == 0 {
		t.Fatal("未生成文字事件")
	}
	eventsText := ""
	for _, e := range events {
		eventsText += e.text
		if strings.Contains(e.text, "{\\an8}") || strings.Contains(e.text, "{\\p1}m 0 0") {
			t.Errorf("用户文本注入成功: %q", e.text)
		}
		if !strings.Contains(e.text, "\\{\\\\an8\\}") {
			t.Errorf("昵称中的大括号未被转义: %q", e.text)
		}
	}
	if strings.Contains(eventsText, "{\\c&H0000FF&}") {
		t.Error("用户输入的颜色覆盖标签未转义")
	}
	if !strings.Contains(eventsText, "\\\\c&H0000FF&") {
		t.Error("颜色控制码应输出为字面量（反斜杠成对）")
	}
}

// TestChatWriterLongTextBounded 长文本限制为有限行并带省略号
func TestChatWriterLongTextBounded(t *testing.T) {
	cfg := chatTestConfig()
	w, path, start := newTestChatWriter(t, cfg)
	long := strings.Repeat("很长的弹幕内容", 40)
	w.AddDanmaku(start, "长文用户", long, 0)
	if err := w.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}
	events := textEvents(parseEvents(t, path))
	if len(events) == 0 {
		t.Fatal("未生成文字事件")
	}
	first := events[0]
	lineBreaks := strings.Count(first.text, "\\N")
	if lineBreaks > cfg.Chat.MaxLinesPerItem-1 {
		t.Errorf("换行数超过最大行数限制: %d（最多 %d 行）", lineBreaks+1, cfg.Chat.MaxLinesPerItem)
	}
	if !strings.Contains(first.text, chatEllipsis) {
		t.Error("超长文本应带省略号")
	}
	if len(first.text) > 400 {
		t.Errorf("单条事件文本过长，未有效截断: %d 字符", len(first.text))
	}
}

// TestChatWriterGiftStyle 礼物 / SC / 上舰为金色文字条
func TestChatWriterGiftStyle(t *testing.T) {
	w, path, start := newTestChatWriter(t, chatTestConfig())
	w.AddGift(start, "土豪", "小心心", 10, 1000, "gold")
	w.AddSuperChat(start.Add(time.Second), "老板", "支持一下", 30000)
	w.AddGuard(start.Add(2*time.Second), "舰长", "舰长", 138000)
	if err := w.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}
	events := textEvents(parseEvents(t, path))
	if len(events) == 0 {
		t.Fatal("未生成文字事件")
	}
	for i, e := range events {
		if !strings.Contains(e.text, chatGiftColor) {
			t.Errorf("第 %d 个文字事件未使用金色: %q", i, e.text)
		}
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 ASS 失败: %v", err)
	}
	if !strings.Contains(string(body), "赠送") || !strings.Contains(string(body), "SC") {
		t.Error("礼物与 SC 文案缺失")
	}
	if strings.Contains(string(body), "\\t(") || strings.Contains(string(body), "\\move(") {
		t.Error("礼物条不应包含动效指令")
	}
}

// TestChatWriterPortraitBounds 竖屏尺寸可用，非法尺寸被拒绝
func TestChatWriterPortraitBounds(t *testing.T) {
	cfg := chatTestConfig()
	cfg.Resolution = "1080x1920"
	w, path, start := newTestChatWriter(t, cfg)
	if w.resX != 1080 || w.resY != 1920 {
		t.Fatalf("竖屏分辨率未生效: %dx%d", w.resX, w.resY)
	}
	if w.SetBounds(0, 0) || w.SetBounds(1921, 1080) || w.SetBounds(999999, 1080) {
		t.Error("非法尺寸不应被接受")
	}
	if w.resX != 1080 || w.resY != 1920 {
		t.Error("非法尺寸不应改变现有几何")
	}
	if err := w.Resize("2560x1440"); err != nil {
		t.Fatalf("合法分辨率调整失败: %v", err)
	}
	if w.resX != 2560 || w.resY != 1440 {
		t.Fatalf("调整后尺寸不正确: %dx%d", w.resX, w.resY)
	}
	if err := w.Resize("100x100"); err == nil {
		t.Error("非法分辨率调整应返回错误")
	}
	w.AddDanmaku(start, "竖屏用户", "竖屏弹幕", 0)
	if err := w.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}
	events := textEvents(parseEvents(t, path))
	if len(events) == 0 {
		t.Fatal("竖屏未生成事件")
	}
	// 竖屏下气泡与文字必须落在画面内
	for _, e := range events {
		if y := eventY(t, e); y < 0 || y > 1920 {
			t.Errorf("竖屏事件 y 越界: %d", y)
		}
	}
}

// TestChatWriterRejectsInvalidConfig 写入器自身必须校验配置边界
func TestChatWriterRejectsInvalidConfig(t *testing.T) {
	dir := t.TempDir()
	start := time.Unix(1700000000, 0)
	bad := chatTestConfig()
	bad.Chat = &configs.ChatLayoutConfig{Rows: 999}
	if _, err := NewChatWriter(filepath.Join(dir, "bad.ass"), start, bad, "t"); err == nil {
		t.Error("越界聊天参数应被写入器拒绝")
	}
	badRes := chatTestConfig()
	badRes.Resolution = "1921x1081"
	if _, err := NewChatWriter(filepath.Join(dir, "bad2.ass"), start, badRes, "t"); err == nil {
		t.Error("非法分辨率应被写入器拒绝")
	}
}

// TestChatWriterCloseIdempotent 重复 Close 与关闭后写入必须安全
func TestChatWriterCloseIdempotent(t *testing.T) {
	w, path, start := newTestChatWriter(t, chatTestConfig())
	w.AddDanmaku(start, "用户", "内容", 0)
	if err := w.Close(); err != nil {
		t.Fatalf("第一次关闭失败: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("重复关闭应安全: %v", err)
	}
	w.AddDanmaku(start.Add(time.Second), "用户", "关闭后的消息", 0)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("文件应存在: %v", err)
	}
}

// TestNewDanmakuWriterLayoutSwitch 统一入口按配置选择布局
func TestNewDanmakuWriterLayoutSwitch(t *testing.T) {
	dir := t.TempDir()
	start := time.Unix(1700000000, 0)
	scrollCfg := configs.GetDefaultDanmakuConfig()
	scrollWriter, err := NewDanmakuWriter(filepath.Join(dir, "scroll.ass"), start, scrollCfg, "t")
	if err != nil {
		t.Fatalf("创建滚动布局失败: %v", err)
	}
	if _, ok := scrollWriter.(*AssWriter); !ok {
		t.Errorf("默认应使用滚动布局，实际类型 %T", scrollWriter)
	}
	scrollWriter.Close()

	chatWriter, err := NewDanmakuWriter(filepath.Join(dir, "chat.ass"), start, chatTestConfig(), "t")
	if err != nil {
		t.Fatalf("创建聊天布局失败: %v", err)
	}
	if _, ok := chatWriter.(*ChatWriter); !ok {
		t.Errorf("layout=chat 应使用聊天布局，实际类型 %T", chatWriter)
	}
	chatWriter.Close()
}

// TestChatWriterAppendOnlyGrowth 事件必须只追加：文件行数随时间单调增长且历史不被删除
func TestChatWriterAppendOnlyGrowth(t *testing.T) {
	cfg := chatTestConfig()
	w, path, start := newTestChatWriter(t, cfg)
	readLines := func() int {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("读取失败: %v", err)
		}
		return strings.Count(string(raw), "Dialogue: ")
	}
	prev := readLines()
	for i := 0; i < 15; i++ {
		w.AddDanmaku(start.Add(time.Duration(i)*time.Second), fmt.Sprintf("增长%d", i), "内容", 0)
		cur := readLines()
		if cur < prev {
			t.Fatalf("第 %d 次写入后事件数减少（历史被重写删除）: %d -> %d", i, prev, cur)
		}
		prev = cur
	}
	if err := w.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}
	if now := readLines(); now < prev {
		t.Errorf("Close 后事件数减少: %d -> %d", prev, now)
	}
}
