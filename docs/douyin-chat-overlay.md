# 抖音风格聊天列表 ASS 弹幕（layout: chat）

2026-10-11 新增。默认行为完全不变：未配置 `layout` 时仍是原来的横向滚动弹幕。

## 1. 这是做什么的

把弹幕 ASS 从“全屏横向滚动”换成抖音直播间的样式：**左下角竖排聊天列表**，最新一条在最底部，旧消息逐条上移，最多同时显示 5 行；每条消息一个半透明深色圆角气泡，昵称青蓝色、正文白色，礼物/醒目留言/上舰使用金色文字条。

## 2. 开启方式

全局配置（`config.yml`）：

```yaml
danmaku_enable: true
danmaku:
  layout: chat            # 不写或写 scroll 即为旧的横向滚动
  font_size: 36
  font_name: Noto Sans CJK SC
  resolution: 1080x1920   # 支持竖屏；旧的四个预设分辨率同样合法
  opacity: 128            # 气泡透明度（0~255，越大越不透明）
  chat:
    rows: 5               # 左下角同时显示行数（1~20）
    width_percent: 72     # 单条气泡最大宽度占画面宽度百分比（20~100）
    bottom_percent: 8     # 底部留白占画面高度百分比（0~40）
    stay_seconds: 15      # 每条消息停留秒数（1~120）
    max_lines_per_item: 2 # 单条消息最多行数（含昵称行，1~3）
```

只对单个直播间生效（`live_rooms` 内的房间覆盖）：

```yaml
live_rooms:
  - url: https://live.douyin.com/xxxx
    danmaku_enable: true
    danmaku:
      layout: chat
      chat:
        rows: 4
        width_percent: 66
```

- 房间级与全局都按**字段**合并：只写 `rows` 时其它参数继承全局。
- `bottom_percent` 是可选指针字段：**不写表示继承上层配置，显式写 `0` 表示不留底部空白**。
- 切回 `layout: scroll` 不会删除已配置的 `chat` 参数，方便来回切换。

通过配置 API 也可以改（字段名相同，`chat` 为对象）：

```bash
curl -X PATCH http://<host>:<端口>/api/config \
  -H 'Content-Type: application/json' \
  -d '{"danmaku":{"layout":"chat","chat":{"rows":5,"width_percent":72,"bottom_percent":8,"stay_seconds":15}}}'
```

房间级覆盖使用同一个接口的房间配置更新入口，JSON 结构一致。本机部署实际监听端口为 **8084**（示例：`http://192.168.10.62:8084/api/config`）。

### 竖屏建议

实测 1088×1920 的竖屏直录画面，`font_size: 36` 偏小，建议：

```yaml
danmaku:
  layout: chat
  font_size: 48        # 竖屏推荐；横屏仍建议 36
  resolution: 1080x1920
```

`font_size` 的默认值仍是旧的 36（保持旧配置行为不变），竖屏请显式调大。字号变大后气泡与行距按比例放大，`rows` 与 `bottom_percent` 无需改动。

## 3. 显示规则（重要）

- **行数与停留时间同时生效**：一条消息最多显示 `stay_seconds` 秒（**硬上限，不延长**），同时最多显示 `rows` 条。
- **弹幕不排队、不延迟**：消息到达即显示，不会为了“让上一条显示满 15 秒”而推迟后面的消息。
- 因此**密集弹幕下**（例如 5 秒内超过 5 条），旧消息会被新消息**挤出可见区域**，实际可见时间短于 `stay_seconds`；弹幕稀疏时才是完整显示 15 秒。这是预期行为。
- 时间同步以录制开始时刻为基准，消息在它真实到达的时间点出现。
- 同一时刻的突发消息不会互相延迟，也不会产生无限的事件输出。
- 时间回拨（系统校时、消息乱序）会被夹取到已写出位置，不会产生负长度事件。

### 内容与样式

