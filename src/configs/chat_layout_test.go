package configs

import (
	"testing"
)

// TestNormalizeLayout 布局标识归一化：空值与未知值必须回退为滚动布局（兼容旧配置）
func TestNormalizeLayout(t *testing.T) {
	cases := map[string]string{
		"":       DanmakuLayoutScroll,
		"scroll": DanmakuLayoutScroll,
		"chat":   DanmakuLayoutChat,
		"CHAT":   DanmakuLayoutChat,
		" Chat ": DanmakuLayoutChat,
		"banner": DanmakuLayoutScroll,
	}
	for in, want := range cases {
		if got := NormalizeLayout(in); got != want {
			t.Errorf("NormalizeLayout(%q) = %q, 期望 %q", in, got, want)
		}
	}
}

// TestIsChatLayout 聊天布局判定
func TestIsChatLayout(t *testing.T) {
	if !IsChatLayout("chat") || !IsChatLayout(" Chat ") {
		t.Error("chat 应被识别为聊天布局")
	}
	if IsChatLayout("") || IsChatLayout("scroll") || IsChatLayout(" unknow ") {
		t.Error("空值与 scroll 不应被识别为聊天布局")
	}
}

// TestChatLayoutDefaults 聊天布局默认值必须与文档一致
func TestChatLayoutDefaults(t *testing.T) {
	d := DefaultChatLayoutConfig()
	if d.Rows != 5 || d.WidthPercent != 72 || d.BottomPercentValue() != 8 || d.StaySeconds != 15 || d.MaxLinesPerItem != 2 {
		t.Fatalf("聊天布局默认值不符合预期: %+v", d)
	}
	if d.BottomPercent == nil {
		t.Fatal("底部留白默认值应由指针承载，便于区分“未设置”与“显式 0”")
	}
	var nilCfg *ChatLayoutConfig
	if got := nilCfg.Effective(); got.Rows != d.Rows || got.BottomPercentValue() != d.BottomPercentValue() {
		t.Fatalf("nil 配置应返回默认值，实际: %+v", got)
	}
	partial := (&ChatLayoutConfig{Rows: 3}).Effective()
	if partial.Rows != 3 || partial.WidthPercent != 72 {
		t.Fatalf("部分字段应保留用户值并补齐默认值，实际: %+v", partial)
	}
}

// TestChatLayoutEffectiveNoSilentClamp 越界配置不得被静默夹取，必须由 Validate 明确拒绝
func TestChatLayoutEffectiveNoSilentClamp(t *testing.T) {
	raw := &ChatLayoutConfig{Rows: 9999, WidthPercent: 100000, BottomPercent: IntPtr(999), StaySeconds: 100000, MaxLinesPerItem: 99}
	if err := raw.Validate("1920x1080"); err == nil {
		t.Error("越界配置必须校验失败，而不是被静默夹取")
	}
	// Effective 只负责补默认值，不改写用户显式给出的值
	eff := raw.Effective()
	if eff.Rows != 9999 || eff.WidthPercent != 100000 || eff.BottomPercentValue() != 999 {
		t.Errorf("Effective 不应改写用户显式值: %+v", eff)
	}
	// 零值/nil 才补默认值
	zero := (&ChatLayoutConfig{}).Effective()
	if zero.Rows != 5 || zero.WidthPercent != 72 || zero.BottomPercentValue() != 8 || zero.StaySeconds != 15 || zero.MaxLinesPerItem != 2 {
		t.Errorf("零值应补齐默认值: %+v", zero)
	}
	// 显式 0 底部留白不被当成未设置
	explicitZero := (&ChatLayoutConfig{BottomPercent: IntPtr(0)}).Effective()
	if explicitZero.BottomPercentValue() != 0 {
		t.Errorf("显式 0 底部留白应保留，实际 %d", explicitZero.BottomPercentValue())
	}
}

// TestChatLayoutValidate 聊天布局参数校验边界
func TestChatLayoutValidate(t *testing.T) {
	valid := DefaultChatLayoutConfig()
	if err := valid.Validate("1920x1080"); err != nil {
		t.Fatalf("默认参数应校验通过: %v", err)
	}

	bad := []ChatLayoutConfig{
		{Rows: 21},
		{Rows: -1},
		{WidthPercent: 10},
		{WidthPercent: 120},
		{BottomPercent: IntPtr(50)},
		{BottomPercent: IntPtr(-6)},
		{StaySeconds: 300},
		{MaxLinesPerItem: 5},
	}
	for i, c := range bad {
		if err := c.Validate("1920x1080"); err == nil {
			t.Errorf("第 %d 组非法参数应校验失败: %+v", i, c)
		}
	}
	// 显式 0 底部留白是合法值
	if err := (&ChatLayoutConfig{BottomPercent: IntPtr(0)}).Validate("1920x1080"); err != nil {
		t.Errorf("显式 0 底部留白应合法: %v", err)
	}
	if err := valid.Validate("123x456"); err == nil {
		t.Error("非法分辨率应校验失败")
	}
	var nilCfg *ChatLayoutConfig
	if err := nilCfg.Validate("1920x1080"); err != nil {
		t.Errorf("nil 配置应视为无需校验: %v", err)
	}
}

