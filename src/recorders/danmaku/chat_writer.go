package danmaku

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/bililive-go/bililive-go/src/configs"
)

// 聊天列表布局的配色与几何常量。
// ASS 颜色格式为 &HAABBGGRR&；气泡透明度不写在颜色里，而是用 \1a 单独设置
// （libass 会忽略 \1c 中的 alpha 通道）。
const (
	chatNickColor   = "&H00D9B36A&" // 昵称：青蓝色 #6AB3D9
	chatTextColor   = "&H00FFFFFF&" // 正文：白色
	chatGiftColor   = "&H0030D4FF&" // 礼物 / SC / 上舰：金色 #FFD430
	chatBubbleColor = "&H00000000&" // 气泡填充色（深色）
	chatTextStyle   = "ChatText"    // 文字样式名
	chatBubbleStyle = "ChatBubble"  // 气泡样式名
	chatGiftStyle   = "Gift"        // 礼物样式名

	chatEventLayerBubble = 1 // 气泡层（绘制在下）
	chatEventLayerText   = 2 // 文字层（绘制在上）

	chatLeftMarginPct = 2  // 气泡左边距占画面宽度百分比（另加半个字号）
	chatRowGapPct     = 1  // 行间距占画面高度百分比
	chatBubbleRadius  = 12 // 圆角基准（按字号比例换算）
	chatEllipsis      = "…"

	chatMaxTextRunes = 256 // 单条消息正文最多渲染的字形数（超出截断，保证内存与文件有界）
	chatMaxNameRunes = 32  // 昵称最多渲染的字形数

	chatMinStayCS = 100 // 停留时间下限（厘秒）
)

// chatMsgKind 聊天列表消息类型
type chatMsgKind int

const (
	chatKindDanmaku chatMsgKind = iota // 普通弹幕
	chatKindGift                       // 礼物 / SC / 上舰（金色文字条）
)

// chatLine 一行中的一个文本片段及其颜色
type chatLine struct {
	text  string
	color string
}

// chatEntry 聊天列表中的一条消息
type chatEntry struct {
	startCS    int64 // 出现时间（厘秒，相对录制开始）
	expireCS   int64 // 消失时间（startCS + 停留时间）
	lines      []chatLine
	textWidth  int // 最宽一行的像素宽度（含昵称）
	visualRows int // 实际占用行数
	bubbleH    int // 气泡高度（按实际行数计算）
	kind       chatMsgKind
}

// ChatWriter 抖音风格左下角聊天列表 ASS 写入器。
//
// 算法（增量、只追加）：
//   - 活跃列表只保留 rows 条消息；每条消息从出现起最多显示 stay 秒，或被更新的消息挤出；
//   - 时间轴推进按段循环：每段结束时间 = min(推进目标, 段内最早到期时间)；
//     段内所有活跃条目按“各自真实气泡高度从底部向上累积”的位置各写一个事件，随后丢弃到期条目；
//   - 历史事件只追加不重写，文件规模与消息条数线性相关，突发消息不会产生无穷输出；
//   - 不使用任何动效指令，避免高频弹幕下的闪烁。
type ChatWriter struct {
	mu     sync.Mutex
	file   *os.File
	closed bool
	// writeErr 首次写入失败后置 true，后续跳过无意义写入
	writeErr bool
	// firstErr 记录首个写入错误，Close 时返回给调用方（不静默丢弃）
	firstErr error

	startAt time.Time
	title   string
	cfg     configs.DanmakuConfig
	layout  configs.ChatLayoutConfig
	clock   func() time.Time // 当前时间来源，测试可注入

	resX int
	resY int

	fontSize     int
	padX         int // 气泡内水平内边距
	padY         int // 气泡内垂直内边距
	gapY         int // 行间距
	lineHeight   int // 单行文字高度
	bubbleWidth  int // 气泡最大宽度（宽度百分比上限）
	bubbleMaxCol int // 单行最大列数（半角为单位）
	baseX        int // 气泡左边距
	baseBottom   int // 最底部一行的底边 y 坐标

	radius    int
	alpha     int   // 气泡透明度（0~255，由 opacity 配置换算）
	stayCS    int64 // 停留时间（厘秒）
	rows      int   // 可见行数
	maxLines  int   // 单条消息最多行数（含昵称行）
	headerLen int

	active []*chatEntry // 活跃消息，从旧到新（最多 rows 条）
	lastCS int64        // 已写出事件的时间位置（厘秒），-1 表示尚未写出
}

