/**
 * 硬件压缩状态卡片（RK3588 / MPP 探测 + 真实编解码自检）
 *
 * 口径说明：
 * - 编码器 / 解码器 / 滤镜 / 硬件加速列表来自 FFmpeg 探测，只表示“当前环境报告支持”；
 * - available 以后端真实编解码自检为准，自检通过也只代表本次自检样本可用，
 *   不等于所有真实录像、所有码率与分辨率都能成功；
 * - 真实执行结果以队列页任务的执行命令与日志为准。
 *
 * 数据获取请使用本文件导出的 useHardwareStatus / fetchHardwareStatus。
 */

import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Alert, Button, Card, Space, Spin, Tag, Tooltip, Typography } from 'antd';
import { ReloadOutlined } from '@ant-design/icons';
import API from '../../utils/api';
import {
  HARDWARE_PROBE_CAVEAT,
  HardwareSelfTest,
  HardwareSelfTestStep,
  HardwareStatus,
  RK3588_KEEP_SOURCE_HINT,
  Rk3588Config,
  checkRk3588CodecProbe,
  describeRk3588Scale,
  formatProbedAt,
  getRk3588CodecLabel,
  getSelfTestStepLabel,
  toNonNegativeInt,
} from '../../types/hardware';

const api = new API();
const { Text, Paragraph } = Typography;

/** 探测结果缓存时长，避免同一页面多处使用时重复请求 */
const CACHE_TTL_MS = 15000;

let cachedStatus: HardwareStatus | null = null;
let cachedAt = 0;
let inflight: Promise<HardwareStatus | null> | null = null;

const toStringArray = (value: unknown): string[] => {
  if (!Array.isArray(value)) {
    return [];
  }
  return value.filter(item => typeof item === 'string' && item !== '') as string[];
};

const toBoolRecord = (value: unknown): Record<string, boolean> => {
  const result: Record<string, boolean> = {};
  if (value && typeof value === 'object' && !Array.isArray(value)) {
    const source = value as Record<string, unknown>;
    Object.keys(source).forEach(key => {
      result[key] = !!source[key];
    });
  }
  return result;
};

/** 自检步骤：后端返回的是对象数组（name/ok/command/error/duration_ms），这里同时兼容字符串数组 */
const toSelfTestSteps = (value: unknown): HardwareSelfTestStep[] => {
  if (!Array.isArray(value)) {
    return [];
  }
  return value.map((item, index) => {
    if (typeof item === 'string') {
      return { name: item, ok: true, command: '', error: '', duration_ms: 0 };
    }
    const step = (item && typeof item === 'object' ? item : {}) as Record<string, unknown>;
    return {
      name: typeof step.name === 'string' ? step.name : `步骤 ${index + 1}`,
      ok: step.ok === undefined ? true : !!step.ok,
      command: typeof step.command === 'string' ? step.command : '',
      error: typeof step.error === 'string' ? step.error : '',
      duration_ms: typeof step.duration_ms === 'number' ? step.duration_ms : 0,
    };
  });
};

const toSelfTest = (value: unknown): HardwareSelfTest | null => {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return null;
  }
  const source = value as Record<string, unknown>;
  return {
    ok: !!source.ok,
    codec: typeof source.codec === 'string' ? source.codec : '',
    steps: toSelfTestSteps(source.steps),
    message: typeof source.message === 'string' ? source.message : '',
    tested_at: (
      typeof source.tested_at === 'string' || typeof source.tested_at === 'number'
        ? source.tested_at
        : null
    ),
    cached: !!source.cached,
  };
};

