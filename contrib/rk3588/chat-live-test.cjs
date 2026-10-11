// 用户授权后使用独立目录和独立应用实例短录，不修改生产配置或录像。
const fs = require('fs');
const path = require('path');
const assert = require('assert/strict');
const { spawnSync } = require('child_process');
const yaml = require('../../src/webapp/node_modules/js-yaml');
const net = require('net');
const sourceConfig = process.env.CHAT_SOURCE_CONFIG;
const sourceTools = process.env.CHAT_SOURCE_TOOLS;
const image = process.env.CHAT_TEST_IMAGE;
const roomURL = process.env.CHAT_TEST_ROOM_URL;
const base = path.resolve(process.env.CHAT_TEST_DIR || '');
const duration = Number(process.env.CHAT_TEST_SECONDS || 100);
assert(sourceConfig && sourceTools && image && roomURL, '必须指定测试镜像、直播间及配置和工具来源');
assert(process.env.CHAT_TEST_DIR && base.startsWith('/opt/Code/rk3588-media-build/chat-'), '测试目录必须位于隔离 chat-* 目录');
assert(Number.isInteger(duration) && duration >= 20 && duration <= 180, '短录时长必须在20至180秒之间');
const room = new URL(roomURL);
assert(room.protocol === 'https:' && ['live.douyin.com', 'v.douyin.com'].includes(room.hostname), '仅接受抖音直播链接');
const name = 'bililive-chat-' + Date.now();
const origin = 'http://127.0.0.1:18084';
function run(command, args) {
  const result = spawnSync(command, args, { stdio: 'inherit' });
  assert.equal(result.status, 0, command + '执行失败');
}
async function api(route, method = 'GET') {
  const response = await fetch(origin + '/api/' + route, { method, signal: AbortSignal.timeout(10000) });
  const body = await response.text();
  assert(response.ok, route + '返回' + response.status + ':' + body);
  return JSON.parse(body);
}
const pause = (ms) => new Promise(resolve => setTimeout(resolve, ms));
(async () => {
  // 拒绝访问已存在的测试实例，避免错误停止其他直播间。
  await new Promise((resolve, reject) => {
    const socket = net.createConnection({ host: '127.0.0.1', port: 18084 });
    socket.setTimeout(1000);
    socket.once('connect', () => { socket.destroy(); reject(new Error('测试端口18084已占用')); });
    socket.once('error', error => { socket.destroy(); error.code === 'ECONNREFUSED' ? resolve() : reject(error); });
    socket.once('timeout', () => { socket.destroy(); reject(new Error('测试端口检查超时')); });
  });
  // 独占创建目录，拒绝覆盖上一次测试数据。
  fs.mkdirSync(base);
  for (const dir of ['conf', 'out', 'appdata']) fs.mkdirSync(path.join(base, dir));
  fs.mkdirSync(path.join(base, 'out/logs'));
  fs.cpSync(sourceTools, path.join(base, 'appdata/external_tools'), { recursive: true });
  const config = yaml.load(fs.readFileSync(sourceConfig, 'utf8'));
  config.rpc = { enable: true, bind: '127.0.0.1:18084' };
  config.out_put_path = '/srv/bililive';
  config.app_data_path = '/var/lib/bililive-go';
  config.ffmpeg_path = '/opt/rk3588/bin/ffmpeg';
  config.live_rooms = [{ url: roomURL, is_listening: true, danmaku_enable: true }];
  config.platform_configs = {};
  config.interval = 5;
  config.out_put_tmpl = '';
  config.video_split_strategies = { on_room_name_changed: false, max_duration: '0s', max_file_size: '0' };
  config.on_record_finished = {
    convert_to_mp4: false, delete_flv_after_convert: false, custom_commandline: '',
    fix_flv_at_first: false, save_cover: false, burn_subtitles: false,
    burn_delete_ass: false, burn_delete_source: false, rk3588: { enable: false },
    cloud_upload: { enable: false },
  };
  config.danmaku_enable = true;
  config.danmaku = { font_size: 36, font_name: 'Noto Sans CJK SC', scroll_area: 'full', scroll_time: 10, resolution: '1920x1080', outline: 1, opacity: 128, record_douyin_gift: false };
  config.log = { out_put_folder: '/srv/bililive/logs', save_last_log: true, save_every_log: false, rotate_days: 1 };
  config.update = { auto_check: false, auto_download: false, check_interval_hours: 6, include_prerelease: false };
  config.notify = {};
  config.openlist = {};
  config.douyu_auth = {};
  config.sooplive_auth = {};
  config.cookies = {};
  config.proxy = { enable: false, url: '' };
  config.task_queue = { max_concurrent: 1 };
  config.read_only_tool_folder = '/opt/bililive/tools';
  config.tool_root_folder = '';
  fs.writeFileSync(path.join(base, 'conf/config.yml'), yaml.dump(config, { lineWidth: -1 }), { mode: 0o600 });
  run('chown', ['-R', '1000:1000', base]);
  let started = false;
  for (const signal of ['SIGINT', 'SIGTERM']) process.once(signal, () => {
    if (started) spawnSync('docker', ['stop', name], { stdio: 'inherit', timeout: 60000 });
    process.exit(1);
  });
  const summary = { source_url: roomURL, directory: base, container: name, seconds_requested: duration };
  try {
    // 此测试源末跳为 IPv6-only，使用宿主网络；RPC仅监听环回地址且不设CPU亲和性。
    run('docker', ['run', '-d', '--name', name, '--stop-timeout', '45', '--network', 'host', '--restart', 'no',
      '--entrypoint', '/usr/bin/timeout', '--user', '1000:1000', '--security-opt', 'no-new-privileges:true', '--cap-drop', 'ALL',
      '-e', 'TZ=Asia/Shanghai', '-e', 'IS_DOCKER=true', '-e', 'BILILIVE_DISABLE_SELF_UPDATE=1',
      '--mount', 'type=bind,src=' + base + '/conf,dst=/etc/bililive-go',
      '--mount', 'type=bind,src=' + base + '/out,dst=/srv/bililive',
      '--mount', 'type=bind,src=' + base + '/appdata,dst=/var/lib/bililive-go',
      '--tmpfs', '/opt/bililive/tmp_for_exec:exec,nosuid,nodev,mode=0700,uid=1000,gid=1000,size=512m', image,
      '--signal=TERM', '--kill-after=45s', '330s', '/usr/local/bin/rk3588-entrypoint']);
    started = true;
    const deadline = Date.now() + 120000;
    let live;
    while (Date.now() < deadline) {
      try { live = (await api('lives')).find(item => item.recording && item.live_url === roomURL); } catch (error) { console.log('等待测试实例就绪：' + error.message); }
      if (live) break;
      await pause(2500);
    }
    assert(live, '两分钟内未开始录制');
    summary.live = live;
    console.log('开始隔离短录：' + live.host_name + '，' + duration + '秒');
    await pause(duration * 1000);
    await api('lives/' + live.id + '/stop');
    await pause(2000);
    await api('lives/' + live.id, 'DELETE');
    summary.stopped_at = new Date().toISOString();
  } finally {
    if (started) {
      const log = spawnSync('docker', ['logs', name], { encoding: 'utf8' });
      fs.writeFileSync(path.join(base, 'container.log'), (log.stdout || '') + (log.stderr || ''), { mode: 0o600 });
      try { run('docker', ['stop', name]); } catch (error) { console.error('停止测试实例失败：' + error.message); }
    }
    fs.writeFileSync(path.join(base, 'capture.json'), JSON.stringify(summary, null, 2), { mode: 0o600 });
  }
  console.log('隔离短录完成：' + base);
})().catch(error => { console.error(error); process.exitCode = 1; });