// NewChatWriter 创建聊天列表布局的 ASS 写入器。
// 参数：filePath 输出路径，startAt 录制起始时间（时间同步基准），cfg 弹幕配置，title 脚本标题。
func NewChatWriter(filePath string, startAt time.Time, cfg configs.DanmakuConfig, title string) (*ChatWriter, error) {
	return NewChatWriterWithCreate(filePath, startAt, cfg, title, os.Create)
}

// CreateFileFunc 文件创建函数，允许调用方自定义创建策略（例如独占创建，拒绝覆盖已有文件）
type CreateFileFunc func(name string) (*os.File, error)

// NewChatWriterWithCreate 与 NewChatWriter 相同，但由调用方决定如何创建输出文件。
func NewChatWriterWithCreate(filePath string, startAt time.Time, cfg configs.DanmakuConfig, title string, create CreateFileFunc) (*ChatWriter, error) {
	if create == nil {
		create = os.Create
	}
	f, err := create(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create ass file: %w", err)
	}
	// 写入器侧再校验一次配置边界，避免绕过配置校验的调用方产生异常几何
	if err := cfg.Chat.Validate(cfg.Resolution); err != nil {
		f.Close()
		return nil, fmt.Errorf("聊天弹幕配置无效: %w", err)
	}

	resX, resY := parseResolution(cfg.Resolution)
	layout := cfg.Chat.Effective()
	fontSize := cfg.FontSize
	if fontSize <= 0 {
		fontSize = configs.GetDefaultDanmakuConfig().FontSize
	}
	w := &ChatWriter{
		file:     f,
		startAt:  startAt,
		title:    title,
		cfg:      cfg,
		layout:   layout,
		clock:    time.Now,
		resX:     resX,
		resY:     resY,
		fontSize: fontSize,
		stayCS:   int64(layout.StaySeconds) * 100,
		rows:     layout.Rows,
		maxLines: layout.MaxLinesPerItem,
		lastCS:   -1,
	}
	if w.stayCS < chatMinStayCS {
		w.stayCS = chatMinStayCS
	}
	if w.rows < 1 {
		w.rows = 1
	}
	if w.maxLines < 1 {
		w.maxLines = 1
	}
	w.applyBounds(resX, resY)

	if err := w.writeHeader(); err != nil {
		f.Close()
		return nil, err
	}
	return w, nil
}

// applyBounds 按画布尺寸重新计算几何参数（非法尺寸会被忽略）
func (w *ChatWriter) applyBounds(resX, resY int) {
	if resX < 160 || resY < 160 {
		return
	}
	w.resX = resX
	w.resY = resY

	w.radius = w.fontSize * chatBubbleRadius / 36
	if w.radius < 2 {
		w.radius = 2
	}
	w.lineHeight = w.fontSize * 13 / 10

	opacity := 128
	if w.cfg.Opacity != nil {
		opacity = *w.cfg.Opacity
	}
	w.alpha = 255 - opacity
	if w.alpha < 0 {
		w.alpha = 0
	}
	if w.alpha > 255 {
		w.alpha = 255
	}

	w.padX = w.fontSize * 9 / 20
	w.padY = w.fontSize / 4
	w.gapY = w.resY * chatRowGapPct / 100
	w.baseX = w.resX*chatLeftMarginPct/100 + w.fontSize/2

	w.bubbleWidth = w.resX * w.layout.WidthPercent / 100
	if w.bubbleWidth > w.resX-w.baseX {
		w.bubbleWidth = w.resX - w.baseX
	}
	// 单行最大列数：扣除内边距与描边余量后按半角折算
	w.bubbleMaxCol = (w.bubbleWidth - w.padX*2 - w.fontSize) / (w.fontSize / 2)
	if w.bubbleMaxCol < 4 {
		w.bubbleMaxCol = 4
	}
	w.baseBottom = w.resY * (100 - w.layout.BottomPercentValue()) / 100
}

