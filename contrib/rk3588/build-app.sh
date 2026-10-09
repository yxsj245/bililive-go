#!/usr/bin/env bash
# 网页与后端均通过项目 Make 入口构建，发布镜像内嵌网页。
set -euo pipefail
root=$(git rev-parse --show-toplevel)
cd "$root"
version=${APP_VERSION:-rk3588-20261009}
media=${MEDIA_IMAGE:-bililive-rk3588-media:20261009}
image=${APP_IMAGE:-bililive-go-rk3588:20261009}
context=${APP_BUILD_CONTEXT:-/opt/Code/rk3588-media-build/app}
make build-web dev NO_TELEMETRY=1 VERSION="$version"
make build NO_TELEMETRY=1 VERSION="$version" PLATFORM=linux ARCH=arm64
# 使用最小上下文，避免将配置、历史录像、依赖和 Git 目录打包进镜像。
mkdir -p "$context/bin" "$context/contrib/rk3588"
cp bin/bililive-linux-arm64 "$context/bin/"
cp contrib/rk3588/entrypoint.sh "$context/contrib/rk3588/"
cp contrib/rk3588/Dockerfile "$context/Dockerfile"
docker build --progress=plain --build-arg MEDIA_IMAGE="$media" --build-arg SOURCE_REVISION="$(git rev-parse HEAD)" -t "$image" "$context"
