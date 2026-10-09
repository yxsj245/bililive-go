# Bililive-go API

## `GET /api/info` Get app info
- Request:
    ```text
    method: GET
    path: http://127.0.0.1:8080/api/info
    ```
- Response:
    ```json
    {
      "app_name": "BiliLive-go",
      "app_version": "0.5.0-rc.3-3-g31ceeda",
      "build_time": "2020-05-05_01:07:16",
      "git_hash": "31ceeda8f508ba5546cfdefef5f3945828a87651",
      "pid": 33295,
      "platform": "darwin/amd64",
      "go_version": "go1.14.2"
    }
    ```
        
## `GET /api/lives` Get all live info 
- Request:  
    ```text
    method: GET
    path: http://127.0.0.1:8080/api/lives
    ```
- Response:   
    ```json
    [
      {
        "id": "212d9c98c7b376b730d4336bb49f6d3f",
        "live_url": "https://live.bilibili.com/14917277",
        "platform_cn_name": "哔哩哔哩",
        "host_name": "湊-阿库娅Official",
        "room_name": "【B站限定】棉花糖＆唱歌！！！！",
        "status": false,
        "listening": true,
        "recording": false
      },
      {
        "id": "63dc965c77d3d81058c92c3e38822256",
        "live_url": "https://live.bilibili.com/11588230",
        "platform_cn_name": "哔哩哔哩",
        "host_name": "白上吹雪Official",
        "room_name": "古老niconico老人会with☆乐园",
        "status": false,
        "listening": true,
        "recording": false
      },
      {
        "id": "dfb964a56725bbad165cb9ea1ef8ac5b",
        "live_url": "https://live.bilibili.com/1030",
        "platform_cn_name": "哔哩哔哩",
        "host_name": "怕上火暴王老菊",
        "room_name": "直播做饭",
        "status": false,
        "listening": true,
        "recording": false
      }
    ]
    ```
        
## `GET /api/lives/{id}` Get live info by id
- Request:  
    ```text
    method: GET
    path: http://127.0.0.1:8080/api/lives/212d9c98c7b376b730d4336bb49f6d3f
    ```
- Response:
    ```json
    {
      "id": "212d9c98c7b376b730d4336bb49f6d3f",
      "live_url": "https://live.bilibili.com/14917277",
      "platform_cn_name": "哔哩哔哩",
      "host_name": "湊-阿库娅Official",
      "room_name": "【B站限定】棉花糖＆唱歌！！！！",
      "status": false,
      "listening": true,
      "recording": false
    }
    ```
        
## `POST /api/lives` Add live
- Request:  
    ```text
    method: POST
    path: http://127.0.0.1:8080/api/lives
    body: 
        [
            {
                "url": "https://live.bilibili.com/14917277",
                "listen": true
            }
        ]
    ```
- Response:
    ```json
    [
        {
            "id": "212d9c98c7b376b730d4336bb49f6d3f",
            "live_url": "https://live.bilibili.com/14917277",
            "platform_cn_name": "哔哩哔哩",
            "host_name": "湊-阿库娅Official",
            "room_name": "【B站限定】棉花糖＆唱歌！！！！",
            "status": false,
            "listening": true,
            "recording": false
        }
    ]
    ```        
        
## `DELETE /api/lives/{id}` Delete live by id
- Request:  
    ```text
    method: DELETE
    path: http://127.0.0.1:8080/api/lives/212d9c98c7b376b730d4336bb49f6d3f
    ```
- Response:
    ```json
    {
        "err_no": 0,
        "err_msg": "",
        "data": "OK"
    }
    ```

## `GET /api/lives/{id}/start` Start listen live by id
- Request:  
    ```text
    method: GET
    path: http://127.0.0.1:8080/api/lives/212d9c98c7b376b730d4336bb49f6d3f/start
    ```
- Response:
    ```json
    {
        "id": "212d9c98c7b376b730d4336bb49f6d3f",
        "live_url": "https://live.bilibili.com/14917277",
        "platform_cn_name": "哔哩哔哩",
        "host_name": "湊-阿库娅Official",
        "room_name": "【B站限定】棉花糖＆唱歌！！！！",
        "status": false,
        "listening": true,
        "recording": false
    }
    ```
        
## `GET /api/lives/{id}/stop` Stop listen and record live by id
- Request:  
    ```text
    method: GET
    path: http://127.0.0.1:8080/api/lives/212d9c98c7b376b730d4336bb49f6d3f/stop
    ```
- Response:
    ```json
    {
        "id": "212d9c98c7b376b730d4336bb49f6d3f",
        "live_url": "https://live.bilibili.com/14917277",
        "platform_cn_name": "哔哩哔哩",
        "host_name": "湊-阿库娅Official",
        "room_name": "【B站限定】棉花糖＆唱歌！！！！",
        "status": false,
        "listening": false,
        "recording": false
    }
    ```
        
## `GET /api/config` Get config info
- Request:  
    ```text
    method: GET
    path: http://127.0.0.1:8080/api/config
    ```
- Response:
    ```json
    {
      "RPC": {
        "Enable": true,
        "Bind": "127.0.0.1:8080"
      },
      "Debug": false,
      "Interval": 15,
      "OutPutPath": "/tmp",
      "Feature": {
        "UseNativeFlvParser": false
      },
      "LiveRooms": null
    }
    ```
        