// SetBounds 在尚未写出任何事件前调整画布几何（例如按视频实际分辨率设置竖屏），
// 并同步重写 ASS 头部，保证 PlayRes 与新几何一致。
// 已经写出事件后不再允许变更（事件中的坐标已固定），避免坐标与头部不一致。
func (w *ChatWriter) SetBounds(width, height int) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || w.writeErr || w.lastCS >= 0 {
		return false
	}
	if !configs.IsSafeDanmakuResolution(width, height) {
		return false
	}
	w.applyBounds(width, height)
	if err := w.file.Truncate(0); err != nil {
		w.writeErr = true
		return false
	}
	if _, err := w.file.Seek(0, 0); err != nil {
		w.writeErr = true
		return false
	}
	if err := w.writeHeader(); err != nil {
		w.writeErr = true
		return false
	}
	return true
}

// Resize 在尚未写出事件前按 "宽x高" 调整几何，非法值返回错误
func (w *ChatWriter) Resize(resolution string) error {
	x, y, err := configs.ParseDanmakuResolution(resolution)
	if err != nil {
		return err
	}
	if !w.SetBounds(x, y) {
		return fmt.Errorf("分辨率超出安全范围或已开始写入: %s", resolution)
	}
	return nil
}

// Bounds 返回当前生效的画布尺寸
func (w *ChatWriter) Bounds() (int, int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.resX, w.resY
}

// writeHeader 写入 ASS 头部（样式与事件格式声明）
func (w *ChatWriter) writeHeader() error {
	outline := 1
	if w.cfg.Outline != nil {
		outline = *w.cfg.Outline
	}
	header := fmt.Sprintf(`[Script Info]
Title: %s
ScriptType: v4.00+
WrapStyle: 2
ScaledBorderAndShadow: yes
PlayResX: %d
PlayResY: %d

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: %s,%s,%d,%s,%s,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,%d,0,7,0,0,0,1
Style: %s,%s,%d,%s,&H00000000,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,7,0,0,0,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
`,
		w.title, w.resX, w.resY,
		chatTextStyle, w.cfg.FontName, w.fontSize, chatTextColor, chatNickColor, outline,
		chatBubbleStyle, w.cfg.FontName, w.fontSize, chatBubbleColor)

	if _, err := w.file.WriteString(header); err != nil {
		w.writeErr = true
		w.noteErr(err)
		return fmt.Errorf("写入弹幕 ASS 头部失败: %w", err)
	}
	pos, err := w.file.Seek(0, 1)
	if err != nil {
		w.writeErr = true
		return fmt.Errorf("获取弹幕 ASS 写入位置失败: %w", err)
	}
	w.headerLen = int(pos)
	return nil
}

// noteErr 记录首个写入错误（Close 时返回，避免静默失败）
func (w *ChatWriter) noteErr(err error) {
	if err != nil && w.firstErr == nil {
		w.firstErr = err
	}
}

// now 返回当前时间（测试可注入）
func (w *ChatWriter) now() time.Time {
	if w.clock == nil {
		return time.Now()
	}
	return w.clock()
}

// toCS 把接收时间换算为相对录制开始的厘秒数
func (w *ChatWriter) toCS(recvAt time.Time) int64 {
	cs := int64(recvAt.Sub(w.startAt) / (10 * time.Millisecond))
	if cs < 0 {
		return 0
	}
	return cs
}

// textWidth 估算文字像素宽度：非 ASCII 字形按一个字号宽度计，ASCII 按半宽计
func (w *ChatWriter) textWidth(text string) int {
	half := w.fontSize / 2
	if half < 1 {
		half = 1
	}
	width := 0
	for _, r := range text {
		if r > 0x7F {
			width += w.fontSize
		} else {
			width += half
		}
	}
	return width
}

