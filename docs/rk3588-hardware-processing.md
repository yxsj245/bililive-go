# RK3588 硬件弹幕烧录与压缩（后端）

本文档说明 bililive-go 后端的 RK3588（MPP/RGA）硬件处理能力：配置项、处理链、接口、
失败保护与已知限制。仅涉及后端；容器与 FFmpeg 构建由部署方另行负责。

## 一、功能范围

| 功能 | 是否使用硬件 | 说明 |
|---|---|---|
| 弹幕烧录（有同名 `.ass`） | 硬解 + 硬编码（MPP），字幕绘制仍由 CPU 完成 | 与压缩合并为**同一次编码** |
| 无弹幕压缩（无 `.ass`） | 硬解 + 硬编码 | 独立完成 H.265/H.264 压缩 |
| 缩放 / 像素格式转换 | 优先 RGA（`scale_rkrga`） | 无 RGA 或走软解时回退软件缩放 |
| 封面提取 | 优先硬解 + `mjpeg_rkmpp` 硬件 JPEG | 失败时明确记录日志并回退软件 JPEG |
| 直播下载、分段、FLV 修复、换封装 | 不使用硬件 | 与编码无关，维持原逻辑 |

**关键约束：后端不会静默回退到软件编码。** 配置的硬件编码器不可用时会直接报错并保留源文件。

## 二、配置

配置位于 `on_record_finished.rk3588`（同时支持 YAML 与 `POST/PUT /api/config`）：

```yaml
on_record_finished:
  burn_subtitles: true        # 是否烧录弹幕（沿用旧字段）
  burn_subtitles_codec: libx264   # 仅未启用硬件处理时使用
  burn_delete_ass: false      # 初始部署建议 false
  burn_delete_source: false   # 初始部署建议 false
  rk3588:
    enable: false             # 是否启用硬件处理链（默认 false）
    compress: false           # 自动后处理在无 ASS 时是否仍压缩（默认 false）
    codec: hevc_rkmpp         # hevc_rkmpp / h264_rkmpp（默认 hevc_rkmpp）
    bitrate: 4M               # 目标码率，支持 4M / 4000k / 4000000（默认 4M）
    decode_fallback: true     # 硬解不可用时是否允许软件解码（默认 true；编码永不回退软编）
    scale_width: 0            # 缩放宽度，0 表示不缩放；与高度必须同时为 0 或同时为偶数
    scale_height: 0
```

语义要点：

- `enable=false` 时完全沿用旧行为（`burn_subtitles_codec` + CRF/preset），既有部署不受影响。
- `enable=true` 时 `burn_subtitles_codec` 被忽略，统一使用 `rk3588.codec`。
- `compress` 只影响**自动后处理**：关闭时无 `.ass` 的录像原样保留，不做任何转码。
- 房间级/平台级可覆盖 `on_record_finished.rk3588`：按子字段合并，未出现的字段继承上层设置；
  显式传 `rk3588: null` 表示该房间不再覆盖、恢复继承。非法覆盖会被接口拒绝（HTTP 400）。
- **房间首次创建 `on_record_finished` 覆盖时，会以"该房间当前生效的配置"（平台级覆盖或全局配置）
  为基值**：只提交 `rk3588` 的请求不会把 `burn_subtitles`、`cloud_upload` 等未提交字段零值覆盖；
  若回退（`rk3588: null`）后其余字段与基值一致，则整个覆盖被删除，真正恢复继承。
- 初始部署建议 `burn_delete_ass=false`、`burn_delete_source=false`，确认输出无误后再按需开启。

## 三、处理链

自动后处理（录制结束）与手动批量入口共用同一套参数生成逻辑（`src/media`）：

```text
有弹幕：硬解(rkmpp/drm_prime) → RGA 格式转换 → hwdownload → ass 烧录 → 硬编码
无弹幕：硬解(rkmpp/drm_prime) → RGA 缩放(可选) → 硬编码（零拷贝）
硬解失败且 decode_fallback=true：软件解码重试一次（编码仍为硬件编码）
软解或 RGA 不可用：软件缩放 + 弹幕烧录 + 硬编码
未启用硬件：libx264 + CRF/preset（旧行为）
```

- MPP 编码器**不支持** `-crf` / `-preset`，硬件路径统一使用 `-b:v <bitrate>`。
- 音频始终 `-c:a copy`（不二次编码），并保留全部音轨（`-map 0:a?`）。
- 烧录必须回到 CPU 帧，因此会多一次 `hwdownload`；这是为在 CPU 上绘制字幕所必需的开销。
- 弹幕字幕会先复制到临时目录的固定文件名（`subtitles.ass`）再交给滤镜，避免路径中
  逗号/冒号/方括号/单引号等字符触发 FFmpeg 滤镜层转义问题。

## 四、接口

### GET /api/hardware/status

返回硬件处理状态，能力全部来自"当前配置指定的 FFmpeg"的真实探测，`available` 以真实编解码
自检（实际编码若干帧并解码验证）为准，不虚报。`?refresh=1` 强制重新探测并重跑自检。