// TestParseDanmakuResolution 分辨率解析与安全边界
func TestParseDanmakuResolution(t *testing.T) {
	ok := []struct {
		in   string
		w, h int
	}{
		{"", 1920, 1080},
		{"1920x1080", 1920, 1080},
		{" 1280X720 ", 1280, 720},
		{"1080x1920", 1080, 1920}, // 竖屏
		{"3840x2160", 3840, 2160},
		{"160x160", 160, 160},
	}
	for _, c := range ok {
		w, h, err := ParseDanmakuResolution(c.in)
		if err != nil {
			t.Errorf("ParseDanmakuResolution(%q) 不应报错: %v", c.in, err)
			continue
		}
		if w != c.w || h != c.h {
			t.Errorf("ParseDanmakuResolution(%q) = %dx%d, 期望 %dx%d", c.in, w, h, c.w, c.h)
		}
	}

	bad := []string{
		"1920", "1920x", "x1080", "abcxdef", "1080x1920x60",
		"0x1080", "-1920x1080", "100x100", "8192x2", "1921x1081",
		"9000x1080", "1080x9000", "1920x200", "200x1920",
	}
	for _, in := range bad {
		if _, _, err := ParseDanmakuResolution(in); err == nil {
			t.Errorf("ParseDanmakuResolution(%q) 应报错", in)
		}
	}
}

// TestIsSafeDanmakuResolution 运行期尺寸安全校验
func TestIsSafeDanmakuResolution(t *testing.T) {
	if !IsSafeDanmakuResolution(1920, 1080) || !IsSafeDanmakuResolution(1080, 1920) {
		t.Error("常规横竖屏尺寸应通过安全校验")
	}
	if IsSafeDanmakuResolution(0, 0) || IsSafeDanmakuResolution(1921, 1080) || IsSafeDanmakuResolution(-1920, 1080) {
		t.Error("非法尺寸不应通过安全校验")
	}
}

// TestDanmakuConfigLayoutNotBreakingLegacy 旧配置（无 layout 字段）行为必须完全不变
func TestDanmakuConfigLayoutNotBreakingLegacy(t *testing.T) {
	var c DanmakuConfig
	if err := c.Validate(); err != nil {
		t.Fatalf("旧配置校验应通过: %v", err)
	}
	// 空 layout 必须保持为空（等价 scroll），不能归一化成 "scroll" 写回配置文件
	if c.Layout != "" {
		t.Errorf("空 layout 不应被改写，实际: %q", c.Layout)
	}
	if NormalizeLayout(c.Layout) != DanmakuLayoutScroll || IsChatLayout(c.Layout) {
		t.Error("空 layout 在渲染判定上必须等价于 scroll")
	}
	if c.Chat != nil {
		t.Errorf("滚动布局且未配置聊天参数时不应生成 Chat，实际: %+v", c.Chat)
	}
	if c.FontSize != 36 || c.ScrollTime != 10 || c.Resolution != "1920x1080" {
		t.Errorf("默认值发生了变化: %+v", c)
	}
}

// TestDanmakuConfigChatLayout SetDefaults 必须为聊天布局补齐参数并校验通过
func TestDanmakuConfigChatLayout(t *testing.T) {
	c := DanmakuConfig{Layout: "chat", Resolution: "1080x1920", FontName: "Noto Sans CJK SC"}
	if err := c.Validate(); err != nil {
		t.Fatalf("聊天布局配置校验应通过: %v", err)
	}
	if c.Chat == nil {
		t.Fatal("聊天布局应补齐默认参数")
	}
	if c.Chat.Rows != 5 || c.Chat.StaySeconds != 15 {
		t.Errorf("聊天参数默认值不正确: %+v", c.Chat)
	}

	bad := DanmakuConfig{Layout: "chat", Chat: &ChatLayoutConfig{Rows: 99}}
	if err := bad.Validate(); err == nil {
		t.Error("非法聊天参数应校验失败")
	}
}

