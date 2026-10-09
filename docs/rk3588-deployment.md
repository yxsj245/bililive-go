# RK3588 专用容器部署与还原

## 范围与边界

本适配面向 RK3588 的 MPP 编解码与 RGA 图像处理。保留原有直播录制、管理和任务队列；录制时保存原流，录制结束后统一执行硬件压缩与可选 ASS 烧录。

- H.265 默认编码器为 `hevc_rkmpp`，H.264 可选 `h264_rkmpp`。
- 中文 ASS 字幕由 libass/CPU 绘制；视频解码与编码使用 MPP。配置缩放时，硬解路径优先 RGA；软件解码回退不能使用 RGA 时采用 CPU 缩放。
- 未设置缩放时不改变录像分辨率；音轨采用流复制。
- 无需重新编码的下载、分段、FLV 修复、MP4 换封装继续原有方式。
- 网页播放的解码在用户浏览器发生；H.265/MKV 并非所有浏览器可直接播放，下载后使用支持 HEVC 的播放器，或选择 H.264。
- 硬件编码失败不能静默改用 CPU 编码；默认仅允许软件解码回退，源视频/ASS 保留。
- 码率是目标而非保证体积更小；源流已是低码率 HEVC 时，重新压缩可能更大。

## 目录和固定版本

- 源码：`/opt/Code/bililive-go`，以 Git 工作区保存未提交补丁；禁止用在线更新覆盖。
- 最终部署与交付：`/opt/DockerDate/bililive-go`，包含运行配置、`manage.sh`、`docs/` 与板载 `appdata/`；旧验证素材与备份已按用户要求清理。开发目录仅作编译调试；发布容器内嵌网页和程序，不挂载开发目录。
- 板载应用数据：部署目录 `appdata/` → `/var/lib/bililive-go`；数据库、工具和日志与录像分离。
- 原录像外盘：`/media/ztl/LinuxDate/files/直播录制`，容器内仍为 `/srv/bililive`。
- 专用 FFmpeg：镜像内 `/opt/rk3588/bin/ffmpeg`，ffprobe 同目录。
- 多媒体构建上下文：`/opt/Code/rk3588-media-build`，不访问历史录像。
- 镜像：`bililive-rk3588-media:20261009` 与 `bililive-go-rk3588:20261009`；实际部署应在 `.env` 将 `BILILIVE_IMAGE` 固定为构建后的镜像 ID。

固定上游提交：

| 组件 | 仓库 | 提交 |
| --- | --- | --- |
| FFmpeg | nyanmisaka/ffmpeg-rockchip | d90e3a1c18d7929383cf88c1b3da2e2d1c966cbf |
| MPP | rockchip-linux/mpp | 14729dd578e570e5f00fd1dd2113f5429012d64b |
| RGA | airockchip/librga（ARM64 预编译库） | 2b32edcb97b601b25683e2941d888c8515da6d55 |

基础 Ubuntu 镜像摘要和源码提交均记录在 `contrib/rk3588/Dockerfile.media`。Ubuntu 包仓库未使用快照锁定，因此未来从零构建并非逐字节复现；已有 Docker 镜像仍保留；旧还原归档已在后续清理中删除，不能再依赖旧包。

## 构建

在源码根目录执行，要求 Linux ARM64、Docker、Go、Node 和 Yarn Classic。源码内注释和文档为中文，文件名保持英文。

```bash
bash contrib/rk3588/build-media.sh
bash contrib/rk3588/smoke-media.sh
bash contrib/rk3588/build-app.sh
```

`build-app.sh` 会先 `make build-web dev` 验证前后端，再 `make build` 生成内嵌网页的发布二进制，使用最小上下文构建容器。初次媒体编译需要下载依赖和编译 FFmpeg，耗时明显高于后续应用构建。

可通过 `MEDIA_IMAGE`、`APP_IMAGE`、`APP_VERSION`、`BUILD_JOBS` 调整构建参数。编译并发默认 8，后处理并发 1。按用户最新要求，新启动任务和容器均不设置 CPU 亲和性；不改变其他已有服务设置。

## 配置与启动

先备份现有配置，再用脚本只更新硬件相关参数。脚本保留直播间监听状态和其他配置，关闭自动更新，保留原视频与 ASS。

