package danmaku

import (
	"time"

	"github.com/bililive-go/bililive-go/src/configs"
)

// DanmakuWriter 弹幕 ASS 写入器接口。
// 横向滚动布局（AssWriter）与抖音风格聊天列表布局（ChatWriter）都实现该接口，
// 三个平台的弹幕录制器只依赖此接口，不感知具体布局。
type DanmakuWriter interface {
	// AddDanmaku 写入一条普通弹幕
	AddDanmaku(recvAt time.Time, username, text string, color int)
	// AddGift 写入一条礼物消息
	AddGift(recvAt time.Time, username, giftName string, num int, price int, coinType string)
	// AddGuard 写入一条上舰/开通贵族消息
	AddGuard(recvAt time.Time, username, giftName string, price int)
	// AddSuperChat 写入一条醒目留言（SC）
	AddSuperChat(recvAt time.Time, username, text string, price int)
	// OutputPath 返回 ASS 文件路径
	OutputPath() string
	// Close 结束写入并刷盘
	Close() error
}

// NewDanmakuWriter 根据弹幕配置创建对应布局的写入器。
// 默认为横向滚动布局，保持旧配置行为不变；layout=chat 时使用聊天列表布局。
func NewDanmakuWriter(filePath string, startAt time.Time, cfg configs.DanmakuConfig, title string) (DanmakuWriter, error) {
	if configs.IsChatLayout(cfg.Layout) {
		return NewChatWriter(filePath, startAt, cfg, title)
	}
	return NewAssWriter(filePath, startAt, cfg, title)
}

// NewDanmakuWriterWithCreate 与 NewDanmakuWriter 相同，但把文件创建交给调用方
// （聊天布局支持自定义创建策略；滚动布局沿用默认创建方式）。
func NewDanmakuWriterWithCreate(filePath string, startAt time.Time, cfg configs.DanmakuConfig, title string, create CreateFileFunc) (DanmakuWriter, error) {
	if configs.IsChatLayout(cfg.Layout) {
		return NewChatWriterWithCreate(filePath, startAt, cfg, title, create)
	}
	return NewAssWriter(filePath, startAt, cfg, title)
}

// 编译期接口断言
var (
	_ DanmakuWriter = (*AssWriter)(nil)
	_ DanmakuWriter = (*ChatWriter)(nil)
)
