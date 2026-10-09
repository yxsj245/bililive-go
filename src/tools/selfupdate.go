package tools

import (
	"os"
	"strings"
)

// EnvDisableSelfUpdate 设置该环境变量后，禁用应用自更新与外部工具（含 FFmpeg）的下载/替换。
//
// 专用容器镜像用它保证镜像内固定的二进制不会被运行期更新覆盖：
// 镜像里已经带好硬件处理所需的 FFmpeg，一旦被自动下载的通用版本替换，MPP/RGA 能力就会消失。
const EnvDisableSelfUpdate = "BILILIVE_DISABLE_SELF_UPDATE"

// SelfUpdateDisabled 返回是否已禁用自更新与外部工具下载。
// 取值 1/true/yes/on（忽略大小写）均视为禁用。
func SelfUpdateDisabled() bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(EnvDisableSelfUpdate)))
	switch value {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// SelfUpdateDisabledMessage 返回统一的提示文案（中文）
func SelfUpdateDisabledMessage() string {
	return "当前部署已禁用应用自更新与外部工具下载（" + EnvDisableSelfUpdate + "=1），请通过更新镜像完成升级"
}
