package configs

import (
	"fmt"
	"strconv"
	"strings"
)

// DanmakuLayoutScroll 传统横向滚动弹幕布局（默认，保持历史行为）
const DanmakuLayoutScroll = "scroll"

// DanmakuLayoutChat 抖音风格左下角竖排聊天列表布局
const DanmakuLayoutChat = "chat"

// 聊天布局参数的取值范围，用于配置校验与运行期兜底，避免出现无界内存或非法几何值。
const (
	chatRowsMin        = 1
	chatRowsMax        = 20
	chatWidthPctMin    = 20
	chatWidthPctMax    = 100
	chatBottomPctMin   = 0
	chatBottomPctMax   = 40
	chatStaySecMin     = 1
	chatStaySecMax     = 120
	chatResolutionMin  = 160
	chatResolutionMax  = 8192
	chatAspectRatioMax = 4.0 // 宽高比上限，避免伪造超长画布导致事件几何异常
)

// ChatLayoutConfig 抖音风格聊天列表弹幕布局参数。
// 仅在 danmaku.layout 为 "chat" 时生效；未启用时保持原滚动弹幕行为不变。
type ChatLayoutConfig struct {
	Rows            int  `yaml:"rows,omitempty" json:"rows,omitempty"`                             // 左下角同时显示的行数（1~20）
	WidthPercent    int  `yaml:"width_percent,omitempty" json:"width_percent,omitempty"`           // 气泡最大宽度占画面宽度百分比（20~100）
	BottomPercent   *int `yaml:"bottom_percent,omitempty" json:"bottom_percent,omitempty"`         // 底部留白占画面高度百分比（0~40）；nil 表示继承上层配置
	StaySeconds     int  `yaml:"stay_seconds,omitempty" json:"stay_seconds,omitempty"`             // 每条消息在列表中停留秒数（1~120）
	MaxLinesPerItem int  `yaml:"max_lines_per_item,omitempty" json:"max_lines_per_item,omitempty"` // 单条消息最多显示行数（1~3），超出以省略号截断
}

// DefaultChatLayoutConfig 返回聊天布局的推荐默认值。
func DefaultChatLayoutConfig() ChatLayoutConfig {
	return ChatLayoutConfig{
		Rows:            5,
		WidthPercent:    72,
		BottomPercent:   IntPtr(8),
		StaySeconds:     15,
		MaxLinesPerItem: 2,
	}
}

// IsChatLayout 判断给定布局标识是否为聊天列表布局。
func IsChatLayout(layout string) bool {
	return strings.EqualFold(strings.TrimSpace(layout), DanmakuLayoutChat)
}

// NormalizeLayout 归一化布局标识：空值或未知值一律回退为滚动布局（兼容旧配置）。
func NormalizeLayout(layout string) string {
	if IsChatLayout(layout) {
		return DanmakuLayoutChat
	}
	return DanmakuLayoutScroll
}

// chatLayoutWithDefaults 返回填好默认值的聊天布局配置（入参可为 nil）。
// 只把零值/nil 视为“未设置”并补默认值；超出范围的值不做静默夹取，
// 由 Validate 明确报错，避免用户以为设置生效但实际被悄悄改写。
func chatLayoutWithDefaults(in *ChatLayoutConfig) ChatLayoutConfig {
	out := DefaultChatLayoutConfig()
	if in == nil {
		return out
	}
	if in.Rows > 0 {
		out.Rows = in.Rows
	}
	if in.WidthPercent > 0 {
		out.WidthPercent = in.WidthPercent
	}
	if in.BottomPercent != nil {
		out.BottomPercent = IntPtr(*in.BottomPercent)
	}
	if in.StaySeconds > 0 {
		out.StaySeconds = in.StaySeconds
	}
	if in.MaxLinesPerItem > 0 {
		out.MaxLinesPerItem = in.MaxLinesPerItem
	}
	return out
}

// BottomPercentValue 返回底部留白百分比（已保证非 nil）
func (c ChatLayoutConfig) BottomPercentValue() int {
	if c.BottomPercent == nil {
		return *DefaultChatLayoutConfig().BottomPercent
	}
	return *c.BottomPercent
}

