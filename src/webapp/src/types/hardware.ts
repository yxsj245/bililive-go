/**
 * RK3588 硬件压缩相关类型与常量
 *
 * 与后端契约对应：
 * - on_record_finished.rk3588：录制完成后的 RK3588 硬件压缩配置
 * - GET /api/hardware/status：硬件（FFmpeg / MPP）探测与自检状态，支持 ?refresh=1 强制重测
 *
 * 约定：本文件只提供类型、默认值与展示文案，不做任何“所有录像都可用”的断言。
 */

/** RK3588 硬件压缩配置（on_record_finished.rk3588） */
export interface Rk3588Config {
  /** 硬件路径总开关：开启后处理阶段使用 RK3588 MPP 硬件编解码，手动压缩也要求为开 */
  enable: boolean;
  /** 录制完成后压缩：开启后即使没有 ASS 弹幕字幕也会压缩；有 ASS 时烧录与压缩一次完成 */
  compress: boolean;
  /** 视频编码器：hevc_rkmpp（H.265）或 h264_rkmpp（H.264） */
  codec: string;
  /** 目标码率，例如 4M、6000k，默认 4M */
  bitrate: string;
  /** 硬件解码失败时是否回退软件解码（仅软解回退） */
  decode_fallback: boolean;
  /** 输出宽度，0 表示不缩放 */
  scale_width: number;
  /** 输出高度，0 表示不缩放 */
  scale_height: number;
}

/** 硬件编码器选项定义 */
export interface Rk3588CodecOption {
  label: string;
  value: string;
  /** 编码标准，用于界面上的中文提示 */
  standard: 'H.265' | 'H.264';
}

/** 硬件真实编解码自检的单个步骤 */
export interface HardwareSelfTestStep {
  /** 步骤名（encode / decode / hardware_decode） */
  name: string;
  /** 该步骤是否成功 */
  ok: boolean;
  /** 实际执行的命令行 */
  command: string;
  /** 失败原因 */
  error: string;
  /** 耗时（毫秒） */
  duration_ms: number;
}

/** 硬件真实编解码自检结果 */
export interface HardwareSelfTest {
  /** 自检是否通过 */
  ok: boolean;
  /** 自检使用的编码器 */
  codec: string;
  /** 自检步骤 */
  steps: HardwareSelfTestStep[];
  /** 自检说明信息 */
  message: string;
  /** 自检时间（与状态生成时间 probed_at 不同） */
  tested_at: string | number | null;
  /** 是否为缓存结果（true 表示未重新实测） */
  cached: boolean;
}

/** 自检步骤中文名 */
export const getSelfTestStepLabel = (name?: string): string => {
  switch (name) {
    case 'encode':
      return '硬件编码';
    case 'decode':
      return '解码';
    case 'hardware_decode':
      return '硬件解码';
    default:
      return name || '自检步骤';
  }
};

/** 硬件探测 / 自检状态（GET /api/hardware/status） */
export interface HardwareStatus {
  /** 硬件路径功能是否启用 */
  enabled: boolean;
  /** 是否就绪（后端判定） */
  ready: boolean;
  /** 实际使用的 ffmpeg 路径 */
  ffmpeg_path: string;
  /** ffmpeg 版本 */
  ffmpeg_version: string;
  /** 当前配置的编码器 */
  encoder: string;
  /** 探测到的编码器列表 */
  encoders: string[];
  /** 探测到的解码器列表 */
  decoders: string[];
  /** 探测到的滤镜列表 */
  filters: string[];
  /** 探测到的硬件加速方式 */
  hwaccels: string[];
  /** 处理链路描述 */
  chain: string;
  /** 当前配置是否开启自动压缩 */
  compress: boolean;
  /** 当前配置的码率 */
  bitrate: string;
  /** 当前配置是否硬解失败回退软解 */
  decode_fallback: boolean;
  /** 当前配置的缩放宽度（0 表示不缩放） */
  scale_width: number;
  /** 当前配置的缩放高度（0 表示不缩放） */
  scale_height: number;
  /** 设备可用性，键为设备路径，值为是否可访问 */
  devices: Record<string, boolean>;
  /** 后端给出的告警信息 */
  warnings: string[];
  /** 是否可用（以后端真实编解码自检结果为准） */
  available: boolean;
  /** 真实编解码自检详情 */
  self_test: HardwareSelfTest | null;
  /** 探测 / 自检时间 */
  probed_at: string | number | null;
}

/** 默认编码器（后端未配置时的界面默认值，可按需调整此常量） */
export const RK3588_DEFAULT_CODEC = 'hevc_rkmpp';

/** 默认码率，与后端默认值保持一致 */
export const RK3588_DEFAULT_BITRATE = '4M';

/** 配置默认值（保守默认：不主动改变现有行为） */
export const RK3588_CONFIG_DEFAULTS: Rk3588Config = {
  enable: false,
  compress: false,
  codec: RK3588_DEFAULT_CODEC,
  bitrate: RK3588_DEFAULT_BITRATE,
  decode_fallback: true,
  scale_width: 0,
  scale_height: 0,
};

