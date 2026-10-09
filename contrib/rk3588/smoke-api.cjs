// 仅用于没有监听直播间、挂载隔离测试目录的验证容器，禁止对生产历史目录执行。
const fs = require('fs');
const assert = require('assert/strict');
const base = process.env.SMOKE_URL || 'http://127.0.0.1:18084';
const work = process.env.SMOKE_DIR || '/opt/Code/rk3588-media-build/api-smoke';
const pause = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
async function api(route, body) {
  const response = await fetch(base + '/api/' + route, body === undefined ? {} : {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body),
  });
  const text = await response.text();
  assert(response.ok, `${route} 返回 ${response.status}: ${text}`);
  return JSON.parse(text);
}
async function waitTask(id) {
  const deadline = Date.now() + 120000;
  while (Date.now() < deadline) {
    const task = await api('pipeline/tasks/' + id);
    if (['completed', 'failed', 'cancelled'].includes(task.status)) return task;
    await pause(1000);
  }
  throw new Error('等待任务超时: ' + id);
}
(async () => {
  const report = { base, tested_at: new Date().toISOString(), tests: [] };
  report.hardware = await api('hardware/status?refresh=1');
  assert(report.hardware.ready, JSON.stringify(report.hardware));
  const burn = await api('pipeline/batch-burn', { paths: ['burn/source.mp4'] });
  assert.equal(burn.enqueued, 1);
  const burned = await waitTask(burn.task_ids[0]);
  assert.equal(burned.status, 'completed', burned.error_message);
  assert(burned.stage_results[0].commands.some((c) => c.includes('hevc_rkmpp') && c.includes('ass=')));
  assert(fs.existsSync(work + '/burn/source.mp4') && fs.existsSync(work + '/burn/source.ass'));
  report.tests.push({ name: '中文 ASS 烧录+H.265 单次编码与保留源文件', task: burned });

  const compressed = await api('pipeline/batch-compress', { paths: ['plain/source.mp4', 'invalid/broken.mp4', '../outside.mp4', 'escape.mp4', 'plain/source.mp4'] });
  assert.equal(compressed.enqueued, 2, JSON.stringify(compressed));
  assert.equal(compressed.skipped.length, 3, JSON.stringify(compressed));
  const tasks = await Promise.all(compressed.task_ids.map(waitTask));
  assert(tasks.some((t) => t.status === 'completed'));
  assert(tasks.some((t) => t.status === 'failed'));
  assert(fs.existsSync(work + '/plain/source.mp4') && fs.existsSync(work + '/invalid/broken.mp4'));
  report.tests.push({ name: '无 ASS 压缩、损坏输入失败保留、越权与重复路径拒绝', response: compressed, tasks });

  const repeat = await api('pipeline/batch-compress', { paths: ['plain/source.mp4'] });
  const repeated = await waitTask(repeat.task_ids[0]);
  assert.equal(repeated.status, 'completed', repeated.error_message);
  const originalOutput = tasks.find((t) => t.status === 'completed').stage_results[0].output_files.find((f) => f.source_path);
  const repeatedOutput = repeated.stage_results[0].output_files.find((f) => f.source_path);
  assert.notEqual(originalOutput.path, repeatedOutput.path);
  report.tests.push({ name: '重复转码发布新名字，不覆盖已有输出', task: repeated });
  fs.writeFileSync(work + '/api-report.json', JSON.stringify(report, null, 2));
  console.log(report.tests.map((t) => '通过：' + t.name).join('\n'));
})().catch((error) => { console.error(error); process.exitCode = 1; });
