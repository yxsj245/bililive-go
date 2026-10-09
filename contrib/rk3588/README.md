# bililive-go RK3588 部署交付

本目录是正式部署与交付目录：`/opt/DockerDate/bililive-go`。运行仅依赖 Docker 镜像、本目录配置和原录像外盘，**不依赖开发目录**。

## 入口

- 网页：[bililive-go 管理页面](http://192.168.10.62:8084)
- [网页压缩设置说明](docs/rk3588-compress-webui.md)
- [部署、硬件边界与还原说明](docs/rk3588-deployment.md)
- [后端配置与 API](docs/rk3588-hardware-processing.md)
- [验证记录](docs/rk3588-validation.md)
- [API 说明](docs/API.md)

## 当前默认行为

录制时保存原直播流；录制结束后使用 MPP 硬解、可选 CPU/libass 绘制中文弹幕、MPP H.265 编码。

- 视频编码器：`hevc_rkmpp`（网页可改 H.264）；目标码率 4M。
- 有弹幕：烧录与压缩一次完成；无弹幕：独立压缩。
- 不缩放，保留音轨；需要缩放时优先 RGA。
- 保留原录像与 ASS，失败/取消也保留；不会批量处理历史录像。
- 后处理并发 1，**不绑定 CPU 亲和性**。
- 应用自更新与自动下载关闭；升级专用版本需要重新编译镜像。
- 原录像目录保持 `/media/ztl/LinuxDate/files/直播录制` → `/srv/bililive`。
- 不修改 USB 守护；沿用已有 Docker 外盘自动识别。

**4M 是起始目标码率，不保证所有源流都变小。** 部分浏览器无法直接播放 H.265/MKV；下载用支持 HEVC 的播放器，或设置 H.264。

## 服务管理

```bash
cd /opt/DockerDate/bililive-go
bash manage.sh status
bash manage.sh start
bash manage.sh logs 100
bash manage.sh restart
```

若需长时间维护停止，已有外盘自动恢复机制可能重新拉起容器，应按原有维护方式暂停该容器；本次不更改该机制。

## 交付目录

| 项目 | 用途 |
| --- | --- |
| [Compose](docker-compose.yml) | 固定镜像、设备节点、局域网端口、外盘绑定 |
| [应用配置](config.yml) | 原直播间与硬件参数；请保管，可能包含凭据 |
| [镜像与环境参数](.env) | 锁定镜像 ID 与访问地址 |
| [管理脚本](manage.sh) | 从本目录启动/停止/诊断服务 |
| [说明文档](docs/) | 使用与还原指导 |
| [验证证据](verification/) | 测试报告、命令日志、中文弹幕截图 |
| [备份归档](backups/) | 原配置备份和完整可还原交付包 |

还原包内带已验证镜像、配置、源码快照/补丁及 SHA256 清单。优先校验后 `docker load`，不必从网络重新拉取不确定的版本。归档里的 `restore.sh` 默认不覆盖已有配置，也不会自动启动服务，避免误写外盘。
