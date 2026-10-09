#!/usr/bin/env bash
# 所有依赖在容器里编译；本脚本只创建构建上下文，不修改宿主机 FFmpeg。
set -euo pipefail
root=$(git rev-parse --show-toplevel)
context=${MEDIA_BUILD_CONTEXT:-/opt/Code/rk3588-media-build}
image=${MEDIA_IMAGE:-bililive-rk3588-media:20261009}
jobs=${BUILD_JOBS:-8}
mkdir -p "$context/sources"
fetch() {
    local name=$1 url=$2 revision=$3
    if ! test -d "$context/sources/$name/.git"; then
        git clone "$url" "$context/sources/$name"
    fi
    git -C "$context/sources/$name" fetch origin "$revision"
    git -C "$context/sources/$name" checkout --detach "$revision"
    test "$(git -C "$context/sources/$name" rev-parse HEAD)" = "$revision"
}
fetch ffmpeg https://github.com/nyanmisaka/ffmpeg-rockchip.git d90e3a1c18d7929383cf88c1b3da2e2d1c966cbf
fetch mpp https://github.com/rockchip-linux/mpp.git 14729dd578e570e5f00fd1dd2113f5429012d64b
fetch rga https://github.com/airockchip/librga.git 2b32edcb97b601b25683e2941d888c8515da6d55
mkdir -p "$context/patches"
cp "$root/contrib/rk3588/mpp-container.patch" "$context/patches/"
cp "$root/contrib/rk3588/Dockerfile.media" "$context/Dockerfile"
docker build --progress=plain --build-arg BUILD_JOBS="$jobs" -t "$image" "$context"
