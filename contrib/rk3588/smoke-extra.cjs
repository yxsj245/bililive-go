// 隔离实例：验证运行中取消与真实 10bit 输入，禁止连接生产目录。
const fs = require('fs');
const assert = require('assert/strict');
const url = (process.env.SMOKE_URL || 'http://127.0.0.1:18084') + '/api/';
const work = process.env.SMOKE_DIR || '/opt/Code/rk3588-media-build/api-smoke';
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
async function api(route, body, method) {
  const res = await fetch(url + route, {
    ...(body === undefined ? {} : { method: method || 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }),
    signal: AbortSignal.timeout(20000),
  });
  const text = await res.text();
  assert(res.ok, route + ': ' + text);
  return JSON.parse(text);
}
(async () => {
  console.log('提交运行中取消测试');
  const submit = await api('pipeline/batch-compress', { paths: ['cancel/source.mp4'] });
  const id = submit.task_ids[0];
  assert(id, JSON.stringify(submit));
  let task;
  for (let i = 0; i < 50; i++) {
    task = await api('pipeline/tasks/' + id);
    if (task.status === 'running') break;
    await sleep(50);
  }
  assert.equal(task.status, 'running');
  await api('pipeline/tasks/' + id + '/cancel', {});
  for (let i = 0; i < 50; i++) {
    task = await api('pipeline/tasks/' + id);
    if (task.status === 'cancelled') break;
    await sleep(100);
  }
  await sleep(300);
  assert.equal(task.status, 'cancelled');
  assert(fs.existsSync(work + '/cancel/source.mp4'));
  assert(fs.existsSync(work + '/cancel/source.ass'));
  assert(!fs.readdirSync(work + '/cancel').some((f) => f.endsWith('.mkv') || f.startsWith('.bililive-')));
  console.log('运行中取消测试通过，提交 10bit 烧录测试');
  const ten = await api('pipeline/batch-burn', { paths: ['tenbit/source.mp4'] });
  let t;
  for (let i = 0; i < 120; i++) {
    t = await api('pipeline/tasks/' + ten.task_ids[0]);
    if (['completed', 'failed'].includes(t.status)) break;
    await sleep(500);
  }
  assert.equal(t.status, 'completed', t.error_message);
  fs.writeFileSync(work + '/extra-report.json', JSON.stringify({ cancel: task, tenbit: t }, null, 2));
  console.log('真实 H264 10bit 输入烧录 H265 通过，回退情况见实际命令记录。');
})().catch((e) => { console.error(e); process.exitCode = 1; });
