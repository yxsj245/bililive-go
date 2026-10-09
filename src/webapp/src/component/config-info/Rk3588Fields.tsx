/**
 * RK3588 硬件压缩配置字段（全局 / 直播间共用）
 *
 * 说明：
 * - 字段名与后端契约 on_record_finished.rk3588 一致；
 * - 通过 namePrefix 决定字段挂载位置，全局与直播间使用同一份字段定义；
 * - 界面文案严格区分“FFmpeg 探测结果”与“实际编码实测”。
 */

import React from 'react';
import { AutoComplete, Form, InputNumber, Select, Space, Switch, Tag, Typography } from 'antd';
import { SimpleConfigField } from './shared-fields';
import {
  HARDWARE_DECODE_FALLBACK_HINT,
  HardwareStatus,
  RK3588_BITRATE_HINT,
  RK3588_BITRATE_PRESETS,
  RK3588_CODEC_OPTIONS,
  RK3588_SCALE_MAX,
  Rk3588Config,
  checkRk3588CodecProbe,
  describeRk3588Config,
  parseRk3588Bitrate,
  toNonNegativeInt,
  validateRk3588Scale,
} from '../../types/hardware';

const { Text } = Typography;

export interface Rk3588FieldsProps {
  /** Form.Item 名称前缀，例如 ['on_record_finished', 'rk3588'] */
  namePrefix: (string | number)[];
  /** 硬件探测状态；null/undefined 表示未获取到 */
  hardwareStatus?: HardwareStatus | null;
  /** 上级（全局）配置，用于直播间级展示继承值 */
  inheritedConfig?: Rk3588Config | null;
  /** 是否禁用全部输入（例如直播间级未开启覆盖时） */
  disabled?: boolean;
}

/** 码率校验：允许留空（按后端默认 4M 处理），其余与后端 ParseBitrate 同口径 */
const validateBitrate = (_rule: unknown, value: unknown) => {
  const result = parseRk3588Bitrate(value as string);
  return result.ok ? Promise.resolve() : Promise.reject(new Error(result.message));
};

