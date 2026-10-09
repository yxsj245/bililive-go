/**
 * 复制降级局部测试
 *
 * 用户规则要求：页面涉及文本复制时，必须保证 http（非安全上下文）下能降级复制。
 * http 下没有 navigator.clipboard，若直接调用会抛错，因此必须回落到
 * document.execCommand('copy')。
 */

import { copyTextToClipboard } from './common';

describe('copyTextToClipboard 复制降级', () => {
  const originalExecCommand = (document as any).execCommand;

  const setClipboard = (value: unknown) => {
    Object.defineProperty(navigator, 'clipboard', { value, configurable: true });
  };

  afterEach(() => {
    jest.restoreAllMocks();
    setClipboard(undefined);
    (document as any).execCommand = originalExecCommand;
  });

  test('安全上下文：优先使用 Clipboard API，不走 execCommand', async () => {
    const writeText = jest.fn().mockResolvedValue(undefined);
    const execCommand = jest.fn(() => true);
    setClipboard({ writeText });
    (document as any).execCommand = execCommand;

    await expect(copyTextToClipboard('hello')).resolves.toBe(true);
    expect(writeText).toHaveBeenCalledWith('hello');
    expect(execCommand).not.toHaveBeenCalled();
  });

  test('http 场景：没有 navigator.clipboard 时降级 execCommand', async () => {
    const execCommand = jest.fn(() => true);
    setClipboard(undefined);
    (document as any).execCommand = execCommand;

    await expect(copyTextToClipboard('curl http://example.com')).resolves.toBe(true);
    expect(execCommand).toHaveBeenCalledWith('copy');
  });

  test('Clipboard API 被拒绝时继续降级', async () => {
    const writeText = jest.fn().mockRejectedValue(new Error('NotAllowedError'));
    const execCommand = jest.fn(() => true);
    setClipboard({ writeText });
    (document as any).execCommand = execCommand;

    await expect(copyTextToClipboard('内容')).resolves.toBe(true);
    expect(execCommand).toHaveBeenCalledWith('copy');
  });

  test('两条路径都不可用时返回 false，不抛错', async () => {
    // 该用例会触发内部 console.error，这里静音以保持测试输出干净
    jest.spyOn(console, 'error').mockImplementation(() => undefined);
    setClipboard(undefined);
    (document as any).execCommand = () => {
      throw new Error('execCommand unavailable');
    };

    await expect(copyTextToClipboard('内容')).resolves.toBe(false);
  });

  test('空文本直接返回 false', async () => {
    await expect(copyTextToClipboard('')).resolves.toBe(false);
  });
});