// TestDanmakuConfigChatParamsKeptWhenSwitchingBack 切回滚动布局时不得删除聊天参数
func TestDanmakuConfigChatParamsKeptWhenSwitchingBack(t *testing.T) {
	c := DanmakuConfig{
		Layout:   "scroll",
		Chat:     &ChatLayoutConfig{Rows: 8, WidthPercent: 60, BottomPercent: IntPtr(0), StaySeconds: 20},
		FontName: "Noto Sans CJK SC",
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("校验应通过: %v", err)
	}
	if c.Chat == nil {
		t.Fatal("切回滚动布局不应删除已配置的聊天参数")
	}
	if c.Chat.Rows != 8 || c.Chat.WidthPercent != 60 || c.Chat.StaySeconds != 20 {
		t.Errorf("聊天参数不应被改写: %+v", c.Chat)
	}
	if c.Chat.BottomPercentValue() != 0 {
		t.Errorf("显式 0 底部留白应保留，实际 %d", c.Chat.BottomPercentValue())
	}
	// 再切回 chat 时参数仍然生效
	c.Layout = "chat"
	if err := c.Validate(); err != nil {
		t.Fatalf("切回聊天布局应校验通过: %v", err)
	}
	if c.Chat.Rows != 8 {
		t.Errorf("切回聊天布局后行数应保持 8，实际 %d", c.Chat.Rows)
	}
}

// TestDanmakuConfigChatInvalidInScrollLayoutStillRejected 非聊天布局下非法聊天参数也必须被拒绝
func TestDanmakuConfigChatInvalidInScrollLayoutStillRejected(t *testing.T) {
	c := DanmakuConfig{
		Layout:   "scroll",
		Chat:     &ChatLayoutConfig{Rows: 100},
		FontName: "Noto Sans CJK SC",
	}
	if err := c.Validate(); err == nil {
		t.Error("滚动布局下越界的聊天参数同样应校验失败（避免房间级 YAML 绕过校验）")
	}
}

// TestMergeDanmakuConfigChat 房间级聊天布局按字段覆盖全局值
func TestMergeDanmakuConfigChat(t *testing.T) {
	base := DanmakuConfig{Layout: "chat", Chat: &ChatLayoutConfig{Rows: 5, WidthPercent: 72, BottomPercent: IntPtr(8), StaySeconds: 15}}
	override := &DanmakuConfig{Layout: "chat", Chat: &ChatLayoutConfig{Rows: 3}}
	merged := mergeDanmakuConfig(&base, override)
	if merged.Chat.Rows != 3 {
		t.Errorf("房间级行数应覆盖为 3，实际: %d", merged.Chat.Rows)
	}
	if merged.Chat.WidthPercent != 72 || merged.Chat.StaySeconds != 15 {
		t.Errorf("未覆盖字段应继承全局值，实际: %+v", merged.Chat)
	}
	if merged.Chat.BottomPercentValue() != 8 {
		t.Errorf("未设置底部留白应继承全局 8，实际: %d", merged.Chat.BottomPercentValue())
	}

	// 未设置 Chat 时完全继承全局，且必须是独立副本
	merged2 := mergeDanmakuConfig(&base, &DanmakuConfig{Layout: "chat"})
	if merged2.Chat.Rows != 5 {
		t.Errorf("未设置时应继承全局行数，实际: %+v", merged2.Chat)
	}
	if merged2.Chat == base.Chat {
		t.Error("继承时应返回副本，避免房间级与全局共享同一指针")
	}

	// 房间级显式把底部留白覆盖为 0
	merged3 := mergeDanmakuConfig(&base, &DanmakuConfig{Layout: "chat", Chat: &ChatLayoutConfig{BottomPercent: IntPtr(0)}})
	if merged3.Chat.BottomPercentValue() != 0 {
		t.Errorf("房间级显式 0 应覆盖全局，实际: %d", merged3.Chat.BottomPercentValue())
	}
	if base.Chat.BottomPercentValue() != 8 {
		t.Error("房间级覆盖不应污染全局配置")
	}

	// 房间改为滚动布局时保留聊天参数但不参与渲染
	merged4 := mergeDanmakuConfig(&base, &DanmakuConfig{Layout: "scroll"})
	if err := merged4.Validate(); err != nil {
		t.Fatalf("滚动布局校验应通过: %v", err)
	}
	if merged4.Chat == nil || merged4.Chat.Rows != 5 {
		t.Errorf("滚动布局下应保留聊天参数，实际: %+v", merged4.Chat)
	}
}