const Rk3588Fields: React.FC<Rk3588FieldsProps> = ({
  namePrefix,
  hardwareStatus,
  inheritedConfig,
  disabled = false,
}) => {
  const watchedCodec = Form.useWatch([...namePrefix, 'codec']) as string | undefined;
  const watchedWidth = Form.useWatch([...namePrefix, 'scale_width']) as number | undefined;
  const watchedHeight = Form.useWatch([...namePrefix, 'scale_height']) as number | undefined;

  const form = Form.useFormInstance();
  const probe = checkRk3588CodecProbe(watchedCodec || '', hardwareStatus);

  // 缩放校验：读取同一前缀下的宽高，按后端规则提示（16~8192 偶数，或都为 0）
  const scaleValidator = () => {
    const currentWidth = form.getFieldValue([...namePrefix, 'scale_width']);
    const currentHeight = form.getFieldValue([...namePrefix, 'scale_height']);
    const messageText = validateRk3588Scale(currentWidth, currentHeight);
    return messageText ? Promise.reject(new Error(messageText)) : Promise.resolve();
  };

  // 编码器探测提示：只描述探测结果，不下“可用”结论
  const renderCodecProbeHint = () => {
    if (!probe.probeKnown) {
      return (
        <Text type="secondary">
          未能获取硬件探测结果，无法判断该编码器是否被当前环境报告支持。
        </Text>
      );
    }
    if (!probe.probed) {
      return (
        <Text type="warning">
          {probe.label} 未出现在 FFmpeg 探测到的编码器列表中，实际执行时可能失败或回退；探测结果不等于实测，请以队列页任务的执行命令与日志为准。
        </Text>
      );
    }
    return (
      <Text type="secondary">
        该编码器出现在 FFmpeg 探测列表中；探测结果不等于实测。
      </Text>
    );
  };

  // 缩放提示：0 表示不缩放，只填一个方向时给出提醒
  const width = toNonNegativeInt(watchedWidth);
  const height = toNonNegativeInt(watchedHeight);
  const scaleHint = (() => {
    if (width === 0 && height === 0) {
      return '宽高均为 0 时保持原分辨率。';
    }
    if (width > 0 && height > 0) {
      return `输出分辨率 ${width}x${height}。`;
    }
    return `当前只设置了${width > 0 ? '宽度' : '高度'}，无法通过校验；请宽高同时填写 16~8192 之间的偶数，或都填 0 保持原分辨率。`;
  })();

  const labelWidth = 200;

  return (
    <>
      <SimpleConfigField
        label="启用 RK3588 硬件路径"
        description="硬件路径总开关：开启后录制结束等处理阶段使用 RK3588 MPP 硬件编解码，手动压缩也要求该项为开。"
      >
        <Form.Item name={[...namePrefix, 'enable']} valuePropName="checked" noStyle>
          <Switch disabled={disabled} />
        </Form.Item>
      </SimpleConfigField>

      <SimpleConfigField
        label="录制完成后压缩"
        description="开启后录制结束即使没有 ASS 弹幕字幕也会压缩；有 ASS 时烧录与压缩一次完成。关闭时不会自动压缩，仅手动压缩或烧录等处理会用到硬件路径。"
      >
        <Form.Item name={[...namePrefix, 'compress']} valuePropName="checked" noStyle>
          <Switch disabled={disabled} />
        </Form.Item>
      </SimpleConfigField>

      <SimpleConfigField label="视频编码器" description="H.265 压缩效率更高，实际体积取决于码率与源流；H.264 兼容性更好。">
        <Form.Item name={[...namePrefix, 'codec']} noStyle>
          <Select
            style={{ width: 280 }}
            disabled={disabled}
            options={RK3588_CODEC_OPTIONS.map(item => ({
              label: item.label,
              value: item.value,
            }))}
          />
        </Form.Item>
        <div style={{ marginTop: 4 }}>{renderCodecProbeHint()}</div>
      </SimpleConfigField>

      <SimpleConfigField label="码率" description={RK3588_BITRATE_HINT}>
        <Form.Item name={[...namePrefix, 'bitrate']} rules={[{ validator: validateBitrate }]} noStyle>
          <AutoComplete
            style={{ width: labelWidth + 40 }}
            disabled={disabled}
            allowClear
            placeholder="4M"
            options={RK3588_BITRATE_PRESETS.map(item => ({ value: item }))}
          />
        </Form.Item>
      </SimpleConfigField>

      <SimpleConfigField label="硬解失败回退软解" description={HARDWARE_DECODE_FALLBACK_HINT}>
        <Form.Item name={[...namePrefix, 'decode_fallback']} valuePropName="checked" noStyle>
          <Switch disabled={disabled} />
        </Form.Item>
      </SimpleConfigField>

      <SimpleConfigField
        label="输出宽度"
        description="单位像素，0 表示不缩放；如需缩放，请与高度同时填写 16~8192 之间的偶数。"
      >
        <Form.Item name={[...namePrefix, 'scale_width']} rules={[{ validator: scaleValidator }]} noStyle>
          <InputNumber
            min={0}
            max={RK3588_SCALE_MAX}
            step={2}
            precision={0}
            style={{ width: 150 }}
            disabled={disabled}
            placeholder="0（不缩放）"
          />
        </Form.Item>
      </SimpleConfigField>

      <SimpleConfigField
        label="输出高度"
        description="单位像素，0 表示不缩放；如需缩放，请与宽度同时填写 16~8192 之间的偶数。"
      >
        <Form.Item name={[...namePrefix, 'scale_height']} rules={[{ validator: scaleValidator }]} noStyle>
          <InputNumber
            min={0}
            max={RK3588_SCALE_MAX}
            step={2}
            precision={0}
            style={{ width: 150 }}
            disabled={disabled}
            placeholder="0（不缩放）"
          />
        </Form.Item>
        <div style={{ marginTop: 4 }}>
          <Text type={(width > 0) !== (height > 0) ? 'warning' : 'secondary'}>{scaleHint}</Text>
        </div>
        {inheritedConfig && (
          <div style={{ marginTop: 4 }}>
            <Space size={4} wrap>
              <Tag color="blue">继承自全局</Tag>
              <Text type="secondary">{describeRk3588Config(inheritedConfig)}</Text>
            </Space>
          </div>
        )}
      </SimpleConfigField>
    </>
  );
};

export default Rk3588Fields;
