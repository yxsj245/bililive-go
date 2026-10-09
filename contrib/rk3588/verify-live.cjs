// 校验已授权短录的产物，不启动新的录制。
const fs = require('fs');
const path = require('path');
const { spawnSync } = require('child_process');
const assert = require('assert/strict');
const base = '/opt/Code/rk3588-media-build';
const task = JSON.parse(fs.readFileSync(base + '/live-task.json', 'utf8'));
assert.equal(task.status, 'completed');
const stage = task.stage_results[0];
const output = stage.output_files.find((f) => f.source_path);
const source = stage.input_files[0].path;
function local(p) { return path.join(base, 'api-smoke', path.relative('/srv/bililive', p)); }
const ass = source.slice(0, -path.extname(source).length) + '.ass';
assert(fs.existsSync(local(source)) && fs.existsSync(local(ass)) && fs.existsSync(local(output.path)));
for (const f of [source, output.path]) {
  const json = local(f) + '.probe.json';
  const result = spawnSync('docker', ['exec', 'bililive-rk3588-test', 'ffprobe', '-v', 'error', '-show_streams', '-show_format', '-of', 'json', f], { stdio: ['ignore', fs.openSync(json, 'w'), 'inherit'] });
  assert.equal(result.status, 0);
}
const sourceInfo = JSON.parse(fs.readFileSync(local(source) + '.probe.json'));
const outputInfo = JSON.parse(fs.readFileSync(local(output.path) + '.probe.json'));
assert.equal(outputInfo.streams.find((s) => s.codec_type === 'video').codec_name, 'hevc');
assert.equal(outputInfo.streams.find((s) => s.codec_type === 'audio').codec_name, 'aac');
const decode = spawnSync('docker', ['exec', 'bililive-rk3588-test', 'ffmpeg', '-v', 'error', '-xerror', '-err_detect', 'explode', '-i', output.path, '-f', 'null', '-'], { stdio: 'inherit' });
assert.equal(decode.status, 0);
const summary = {
  task_id: task.id,
  status: task.status,
  source: sourceInfo,
  output: outputInfo,
  source_size: fs.statSync(local(source)).size,
  output_size: fs.statSync(local(output.path)).size,
  ass_size: fs.statSync(local(ass)).size,
  commands: stage.commands,
  source_preserved: true,
  ass_preserved: true,
  full_decode_ok: true,
};
fs.writeFileSync(base + '/live-validation.json', JSON.stringify(summary, null, 2));
console.log(JSON.stringify({ source_size: summary.source_size, output_size: summary.output_size, saved_percent: +(100 * (1 - summary.output_size / summary.source_size)).toFixed(2), ass_size: summary.ass_size, full_decode_ok: true }));
