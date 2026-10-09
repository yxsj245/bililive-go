# RK3588 应用数据与录像分离

## 当前布局

| 宿主路径 | 容器路径 | 内容 |
| --- | --- | --- |
| `/opt/DockerDate/bililive-go/appdata` | `/var/lib/bililive-go` | 板载存储上的新数据库、必要工具和运行日志 |
| `/media/ztl/LinuxDate/files/直播录制` | `/srv/bililive` | 只保存之后新录制的视频、弹幕及其后处理输出 |
| `/opt/DockerDate/bililive-go/config.yml` | `/etc/bililive-go/config.yml` | 保留的 26 个直播间、硬件参数和运行设置 |

Compose 的 `BILILIVE_APPDATA_DIR` 默认 `./appdata`（相对部署目录），可改成其他板载目录。禁止重新设到录像外盘。所有 bind 使用 `create_host_path: false`；启动前必须创建 appdata/logs，属主与容器 UID/GID 一致，本机为 1000:1000。

```yaml
app_data_path: /var/lib/bililive-go
log:
  out_put_folder: /var/lib/bililive-go/logs
```

仅改 Compose 挂载后，必须 `docker compose up -d` 重建容器；`docker compose restart` 不会应用新挂载。应用和 FFmpeg 镜像没有变化，硬件权限与无 CPU 亲和性策略保留；生产不依赖开发目录。

## 为什么空闲仍有统计数据

应用原有运行统计默认开启，每 5 秒保存磁盘 I/O、内存等历史数据，直播间全部关闭时也会保存。此前 app_data_path 默认位于外盘，导致外盘约 22～25 KiB/s 的持续小写入。此次不是关闭统计功能，而是让其写到板载存储，不再干扰外盘空闲。新的必要数据库和日志仍会生成，不代表恢复了已删除历史数据。

## 用户确认的清理

2026-10-09 用户确认不保留旧录像、历史应用数据、测试素材或备份，仅保留直播间配置及必要运行软件。实际正式录像目录原本没有录像，只有 .appdata 和日志。

已删除：

- 外盘旧 .appdata（数据库、工具缓存及旧工具数据）和应用日志。
- 部署 verification（含授权短录测试视频、ASS、处理输出、截图与报告）。
- 部署 backups（含首次备份和两份交付归档及解压副本）。
- 开发构建区 smoke、api-smoke 的合成/短录测试素材与调试数据。

仅将旧工具目录中的程序文件复制到板载存储，排除 *.db、*.db-*、*.log 和旧 appConfig.json，避免无意义重新下载依赖。抖音工具重新生成最小服务配置（本地监听、鉴权、关闭工具自更新），没有沿用旧账号、任务或录制数据。主应用数据库全部重新初始化；26 个房间从 config.yml 恢复，保持全部不监听。源码、镜像、运行脚本、文档和硬件设置保留。未改其他服务、USB 守护、内核或宿主 FFmpeg。

删除不可恢复。旧还原归档不再存在，README 或旧验证报告中出现的历史归档不能再使用。后续只有用户主动需要时才创建备份；不要自动把清理的数据重新备份。

## 验证结果

- 容器 healthy；26 房间、0 监听、0 录制，所有任务状态计数为 0。
- 抖音工具本地 HTTP 200；调度工具运行；专用 FFmpeg 存在。
- 新数据库、工具进程路径及日志位于 /var/lib/bililive-go；宿主对应板载 eMMC。
- 18:37:22～18:38:22 对 /sys/block/sda/stat 连续六个 10 秒区间采样，每个区间 read_ios/write_ios/read_KiB/write_KiB 均为 0。
- 外盘录像目录为空；没有新增测试录制。

上述证明此次空闲采样没有主机块设备读写，不能保证硬盘已经物理停转或以后永远为零。其他服务、访问录像列表/下载文件或新录制仍可唤醒硬盘；本次不设置硬盘待机、不拔盘测试。

## 日常管理与重新部署

```bash
cd /opt/DockerDate/bililive-go
install -d -o 1000 -g 1000 -m 0755 appdata appdata/logs
docker compose config -q
docker compose up -d
bash manage.sh status
```

新机器仍需正确镜像、外盘挂载、设备映射和局域网地址。管理脚本 start 使用 up -d，会按修改后的 Compose 重建。只保留 config.yml 即可保留直播间配置；重置 appdata 会丢失运行历史，必须停止服务且确认不在录制后才处理。不要删除当前配置或必要程序来清空历史。
