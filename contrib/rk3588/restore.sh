#!/usr/bin/env bash
# 从解压后的交付包恢复当前适配版，不覆盖已有部署，不改 USB 守护或历史录像。
set -euo pipefail
package=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
target=${1:-/opt/DockerDate/bililive-go}
if [[ "$target" != /* || "$target" == / ]]; then
    echo '还原目录必须是非根目录的绝对路径。' >&2
    exit 2
fi
cd "$package"
sha256sum -c SHA256SUMS
if test -e "$target/config.yml" || test -e "$target/docker-compose.yml"; then
    echo '目标已有运行配置，请先独立备份并选择新的空目录，脚本不会覆盖。' >&2
    exit 1
fi
docker load -i images.tar
mkdir -p "$target/docs"
cp -a deploy/. "$target/"
cp -a docs/. "$target/docs/"
if test -d verification; then
    mkdir -p "$target/verification"
    cp -a verification/. "$target/verification/"
fi
# 板载应用目录必须预先存在；新数据库由应用初始化，不恢复旧历史数据。
install -d -o 1000 -g 1000 -m 0755 "$target/appdata" "$target/appdata/logs"
printf '已恢复至 %s。确认外盘路径、板载 appdata 位置/UID/GID 和 IP 正确后执行：\n' "$target"
printf 'cd %q && bash manage.sh start\n' "$target"