## `PUT /api/config` Save lives info to config file
- Request:  
    ```text
    method: PUT
    path: http://127.0.0.1:8080/api/config
    ```
- Response:
    ```json
    {
        "err_no": 0,
        "err_msg": "",
        "data": "OK"
    }
    ```

## `GET /api/raw-config` Get raw config file
- Request:
    ```text
    method: GET
    path: http://127.0.0.1:8080/api/raw-config
    ```
- Response:
    ```json
    {
        "config": "rpc:\n  enable: true\n  bind: 0.0.0.0:8080\ndebug: false\ninterval: 15\nout_put_path: ./\nfeature:\n  use_native_flv_parser: false\nlive_rooms:\n- url: https://www.huya.com/991111\n  is_listening: false\nout_put_tmpl: \"\"\nvideo_split_strategies:\n  on_room_name_changed: false\n  max_duration: 0s\ncookies:\n  live.douyin.com: name1=qwer;name2=asdf;aaaa\non_record_finished:\n  convert_to_mp4: true\n  delete_flv_after_convert: false\ntimeout_in_us: 50000000\n"
    }
    ```

## `PUT /api/raw-config` Save the whole config file
- Request:
    ```text
    method: PUT
    path: http://127.0.0.1:8080/api/raw-config
    body:
        {
            "config": "rpc:\n  enable: true\n  bind: 0.0.0.0:8080\ndebug: false\ninterval: 15\nout_put_path: ./\nfeature:\n  use_native_flv_parser: false\nlive_rooms:\n- url: https://www.huya.com/991111\n  is_listening: false\nout_put_tmpl: \"\"\nvideo_split_strategies:\n  on_room_name_changed: false\n  max_duration: 0s\ncookies:\n  live.douyin.com: name1=qwer;name2=asdf;aaaa\non_record_finished:\n  convert_to_mp4: true\n  delete_flv_after_convert: false\ntimeout_in_us: 50000000\n"
        }
    ```
- Response:
    ```json
    {
        "err_no": 0,
        "err_msg": "",
        "data": "OK"
    }
    ```
## `GET /api/hardware/status` Get hardware processing status (RK3588)

返回 RK3588（MPP/RGA）硬件处理能力状态。所有能力字段均来自"当前配置指定的 FFmpeg"的真实探测，
`available` 以真实编解码自检（实际编码若干帧再解码验证）为准，不做能力假设。
普通调用读取缓存；`?refresh=1` 强制重新探测并重跑自检。

- Request:
    ```text
    method: GET
    path: http://127.0.0.1:8080/api/hardware/status
    ```
- Response:
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

- 字段说明：`encoders`/`decoders` 只列硬件实现；`filters` 只列处理链关心且实际存在的滤镜；
  `ready = enabled && available`；`warnings` 用中文说明能力缺口（缺编码器、缺 `ass` 滤镜、
  缺设备节点等）。详见 [docs/rk3588-hardware-processing.md](rk3588-hardware-processing.md)。

## `POST /api/pipeline/batch-compress` Batch hardware compression

批量提交硬件压缩任务，**没有同名 `.ass` 弹幕也会处理**。要求 `on_record_finished.rk3588.enable=true`，
否则返回 400（避免退化为软件编码）。若全局开启了 `on_record_finished.burn_subtitles` 且存在同名
`.ass`，则按"烧录 + 压缩一次完成"处理。

- Request:
    ```text
    method: POST
    path: http://127.0.0.1:8080/api/pipeline/batch-compress
    body:
        {
            "paths": [
                "主播A/2026-01-01 20-00-00.mkv",
                "主播B/2026-01-01 21-00-00.flv"
            ]
        }
    ```
- Response:
    ```json
    {
        "enqueued": 1,
        "skipped": ["主播B/2026-01-01 21-00-00.flv - 文件仍在写入（可能正在录制），已拒绝处理"],
        "task_ids": [12]
    }
    ```

- 安全与去重：路径经词法校验与软链接解析后必须仍位于输出目录内，且必须是常规文件；
  同一请求内的重复文件按规范路径去重；正在写入的文件（体积/修改时间仍在变化）一律拒绝。
- `POST /api/pipeline/batch-burn` 的行为与之共用同一构建器，区别是默认要求存在同名 `.ass`；
  当 `rk3588.compress=true` 时，无 `.ass` 的文件也会被压缩。

## 硬件处理配置（`on_record_finished.rk3588`）

`GET /api/config`、`PUT /api/config`、房间/平台级配置接口均支持 `rk3588` 子对象：

```json
{
  "on_record_finished": {
    "burn_subtitles": true,
    "burn_delete_ass": false,
    "burn_delete_source": false,
    "rk3588": {
      "enable": true,
      "compress": true,
      "codec": "hevc_rkmpp",
      "bitrate": "4M",
      "decode_fallback": true,
      "scale_width": 0,
      "scale_height": 0
    }
  }
}
```

- 按子字段合并：未出现的字段保持原值；房间/平台级显式传 `"rk3588": null` 表示恢复继承全局设置。
- 房间/平台级传非法值（如 `codec: libx264`、奇数缩放尺寸）会返回 400，且不会写入一半配置。
