#!/usr/bin/env bash
# 仅生成隔离测试素材，不访问业务录像，不要求全特权模式。
set -euo pipefail
root=$(git rev-parse --show-toplevel)
image=${MEDIA_IMAGE:-bililive-rk3588-media:20261009}
work=${SMOKE_DIR:-/opt/Code/rk3588-media-build/smoke}
mkdir -p "$work"
cp "$root/contrib/rk3588/smoke.ass" "$work/source.ass"
devices=(--device /dev/mpp_service --device /dev/rga --device /dev/dri/card0 --device /dev/dri/renderD128)
for p in /dev/dma_heap/*; do if [[ "$p" != */cma-uncached ]]; then devices+=(--device "$p"); fi; done
chown -R 1000:1000 "$work"
docker run --rm --user 1000:1000 --group-add 44 --group-add 992 --security-opt no-new-privileges:true --cap-drop ALL "${devices[@]}" --mount type=bind,src=/sys/firmware/devicetree/base,dst=/opt/rk3588/device-tree,readonly --mount "type=bind,src=$work,dst=/test" "$image" sh -ec '
ffmpeg -hide_banner -y -f lavfi -i testsrc2=size=1280x720:rate=30 -f lavfi -i sine=frequency=440:sample_rate=48000 -t 4 -c:v h264_rkmpp -b:v 6M -c:a aac /test/source.mp4 > /test/generate.log 2>&1
ffmpeg -hide_banner -y -hwaccel rkmpp -hwaccel_output_format drm_prime -i /test/source.mp4 -map 0:v:0 -map 0:a? -c:v hevc_rkmpp -b:v 2M -c:a copy /test/compressed.mkv > /test/compress.log 2>&1
ffmpeg -hide_banner -y -hwaccel rkmpp -hwaccel_output_format drm_prime -i /test/source.mp4 -vf "hwdownload,format=nv12,ass=/test/source.ass" -c:v hevc_rkmpp -b:v 2M -c:a copy /test/burned.mkv > /test/burn.log 2>&1
ffmpeg -hide_banner -y -hwaccel rkmpp -hwaccel_output_format drm_prime -i /test/source.mp4 -vf scale_rkrga=w=640:h=360:format=nv12 -c:v hevc_rkmpp -b:v 1M -c:a copy /test/scaled.mkv > /test/scale.log 2>&1
ffmpeg -hide_banner -y -hwaccel rkmpp -hwaccel_output_format drm_prime -i /test/source.mp4 -vf scale_rkrga=w=640:h=360:format=nv12 -frames:v 1 -c:v mjpeg_rkmpp /test/cover.jpg > /test/cover.log 2>&1
for f in compressed burned scaled; do
    ffprobe -v error -show_streams -show_format -of json /test/$f.mkv > /test/$f.json
    ffmpeg -v error -i /test/$f.mkv -f null - > /test/$f.decode.log 2>&1
 done
ffmpeg -v error -y -i /test/burned.mkv -ss 1 -frames:v 1 /test/burned.png
ffmpeg -v error -y -i /test/compressed.mkv -ss 1 -frames:v 1 /test/compressed.png
ffprobe -v error -show_streams -of json /test/cover.jpg > /test/cover.json
printf "容器 H.264/H.265、零拷贝硬解压缩、中文 ASS、RGA 缩放、MPP JPEG 及完整解码验证通过。\n"
'
