/**
 * RK3588 硬件压缩前端工具函数的局部测试
 *
 * 重点覆盖与后端同口径的校验规则：
 * - 码率：4M / 4000k / 4000000，范围 10k~200M（对应 configs.ParseBitrate）
 * - 缩放：宽高同时为 0，或同时为 16~8192 之间的偶数（对应 configs.ValidateScaleSize）
 */

import {
  RK3588_CONFIG_DEFAULTS,
  RK3588_KEEP_SOURCE_HINT,
  checkRk3588CodecProbe,
  describeRk3588Config,
  describeRk3588Scale,
  formatProbedAt,
  getSelfTestStepLabel,
  normalizeRk3588Config,
  parseRk3588Bitrate,
  validateRk3588Scale,
} from './hardware';
import type { HardwareStatus } from './hardware';

const buildStatus = (encoders: string[]): HardwareStatus => ({
  enabled: true,
  ready: true,
  ffmpeg_path: '/usr/bin/ffmpeg',
  ffmpeg_version: 'ffmpeg version 6.1',
  encoder: 'hevc_rkmpp',
  encoders,
  decoders: [],
  filters: [],
  hwaccels: ['rkmpp'],
  chain: '硬解(rkmpp/nv12) → 硬件编码(hevc_rkmpp 4M)',
  compress: false,
  bitrate: '4M',
  decode_fallback: true,
  scale_width: 0,
  scale_height: 0,
  devices: { '/dev/mpp_service': true },
  warnings: [],
  available: true,
  self_test: {
    ok: true,
    codec: 'hevc_rkmpp',
    steps: [{ name: 'encode', ok: true, command: 'ffmpeg -c:v hevc_rkmpp ...', error: '', duration_ms: 12 }],
    message: '',
    tested_at: '2024-05-05T10:00:00Z',
    cached: false,
  },
  probed_at: '2024-05-05T10:00:00Z',
});

describe('RK3588 配置默认值', () => {
  test('默认不启用、4M、回退软解、不缩放', () => {
    expect(RK3588_CONFIG_DEFAULTS).toEqual({
      enable: false,
      compress: false,
      codec: 'hevc_rkmpp',
      bitrate: '4M',
      decode_fallback: true,
      scale_width: 0,
      scale_height: 0,
    });
  });

  test('normalizeRk3588Config 补齐空值与非法缩放', () => {
    expect(normalizeRk3588Config(null)).toEqual(RK3588_CONFIG_DEFAULTS);
    expect(normalizeRk3588Config({ codec: '', bitrate: '  ', scale_width: -5, scale_height: 'abc' as any })).toEqual({
      ...RK3588_CONFIG_DEFAULTS,
    });
    expect(normalizeRk3588Config({ bitrate: 6 as any }).bitrate).toBe('6M');
    expect(normalizeRk3588Config({ scale_width: 1280.9, scale_height: 720 }).scale_width).toBe(1280);
  });
});

describe('码率校验（与后端 ParseBitrate 同口径）', () => {
  test('合法写法', () => {
    expect(parseRk3588Bitrate('4M').ok).toBe(true);
    expect(parseRk3588Bitrate('4M').bits).toBe(4000000);
    expect(parseRk3588Bitrate('4000k').bits).toBe(4000000);
    expect(parseRk3588Bitrate('4000000').bits).toBe(4000000);
    expect(parseRk3588Bitrate('4.5m').bits).toBe(4500000);
  });

  test('留空表示使用后端默认值', () => {
    expect(parseRk3588Bitrate('').ok).toBe(true);
    expect(parseRk3588Bitrate(undefined).ok).toBe(true);
  });

  test('非法与越界', () => {
    expect(parseRk3588Bitrate('4MB').ok).toBe(false);
    expect(parseRk3588Bitrate('abc').ok).toBe(false);
    expect(parseRk3588Bitrate('0').ok).toBe(false);
    expect(parseRk3588Bitrate('5k').ok).toBe(false);
    expect(parseRk3588Bitrate('300M').ok).toBe(false);
  });
});

describe('缩放校验（与后端 ValidateScaleSize 同口径）', () => {
  test('都为 0 表示不缩放', () => {
    expect(validateRk3588Scale(0, 0)).toBe('');
    expect(validateRk3588Scale(undefined, undefined)).toBe('');
  });

  test('合法缩放为 16~8192 的偶数', () => {
    expect(validateRk3588Scale(1280, 720)).toBe('');
    expect(validateRk3588Scale(1920, 1080)).toBe('');
  });

  test('只填一个方向被拒绝', () => {
    expect(validateRk3588Scale(1280, 0)).not.toBe('');
    expect(validateRk3588Scale(0, 720)).not.toBe('');
  });

  test('越界与奇数被拒绝', () => {
    expect(validateRk3588Scale(8, 720)).not.toBe('');
    expect(validateRk3588Scale(1280, 9000)).not.toBe('');
    expect(validateRk3588Scale(1281, 720)).not.toBe('');
  });
});

describe('展示辅助', () => {
  test('缩放描述', () => {
    expect(describeRk3588Scale(0, 0)).toBe('保持原分辨率');
    expect(describeRk3588Scale(1280, 720)).toBe('1280x720');
    expect(describeRk3588Scale(1280, 0)).toContain('宽 1280');
  });

  test('参数摘要包含编码器、码率与缩放', () => {
    const text = describeRk3588Config({ codec: 'h264_rkmpp', bitrate: '6M', scale_width: 1280, scale_height: 720 });
    expect(text).toContain('H.264');
    expect(text).toContain('码率 6M');
    expect(text).toContain('1280x720');
    expect(text).toContain('硬解失败回退软解');
  });

  test('探测时间兼容 RFC3339 与时间戳', () => {
    expect(formatProbedAt('2024-05-05T10:00:00Z')).toContain('2024');
    expect(formatProbedAt(1714903200)).toContain('2024');
    expect(formatProbedAt(null)).toBe('未知');
  });

  test('自检步骤中文名与保留口径文案', () => {
    expect(getSelfTestStepLabel('encode')).toBe('硬件编码');
    expect(getSelfTestStepLabel('hardware_decode')).toBe('硬件解码');
    expect(getSelfTestStepLabel('unknown_step')).toBe('unknown_step');
    // 保留口径不能无条件宣称“成功后永远保留”
    expect(RK3588_KEEP_SOURCE_HINT).toContain('burn_delete_source');
  });

  test('编码器探测比对：命中 / 未命中 / 无探测结果', () => {
    expect(checkRk3588CodecProbe('hevc_rkmpp', buildStatus(['hevc_rkmpp', 'h264_rkmpp'])).probed).toBe(true);
    const miss = checkRk3588CodecProbe('h264_rkmpp', buildStatus(['hevc_rkmpp']));
    expect(miss.probeKnown).toBe(true);
    expect(miss.probed).toBe(false);
    expect(checkRk3588CodecProbe('hevc_rkmpp', null).probeKnown).toBe(false);
    expect(checkRk3588CodecProbe('hevc_rkmpp', buildStatus([])).probeKnown).toBe(false);
  });
});