/** 兼容 {data: {...}} 与直接返回两种包装形式，缺字段时返回 null */
export const normalizeHardwareStatus = (raw: unknown): HardwareStatus | null => {
  if (!raw || typeof raw !== 'object') {
    return null;
  }
  const wrapper = raw as Record<string, unknown>;
  const source = (
    wrapper.data && typeof wrapper.data === 'object' && !Array.isArray(wrapper.data)
      ? wrapper.data
      : wrapper
  ) as Record<string, unknown>;

  if (
    source.encoders === undefined
    && source.available === undefined
    && source.ffmpeg_path === undefined
    && source.self_test === undefined
  ) {
    return null;
  }

  return {
    enabled: !!source.enabled,
    ready: !!source.ready,
    ffmpeg_path: typeof source.ffmpeg_path === 'string' ? source.ffmpeg_path : '',
    ffmpeg_version: typeof source.ffmpeg_version === 'string' ? source.ffmpeg_version : '',
    encoder: typeof source.encoder === 'string' ? source.encoder : '',
    encoders: toStringArray(source.encoders),
    decoders: toStringArray(source.decoders),
    filters: toStringArray(source.filters),
    hwaccels: toStringArray(source.hwaccels),
    chain: typeof source.chain === 'string' ? source.chain : '',
    compress: !!source.compress,
    bitrate: typeof source.bitrate === 'string' ? source.bitrate : '',
    decode_fallback: !!source.decode_fallback,
    scale_width: toNonNegativeInt(source.scale_width),
    scale_height: toNonNegativeInt(source.scale_height),
    devices: toBoolRecord(source.devices),
    warnings: toStringArray(source.warnings),
    available: !!source.available,
    self_test: toSelfTest(source.self_test),
    probed_at: (
      typeof source.probed_at === 'string' || typeof source.probed_at === 'number'
        ? source.probed_at
        : null
    ),
  };
};

/**
 * 获取硬件探测 / 自检状态
 * @param refresh true 时带 ?refresh=1 强制后端重新自检（忽略后端缓存）
 */
export const fetchHardwareStatus = async (refresh = false): Promise<HardwareStatus | null> => {
  const now = Date.now();
  if (!refresh) {
    if (cachedStatus && now - cachedAt < CACHE_TTL_MS) {
      return cachedStatus;
    }
    if (inflight) {
      return inflight;
    }
  }

  const task = (async () => {
    try {
      const rsp: unknown = await api.getHardwareStatus(refresh);
      const status = normalizeHardwareStatus(rsp);
      if (status) {
        cachedStatus = status;
        cachedAt = Date.now();
      }
      return status;
    } catch (error) {
      console.error('获取硬件状态失败:', error);
      return null;
    } finally {
      inflight = null;
    }
  })();

  inflight = task;
  return task;
};

