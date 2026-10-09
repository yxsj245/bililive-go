// 更新硬件处理与专用容器存储路径，保留直播间、录像目录和其他设置。
const fs = require('fs');
const path = require('path');
const yaml = require('../../src/webapp/node_modules/js-yaml');
const file = process.argv[2];
if (!file) throw new Error('请传入现有配置文件路径。');
const resolved = path.resolve(file);
const original = fs.readFileSync(resolved, 'utf8');
const config = yaml.load(original);
config.ffmpeg_path = '/opt/rk3588/bin/ffmpeg';
// 配合 Compose 板载 appdata 挂载；不复制旧数据库，避免空闲统计写外盘。
config.app_data_path = '/var/lib/bililive-go';
config.log = { ...config.log, out_put_folder: '/var/lib/bililive-go/logs' };
config.on_record_finished = {
  ...config.on_record_finished,
  // 旧 FLV 修复会原地替换源文件，安全验证默认关闭；需要时手动开启。
  fix_flv_at_first: false,
  burn_subtitles: true,
  burn_subtitles_codec: 'hevc_rkmpp',
  burn_delete_ass: false,
  burn_delete_source: false,
  delete_flv_after_convert: false,
  rk3588: {
    enable: true,
    compress: true,
    codec: 'hevc_rkmpp',
    bitrate: '4M',
    decode_fallback: true,
    scale_width: 0,
    scale_height: 0,
  },
};
config.danmaku = { ...config.danmaku, font_name: 'Noto Sans CJK SC' };
config.task_queue = { ...config.task_queue, max_concurrent: 1 };
config.update = { ...config.update, auto_check: false, auto_download: false };
const temporary = resolved + '.rk3588.tmp';
fs.writeFileSync(temporary, yaml.dump(config, { lineWidth: -1, noRefs: true }), { mode: 0o600, flag: 'wx' });
fs.renameSync(temporary, resolved);
console.log('已配置 RK3588 硬件处理；源录像/ASS保留，并发1，自动更新关闭；直播间监听状态保持原样。');
