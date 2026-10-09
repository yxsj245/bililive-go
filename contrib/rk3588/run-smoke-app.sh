#!/usr/bin/env bash
# 仅用于隔离测试容器，外部监听localhost，不挂载业务外盘。
set -euo pipefail
root=$(git rev-parse --show-toplevel)
base=${MEDIA_BUILD_CONTEXT:-/opt/Code/rk3588-media-build}
image=${APP_IMAGE:-bililive-go-rk3588:20261009}
node "$root/contrib/rk3588/prepare-smoke.cjs"
devices=(--device /dev/mpp_service --device /dev/rga --device /dev/dri/card0 --device /dev/dri/renderD128)
for p in /dev/dma_heap/*; do if [[ "$p" != */cma-uncached ]]; then devices+=(--device "$p"); fi; done
chown -R 1000:1000 "$base/api-smoke"
chown 1000:1000 "$base/smoke-config.yml"
docker run -d --user 1000:1000 --group-add 44 --group-add 992 --name bililive-rk3588-test --restart no --security-opt no-new-privileges:true --cap-drop ALL --tmpfs /opt/bililive/tmp_for_exec:exec,nosuid,nodev,mode=0700,uid=1000,gid=1000,size=1024m "${devices[@]}" --mount type=bind,src=/sys/firmware/devicetree/base,dst=/opt/rk3588/device-tree,readonly -p 127.0.0.1:18084:8080 --mount "type=bind,src=$base/api-smoke,dst=/srv/bililive" --mount "type=bind,src=$base/smoke-config.yml,dst=/etc/bililive-go/config.yml" "$image"