// Clone 复制一份聊天布局配置（含指针字段），避免房间级覆盖污染全局配置
func (c ChatLayoutConfig) Clone() ChatLayoutConfig {
	out := c
	if c.BottomPercent != nil {
		out.BottomPercent = IntPtr(*c.BottomPercent)
	}
	return out
}

// Effective 返回填好默认值的聊天布局配置副本。
func (c *ChatLayoutConfig) Effective() ChatLayoutConfig {
	return chatLayoutWithDefaults(c)
}

// Validate 校验聊天布局参数，同时校验分辨率与布局的匹配关系。
func (c *ChatLayoutConfig) Validate(resolution string) error {
	if c == nil {
		return nil
	}
	if c.Rows != 0 && (c.Rows < chatRowsMin || c.Rows > chatRowsMax) {
		return fmt.Errorf("聊天弹幕行数必须在 %d~%d 之间，当前值: %d", chatRowsMin, chatRowsMax, c.Rows)
	}
	if c.WidthPercent != 0 && (c.WidthPercent < chatWidthPctMin || c.WidthPercent > chatWidthPctMax) {
		return fmt.Errorf("聊天弹幕宽度百分比必须在 %d~%d 之间，当前值: %d", chatWidthPctMin, chatWidthPctMax, c.WidthPercent)
	}
	if c.BottomPercent != nil && (*c.BottomPercent < chatBottomPctMin || *c.BottomPercent > chatBottomPctMax) {
		return fmt.Errorf("聊天弹幕底部留白百分比必须在 %d~%d 之间，当前值: %d", chatBottomPctMin, chatBottomPctMax, *c.BottomPercent)
	}
	if c.StaySeconds != 0 && (c.StaySeconds < chatStaySecMin || c.StaySeconds > chatStaySecMax) {
		return fmt.Errorf("聊天弹幕停留时间必须在 %d~%d 秒之间，当前值: %d", chatStaySecMin, chatStaySecMax, c.StaySeconds)
	}
	if c.MaxLinesPerItem != 0 && (c.MaxLinesPerItem < 1 || c.MaxLinesPerItem > 3) {
		return fmt.Errorf("聊天弹幕单条最大行数必须在 1~3 之间，当前值: %d", c.MaxLinesPerItem)
	}
	if _, _, err := ParseDanmakuResolution(resolution); err != nil {
		return err
	}
	return nil
}

// IsSafeDanmakuResolution 校验运行期传入的画布尺寸是否安全。
// 与 ParseDanmakuResolution 使用同一套边界，供写入器在运行期调整几何时调用。
func IsSafeDanmakuResolution(width, height int) bool {
	_, _, err := ParseDanmakuResolution(strconv.Itoa(width) + "x" + strconv.Itoa(height))
	return err == nil
}

// ParseDanmakuResolution 解析并校验弹幕画布分辨率。
// 除预设分辨率外，额外接受边长 160~8192 的偶数分辨率（例如竖屏 1080x1920），
// 但限制宽高比不超过 chatAspectRatioMax，避免极端画布造成几何取值异常。
func ParseDanmakuResolution(res string) (int, int, error) {
	s := strings.ToLower(strings.TrimSpace(res))
	if s == "" {
		return 1920, 1080, nil
	}
	parts := strings.SplitN(s, "x", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("分辨率格式必须为 宽x高，例如 1920x1080，当前值: %s", res)
	}
	w, errW := strconv.Atoi(strings.TrimSpace(parts[0]))
	h, errH := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errW != nil || errH != nil {
		return 0, 0, fmt.Errorf("分辨率必须为纯数字的 宽x高，例如 1920x1080，当前值: %s", res)
	}
	if w < chatResolutionMin || w > chatResolutionMax || h < chatResolutionMin || h > chatResolutionMax {
		return 0, 0, fmt.Errorf("分辨率边长必须在 %d~%d 之间，当前值: %s", chatResolutionMin, chatResolutionMax, res)
	}
	if w%2 != 0 || h%2 != 0 {
		return 0, 0, fmt.Errorf("分辨率宽高必须为偶数，当前值: %s", res)
	}
	long, short := w, h
	if h > w {
		long, short = h, w
	}
	if float64(long)/float64(short) > chatAspectRatioMax {
		return 0, 0, fmt.Errorf("分辨率宽高比不得超过 %.1f，当前值: %s", chatAspectRatioMax, res)
	}
	return w, h, nil
}