/** 可选编码器 */
export const RK3588_CODEC_OPTIONS: Rk3588CodecOption[] = [
  { label: 'H.265 / HEVC（hevc_rkmpp）', value: 'hevc_rkmpp', standard: 'H.265' },
  { label: 'H.264 / AVC（h264_rkmpp）', value: 'h264_rkmpp', standard: 'H.264' },
];

/** 常用码率预设 */
export const RK3588_BITRATE_PRESETS: string[] = ['2M', '4M', '6M', '8M', '10M'];

/** 码率输入提示 */
export const RK3588_BITRATE_HINT = '例如 4M、4000k、4000000（合法范围 10k~200M）；留空按默认 4M 处理';

/** 码率合法范围，与后端 ParseBitrate 保持一致 */
export const RK3588_BITRATE_MIN_BITS = 10 * 1000;
export const RK3588_BITRATE_MAX_BITS = 200 * 1000 * 1000;

/** 缩放尺寸范围，与后端 ValidateScaleSize 保持一致 */
export const RK3588_SCALE_MIN = 16;
export const RK3588_SCALE_MAX = 8192;

/**
 * 解析码率字符串（与后端 ParseBitrate 同口径：4M / 4000k / 4000000）
 * 空字符串表示由后端默认值补齐（默认 4M），不算错误
 */
export const parseRk3588Bitrate = (
  value?: string | number | null
): { ok: boolean; bits: number; message: string } => {
  const text = (value === undefined || value === null ? '' : String(value)).trim().toLowerCase();
  if (text === '') {
    return { ok: true, bits: 0, message: '' };
  }

  let multiplier = 1;
  let numberText = text;
  if (text.endsWith('k')) {
    multiplier = 1000;
    numberText = text.slice(0, -1);
  } else if (text.endsWith('m')) {
    multiplier = 1000 * 1000;
    numberText = text.slice(0, -1);
  }

  const parsed = Number(numberText.trim());
  if (numberText.trim() === '' || !Number.isFinite(parsed)) {
    return { ok: false, bits: 0, message: `码率格式无效: ${value}（示例: 4M / 4000k / 4000000）` };
  }
  if (parsed <= 0) {
    return { ok: false, bits: 0, message: `码率必须大于 0: ${value}` };
  }

  const bits = Math.floor(parsed * multiplier);
  if (bits < RK3588_BITRATE_MIN_BITS || bits > RK3588_BITRATE_MAX_BITS) {
    return { ok: false, bits, message: `码率超出合理范围（10k~200M）: ${value}` };
  }
  return { ok: true, bits, message: '' };
};

/**
 * 缩放校验（与后端 ValidateScaleSize 同口径）：
 * 必须宽高同时为 0（不缩放），或同时为 16~8192 之间的偶数
 * @returns 错误信息，空字符串表示通过
 */
export const validateRk3588Scale = (width?: number, height?: number): string => {
  const w = toNonNegativeInt(width);
  const h = toNonNegativeInt(height);
  if (w === 0 && h === 0) {
    return '';
  }
  if (w === 0 || h === 0) {
    return '缩放需宽高同时填写，或都填 0 保持原分辨率';
  }
  if (w < RK3588_SCALE_MIN || h < RK3588_SCALE_MIN || w > RK3588_SCALE_MAX || h > RK3588_SCALE_MAX) {
    return `缩放尺寸需在 ${RK3588_SCALE_MIN}~${RK3588_SCALE_MAX} 之间`;
  }
  if (w % 2 !== 0 || h % 2 !== 0) {
    return '缩放尺寸需为偶数';
  }
  return '';
};

/** 探测 / 自检结论的边界说明（界面统一引用） */
export const HARDWARE_PROBE_CAVEAT =
  '编码器、解码器、滤镜与硬件加速列表来自 FFmpeg 探测，可用状态以后端的真实编解码自检为准；自检通过也只代表本次自检样本可用，并不等于所有真实录像、所有码率与分辨率都能成功。实际执行结果请以队列页任务里的执行命令与日志为准。';

/** 硬解回退说明 */
export const HARDWARE_DECODE_FALLBACK_HINT =
  '开启后：硬件解码失败时仅回退为软件解码（软解），不会自动切换到其他硬件编码器，也不保证任务一定成功；失败时保留原文件。';

/**
 * 原始文件保留说明
 * 准确口径：失败一定保留原件；成功路径是否删除源文件由「烧录后删除源视频」（burn_delete_source）决定，
 * 不能无条件宣称“成功后也永远保留”。
 */
export const RK3588_KEEP_SOURCE_HINT =
  '任务失败时一定保留原始文件；成功时是否删除源文件取决于「烧录后删除源视频」（burn_delete_source）设置，开启时会在全部处理阶段完成后删除源文件。';