// runeWidth 单字形像素宽度
func (w *ChatWriter) runeWidth(r rune) int {
	if r > 0x7F {
		return w.fontSize
	}
	half := w.fontSize / 2
	if half < 1 {
		half = 1
	}
	return half
}

// truncate 在指定列数内截断文本；发生截断时追加省略号
func (w *ChatWriter) truncate(text string, maxCols int) string {
	limit := maxCols * (w.fontSize / 2)
	result := make([]rune, 0, 32)
	used := 0
	for _, r := range text {
		rw := w.runeWidth(r)
		if maxCols >= 0 && used+rw > limit {
			result = append(result, []rune(chatEllipsis)...)
			return string(result)
		}
		result = append(result, r)
		used += rw
	}
	return string(result)
}

// truncateWidth 按像素宽度截断文本；发生截断时追加省略号
func (w *ChatWriter) truncateWidth(text string, maxWidth int) string {
	if maxWidth < w.fontSize {
		maxWidth = w.fontSize
	}
	result := make([]rune, 0, 32)
	used := 0
	for _, r := range text {
		rw := w.runeWidth(r)
		if used+rw > maxWidth {
			result = append(result, []rune(chatEllipsis)...)
			return string(result)
		}
		result = append(result, r)
		used += rw
	}
	return string(result)
}

// packChars 按可用像素宽度装行，最多 maxLines 行；放不下的内容以省略号结尾
func (w *ChatWriter) packChars(runes []rune, maxLines int, availWidth int) []string {
	if maxLines < 1 {
		maxLines = 1
	}
	if availWidth < w.fontSize {
		availWidth = w.fontSize
	}
	lines := make([]string, 0, maxLines)
	var cur []rune
	curWidth := 0
	for _, r := range runes {
		rw := w.runeWidth(r)
		if curWidth+rw > availWidth && len(cur) > 0 {
			if len(lines) == maxLines-1 {
				// 已是最后一行：剩余内容丢弃并以省略号提示
				cur = append(cur, []rune(chatEllipsis)...)
				lines = append(lines, string(cur))
				return lines
			}
			lines = append(lines, string(cur))
			cur = cur[:0]
			curWidth = 0
		}
		cur = append(cur, r)
		curWidth += rw
	}
	if len(cur) > 0 || len(lines) == 0 {
		lines = append(lines, string(cur))
	}
	return lines
}