/** 硬件探测状态 Hook */
export const useHardwareStatus = () => {
  const [status, setStatus] = useState<HardwareStatus | null>(null);
  const [loading, setLoading] = useState(false);
  const mountedRef = useRef(true);

  const load = useCallback(async (refresh: boolean) => {
    setLoading(true);
    const result = await fetchHardwareStatus(refresh);
    if (mountedRef.current) {
      setStatus(result);
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    mountedRef.current = true;
    load(false);
    return () => {
      mountedRef.current = false;
    };
  }, [load]);

  const reload = useCallback(() => {
    load(true);
  }, [load]);

  return { status, loading, reload };
};

/** 字符串列表渲染为 Tag 组 */
const renderTagList = (items: string[], emptyText: string, color?: string) => {
  if (items.length === 0) {
    return <Text type="secondary">{emptyText}</Text>;
  }
  return (
    <Space size={4} wrap>
      {items.map(item => (
        <Tag key={item} color={color}>
          {item}
        </Tag>
      ))}
    </Space>
  );
};

export interface HardwareStatusCardProps {
  /** 当前生效的 rk3588 配置，用于提示实际配置的编码器与参数 */
  rk3588?: Rk3588Config | null;
  /** 探测状态（由父组件通过 useHardwareStatus 获取） */
  status?: HardwareStatus | null;
  /** 是否正在探测 / 自检 */
  loading?: boolean;
  /** 重新探测（会带 ?refresh=1 强制后端重测自检） */
  onReload?: () => void;
}

const HardwareStatusCard: React.FC<HardwareStatusCardProps> = ({
  rk3588,
  status,
  loading = false,
  onReload,
}) => {
  const codec = rk3588?.codec || status?.encoder || '';
  const probe = checkRk3588CodecProbe(codec, status);
  const selfTest = status?.self_test || null;
  const bitrate = rk3588?.bitrate || status?.bitrate || '';
  const scaleWidth = rk3588 ? rk3588.scale_width : status?.scale_width;
  const scaleHeight = rk3588 ? rk3588.scale_height : status?.scale_height;
  const decodeFallback = rk3588 ? rk3588.decode_fallback : status?.decode_fallback;

  return (
    <Card
      title="硬件压缩状态（探测 + 自检）"
      size="small"
      style={{ marginBottom: 16 }}
      id="global-hardware-status"
      extra={
        onReload ? (
          <Button size="small" icon={<ReloadOutlined />} loading={loading} onClick={onReload}>
            重新探测并自检
          </Button>
        ) : null
      }
    >
      {loading && !status ? (
        <Spin size="small" />
      ) : (
        <>
          {!status && (
            <Alert
              type="info"
              showIcon
              style={{ marginBottom: 12 }}
              message="未能获取硬件状态"
              description="后端未提供 /api/hardware/status 或请求失败。此时 RK3588 压缩配置可能尚未被后端支持：保存不会报错，但可能不生效（刷新后回到默认值）。实际是否生效请以队列页任务的执行命令与日志为准。"
            />
          )}

          <Space direction="vertical" size={6} style={{ width: '100%' }}>
            <Space size={8} wrap>
              <span>硬件可用状态：</span>
              {status ? (
                <Tag color={status.available ? 'green' : 'red'}>
                  {status.available ? '可用（自检通过）' : '不可用'}
                </Tag>
              ) : (
                <Tag>未知</Tag>
              )}
              <Tag color={status?.enabled ? 'blue' : 'default'}>
                {status?.enabled ? '硬件路径已启用' : '硬件路径未启用'}
              </Tag>
              <Tag color={status?.ready ? 'green' : 'default'}>
                {status?.ready ? '就绪' : '未就绪'}
              </Tag>
            </Space>

            <div>
              <Text type="secondary">真实编解码自检：</Text>
              {selfTest ? (
                <>
                  <Tag color={selfTest.ok ? 'green' : 'red'}>{selfTest.ok ? '通过' : '未通过'}</Tag>
                  {selfTest.codec && <Text>编码器 {getRk3588CodecLabel(selfTest.codec)}</Text>}
                  <Tag style={{ marginLeft: 8 }} color={selfTest.cached ? 'default' : 'blue'}>
                    {selfTest.cached ? '缓存结果' : '本次实测'}
                  </Tag>
                </>
              ) : (
                <Text type="secondary">后端未返回自检信息</Text>
              )}
            </div>

            {selfTest?.message && (
              <div>
                <Text type="secondary">自检信息：</Text>
                <Text>{selfTest.message}</Text>
              </div>
            )}

            {(selfTest?.steps || []).length > 0 && (
              <div>
                <Text type="secondary">自检步骤：</Text>
                <div
                  style={{
                    marginTop: 4,
                    maxHeight: 200,
                    overflow: 'auto',
                    background: '#fafafa',
                    borderRadius: 4,
                    padding: '6px 8px',
                    fontSize: 12,
                  }}
                >
                  {(selfTest?.steps || []).map((step, index) => (
                    <div key={`${index}-${step.name}`} style={{ marginBottom: 6 }}>
                      <Space size={6} wrap>
                        <Tag color={step.ok ? 'green' : 'red'}>{step.ok ? '通过' : '失败'}</Tag>
                        <Text strong>{getSelfTestStepLabel(step.name)}</Text>
                        {step.duration_ms > 0 && <Text type="secondary">{step.duration_ms} ms</Text>}
                      </Space>
                      {step.command && (
                        <Paragraph
                          code
                          copyable
                          style={{ fontSize: 11, margin: '2px 0 0', wordBreak: 'break-all' }}
                        >
                          {step.command}
                        </Paragraph>
                      )}
                      {step.error && (
                        <div style={{ color: '#cf1322', wordBreak: 'break-all' }}>{step.error}</div>
                      )}
                    </div>
                  ))}
                </div>
              </div>
            )}

            <div>
              <Text type="secondary">自检时间：</Text>
              <Text>{formatProbedAt(selfTest?.tested_at)}</Text>
            </div>

            <div>
              <Text type="secondary">状态生成时间：</Text>
              <Text>{formatProbedAt(status?.probed_at)}</Text>
              <Text type="secondary" style={{ marginLeft: 8 }}>
                （点「重新探测并自检」可强制重测）
              </Text>
            </div>

            <div>
              <Text type="secondary">实际配置编码器：</Text>
              <Text>{codec ? getRk3588CodecLabel(codec) : '未配置'}</Text>
              {probe.probeKnown && (
                <Tag style={{ marginLeft: 8 }} color={probe.probed ? 'green' : 'orange'}>
                  {probe.probed ? '出现在探测列表' : '未出现在探测列表'}
                </Tag>
              )}
            </div>

            <div>
              <Text type="secondary">实际配置参数：</Text>
              <Text>
                码率 {bitrate || '未返回'} / {describeRk3588Scale(scaleWidth, scaleHeight)} /{' '}
                {decodeFallback ? '硬解失败回退软解' : '硬解失败不回退'}
                {status ? ` / 录制完成后压缩${status.compress ? '已开启' : '未开启'}` : ''}
              </Text>
            </div>

            <div>
              <Text type="secondary">FFmpeg：</Text>
              <Text>{status?.ffmpeg_version || '未返回版本'}</Text>
              <Text type="secondary" style={{ marginLeft: 8 }}>
                {status?.ffmpeg_path || '未获取路径'}
              </Text>
            </div>

            <div>
              <Text type="secondary">硬件加速方式：</Text>
              {renderTagList(status?.hwaccels || [], '未探测到硬件加速方式', 'purple')}
            </div>

            {status?.chain && (
              <div>
                <Text type="secondary">处理链路：</Text>
                <Text code>{status.chain}</Text>
              </div>
            )}

            <div>
              <Text type="secondary">探测到的硬件编码器：</Text>
              {renderTagList(status?.encoders || [], '未探测到硬件编码器', 'geekblue')}
            </div>

            <div>
              <Text type="secondary">探测到的硬件解码器：</Text>
              {renderTagList(status?.decoders || [], '未探测到硬件解码器')}
            </div>

            <div>
              <Text type="secondary">处理链所需滤镜（探测到）：</Text>
              {renderTagList(status?.filters || [], '未探测到滤镜')}
            </div>

            <div>
              <Text type="secondary">设备节点：</Text>
              {Object.keys(status?.devices || {}).length === 0 ? (
                <Text type="secondary">未返回设备信息</Text>
              ) : (
                <Space size={4} wrap>
                  {Object.keys(status?.devices || {}).map(device => {
                    const deviceMap = status?.devices || {};
                    return (
                      <Tooltip key={device} title={deviceMap[device] ? '节点存在' : '节点不存在'}>
                        <Tag color={deviceMap[device] ? 'green' : 'red'}>{device}</Tag>
                      </Tooltip>
                    );
                  })}
                </Space>
              )}
            </div>
          </Space>

          {(status?.warnings || []).length > 0 && (
            <Alert
              type="warning"
              showIcon
              style={{ marginTop: 12 }}
              message="后端告警"
              description={
                <div>
                  {(status?.warnings || []).map(item => (
                    <div key={item}>{item}</div>
                  ))}
                </div>
              }
            />
          )}

          {status && !status.available && (
            <Alert
              type="warning"
              showIcon
              style={{ marginTop: 12 }}
              message="硬件自检未通过"
              description="当前状态为不可用：压缩或烧录任务可能失败并回退软解，失败时保留原文件。可点「重新探测并自检」重测。"
            />
          )}

          {probe.probeKnown && !probe.probed && (
            <Alert
              type="warning"
              showIcon
              style={{ marginTop: 12 }}
              message="实际配置的编码器未出现在探测列表中"
              description={`${probe.label} 未被探测结果报告支持，实际执行时可能失败或回退；失败时保留原文件。`}
            />
          )}

          <Alert
            type="info"
            showIcon
            style={{ marginTop: 12 }}
            message="探测 / 自检结果不等于所有真实录像都可用"
            description={
              <div>
                <div>{HARDWARE_PROBE_CAVEAT}</div>
                <div style={{ marginTop: 4 }}>{RK3588_KEEP_SOURCE_HINT}</div>
              </div>
            }
          />
        </>
      )}
    </Card>
  );
};

export default HardwareStatusCard;
