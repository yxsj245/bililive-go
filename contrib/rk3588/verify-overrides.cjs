// 隔离实例配置回归：未开启监听，不录制，测试后删除临时房间。
const assert = require('assert/strict');
const fs = require('fs');
const base = 'http://127.0.0.1:18084/api/';
async function api(route, body, method) {
  const response = await fetch(base + route, { signal: AbortSignal.timeout(20000), ...(body === undefined ? {} : { method: method || 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }) });
  const text = await response.text();
  assert(response.ok, route + ': ' + text);
  return JSON.parse(text);
}
(async () => {
  const rooms = await api('lives', [{ url: 'https://live.douyin.com/85437864157?camera_id=0', listen: false }]);
  const id = rooms[0].id;
  try {
    await api('config/rooms/id/' + id, { on_record_finished: { rk3588: { bitrate: '2M' } } }, 'PATCH');
    const overridden = await api('lives/' + id);
    assert.equal(overridden.room_config.on_record_finished.burn_subtitles, true);
    assert.equal(overridden.room_config.on_record_finished.rk3588.bitrate, '2M');
    assert.equal(overridden.room_config.on_record_finished.rk3588.enable, true);
    await api('config/rooms/id/' + id, { on_record_finished: { rk3588: null } }, 'PATCH');
    const cleared = await api('lives/' + id);
    assert(!cleared.room_config.on_record_finished);
    fs.writeFileSync('/opt/Code/rk3588-media-build/override-report.json', JSON.stringify({ overridden, cleared, listening: false }, null, 2));
    console.log('房间首次覆盖继承烧录/硬件开关；清除覆盖彻底恢复继承，验证通过。');
  } finally {
    await api('lives/' + id, {}, 'DELETE');
  }
})().catch((e) => { console.error(e); process.exitCode = 1; });