// entryLines 生成渲染片段：昵称与正文尽量同一行（抖音风格），最多 maxLines 行且必含昵称。
// 礼物 / SC / 上舰使用金色单行文字条。
func (w *ChatWriter) entryLines(username, text string, kind chatMsgKind) ([]chatLine, int) {
	name := strings.TrimSpace(username)
	body := strings.TrimSpace(text)
	if kind == chatKindGift {
		gift := strings.TrimSpace(name + " 赠送 " + body)
		return []chatLine{{text: w.truncate(gift, w.bubbleMaxCol), color: chatGiftColor}}, 1
	}
	if name == "" {
		name = "未知用户"
	}
	if nameRunes := []rune(name); len(nameRunes) > chatMaxNameRunes {
		name = string(nameRunes[:chatMaxNameRunes])
	}
	nameRunes := []rune(name + "：")
	// 昵称优先，但不允许挤掉正文：最多占气泡可写宽度的 50%，并保证至少 4 个字的正文空间
	nameBudget := (w.bubbleWidth - w.padX*2) / 2
	minBodyWidth := w.fontSize * 4
	if maxName := w.bubbleWidth - w.padX*2 - w.fontSize - minBodyWidth; maxName < nameBudget {
		nameBudget = maxName
	}
	if nameBudget < w.fontSize {
		nameBudget = w.fontSize
	}
	if w.textWidth(string(nameRunes)) > nameBudget {
		nameRunes = []rune(w.truncateWidth(string(nameRunes), nameBudget))
	}
	nameWidth := w.textWidth(string(nameRunes))

	bodyRunes := []rune(strings.ReplaceAll(strings.ReplaceAll(body, "\r", " "), "\n", " "))
	if len(bodyRunes) > chatMaxTextRunes {
		bodyRunes = bodyRunes[:chatMaxTextRunes]
	}

	// 首行剩余宽度（扣除昵称、内边距与描边余量）
	firstAvail := w.bubbleWidth - w.padX*2 - w.fontSize - nameWidth
	bodyAvail := w.bubbleWidth - w.padX*2 - w.fontSize

	var bodyLines []string
	firstOnSameLine := firstAvail >= w.fontSize // 首行至少还能放下一个字
	if !firstOnSameLine && w.maxLines <= 1 {
		// 只允许一行时不能再换行：截断昵称，把空间留给正文（至少留 2 个字的宽度）
		keep := w.bubbleWidth - w.padX*2 - w.fontSize - w.fontSize*2
		nameRunes = []rune(w.truncateWidth(string(nameRunes), keep))
		nameWidth = w.textWidth(string(nameRunes))
		firstAvail = w.bubbleWidth - w.padX*2 - w.fontSize - nameWidth
		firstOnSameLine = firstAvail > 0
	}
	if firstOnSameLine {
		bodyLines = w.packChars(bodyRunes, w.maxLines, firstAvail)
	} else if w.maxLines > 1 {
		bodyLines = w.packChars(bodyRunes, w.maxLines-1, bodyAvail)
	} else {
		// 一行模式且昵称已占满：正文只取能在剩余空间显示的部分，不换行
		bodyLines = w.packChars(bodyRunes, 1, bodyAvail)
		if len(bodyLines) > 0 && firstAvail <= 0 {
			bodyLines = nil
		}
	}

	lines := make([]chatLine, 0, w.maxLines+1)
	if firstOnSameLine && len(bodyLines) > 0 {
		// 昵称与正文首段同一行：分成两个片段上色
		lines = append(lines, chatLine{text: string(nameRunes), color: chatNickColor})
		lines = append(lines, chatLine{text: bodyLines[0], color: chatTextColor})
		bodyLines = bodyLines[1:]
	} else {
		lines = append(lines, chatLine{text: string(nameRunes), color: chatNickColor})
	}
	for _, l := range bodyLines {
		lines = append(lines, chatLine{text: l, color: chatTextColor})
	}

	// 实际占用行数：首行含昵称，后续为正文行
	visualRows := 1 + len(bodyLines)
	if visualRows > w.maxLines {
		visualRows = w.maxLines
	}
	if visualRows < 1 {
		visualRows = 1
	}
	return lines, visualRows
}

// addEntry 登记一条消息并推进时间轴
func (w *ChatWriter) addEntry(recvAt time.Time, lines []chatLine, visualRows int, kind chatMsgKind) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || w.writeErr {
		return
	}
	if recvAt.IsZero() {
		recvAt = w.now()
	}
	nowCS := w.toCS(recvAt)
	if nowCS < w.lastCS {
		// 时间倒流（时钟回拨或消息乱序）：夹取到已写出位置，避免负长度事件
		nowCS = w.lastCS
	}
	if err := w.advanceLocked(nowCS); err != nil {
		w.writeErr = true
		return
	}

	entry := &chatEntry{
		startCS:    nowCS,
		expireCS:   nowCS + w.stayCS,
		lines:      lines,
		visualRows: visualRows,
		kind:       kind,
	}
	// 同一行的片段要合并计算宽度（第 1 行为昵称 + 正文首段），保证气泡不裁字
	maxFragments := min(2, len(lines))
	for _, l := range lines[:maxFragments] {
		entry.textWidth += w.textWidth(l.text)
	}
	for _, l := range lines[maxFragments:] {
		if width := w.textWidth(l.text); width > entry.textWidth {
			entry.textWidth = width
		}
	}
	entry.bubbleH = visualRows*w.lineHeight + w.padY*2

	w.active = append(w.active, entry)
	// 活跃列表只保留最新的 rows 条消息，内存与文件规模都有界
	if extra := len(w.active) - w.rows; extra > 0 {
		w.active = append([]*chatEntry(nil), w.active[extra:]...)
	}
}