```json
{
  "enabled": true,
  "ffmpeg_path": "/usr/local/bin/ffmpeg",
  "encoder": "hevc_rkmpp",
  "encoders": ["h264_rkmpp", "hevc_rkmpp", "mjpeg_rkmpp"],
  "decoders": ["h264_rkmpp", "hevc_rkmpp", "vp9_rkmpp"],
  "filters": ["ass", "format", "hwdownload", "scale", "scale_rkrga", "subtitles"],
  "devices": {"/dev/mpp_service": true, "/dev/rga": true, "/dev/dri": true, "/dev/dma_heap": true},
  "warnings": [],
  "available": true,
  "ready": true,
  "compress": true,
  "bitrate": "4M",
  "decode_fallback": true,
  "scale_width": 0,
  "scale_height": 0,
  "ffmpeg_version": "ffmpeg version 7.1",
  "hwaccels": ["drm", "rkmpp"],
  "chain": "硬解(rkmpp/drm_prime) → RGA缩放 → 无弹幕压缩 → 硬件编码(hevc_rkmpp 4M)",
  "self_test": {"ok": true, "codec": "hevc_rkmpp", "steps": [], "message": "硬件编解码自检通过", "cached": true},
  "probed_at": "2026-01-01T00:00:00Z"
}
```

- `encoders` / `decoders` 只列出硬件实现；`filters` 只列出处理链关心且实际存在的滤镜。
- 自检结果单独缓存（默认 10 分钟），普通轮询不会反复执行真实编码。

### 能力探测与自检的缓存语义

- 只有**完整成功**的探测结果（未失败、未因取消中断）才会写入缓存；任何列表探测失败或结果为空
  都会标记 `incomplete`/`err`，并且**不进入缓存**，避免把"探测失败"当成"设备没有硬件编码器"。
- 失败的 `refresh`（含任务取消导致的失败）**不会覆盖**已有的有效缓存，接口继续返回旧的有效结果。
- 取消导致的自检结果同样不写入缓存、不更新节流时间，后续任务可立即重新自检。
- 真实自检受 15 秒最小刷新间隔与 singleflight 保护：并发 `refresh=1` 只会执行一次，避免抢占 MPP 硬件。

### POST /api/pipeline/batch-compress

批量硬件压缩，**无 `.ass` 也处理**。要求 `rk3588.enable=true`，否则返回 400。

```json
{"paths": ["主播A/2026-01-01 20-00-00.mkv", "主播B/xxx.flv"]}
```

响应：

```json
{"enqueued": 1, "skipped": ["xxx.flv - 文件仍在写入（可能正在录制），已拒绝处理"], "task_ids": [12]}
```

批量语义（`batch-burn` 与 `batch-compress` 共用同一构建器）：

- `batch-burn`：默认要求存在同名 `.ass`；若 `rk3588.compress=true`，无 `.ass` 的文件也会被压缩。
- `batch-compress`：强制压缩（忽略 `compress` 字段），但若全局开启了 `burn_subtitles`
  且存在同名 `.ass`，则按"烧录 + 压缩一次完成"处理。
- 路径安全：词法校验 → 解析软链接后再次确认仍在输出目录内 → 必须是常规文件。
- 同一请求内的重复文件按解析后的规范路径去重。
- 正在写入的文件（采样期间体积或修改时间变化，可能正在录制）一律拒绝。

## 五、失败保护与输出规则

- 输出先写同目录唯一临时文件（`os.CreateTemp`），校验通过后用**硬链接**原子发布：
  目标已存在时换用带序号的新文件名（`xxx.1.mkv`），**绝不覆盖现存文件**。
- 校验内容：文件非空、含视频流、时长与源一致（容差 2% 或 1 秒）、音轨未丢失、
  并用 `-xerror -err_detect explode` 真实解码开头 5 秒确认帧可解码。
- 失败或取消：只清理临时文件，**源视频与弹幕字幕全部保留**；删除标记只在整体成功后生效。
- 若源文件在处理期间体积/修改时间发生变化（说明仍在被写入），即使配置了
  `burn_delete_source=true` 也不删除源文件。

## 六、容器与 FFmpeg 要求

- FFmpeg 需 `--enable-rkmpp --enable-rkrga --enable-libdrm --enable-version3`，
  烧录需 `--enable-libass` 与中文字体（fontconfig + CJK 字体）。
- 设备映射：`/dev/mpp_service`、`/dev/rga`、`/dev/dri`（含 `renderD128`）、`/dev/dma_heap`。
- 实测要点（宿主机 FFmpeg 6.1 + MPP/RGA 验证）：
  - `-hwaccel rkmpp -hwaccel_output_format drm_prime` 输出 DRM 帧；
  - RGA 滤镜只接受 DRM 帧，`hwupload` 在部分构建上不可用，因此软件解码路径不使用 RGA；
  - MPP 编码器接受软件帧（`hwdownload` 之后可直接编码）；
  - `-crf` / `-preset` 对 `hevc_rkmpp` 会直接报 `Unrecognized option 'crf'`。

## 七、已知限制

- 字幕文字绘制仍在 CPU 上完成；硬解收益主要体现在解码与编码。
- 10bit（p010）源在烧录链路上会经 RGA 转成 `nv12`（8bit）；纯压缩零拷贝路径保留原始位深。
  真实 10bit 源的处理结果需在容器内实测确认。
- 不保证 H.265 一定比源文件更小：源本身已是低码率 HEVC 时体积可能相近甚至更大，
  建议先用代表性录像对比画质与体积。
- 硬件处理不涉及 NPU；无需开放 NPU 设备。

## 八、测试

```bash
# 局部单元测试（无需 FFmpeg）
go test -tags dev ./src/media/... ./src/configs/... ./src/pipeline/... ./src/servers/...

# 真实硬件链路集成测试（容器内或 RK3588 宿主机）
RK3588_TEST_FFMPEG=/usr/local/bin/ffmpeg \
  go test -tags dev -run Integration -v ./src/pipeline/stages/
```

集成测试覆盖：有弹幕烧录+压缩、无弹幕纯压缩、后续阶段失败保留源与弹幕、取消保留源文件、
已存在输出不被覆盖。缺少 `ass` 滤镜的环境会自动跳过烧录相关用例。