- 昵称与正文**同一行**（抖音风格），超长时才换行；整条最多 `max_lines_per_item` 行（默认 2 行，含昵称），放不下的内容以 `…` 结尾。
- 昵称最多占气泡可写宽度的 **50%**，超出会被截断（`…`），以保证正文始终有约 4 个字的显示空间；`max_lines_per_item: 1` 时昵称同样会被截断，确保整条只有一行。
- 文字宽度按字号估算（中文按 1 个字宽、ASCII 按 0.5 个字宽），实际字体略有差异时表现为换行位置或省略号位置小幅变化。
- 单条消息最多渲染 256 个字，昵称最多 32 个字，保证内存与 ASS 文件规模有界。
- 气泡宽度按**实际文字宽度**，上限为 `width_percent`；气泡高度按真实行数计算，因此不同行数的消息不会互相重叠。
- 礼物、醒目留言、上舰为金色单行文字条（`昵称 赠送 礼物 xN (¥金额)`），不做头像、等级、礼物图标与动效。
- 用户输入统一转义：`\`、`{`、`}`、换行都不会被解释为 ASS 控制码，无法注入样式或绘图指令。

## 4. ASS 实现要点（便于排查）

| 项目 | 说明 |
| --- | --- |
| 画布 | `PlayResX/PlayResY` 取 `danmaku.resolution`，支持横屏与竖屏 |
| 气泡 | Layer 1，`{\an7\pos(x,y)\p1\bord0\shad0\1c&H00000000&\1a&Hxx&}` + 圆角矩形绘制指令 |
| 文字 | Layer 2，`{\an7\pos(x,y)\q2}`，逐段 `{\c...}` 上色；**不使用任何动效指令**（避免高频弹幕闪烁） |
| 透明度 | 只通过 `\1a` 设置；`\1c` 里的 alpha 会被 libass 忽略 |
| 事件生成 | 只追加、不重写历史：新消息到来时把上一段时间内可见的条目按各自位置写成事件段 |

## 5. 离线出样片（不需要真的开直播）

`tools/chat-preview` 可以把弹幕原始消息 JSONL 渲染成 chat 版 ASS，完全离线：

```bash
go run ./tools/chat-preview \
  -in messages.jsonl -out chat.ass -width 1088 -height 1920 -v
```

JSONL 每行一条消息：

```json
{"time_ms":1234,"username":"昵称","text":"内容","gift_name":"","num":0,"price":0,"coin_type":"gold","type":"danmaku"}
```

- `type` 可选 `danmaku`（默认）/ `gift` / `guard` / `super_chat`（`sc` 亦可）。
- `time_ms` 为相对录制开始的毫秒数；`-width/-height` 按真实视频分辨率填写。
- 其它可选参数：`-rows`、`-width-percent`、`-bottom-percent`、`-stay-seconds`、`-max-lines`、`-font-name`、`-limit`、`-config`。

渲染单帧检查样式：

```bash
# 把样片放到固定简易路径，避免路径中的 ':' 需要转义
cp chat.ass /tmp/preview/chat.ass
docker run --rm --entrypoint sh -v /tmp/preview:/data <媒体镜像> -c \
  'ffmpeg -f lavfi -i color=c=0x1E1E28:s=1088x1920:d=20:r=1 \
     -vf "ass=/data/chat.ass" -ss 10 -frames:v 1 -y /data/frame.png'
```

滤镜参数里的 `:` 是分隔符，路径中若含 `:`（如 Windows 盘符、带冒号的目录名）需要转义，因此建议用不带冒号的固定简易路径。

## 6. 把已有录像的弹幕转成 chat 版

ASS 里只有 `Dialogue` 行和已经被布局过的文字，**无法可靠还原**“谁在什么时间说了什么”，因此不建议从旧 ASS 反推。推荐两种做法：

**原始消息需要预先采集**：弹幕录制器当前只产出 ASS 文件与计数/SSE 推送，并不保留可供导出的原始消息库；因此要生成 chat 版 ASS，必须先把“谁在什么时间说了什么”采集为第 5 节的 JSONL（例如自行抓取或导出弹幕日志）。

1. **用原始消息 JSONL（推荐）**：把采集到的 JSONL 交给 `tools/chat-preview` 生成 chat 版 ASS，再用 ffmpeg 烧录。
2. **从旧 ASS 拆分（应急，精度有限）**：旧 ASS 的 `Dialogue` 行只保留了已经布局过的文字，可以按旧的 `昵称: 正文` 约定用脚本拆成 `{time_ms, username, text}`。这样得到的时间是**旧 ASS 上的显示时间**，不等于消息原始接收时间（横滚布局还会因为防重叠而整体推迟）。本次实测 579 条旧 ASS 用这种方式对时可用，但只适合临时出样片。
3. 若以后需要批量转换历史录像，建议新增独立导出能力（例如把原始消息写入 JSONL 的导出子命令），再交给 `chat-preview`，属新功能，本次未实现。

## 7. 字体与部署注意

- 字体名要与运行环境实际安装的字体一致：容器内为 **Noto Sans CJK SC**，Windows 开发机可用 Microsoft YaHei，字体缺失时 libass 会回退，位置估算可能偏窄。
- 聊天气泡宽度按字号估算文字宽度，字体差异较大时以实际渲染图为准，可微调 `width_percent`。
- 新的 `layout`/`chat` 字段带 `omitempty`，并且默认值处理**不会**把空 `layout` 改写成 `scroll`：因此**未配置 `layout` 的既有配置文件回写时不会新增该字段**（已用序列化测试验证）。显式写过 `layout: scroll` 的配置会原样保留；未配置时行为与 scroll 完全一致。
- 生产部署目录的配置、容器与镜像不在本次改动范围内。