// advanceLocked 把时间轴推进到 until：按段写出当前活跃条目，每段结束时间取 min(until, 段内最早到期)。
func (w *ChatWriter) advanceLocked(until int64) error {
	if len(w.active) == 0 {
		if until > w.lastCS {
			w.lastCS = until
		}
		return nil
	}
	if w.lastCS < 0 {
		w.lastCS = w.active[0].startCS
	}
	for w.lastCS < until {
		end := until
		for _, e := range w.active {
			if e.expireCS > w.lastCS && e.expireCS < end {
				end = e.expireCS
			}
		}
		if end <= w.lastCS {
			break
		}
		if err := w.flushLocked(w.lastCS, end); err != nil {
			w.noteErr(err)
			return err
		}
		w.lastCS = end
		// 丢弃已到期条目（其可见区间已全部写出）
		kept := w.active[:0]
		for _, e := range w.active {
			if e.expireCS > w.lastCS {
				kept = append(kept, e)
			}
		}
		w.active = kept
		if len(w.active) == 0 {
			break
		}
	}
	if until > w.lastCS {
		w.lastCS = until
	}
	return nil
}

// flushLocked 写出 [startCS, endCS] 区间内所有活跃条目的事件。
// 显示位置：从底部向上按每个条目真实的气泡高度累积，最新的在最下方。
func (w *ChatWriter) flushLocked(startCS, endCS int64) error {
	if endCS <= startCS {
		return nil
	}
	topY := w.baseBottom
	for i := len(w.active) - 1; i >= 0; i-- {
		e := w.active[i]
		topY -= e.bubbleH // 气泡顶边
		if topY < 0 {
			// 已经超出画布顶部：更旧的条目不再渲染，避免被夹到 0 后互相重叠
			break
		}
		if err := w.writeEntry(e, topY, startCS, endCS); err != nil {
			w.noteErr(err)
			return err
		}
		topY -= w.gapY
	}
	return nil
}

// writeEntry 生成并写出单个条目的事件（圆角气泡 + 文字，不使用任何动效）。
// 参数 topY 为气泡顶边的 y 坐标（调用方按累积高度计算），内部不再做高度加减。
func (w *ChatWriter) writeEntry(e *chatEntry, topY int, startCS, endCS int64) error {
	if topY < 0 {
		topY = 0
	}
	// 气泡宽度按实际文字宽度，最大不超过配置的宽度百分比
	textW := e.textWidth + w.padX*2 + w.fontSize/2
	if textW > w.bubbleWidth {
		textW = w.bubbleWidth
	}
	if minW := w.fontSize * 2; textW < minW {
		textW = minW
	}
	radius := w.radius
	if radius > e.bubbleH/2 {
		radius = e.bubbleH / 2
	}
	if radius > textW/2 {
		radius = textW / 2
	}

	start := formatTime(startCS)
	end := formatTime(endCS)

	var sb strings.Builder
	// 第 1 层：深色圆角气泡；透明度用 \1a 单独设置（\1c 中的 alpha 会被 libass 忽略）
	fmt.Fprintf(&sb, "Dialogue: %d,%s,%s,%s,,0,0,0,,{\\an7\\pos(%d,%d)\\p1\\bord0\\shad0\\1c%s\\1a&H%02X&}%s\n",
		chatEventLayerBubble, start, end, chatBubbleStyle,
		w.baseX, topY,
		chatBubbleColor, w.alpha, chatRoundedRect(0, 0, textW, e.bubbleH, radius))

	// 第 2 层：文字。逐片段上色（\c 为持久标签），\N 仅用于行间换行
	var text strings.Builder
	for i, l := range e.lines {
		// 前两个片段属于第一行（昵称 + 正文首段），其余片段各自一行
		if i >= 2 {
			text.WriteString("\\N")
		}
		fmt.Fprintf(&text, "{\\c%s}%s", l.color, escapeText(l.text))
	}
	fmt.Fprintf(&sb, "Dialogue: %d,%s,%s,%s,,0,0,0,,{\\an7\\pos(%d,%d)\\q2}%s\n",
		chatEventLayerText, start, end, chatTextStyle,
		w.baseX+w.padX, topY+w.padY, text.String())

	_, err := w.file.WriteString(sb.String())
	return err
}

