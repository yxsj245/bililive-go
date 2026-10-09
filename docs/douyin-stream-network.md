# 抖音取流失败：HTTP 重定向到不可达的 IPv6 节点

## 现象与原因

2026-10-09，艾米悠米传媒（直播间 276290357729）的取流连续失败。应用提示“流探测代理启动失败，将直接连接上游”，随后 FFmpeg 同样报 `Network is unreachable`，并非仅探测信息缺失。

抖音工具返回 `http://pull-flv-l1.douyincdn.com/...`。该 HTTP 地址返回 302，目标是 IPv6 字面地址；生产 Docker bridge 只有 IPv4 地址及网关，不能访问该节点。单纯调整 DNS 优先级或强制首次连接 IPv4 无法避免上游返回 IPv6 的重定向。

## 修复范围

在抖音 `GetStreamInfos` 获取直播流后，优先将已知 `douyincdn.com` 及其子域的默认 HTTP 地址转换为 HTTPS。路径及原始查询字符串保持不变，不重新编码签名参数。

- 原本为 HTTPS：保持不变。
- 其他 CDN、IP 字面地址、自定义非 80 端口：保持不变。
- 不改系统 IPv6、Docker daemon、宿主网络、其他服务或编码参数。
- 不降级 TLS 校验，也不把 IPv6 地址随意改成猜测的 IPv4 地址。

同一条签名流在容器内测试：HTTP 返回 302 到 IPv6；HTTPS 返回 `200 OK` 与 `video/x-flv`。

## 验证与排障

局部测试：`go test ./src/live/douyin`。用项目 Make 入口编译，发布版内嵌原有网页，使用新镜像标签部署。

部署后检查房间 `recording: true`、流分辨率/编码信息，以及录像文件持续增长；不能只以 HTTP 200 或容器健康状态判断录像成功。

日志保存在板载 `appdata/logs/`。未来出现同类问题时先核查重定向链、容器路由及同 URL 的 HTTPS 可用性。HTTPS 优先不是任意 CDN 故障的通用保证；签名过期、上游停播、网络中断仍需要分别诊断。

## 本次部署结果

- 版本：`rk3588-20261009-https`。
- 发布镜像：`bililive-go-rk3588:20261009-https`，部署固定 ID `sha256:3b9ac87c7ca10ff7e27f579184cb21eed404f3772d6d1024f54ba701c54b5322`。
- 局部测试、`make build-web dev`、`make build`、`GOTOOLCHAIN=go1.26.1 make lint`（0 issues）及 `git diff --check` 均通过；未运行全量测试。
- 本机独立 `bililive-rk3588-media:20261009` 标签已不存在，默认构建脚本在 Docker 打包阶段因此失败。本次继承现存 `bililive-go-rk3588:20261009`，仅 COPY 重新编译的应用二进制，保留已验证的全部多媒体环境。旧应用镜像未删除；后续重建需先确认可用基础镜像，不能假定旧独立标签仍在。
- 18:53:31 流探测成功：H.264、1080×1920、30 fps；ffprobe 确认视频 H.264、音频 AAC。
- 直播间 `recording: true`，20 秒观察录像从 17,301,504 字节增长至 39,845,888 字节。沿用用户已启用的直播间监听，没有新建测试直播间或停止用户的录制。
- 容器健康，CPU 亲和性为空；H.265 后处理仍是 4M、保持源分辨率，未改宿主网络、USB 守护和其他服务。