/** 未开启删除源文件时的保留说明 */
export const RK3588_KEEP_SOURCE_HINT_OFF =
  '当前未开启「烧录后删除源视频」：成功后保留原文件并生成压缩文件，任务失败时同样保留原件。';

/** 已开启删除源文件时的提醒 */
export const RK3588_DELETE_SOURCE_WARNING =
  '注意：当前已开启「烧录后删除源视频」，成功完成全部处理阶段后可能删除源视频（烧录与压缩一次完成的场景同样适用）；任务失败时一定保留原件。';

/** 数值兜底：非法或负数一律归零（0 表示不缩放） */
export const toNonNegativeInt = (value: unknown): number => {
  const num = typeof value === 'number' ? value : Number(value);
  if (!Number.isFinite(num) || num <= 0) {
    return 0;
  }
  return Math.floor(num);
};

/** 规范化配置，补齐默认值并统一类型 */
export const normalizeRk3588Config = (raw?: Partial<Rk3588Config> | null): Rk3588Config => {
  const source = raw || {};
  const rawBitrate = source.bitrate;
  const bitrate =
    typeof rawBitrate === 'number'
      ? `${rawBitrate}M`
      : (rawBitrate === undefined || rawBitrate === null ? '' : String(rawBitrate)).trim();

  return {
    enable: source.enable ?? RK3588_CONFIG_DEFAULTS.enable,
    compress: source.compress ?? RK3588_CONFIG_DEFAULTS.compress,
    codec: source.codec || RK3588_CONFIG_DEFAULTS.codec,
    bitrate: bitrate || RK3588_CONFIG_DEFAULTS.bitrate,
    decode_fallback: source.decode_fallback ?? RK3588_CONFIG_DEFAULTS.decode_fallback,
    scale_width: toNonNegativeInt(source.scale_width),
    scale_height: toNonNegativeInt(source.scale_height),
  };
};

/** 取编码器选项 */
export const getRk3588CodecOption = (codec?: string): Rk3588CodecOption | undefined =>
  RK3588_CODEC_OPTIONS.find(item => item.value === codec);

/** 编码器中文名 */
export const getRk3588CodecLabel = (codec?: string): string => {
  const option = getRk3588CodecOption(codec);
  if (option) {
    return option.label;
  }
  return codec ? `${codec}（自定义）` : '未配置';
};

/** 缩放描述 */
export const describeRk3588Scale = (width?: number, height?: number): string => {
  const w = toNonNegativeInt(width);
  const h = toNonNegativeInt(height);
  if (w === 0 && h === 0) {
    return '保持原分辨率';
  }
  if (w > 0 && h > 0) {
    return `${w}x${h}`;
  }
  return w > 0 ? `宽 ${w}（高保持原始）` : `高 ${h}（宽保持原始）`;
};

/** 单行描述当前压缩参数，用于文件列表确认弹窗 */
export const describeRk3588Config = (raw?: Partial<Rk3588Config> | null): string => {
  const config = normalizeRk3588Config(raw);
  return [
    getRk3588CodecLabel(config.codec),
    `码率 ${config.bitrate}`,
    describeRk3588Scale(config.scale_width, config.scale_height),
    config.decode_fallback ? '硬解失败回退软解' : '硬解失败不回退',
  ].join(' / ');
};

/** 探测时间展示：兼容时间戳与字符串两种返回形式 */
export const formatProbedAt = (value?: string | number | null): string => {
  if (value === undefined || value === null || value === '') {
    return '未知';
  }
  if (typeof value === 'number' || /^\d+$/.test(String(value))) {
    const num = Number(value);
    if (Number.isFinite(num) && num > 0) {
      // 兼容秒与毫秒两种时间戳
      const ms = num > 1e12 ? num : num * 1000;
      return new Date(ms).toLocaleString();
    }
  }
  // 兼容后端 time.Time 的 RFC3339 字符串
  const text = String(value);
  const parsedDate = new Date(text);
  if (!Number.isNaN(parsedDate.getTime())) {
    return parsedDate.toLocaleString();
  }
  return text;
};

/** 编码器探测比对结果 */
export interface Rk3588CodecProbeResult {
  codec: string;
  label: string;
  /** 是否成功拿到探测列表 */
  probeKnown: boolean;
  /** 是否出现在探测到的编码器列表中 */
  probed: boolean;
}

/** 比对“实际配置编码器”与探测结果 */
export const checkRk3588CodecProbe = (
  codec: string,
  status?: HardwareStatus | null
): Rk3588CodecProbeResult => {
  const label = getRk3588CodecLabel(codec);
  const encoders = status?.encoders || [];
  if (!status || encoders.length === 0) {
    return { codec, label, probeKnown: false, probed: false };
  }
  const target = (codec || '').toLowerCase();
  const probed = encoders.some(item => {
    const name = (item || '').toLowerCase();
    return name === target || (target !== '' && name.indexOf(target) >= 0);
  });
  return { codec, label, probeKnown: true, probed };
};