// AddDanmaku 写入一条普通弹幕
func (w *ChatWriter) AddDanmaku(recvAt time.Time, username, text string, color int) {
	lines, rows := w.entryLines(username, text, chatKindDanmaku)
	w.addEntry(recvAt, lines, rows, chatKindDanmaku)
}

// AddGift 写入一条礼物消息（金色文字聊天条，不实现礼物图标与动效）
func (w *ChatWriter) AddGift(recvAt time.Time, username, giftName string, num int, price int, coinType string) {
	if num <= 0 {
		num = 1
	}
	body := fmt.Sprintf("%s x%d", giftName, num)
	if coinType == "gold" && price > 0 {
		body = fmt.Sprintf("%s x%d (¥%.1f)", giftName, num, float64(price)*float64(num)/1000.0)
	}
	lines, rows := w.entryLines(username, body, chatKindGift)
	w.addEntry(recvAt, lines, rows, chatKindGift)
}

// AddGuard 写入一条上舰/开通贵族消息（金色文字聊天条）
func (w *ChatWriter) AddGuard(recvAt time.Time, username, giftName string, price int) {
	body := fmt.Sprintf("开通了%s", giftName)
	if price > 0 {
		body = fmt.Sprintf("开通了%s (¥%d)", giftName, price/1000)
	}
	lines, rows := w.entryLines(username, body, chatKindGift)
	w.addEntry(recvAt, lines, rows, chatKindGift)
}

// AddSuperChat 写入一条醒目留言（金色文字聊天条，价格并入正文）
func (w *ChatWriter) AddSuperChat(recvAt time.Time, username, text string, price int) {
	body := text
	if price > 0 {
		body = fmt.Sprintf("[SC ¥%d] %s", price, text)
	}
	lines, rows := w.entryLines(username, body, chatKindGift)
	w.addEntry(recvAt, lines, rows, chatKindGift)
}

// OutputPath 返回 ASS 文件路径
func (w *ChatWriter) OutputPath() string {
	return w.file.Name()
}

// Close 把时间轴推进到最后一条消息的到期时间并关闭文件（可重复调用）
func (w *ChatWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	if !w.writeErr {
		until := w.lastCS
		for _, e := range w.active {
			if e.expireCS > until {
				until = e.expireCS
			}
		}
		if until > 0 {
			if err := w.advanceLocked(until); err != nil {
				w.writeErr = true
			}
		}
	}
	if w.file != nil {
		if err := w.file.Close(); err != nil {
			w.noteErr(err)
		}
	}
	// 写入过程中出现过错误时必须显式返回，避免调用方以为输出完整
	return w.firstErr
}

// chatRoundedRect 生成 ASS 圆角矩形绘制指令（\p1 模式下坐标以像素为单位）
func chatRoundedRect(x, y, width, height, radius int) string {
	if width <= 0 {
		width = 1
	}
	if height <= 0 {
		height = 1
	}
	maxRadius := height / 2
	if width/2 < maxRadius {
		maxRadius = width / 2
	}
	if radius > maxRadius {
		radius = maxRadius
	}
	if radius < 0 {
		radius = 0
	}
	r := radius
	right := x + width
	bottom := y + height
	c := int(float64(r) * 0.5523) // 圆角贝塞尔控制点偏移

	return fmt.Sprintf("m %d %d l %d %d b %d %d %d %d %d %d l %d %d b %d %d %d %d %d %d l %d %d b %d %d %d %d %d %d l %d %d b %d %d %d %d %d %d c",
		x+r, y,
		right-r, y,
		right-r+c, y, right, y+r-c, right, y+r,
		right, bottom-r,
		right, bottom-r+c, right-r+c, bottom, right-r, bottom,
		x+r, bottom,
		x+r-c, bottom, x, bottom-r+c, x, bottom-r,
		x, y+r,
		x, y+r-c, x+r-c, y, x+r, y)
}
