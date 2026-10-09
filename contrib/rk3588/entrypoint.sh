#!/bin/sh
# 不递归修改外盘录像所有权，不修改宿主机配置或系统工具。
set -eu
if ! test -d /srv/bililive || ! test -w /srv/bililive; then
    echo '录像目录不存在或不可写，服务保持停止。' >&2
    exit 1
fi
exec /usr/local/bin/bililive-go -c /etc/bililive-go/config.yml