```bash
node contrib/rk3588/prepare-config.cjs /opt/DockerDate/bililive-go/config.yml
cp contrib/rk3588/docker-compose.yml /opt/DockerDate/bililive-go/docker-compose.yml
cd /opt/DockerDate/bililive-go
install -d -o 1000 -g 1000 -m 0755 appdata appdata/logs
# 配置 app_data_path: /var/lib/bililive-go，以及 log.out_put_folder: /var/lib/bililive-go/logs
docker compose config -q
docker compose up -d
```

默认监听局域网 `192.168.10.62:8084`，不自动开放公网或 IPv6。通过 `.env` 的 `BILILIVE_BIND_IP`、`BILILIVE_PORT`、`BILILIVE_RECORD_DIR`、`BILILIVE_APPDATA_DIR`、`BILILIVE_IMAGE` 调整地址、挂载与镜像。数据绑定禁用自动创建不存在的宿主目录。

容器以录像目录属主 UID/GID（本机 1000:1000）运行，附加 video/render 组；只调整部署配置的所有权，不递归修改历史录像。设备仅映射 MPP、RGA、DMA heap、DRM 分配器 `card0/renderD128`；不映射 NPU 的 `card1/renderD129`，不启用 `privileged`，去掉 Linux capabilities，并开启 no-new-privileges。MPP 有一处容器专用小补丁：读取 `/opt/rk3588/device-tree/compatible`，对应只读挂载的宿主设备树。避免 Docker 的 `/proc`/`/sys` 遮蔽导致识别不到 RK3588，不挂载整个宿主 `/sys`。不要复制原镜像 entrypoint 的递归 chown，它可能改变整个历史录像目录所有权。

**按用户要求不做 USB 守护集成或修改**。Compose 保留外盘 bind，由已有 Docker 容器自动识别机制处理；不创建新规则、不重启 USB 守护。Compose 设置 `restart: no`，防止外盘容器绕过已有守护抢跑。若转移到没有该机制的机器，须自行制定外盘缺盘与启动策略，不能照搬自启假设。

## 默认后处理

```yaml
ffmpeg_path: /opt/rk3588/bin/ffmpeg
on_record_finished:
  burn_subtitles: true
  burn_delete_ass: false
  burn_delete_source: false
  rk3588:
    enable: true
    compress: true
    codec: hevc_rkmpp
    bitrate: 4M
    decode_fallback: true
    scale_width: 0
    scale_height: 0
task_queue:
  max_concurrent: 1
update:
  auto_check: false
  auto_download: false
```

`enable` 控制硬件路径，`compress` 控制自动处理无弹幕视频。手动独立压缩要求 `enable: true`，但不依赖自动 `compress` 开关。有 ASS 时按烧录开关合并执行一次转码。网页入口和按房间设置见 `docs/rk3588-compress-webui.md`。

## 运行诊断

```bash
cd /opt/DockerDate/bililive-go
docker compose ps
docker logs --tail 100 bililive-go
curl -fsS http://192.168.10.62:8084/api/hardware/status
curl -fsS 'http://192.168.10.62:8084/api/hardware/status?refresh=1'
docker inspect bililive-go --format '{{.HostConfig.CpusetCpus}} {{.HostConfig.Privileged}}'
```

硬件列表是能力探测，自检是隔离测试素材的实际执行；不能由此保证每种分辨率、像素格式或损坏录像均支持。以任务页实际 FFmpeg 命令、回退记录和输出校验为准。不会自动对历史录像批量处理。

## 数据清理与后续备份

2026-10-09 按用户确认删除旧外盘应用数据库/日志、验证素材（含短录）和旧交付归档，保留 26 个直播间配置、硬件参数、源码及 Docker 镜像。板载应用数据库已重新初始化，不恢复旧任务、统计、调度历史。此后新的运行数据仍会正常生成，但不再写外盘。

当前没有旧备份可供回滚。以后如主动执行 `contrib/rk3588/archive.sh`，归档保存运行定义、源码和镜像；验证目录不存在时跳过，不打包应用数据库、工具缓存和录像。还原脚本不会覆盖现存配置或自动启动，需确认板载应用目录、UID/GID 与外盘绑定。详见 `docs/rk3588-storage-layout.md`。

## 验证记录

完整实测结果将在 `docs/rk3588-validation.md` 中补齐，包括容器硬解/硬编、中文字幕画面、音轨、RGA、封面、自动/批量任务、失败保留、重启和网页访问。
